package service

import (
	"context"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestResolveEffectiveMemoryPolicyTruthTable(t *testing.T) {
	tests := []struct {
		name       string
		capability bool
		user       bool
		session    string
		enabled    bool
		reason     string
	}{
		{name: "capability off dominates", capability: false, user: true, session: model.MemorySessionPolicyEnabled, reason: model.MemoryPolicyReasonCapabilityDisabled},
		{name: "session disabled dominates user", capability: true, user: true, session: model.MemorySessionPolicyDisabled, reason: model.MemoryPolicyReasonSessionDisabled},
		{name: "session enabled overrides user", capability: true, user: false, session: model.MemorySessionPolicyEnabled, enabled: true, reason: model.MemoryPolicyReasonSessionEnabled},
		{name: "inherit enabled user", capability: true, user: true, session: model.MemorySessionPolicyInherit, enabled: true, reason: model.MemoryPolicyReasonUserEnabled},
		{name: "inherit defaults off", capability: true, user: false, session: "", reason: model.MemoryPolicyReasonUserDisabled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := ResolveEffectiveMemoryPolicy(test.capability, test.user, 3, test.session, 4)
			if policy.EffectiveEnabled != test.enabled || policy.Reason != test.reason || policy.SessionPolicy == "" || policy.UserPreferenceVersion != 3 || policy.SessionPolicyVersion != 4 {
				t.Fatalf("resolved policy = %+v", policy)
			}
		})
	}
}

func TestMemoryPolicyServicePreservesPreferenceWhenCapabilityIsOff(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	enabledService := NewMemoryPolicyService(repos.Memory, true)
	preference, err := enabledService.UpdatePreference(context.Background(), session.UserID, true, 0)
	if err != nil || !preference.EffectiveEnabled {
		t.Fatalf("enabled preference = %+v err=%v", preference, err)
	}
	policy, err := enabledService.Resolve(context.Background(), session.UserID, session.ID)
	if err != nil || !policy.EffectiveEnabled || policy.Reason != model.MemoryPolicyReasonUserEnabled {
		t.Fatalf("inherited enabled policy = %+v err=%v", policy, err)
	}

	disabledCapability := NewMemoryPolicyService(repos.Memory, false)
	preference, err = disabledCapability.GetPreference(context.Background(), session.UserID)
	if err != nil || !preference.Enabled || preference.EffectiveEnabled || preference.Reason != model.MemoryPolicyReasonCapabilityDisabled {
		t.Fatalf("stored preference under disabled capability = %+v err=%v", preference, err)
	}
	policy, err = disabledCapability.Resolve(context.Background(), session.UserID, session.ID)
	if err != nil || policy.EffectiveEnabled || !policy.UserEnabled || policy.Reason != model.MemoryPolicyReasonCapabilityDisabled {
		t.Fatalf("disabled capability policy = %+v err=%v", policy, err)
	}
}

type policyCountingMemoryProvider struct{ calls int }

func (p *policyCountingMemoryProvider) Snapshot(context.Context, MemorySnapshotRequest) (MemorySnapshot, error) {
	p.calls++
	return MemorySnapshot{SchemaVersion: MemorySnapshotSchemaVersion, Version: "empty", MemoryIDs: []string{}}, nil
}

type policyCountingMemoryCapture struct{ calls int }

func (c *policyCountingMemoryCapture) EnqueueExtraction(MemoryExtractionRequest) MemoryEnqueueResult {
	c.calls++
	return MemoryEnqueueResult{Accepted: true}
}

func TestDefaultDisabledPolicySkipsAgentRecallAndCapture(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	provider := &policyCountingMemoryProvider{}
	capture := &policyCountingMemoryCapture{}
	policyService := NewMemoryPolicyService(repos.Memory, true)
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-policy-off", ChunkID: 1, ChunkIndex: 0, Score: .9, Content: "当前证据",
	}}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: .3}, ChatDependencies{
		LongTermMemory: provider, MemoryCapture: capture, MemoryPolicy: policyService,
	})
	result, err := NewVideoAgentService(chatSvc).Ask(context.Background(), VideoAgentRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "请简洁回答", TopK: 1,
	}, &fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "回答 [C1]"}}, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if result.MemoryPolicy.EffectiveEnabled || result.MemoryPolicy.Reason != model.MemoryPolicyReasonUserDisabled || result.Memory != nil {
		t.Fatalf("agent memory policy = %+v memory=%+v", result.MemoryPolicy, result.Memory)
	}
	if provider.calls != 0 || capture.calls != 0 {
		t.Fatalf("disabled policy touched memory provider/capture: recall=%d capture=%d", provider.calls, capture.calls)
	}
}

