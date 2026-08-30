package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestVideoAgentAskResearchRunsPlannerToolAndPersistsAnswer(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	retriever := &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-research-1", ChunkID: 1, ChunkIndex: 2, Content: "owner 校验证据",
	}}}
	chatClient := &scriptedChatClient{responses: []string{
		`{"done":false,"tool":"search_transcript","reason":"先定位转写证据","arguments":{"question":"owner 校验","top_k":1}}`,
		"not-json",
		`{"done":false,"tool":"build_cited_answer","reason":"证据已经足够，生成回答","arguments":{"question":"owner 校验","intermediate":"视频明确要求校验 owner。","citations":[{"task_id":1,"evidence_id":"ev-research-1","chunk_id":1,"chunk_index":2,"content":"owner 校验证据"}]}}`,
		"最终研究答案 [C1]",
		`{"done":true,"stop_reason":"已生成带引用回答"}`,
	}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: 0.3})
	ledger := NewEvidenceLedgerService(repos)
	chatSvc.SetEvidenceLedger(ledger)
	agent := NewVideoAgentService(chatSvc)

	result, err := agent.AskResearch(context.Background(), VideoResearchRequest{
		UserID: 7, SessionID: session.ID, Goal: "请研究 owner 校验", TopK: 1,
	}, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small", LLMModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("AskResearch() error = %v", err)
	}
	if result.Answer != "最终研究答案" || result.Template != string(VideoAgentResearchTemplate) || result.Model != "chat-model" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].CitationID != "C1" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if traceTools(result.Trace) != "search_transcript|build_cited_answer" {
		t.Fatalf("trace = %+v", result.Trace)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "最终研究答案" || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v", messages)
	}
	var snapshot struct {
		Template string           `json:"template"`
		Trace    []VideoAgentStep `json:"trace"`
	}
	if err := json.Unmarshal([]byte(*messages[1].RetrievalSnapshot), &snapshot); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if snapshot.Template != string(VideoAgentResearchTemplate) || traceTools(snapshot.Trace) != "search_transcript|build_cited_answer" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if strings.Contains(messages[1].Content, "[C") {
		t.Fatalf("stored answer leaked citation markers: %q", messages[1].Content)
	}
	ledgerView, err := ledger.GetRun(context.Background(), 7, result.RunID)
	if err != nil || ledgerView == nil || len(ledgerView.Claims) != 1 || len(ledgerView.Evidence) != 1 {
		t.Fatalf("research ledger = %+v err=%v", ledgerView, err)
	}
}
