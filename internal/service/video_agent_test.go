package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestVideoAgentClassifiesIntentTemplates(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     VideoAgentTemplate
	}{
		{name: "direct qa", question: "视频里为什么要校验 owner？", want: VideoAgentDirectQA},
		{name: "summarize topic", question: "总结一下视频里 Redis 分布式锁的观点", want: VideoAgentSummarizeTopic},
		{name: "summarize synonym", question: "归纳作者对 RAG 优化的说法", want: VideoAgentSummarizeTopic},
		{name: "compare topic", question: "对比前后两段对 RAG 的区别", want: VideoAgentCompareTopics},
		{name: "compare change", question: "视频前后观点有什么变化？", want: VideoAgentCompareTopics},
		{name: "critique topic", question: "这段方案有什么风险和不足？", want: VideoAgentCritiqueTopic},
		{name: "critique rebuttal", question: "反驳一下这个说法哪里不严谨", want: VideoAgentCritiqueTopic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyVideoAgentTemplate(tt.question)
			if got != tt.want {
				t.Fatalf("ClassifyVideoAgentTemplate(%q) = %q, want %q", tt.question, got, tt.want)
			}
		})
	}
}

func TestVideoAgentClassifiesUnknownQuestionAsDirectQA(t *testing.T) {
	got := ClassifyVideoAgentTemplate("owner 校验是什么？")
	if got != VideoAgentDirectQA {
		t.Fatalf("template = %q, want %q", got, VideoAgentDirectQA)
	}
}

func TestVideoAgentAskRejectsBlankQuestion(t *testing.T) {
	svc := &VideoAgentService{}
	_, err := svc.Ask(context.Background(), VideoAgentRequest{Question: "   "}, nil, nil, ai.Profile{})
	if err == nil {
		t.Fatal("Ask() succeeded for blank question")
	}
}

func TestVideoAgentAskDirectQAExecutesSearchAndBuildCitedAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	embedding := &fakeEmbeddingClient{dim: 3}
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"直接回答",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Content: "owner 校验引用片段"},
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID:    7,
		SessionID: session.ID,
		Question:  "为什么要校验 owner？",
		TopK:      3,
	}, embedding, chatClient, ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "直接回答" || result.Template != string(VideoAgentDirectQA) || result.Model != "chat-model" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].Content != "owner 校验引用片段" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if traceTools(result.Trace) != "search_transcript|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}
	if retriever.lastReq.TaskID != task.ID {
		t.Fatalf("retriever task id = %d, want %d", retriever.lastReq.TaskID, task.ID)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	if !strings.Contains(*messages[1].RetrievalSnapshot, "build_cited_answer") {
		t.Fatalf("snapshot = %s, want trace", *messages[1].RetrievalSnapshot)
	}
}

func TestVideoAgentAskSummarizeExecutesWindowSummarizeAndBuildAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	seedVideoChunks(t, repos, 7, task.ID, "text-embedding-3-small", []string{
		"chunk-0 背景", "chunk-1 Redis owner", "chunk-2 风险",
	})
	embedding := &fakeEmbeddingClient{dim: 3}
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"总结中间结果",
		"最终总结回答",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 2, ChunkIndex: 1, Content: "chunk-1 Redis owner"},
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID:    7,
		SessionID: session.ID,
		Question:  "总结一下 Redis owner 风险",
		TopK:      1,
	}, embedding, chatClient, ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "最终总结回答" || result.Template != string(VideoAgentSummarizeTopic) {
		t.Fatalf("result = %+v", result)
	}
	if traceTools(result.Trace) != "search_transcript|get_transcript_window|summarize_segments|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}
	if len(chatClient.messages) != 3 {
		t.Fatalf("chat calls = %d, want rewrite, summarize and final answer", len(chatClient.messages))
	}
	if !messagesContain(chatClient.messages[1], "chunk-0 背景") || !messagesContain(chatClient.messages[1], "chunk-2 风险") {
		t.Fatalf("summarize prompt = %+v, want expanded window", chatClient.messages[1])
	}
}

func TestVideoAgentAskCompareExecutesWindowCompareAndBuildAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	seedVideoChunks(t, repos, 7, task.ID, "text-embedding-3-small", []string{
		"chunk-0 前半段观点", "chunk-1 过渡", "chunk-2 后半段变化",
	})
	embedding := &fakeEmbeddingClient{dim: 3}
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"对比中间结果",
		"最终对比回答",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 0, Content: "chunk-0 前半段观点"},
		{ChunkID: 3, ChunkIndex: 2, Content: "chunk-2 后半段变化"},
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID:    7,
		SessionID: session.ID,
		Question:  "对比前后两段观点有什么变化",
		TopK:      2,
	}, embedding, chatClient, ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "最终对比回答" || result.Template != string(VideoAgentCompareTopics) {
		t.Fatalf("result = %+v", result)
	}
	if traceTools(result.Trace) != "search_transcript|get_transcript_window|get_transcript_window|compare_segments|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}
	if len(chatClient.messages) != 3 {
		t.Fatalf("chat calls = %d, want rewrite, compare and final answer", len(chatClient.messages))
	}
	if !messagesContain(chatClient.messages[1], "前半段观点") || !messagesContain(chatClient.messages[1], "后半段变化") {
		t.Fatalf("compare prompt = %+v, want both windows", chatClient.messages[1])
	}
}

func TestVideoAgentAskCritiqueExecutesWindowSummarizeAndBuildAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	seedVideoChunks(t, repos, 7, task.ID, "text-embedding-3-small", []string{
		"chunk-0 背景", "chunk-1 释放锁没有 owner 校验会有风险", "chunk-2 后续说明",
	})
	embedding := &fakeEmbeddingClient{dim: 3}
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"风险中间结果",
		"最终风险回答",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 2, ChunkIndex: 1, Content: "chunk-1 释放锁没有 owner 校验会有风险"},
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID:    7,
		SessionID: session.ID,
		Question:  "这个分布式锁方案有什么风险和不足？",
		TopK:      1,
	}, embedding, chatClient, ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "最终风险回答" || result.Template != string(VideoAgentCritiqueTopic) {
		t.Fatalf("result = %+v", result)
	}
	if traceTools(result.Trace) != "search_transcript|get_transcript_window|summarize_segments|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}
	if len(chatClient.messages) != 3 {
		t.Fatalf("chat calls = %d, want rewrite, critique summarize and final answer", len(chatClient.messages))
	}
	if !messagesContain(chatClient.messages[1], "问题、风险、不足或不严谨") {
		t.Fatalf("critique prompt = %+v, want risk framing", chatClient.messages[1])
	}
}

func newVideoAgentTestSession(t *testing.T) (*repository.Repositories, *model.VideoTask, *model.ChatSession) {
	t.Helper()
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "33333333333333333333333333333333", Filename: "agent.mp4", FileURL: "videos/agent.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "agent"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return repos, task, session
}

func traceTools(trace []VideoAgentStep) string {
	tools := make([]string, 0, len(trace))
	for _, step := range trace {
		tools = append(tools, step.Tool)
	}
	return strings.Join(tools, "|")
}
