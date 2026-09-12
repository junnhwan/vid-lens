package service

import (
	"context"
	"testing"

	"vid-lens/internal/ai"
)

type productFixtureClients struct{ chat ai.ChatClient }

func (c productFixtureClients) NewEmbeddingClient(ai.Profile) (ai.EmbeddingClient, error) {
	return &fakeEmbeddingClient{dim: 3}, nil
}
func (c productFixtureClients) NewChatClient(ai.Profile) (ai.ChatClient, error) { return c.chat, nil }

// This fixture crosses the same execution/profile/agent/journal/message boundary
// as the HTTP endpoints. Only the external AI and retrieval providers are scripted.
func TestProductEvaluationRealExecutionPersistsAndCanonicalizes(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "stream"}[stream], func(t *testing.T) {
			repos, task, session := newVideoAgentTestSession(t)
			chat := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "fixture-owner", Content: "必须验证 owner"}}}, ChatConfig{TopK: 1})
			client := &scriptedChatClient{responses: []string{
				`{"done":false,"reason":"定位并核对依据","tool":"search_transcript","arguments":{"query":"owner","top_k":1}}`,
				`{"done":false,"reason":"定位并核对依据","tool":"build_cited_answer","arguments":{"question":"owner","intermediate":"必须验证 owner","citations":[{"evidence_id":"fixture-owner"}]}}`,
				"必须验证 owner [C1]",
			}}
			executor := NewConversationExecution(chat, NewVideoAgentService(chat), stubConversationProfileProvider{profile: ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"}}, productFixtureClients{chat: client})
			req := ConversationRequest{Kind: ConversationKindAgent, UserID: 7, SessionID: session.ID, Question: "owner 要求是什么", TopK: 1}
			var result ConversationResult
			var err error
			done := false
			if stream {
				result, err = executor.Stream(context.Background(), req, func(event ConversationStreamEvent) error {
					if event.Type == "done" {
						done = true
					}
					return nil
				})
			} else {
				result, err = executor.Execute(context.Background(), req)
			}
			if err != nil || result.Agent == nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if stream && !done {
				t.Fatal("missing persisted done")
			}
			got := result.Agent
			messages, err := repos.Chat.ListMessages(7, session.ID)
			if err != nil || len(messages) != 2 || got.MessageID <= 0 || len(got.Citations) != 1 || got.Citations[0].TaskID != task.ID {
				t.Fatalf("persistence/canonicalization: %+v messages=%d err=%v", got, len(messages), err)
			}
			run, err := repos.AgentExecution.GetExecution(context.Background(), 7, got.RunID)
			if err != nil || run.Run.Status != "completed" || run.Run.LLMCallsUsed == 0 || len(run.ToolCalls) == 0 {
				t.Fatalf("missing journal: %+v %v", run, err)
			}
		})
	}
}
