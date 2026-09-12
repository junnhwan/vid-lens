package service

import (
	"context"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestDurableCaptureIsCommittedWithAnswerAndRechecksConsent(t *testing.T) {
	for _, disableBeforeCapture := range []bool{false, true} {
		t.Run(map[bool]string{false: "capture", true: "revoked"}[disableBeforeCapture], func(t *testing.T) {
			repos, session, _ := knowledgeAgentFixture(t)
			repos.Chat.EnableDurableMemoryCapture(true)
			ctx := context.Background()
			policies := NewMemoryPolicyService(repos.Memory, true)
			if _, err := policies.UpdateSessionPolicy(ctx, 7, session.ID, "enabled", 0); err != nil {
				t.Fatal(err)
			}
			chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{}, ChatConfig{TopK: 5}, ChatDependencies{MemoryPolicy: policies})
			agent := NewVideoAgentService(chatSvc)
			client := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "两个视频的证据 [C1][C2]"}}
			request := VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "请用中文回答 owner", RunID: "memory-durable-run"}
			profile := ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"}
			if _, err := agent.RunAgent(ctx, request, &fakeEmbeddingClient{dim: 3}, client, profile); err != nil {
				t.Fatal(err)
			}
			status, err := repos.Memory.CaptureStatus(ctx, 7)
			if err != nil || status.Pending != 1 {
				t.Fatalf("missing transactional task: %+v %v", status, err)
			}
			if _, err := agent.RunAgent(ctx, request, &fakeEmbeddingClient{dim: 3}, client, profile); err != nil {
				t.Fatal(err)
			}
			status, _ = repos.Memory.CaptureStatus(ctx, 7)
			if status.Pending != 1 {
				t.Fatal("replay duplicated capture")
			}
			if disableBeforeCapture {
				if _, err := policies.UpdateSessionPolicy(ctx, 7, session.ID, "disabled", 1); err != nil {
					t.Fatal(err)
				}
			}
			worker := &DurableMemoryCapture{repo: repos.Memory, extractor: ExplicitPreferenceExtractor{}, writer: &AsyncMemoryWriter{store: repos.Memory, authorizer: NewRepositoryMemoryAuthorizer(repos)}}
			if worked, err := worker.process(ctx); err != nil || !worked {
				t.Fatalf("process=%v %v", worked, err)
			}
			items, err := repos.Memory.ListForUser(ctx, 7, model.MemoryScopeUser, "7")
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if disableBeforeCapture {
				want = 0
			}
			if len(items) != want {
				t.Fatalf("items=%+v", items)
			}
			if len(items) > 0 && items[0].SourceRef == "" {
				t.Fatal("lost original message source")
			}
			if worked, err := worker.process(ctx); err != nil || worked {
				t.Fatalf("task not acknowledged: %v %v", worked, err)
			}
		})
	}
}
