package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/repository"
)

func TestProductCorrectsInvalidArgumentsOnceWithoutRepeatingSuccessfulTool(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	client := &scriptedChatClient{responses: []string{
		`{"tool":"search_transcript","reason":"search","arguments":{"unknown":"bad"}}`,
		`{"tool":"search_transcript","reason":"correct arguments","arguments":{"question":"owner"}}`,
		`{"tool":"build_cited_answer","reason":"answer","arguments":{"question":"owner","citations":[{"evidence_id":"e1"}]}}`,
		"校验owner [C1]",
	}}
	retriever := &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "e1", Content: "校验owner"}}}
	chat := NewChatService(repos, retriever, ChatConfig{TopK: 1})
	execution := NewConversationExecution(chat, NewVideoAgentService(chat), stubConversationProfileProvider{profile: ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"}}, productFixtureClients{chat: client})
	result, err := execution.Execute(context.Background(), ConversationRequest{Kind: ConversationKindAgent, UserID: 7, SessionID: session.ID, Question: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	records, _ := repos.AgentExecution.GetExecution(context.Background(), 7, result.Agent.RunID)
	if records.Run.ToolCallsUsed != 3 || len(records.ToolCalls) != 6 {
		t.Fatalf("missing correction accounting: %+v", records.Run)
	}
	if records.ToolCalls[1].ResultCheckpoint == "" {
		t.Fatal("missing validation observation")
	}
}

type failingTerminalStore struct {
	AgentExecutionStore
	deadline bool
}

func (s *failingTerminalStore) MarkRunTerminal(ctx context.Context, _ repository.AgentRunTerminalUpdate) (bool, error) {
	deadline, ok := ctx.Deadline()
	s.deadline = ok && time.Until(deadline) <= 5*time.Second
	return false, errors.New("fixture database unavailable")
}

func TestProductDoesNotEmitDoneWhenTerminalPersistenceFails(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	chat := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 1})
	agent := NewVideoAgentService(chat)
	store := &failingTerminalStore{AgentExecutionStore: repos.AgentExecution}
	agent.executionJournal = NewAgentExecutionJournal(store)
	client := &scriptedChatClient{responses: []string{`{"done":true}`, "视频依据不足"}}
	done := false
	_, err := agent.Stream(context.Background(), VideoAgentStreamRequest{UserID: 7, SessionID: session.ID, Question: "未知事实"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"}, func(e AgentStreamEvent) error {
		if e.Type == AgentEventDone {
			done = true
		}
		return nil
	})
	if err == nil || done || !store.deadline {
		t.Fatalf("err=%v done=%v deadline=%v", err, done, store.deadline)
	}
}
