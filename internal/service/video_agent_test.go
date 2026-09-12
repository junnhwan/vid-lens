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
		testSearchDecision, testAnswerDecision("", 1, 1), "直接回答 [C1]",
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
	if result.Answer == "" || result.Template != string(VideoAgentLoopTemplate) || result.Model != "chat-model" {
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
		testSearchDecision, testAnswerDecision("ev-agent-1", 1, 9, "ev-agent-2"), "工具结果会反馈给模型，另一条说明最终文件列表 [C2, C1]",
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
	if len(chatClient.messages) != 3 || !messagesContain(chatClient.messages[2], "前邻居上下文只给模型") || !messagesContain(chatClient.messages[2], "后邻居上下文也只给模型") {
		t.Fatalf("final answer prompt lost expanded context: %+v", chatClient.messages)
	}
	if !messagesContain(chatClient.messages[2], "[C1]") || !messagesContain(chatClient.messages[2], "[C2]") || !messagesContain(chatClient.messages[2], "完全无关的第二条唯一文本") {
		t.Fatalf("final answer prompt lost candidate citations: %+v", chatClient.messages[2])
	}
	if result.Answer == "" || strings.Contains(result.Answer, "[C") {
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

const testSearchDecision = `{"tool":"search_transcript","reason":"locate evidence","arguments":{"question":"owner","top_k":2}}`

func testAnswerDecision(evidence string, taskID, chunkID int64, extra ...string) string {
	citations := []RetrievedChunk{{EvidenceID: evidence, TaskID: taskID, ChunkID: chunkID}}
	for _, id := range extra {
		citations = append(citations, RetrievedChunk{EvidenceID: id})
	}
	args, _ := json.Marshal(buildCitedAnswerToolArguments{Question: "owner", Citations: citations})
	d, _ := json.Marshal(VideoAgentLoopDecision{Tool: VideoAgentToolBuildCitedAnswer, Reason: "answer", Arguments: args})
	return string(d)
}
