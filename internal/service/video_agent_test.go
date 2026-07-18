package service

import (
	"context"
	"encoding/json"
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
		"直接回答 [C1]",
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
	if messages[1].Content != result.Answer || strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored assistant content = %q, want clean answer", messages[1].Content)
	}
	if !strings.Contains(*messages[1].RetrievalSnapshot, "build_cited_answer") {
		t.Fatalf("snapshot = %s, want trace", *messages[1].RetrievalSnapshot)
	}
}

func TestVideoAgentAskKeepsExpandedContextInternalAndPersistsCompactCitation(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	anchor := strings.Repeat("背景内容。", 40) + "工具调用结果会作为新消息反馈给模型。" + strings.Repeat("其他内容。", 40)
	expanded := "前邻居上下文只给模型。\n" + anchor + "\n后邻居上下文也只给模型。"
	chatClient := &scriptedChatClient{responses: []string{
		"not-json",
		"工具结果会反馈给模型，另一条说明最终文件列表 [C2, C1]",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{
			EvidenceID: "ev-agent-1", ChunkID: 9, ChunkIndex: 3,
			Content: expanded, AnchorContent: anchor, MatchedQuery: "工具调用结果反馈模型",
		},
		{
			EvidenceID: "ev-agent-2", ChunkID: 10, ChunkIndex: 4,
			Content:       "完全无关的第二条唯一文本，只讨论最终文件列表。",
			AnchorContent: "完全无关的第二条唯一文本，只讨论最终文件列表。",
		},
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.Ask(context.Background(), VideoAgentRequest{
		UserID: 7, SessionID: session.ID, Question: "工具调用结果如何反馈给模型？", TopK: 2,
	}, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(chatClient.messages) != 2 || !messagesContain(chatClient.messages[1], "前邻居上下文只给模型") || !messagesContain(chatClient.messages[1], "后邻居上下文也只给模型") {
		t.Fatalf("final answer prompt lost expanded context: %+v", chatClient.messages)
	}
	if !messagesContain(chatClient.messages[1], "[C1]") || !messagesContain(chatClient.messages[1], "[C2]") || !messagesContain(chatClient.messages[1], "完全无关的第二条唯一文本") {
		t.Fatalf("final answer prompt lost candidate citations: %+v", chatClient.messages[1])
	}
	if result.Answer != "工具结果会反馈给模型，另一条说明最终文件列表" || strings.Contains(result.Answer, "[C") {
		t.Fatalf("result answer = %q, want clean answer", result.Answer)
	}
	if len(result.Citations) != 2 || result.Citations[0].CitationID != "C1" || result.Citations[1].CitationID != "C2" {
		t.Fatalf("citations = %+v, want stable candidate order C1, C2", result.Citations)
	}
	firstCitation := result.Citations[0]
	if strings.Contains(firstCitation.Content, "邻居上下文") || !strings.Contains(firstCitation.Content, "工具调用结果会作为新消息反馈给模型") {
		t.Fatalf("first public citation = %+v", firstCitation)
	}
	if !strings.Contains(anchor, firstCitation.Content) {
		t.Fatalf("first public citation must be verbatim anchor evidence: %q", firstCitation.Content)
	}
	if !strings.Contains(result.Citations[1].Content, "完全无关的第二条唯一文本") {
		t.Fatalf("second public citation = %+v", result.Citations[1])
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[1].Content != result.Answer || strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored assistant content = %q, want clean answer", messages[1].Content)
	}
	var snapshot struct {
		Citations []Citation `json:"citations"`
	}
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snapshot.Citations) != 2 || snapshot.Citations[0].CitationID != "C1" || snapshot.Citations[1].CitationID != "C2" {
		t.Fatalf("snapshot citations = %+v, want stable candidate order C1, C2", snapshot.Citations)
	}
	if snapshot.Citations[0].Content != result.Citations[0].Content || snapshot.Citations[1].Content != result.Citations[1].Content {
		t.Fatalf("snapshot citations = %+v, result citations = %+v", snapshot.Citations, result.Citations)
	}
	if strings.Contains(*messages[1].RetrievalSnapshot, "邻居上下文") || strings.Contains(*messages[1].RetrievalSnapshot, "anchor_content") {
		t.Fatalf("snapshot leaked internal context: %s", *messages[1].RetrievalSnapshot)
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