func TestChatSessionResponsesExposeRawAndEffectiveMemoryPolicy(t *testing.T) {
	repos, task, existing := newVideoAgentTestSession(t)
	policyService := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policyService.UpdatePreference(context.Background(), existing.UserID, true, 0); err != nil {
		t.Fatal(err)
	}
	chatSvc := NewChatServiceWithDependencies(repos, nil, ChatConfig{}, ChatDependencies{MemoryPolicy: policyService})
	sessions, err := chatSvc.ListSessions(existing.UserID, task.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %+v err=%v", sessions, err)
	}
	if sessions[0].MemoryPolicy != model.MemorySessionPolicyInherit || sessions[0].MemoryPolicyVersion != 0 || sessions[0].EffectiveMemoryPolicy == nil || !sessions[0].EffectiveMemoryPolicy.EffectiveEnabled {
		t.Fatalf("listed session policy = %+v", sessions[0])
	}
	created, err := chatSvc.CreateSession(existing.UserID, task.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if created.MemoryPolicy != model.MemorySessionPolicyInherit || created.EffectiveMemoryPolicy == nil || created.EffectiveMemoryPolicy.Reason != model.MemoryPolicyReasonUserEnabled {
		t.Fatalf("created session policy = %+v", created)
	}
}

type blockingCapturedMemoryStore struct {
	repository *repository.MemoryRepository
	entered    chan struct{}
	proceed    chan struct{}
	completed  chan struct{}
}

func (s *blockingCapturedMemoryStore) Append(ctx context.Context, item *model.AgentMemoryItem) (repository.MemoryAppendResult, error) {
	return s.repository.Append(ctx, item)
}

func (s *blockingCapturedMemoryStore) AppendCaptured(ctx context.Context, sessionID int64, item *model.AgentMemoryItem) (repository.MemoryAppendResult, bool, error) {
	close(s.entered)
	<-s.proceed
	result, allowed, err := s.repository.AppendCaptured(ctx, sessionID, item)
	close(s.completed)
	return result, allowed, err
}

func (s *blockingCapturedMemoryStore) SetEmbeddingRef(ctx context.Context, userID int64, memoryID, ref string) error {
	return s.repository.SetEmbeddingRef(ctx, userID, memoryID, ref)
}

func TestAsyncWriterRechecksPolicyAfterQueuedCapture(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	policyService := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policyService.UpdateSessionPolicy(context.Background(), session.UserID, session.ID, model.MemorySessionPolicyEnabled, 0); err != nil {
		t.Fatal(err)
	}
	store := &blockingCapturedMemoryStore{
		repository: repos.Memory, entered: make(chan struct{}), proceed: make(chan struct{}), completed: make(chan struct{}),
	}
	writer := NewAsyncMemoryWriter(store, fakeMemoryAuthorizer{}, nil, 1)
	defer writer.Close(context.Background())
	result := writer.Enqueue(MemoryCandidate{
		UserID: session.UserID, SessionID: session.ID,
		Scope: MemoryScope{Type: model.MemoryScopeUser, ID: "7"}, Kind: "response_preference",
		Content: "回答风格：简洁", SourceType: "user_message", SourceRef: "chat_message:1", Importance: .7,
	})
	if !result.Accepted {
		t.Fatalf("enqueue = %+v", result)
	}
	<-store.entered
	if _, err := policyService.UpdateSessionPolicy(context.Background(), session.UserID, session.ID, model.MemorySessionPolicyDisabled, 1); err != nil {
		t.Fatal(err)
	}
	close(store.proceed)
	<-store.completed
	items, err := repos.Memory.ListForUser(context.Background(), session.UserID, model.MemoryScopeUser, "7")
	if err != nil || len(items) != 0 {
		t.Fatalf("queued capture survived opt-out: items=%+v err=%v", items, err)
	}
}
