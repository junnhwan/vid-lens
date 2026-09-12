package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestStructuredPreferenceExtractionRequiresEnduringFirstPartyIntent(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"以后请用中文并简洁回答，优先使用要点列表", 3}, {"这次详细讲", 0}, {"以后不要详细回答", 0}, {"他说以后请用中文回答", 0}, {"以后请用中文，这次详细讲", 0}, {"请用中文回答", 0}, {"from now on answer in English and be concise", 2}, {"以后请简洁回答，不要复述原文", 1},
		{"He said ‘from now on answer in English’", 0}, {"He said from now on answer in English", 0}, {"他要求以后用英文回答", 0}, {"她要求以后请用中文回答", 0}, {"引用：『以后简洁回答』", 0},
	} {
		t.Run(tc.text, func(t *testing.T) {
			items, err := (ExplicitPreferenceExtractor{}).Extract(context.Background(), MemoryExtractionRequest{UserID: 7, SessionID: 1, UserText: tc.text, SourceRef: "chat_message:1"})
			if err != nil || len(items) != tc.want {
				t.Fatalf("%+v %v", items, err)
			}
		})
	}
}

type forbiddenPreferenceEmbedder struct{ t *testing.T }

func (e forbiddenPreferenceEmbedder) EmbedMemory(context.Context, int64, string) (MemoryEmbedding, error) {
	e.t.Fatal("structured preference lookup must not embed")
	return MemoryEmbedding{}, nil
}

func TestChatMemoryOutboxMultiDimensionSupersedeAdoptionAndSourceDeletion(t *testing.T) {
	repos, session, _ := knowledgeAgentFixture(t)
	ctx := context.Background()
	repos.Chat.EnableDurableMemoryCapture(true)
	policies := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policies.UpdateSessionPolicy(ctx, 7, session.ID, "enabled", 0); err != nil {
		t.Fatal(err)
	}
	provider := NewScopedMemoryProvider(NewSemanticMemoryRetriever(repos.Memory, forbiddenPreferenceEmbedder{t}), NewRepositoryMemoryAuthorizer(repos), DefaultAgentMemoryConfig())
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{}, ChatConfig{TopK: 5}, ChatDependencies{MemoryPolicy: policies, LongTermMemory: provider})
	worker := &DurableMemoryCapture{repo: repos.Memory, extractor: ExplicitPreferenceExtractor{}, writer: &AsyncMemoryWriter{store: repos.Memory, authorizer: NewRepositoryMemoryAuthorizer(repos), projector: failingMemoryProjector{}}}
	save := func(text string) int64 {
		t.Helper()
		if _, err := chatSvc.saveChatExchange(ctx, 7, session.ID, text, "fixture answer", nil, 0, "fixture"); err != nil {
			t.Fatal(err)
		}
		if worked, err := worker.process(ctx); !worked || err != nil {
			t.Fatalf("capture %v %v", worked, err)
		}
		items, err := repos.Memory.ListStructuredPreferences(ctx, 7, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Kind == "response.verbosity" {
				var id int64
				fmt.Sscanf(item.SourceRef, "chat_message:%d", &id)
				return id
			}
		}
		return 0
	}
	first := save("以后请用中文且简洁回答")
	items, err := repos.Memory.ListStructuredPreferences(ctx, 7, time.Now())
	if err != nil || len(items) != 2 {
		t.Fatalf("multi %v %v", items, err)
	}
	prepared := &preparedRAGChat{Session: session, Question: "继续", Messages: []ai.ChatMessage{{Role: "user", Content: "继续"}}}
	chatSvc.injectChatPreferences(ctx, prepared, chatSvc.effectiveMemoryPolicyForRequest(ctx, session))
	if !strings.Contains(prepared.Messages[0].Content, "回答语言：中文") || !strings.Contains(prepared.Messages[0].Content, "回答风格：简洁") {
		t.Fatalf("Chat adoption %+v", prepared.Messages)
	}
	shared, err := provider.Snapshot(ctx, MemorySnapshotRequest{UserID: 7, Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "7"}}})
	if err != nil || len(shared.Items) != 2 {
		t.Fatalf("shared adoption %+v %v", shared, err)
	}
	save("今后请详细回答")
	items, err = repos.Memory.ListStructuredPreferences(ctx, 7, time.Now())
	if err != nil || len(items) != 2 {
		t.Fatalf("supersede %+v %v", items, err)
	}
	for _, item := range items {
		if item.Kind == "response.verbosity" && !strings.Contains(item.Content, "详细") {
			t.Fatal("old value survived")
		}
	}
	// A stale worker delivering the older source cannot undo a newer preference.
	old := MemoryCandidate{UserID: 7, SessionID: session.ID, Scope: MemoryScope{Type: model.MemoryScopeUser, ID: "7"}, Kind: "response.verbosity", Content: "回答风格：简洁", SourceType: "user_message", SourceRef: fmt.Sprintf("chat_message:%d", first), Importance: .7}
	if err := worker.writer.write(ctx, old); err != nil {
		t.Fatal(err)
	}

	agent := NewVideoAgentService(chatSvc)
	client := &scriptedChatClient{responses: []string{testSearchDecision, `{"done":true}`, "已根据视频回答 [C1][C2]"}}
	if _, err := agent.RunAgent(ctx, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "继续解释 owner", RunID: "preference-adoption-run"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "fixture"}); err != nil {
		t.Fatal(err)
	}
	adopted := false
	for _, call := range client.messages {
		for _, message := range call {
			if strings.Contains(message.Content, "回答语言：中文") && strings.Contains(message.Content, "回答风格：详细") {
				adopted = true
			}
		}
	}
	if !adopted {
		t.Fatal("Agent did not receive shared current preference snapshot")
	}
	if err := repos.Chat.DeleteSession(session.ID); err != nil {
		t.Fatal(err)
	}
	items, err = repos.Memory.ListStructuredPreferences(ctx, 7, time.Now())
	if err != nil || len(items) != 0 {
		t.Fatalf("orphan source recalled %+v %v", items, err)
	}
	other, err := repos.Memory.ListStructuredPreferences(ctx, 8, time.Now())
	if err != nil || len(other) != 0 {
		t.Fatal("owner isolation")
	}
}
