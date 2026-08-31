package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type fakeMemoryRetriever struct {
	mu     sync.Mutex
	items  []model.AgentMemoryItem
	flip   bool
	called bool
}

func (r *fakeMemoryRetriever) Retrieve(_ context.Context, _ MemoryRetrieveRequest) ([]model.AgentMemoryItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called = true
	items := append([]model.AgentMemoryItem(nil), r.items...)
	if r.flip {
		for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
			items[left], items[right] = items[right], items[left]
		}
	}
	r.flip = !r.flip
	return items, nil
}

type fakeMemoryAuthorizer struct {
	denied map[string]error
}

func (a fakeMemoryAuthorizer) Authorize(_ context.Context, _ int64, scope MemoryScope, _ bool) error {
	return a.denied[scope.Type+":"+scope.ID]
}

func memoryItem(id string, userID int64, scopeType, scopeID, kind, content, sourceRef string, importance float64, created time.Time) model.AgentMemoryItem {
	return model.AgentMemoryItem{
		ID: id, UserID: userID, ScopeType: scopeType, ScopeID: scopeID, Kind: kind, Content: content,
		SourceType: "user_confirmation", SourceRef: sourceRef, Importance: importance,
		Status: model.MemoryStatusActive, Version: 1, CreatedAt: created,
	}
}

func TestMemorySnapshotEnforcesUserAndAllScopeIsolation(t *testing.T) {
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	retriever := &fakeMemoryRetriever{items: []model.AgentMemoryItem{
		memoryItem("user-ok", 1, model.MemoryScopeUser, "1", "preference", "中文回答", "message:1", 0.9, now),
		memoryItem("video-ok", 1, model.MemoryScopeVideo, "10", "alias", "主视频", "confirm:2", 0.8, now),
		memoryItem("kb-ok", 1, model.MemoryScopeKnowledgeBase, "20", "term", "领域术语", "confirm:3", 0.7, now),
		memoryItem("run-ok", 1, model.MemoryScopeRun, "run-a", "open_question", "待确认问题", "run:4", 0.6, now),
		memoryItem("other-user", 2, model.MemoryScopeUser, "1", "preference", "越权用户", "message:5", 1, now),
		memoryItem("other-video", 1, model.MemoryScopeVideo, "11", "alias", "其他视频", "confirm:6", 1, now),
		memoryItem("other-kb", 1, model.MemoryScopeKnowledgeBase, "21", "term", "其他知识库", "confirm:7", 1, now),
		memoryItem("other-run", 1, model.MemoryScopeRun, "run-b", "open_question", "其他运行", "run:8", 1, now),
	}}
	provider := NewScopedMemoryProvider(retriever, fakeMemoryAuthorizer{}, AgentMemoryConfig{TopK: 10, MaxChars: 1000, MaxTokens: 1000, Now: func() time.Time { return now }})
	snapshot, err := provider.Snapshot(context.Background(), MemorySnapshotRequest{UserID: 1, Scopes: []MemoryScope{
		{Type: model.MemoryScopeUser, ID: "1"},
		{Type: model.MemoryScopeVideo, ID: "10"},
		{Type: model.MemoryScopeKnowledgeBase, ID: "20"},
		{Type: model.MemoryScopeRun, ID: "run-a"},
	}})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	want := map[string]bool{"user-ok": true, "video-ok": true, "kb-ok": true, "run-ok": true}
	if len(snapshot.Items) != len(want) {
		t.Fatalf("snapshot items = %+v", snapshot.Items)
	}
	for _, item := range snapshot.Items {
		if !want[item.ID] {
			t.Fatalf("isolated item recalled: %+v", item)
		}
	}
}

func TestMemorySnapshotRejectsUnauthorizedScopeBeforeRecall(t *testing.T) {
	retriever := &fakeMemoryRetriever{}
	provider := NewScopedMemoryProvider(retriever, fakeMemoryAuthorizer{denied: map[string]error{"video:99": errors.New("forbidden")}}, DefaultAgentMemoryConfig())
	_, err := provider.Snapshot(context.Background(), MemorySnapshotRequest{UserID: 1, Scopes: []MemoryScope{{Type: model.MemoryScopeVideo, ID: "99"}}})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if retriever.called {
		t.Fatal("unauthorized scope reached retriever")
	}
}

func TestRepositoryMemoryAuthorizerEnforcesOwnedUserVideoAndKnowledgeBase(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	otherUser := &model.User{Username: "memory-other", PasswordHash: "x", Role: model.RoleUser}
	if err := repos.User.Create(otherUser); err != nil {
		t.Fatal(err)
	}
	otherTask := &model.VideoTask{UserID: otherUser.ID, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "other.mp4"}
	if err := repos.Task.Create(otherTask); err != nil {
		t.Fatal(err)
	}
	ownedKB := &model.KnowledgeBase{UserID: session.UserID, Name: "owned"}
	otherKB := &model.KnowledgeBase{UserID: otherUser.ID, Name: "other"}
	if err := repos.KnowledgeBase.Create(ownedKB); err != nil {
		t.Fatal(err)
	}
	if err := repos.KnowledgeBase.Create(otherKB); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.AgentExecution.CreateRun(context.Background(), &model.AgentRun{
		ID: "run-owned", UserID: session.UserID, SessionID: session.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID,
		Goal: "memory authorization", Mode: "agent", AgentProfile: "default",
		ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: "{}", MaxSteps: 1, MaxAttemptsPerStep: 1,
	}); err != nil {
		t.Fatal(err)
	}
	authorizer := NewRepositoryMemoryAuthorizer(repos)
	for _, scope := range []MemoryScope{
		{Type: model.MemoryScopeUser, ID: fmt.Sprint(session.UserID)},
		{Type: model.MemoryScopeVideo, ID: fmt.Sprint(task.ID)},
		{Type: model.MemoryScopeKnowledgeBase, ID: fmt.Sprint(ownedKB.ID)},
		{Type: model.MemoryScopeRun, ID: "run-owned"},
	} {
		if err := authorizer.Authorize(context.Background(), session.UserID, scope, false); err != nil {
			t.Fatalf("Authorize(%+v) error = %v", scope, err)
		}
	}
	for _, scope := range []MemoryScope{
		{Type: model.MemoryScopeUser, ID: fmt.Sprint(otherUser.ID)},
		{Type: model.MemoryScopeVideo, ID: fmt.Sprint(otherTask.ID)},
		{Type: model.MemoryScopeKnowledgeBase, ID: fmt.Sprint(otherKB.ID)},
	} {
		if err := authorizer.Authorize(context.Background(), session.UserID, scope, false); err == nil {
			t.Fatalf("Authorize(%+v) allowed cross-owner access", scope)
		}
	}
}

func TestMemorySnapshotHonorsTopKCharacterAndTokenCaps(t *testing.T) {
	now := time.Now().UTC()
	retriever := &fakeMemoryRetriever{items: []model.AgentMemoryItem{
		memoryItem("m1", 1, model.MemoryScopeUser, "1", "a", "12345678", "message:1", 1, now),
		memoryItem("m2", 1, model.MemoryScopeUser, "1", "b", "abcdefgh", "message:2", .9, now.Add(-time.Second)),
		memoryItem("m3", 1, model.MemoryScopeUser, "1", "c", "overflow", "message:3", .8, now.Add(-2*time.Second)),
	}}
	provider := NewScopedMemoryProvider(retriever, fakeMemoryAuthorizer{}, AgentMemoryConfig{TopK: 2, MaxChars: 10, MaxTokens: 2, Now: func() time.Time { return now }})
	snapshot, err := provider.Snapshot(context.Background(), MemorySnapshotRequest{UserID: 1, TopK: 99, MaxChars: 99, MaxTokens: 99, Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) > 2 || snapshot.Budget.UsedChars > 10 || snapshot.Budget.UsedTokens > 2 {
		t.Fatalf("budget exceeded: %+v items=%+v", snapshot.Budget, snapshot.Items)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "m1" {
		t.Fatalf("bounded items = %+v", snapshot.Items)
	}
}

func TestMemorySnapshotExcludesExpiredDeletedWithdrawnAndSourcelessItems(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	items := []model.AgentMemoryItem{
		memoryItem("active", 1, model.MemoryScopeUser, "1", "a", "可召回", "message:1", 1, now),
		memoryItem("expired", 1, model.MemoryScopeUser, "1", "b", "过期", "message:2", 1, now),
		memoryItem("withdrawn", 1, model.MemoryScopeUser, "1", "c", "撤回", "message:3", 1, now),
		memoryItem("deleted", 1, model.MemoryScopeUser, "1", "d", "删除", "message:4", 1, now),
		memoryItem("sourceless", 1, model.MemoryScopeUser, "1", "e", "无来源", "", 1, now),
		memoryItem("sensitive", 1, model.MemoryScopeUser, "1", "f", "API Key sk-abcdefghijklmnopqrstuvwxyz", "message:5", 1, now),
	}
	items[1].ExpiresAt = &past
	items[2].Status = model.MemoryStatusWithdrawn
	items[3].Status = model.MemoryStatusDeleted
	items[3].DeletedAt = gorm.DeletedAt{Time: now, Valid: true}
	provider := NewScopedMemoryProvider(&fakeMemoryRetriever{items: items}, fakeMemoryAuthorizer{}, AgentMemoryConfig{TopK: 10, MaxChars: 100, MaxTokens: 100, Now: func() time.Time { return now }})
	snapshot, err := provider.Snapshot(context.Background(), MemorySnapshotRequest{UserID: 1, Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "active" || snapshot.Items[0].SourceRef == "" {
		t.Fatalf("lifecycle filtering = %+v", snapshot.Items)
	}
}

func TestMemorySnapshotKeepsConflictsTogetherAndExplainable(t *testing.T) {
	now := time.Now().UTC()
	items := []model.AgentMemoryItem{
		memoryItem("conflict-a", 1, model.MemoryScopeVideo, "10", "speaker_alias", "讲者叫 Alice", "confirm:1", .9, now),
		memoryItem("conflict-b", 1, model.MemoryScopeVideo, "10", "speaker_alias", "讲者叫 Bob", "confirm:2", .8, now.Add(-time.Second)),
	}
	items[0].Status, items[1].Status = model.MemoryStatusConflicted, model.MemoryStatusConflicted
	provider := NewScopedMemoryProvider(&fakeMemoryRetriever{items: items}, fakeMemoryAuthorizer{}, AgentMemoryConfig{TopK: 4, MaxChars: 100, MaxTokens: 100, Now: func() time.Time { return now }})
	snapshot, err := provider.Snapshot(context.Background(), MemorySnapshotRequest{UserID: 1, Scopes: []MemoryScope{{Type: model.MemoryScopeVideo, ID: "10"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 2 || !snapshot.Items[0].Conflict || !snapshot.Items[1].Conflict || len(snapshot.Conflicts) != 1 {
		t.Fatalf("conflict snapshot = %+v", snapshot)
	}
	if got := snapshot.Conflicts[0].MemoryIDs; !reflect.DeepEqual(got, []string{"conflict-a", "conflict-b"}) {
		t.Fatalf("conflict ids = %v", got)
	}
	contextText := snapshot.PromptContext()
	if !strings.Contains(contextText, "不是当前视频事实证据") || !strings.Contains(contextText, "冲突") {
		t.Fatalf("prompt context = %q", contextText)
	}
}

func TestMemorySnapshotVersionAndIDsAreStableAcrossRetrieverOrder(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	retriever := &fakeMemoryRetriever{items: []model.AgentMemoryItem{
		memoryItem("m2", 1, model.MemoryScopeUser, "1", "b", "second", "message:2", .8, now.Add(-time.Second)),
		memoryItem("m1", 1, model.MemoryScopeUser, "1", "a", "first", "message:1", .9, now),
	}}
	provider := NewScopedMemoryProvider(retriever, fakeMemoryAuthorizer{}, AgentMemoryConfig{TopK: 5, MaxChars: 100, MaxTokens: 100, Now: func() time.Time { return now }})
	request := MemorySnapshotRequest{UserID: 1, Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}}
	first, err := provider.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Snapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.SchemaVersion != MemorySnapshotSchemaVersion || first.Version != second.Version || !reflect.DeepEqual(first.MemoryIDs, second.MemoryIDs) {
		t.Fatalf("unstable snapshot: first=%+v second=%+v", first, second)
	}
}

type fakeMemoryWriteStore struct {
	mu        sync.Mutex
	items     []model.AgentMemoryItem
	appendErr error
}

func (s *fakeMemoryWriteStore) Append(_ context.Context, item *model.AgentMemoryItem) (repository.MemoryAppendResult, error) {
	if s.appendErr != nil {
		return repository.MemoryAppendResult{}, s.appendErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item.ID = fmt.Sprintf("memory-%d", len(s.items)+1)
	s.items = append(s.items, *item)
	return repository.MemoryAppendResult{Item: *item, Created: true}, nil
}

func (s *fakeMemoryWriteStore) AppendCaptured(ctx context.Context, _ int64, item *model.AgentMemoryItem) (repository.MemoryAppendResult, bool, error) {
	result, err := s.Append(ctx, item)
	return result, err == nil, err
}

func (*fakeMemoryWriteStore) SetEmbeddingRef(context.Context, int64, string, string) error {
	return nil
}

type failingMemoryProjector struct{}

func (failingMemoryProjector) Project(context.Context, model.AgentMemoryItem) (string, error) {
	return "", errors.New("embedding unavailable")
}

type fakeMemoryExtractor struct{ candidates []MemoryCandidate }

func (e fakeMemoryExtractor) Extract(_ context.Context, _ MemoryExtractionRequest) ([]MemoryCandidate, error) {
	return append([]MemoryCandidate(nil), e.candidates...), nil
}

type recordingMemoryWriter struct {
	mu         sync.Mutex
	candidates []MemoryCandidate
}

func (w *recordingMemoryWriter) Enqueue(candidate MemoryCandidate) MemoryEnqueueResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.candidates = append(w.candidates, candidate)
	return MemoryEnqueueResult{Accepted: true}
}

type fakeMemoryEmbedder struct{}

func (fakeMemoryEmbedder) EmbedMemory(_ context.Context, _ int64, content string) (MemoryEmbedding, error) {
	return MemoryEmbedding{Model: "fake-embedding", Vector: []float32{float32(len(content)), 1}}, nil
}

type fakeMemoryRecallRepository struct {
	searchItems  []model.AgentMemoryItem
	listItems    []model.AgentMemoryItem
	searchErr    error
	searchCalls  int
	listCalls    int
	searchModel  string
	searchVector []float32
}

func (r *fakeMemoryRecallRepository) SearchRecallable(_ context.Context, _ int64, _ map[string][]string, modelName string, vector []float32, _ int, _ time.Time) ([]model.AgentMemoryItem, error) {
	r.searchCalls++
	r.searchModel = modelName
	r.searchVector = append([]float32(nil), vector...)
	return append([]model.AgentMemoryItem(nil), r.searchItems...), r.searchErr
}

func (r *fakeMemoryRecallRepository) ListRecallable(_ context.Context, _ int64, _ map[string][]string, _ int, _ time.Time) ([]model.AgentMemoryItem, error) {
	r.listCalls++
	return append([]model.AgentMemoryItem(nil), r.listItems...), nil
}

type failingMemoryEmbedder struct{}

func (failingMemoryEmbedder) EmbedMemory(context.Context, int64, string) (MemoryEmbedding, error) {
	return MemoryEmbedding{}, errors.New("embedding unavailable")
}

func TestSemanticMemoryRetrieverUsesQueryEmbeddingAndFallsBack(t *testing.T) {
	now := time.Now().UTC()
	semanticItem := memoryItem("semantic", 1, model.MemoryScopeUser, "1", "preference", "semantic", "message:1", .5, now)
	repository := &fakeMemoryRecallRepository{searchItems: []model.AgentMemoryItem{semanticItem}}
	retriever := NewSemanticMemoryRetriever(repository, fakeMemoryEmbedder{})
	items, err := retriever.Retrieve(context.Background(), MemoryRetrieveRequest{
		UserID: 1, Query: "meaningful query", Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}, Limit: 3, Now: now,
	})
	if err != nil || len(items) != 1 || items[0].ID != "semantic" {
		t.Fatalf("semantic retrieve = %+v err=%v", items, err)
	}
	if repository.searchCalls != 1 || repository.listCalls != 0 || repository.searchModel != "fake-embedding" || len(repository.searchVector) == 0 {
		t.Fatalf("semantic search calls=%d list=%d model=%q vector=%v", repository.searchCalls, repository.listCalls, repository.searchModel, repository.searchVector)
	}

	fallback := &fakeMemoryRecallRepository{listItems: []model.AgentMemoryItem{semanticItem}}
	items, err = NewSemanticMemoryRetriever(fallback, failingMemoryEmbedder{}).Retrieve(context.Background(), MemoryRetrieveRequest{
		UserID: 1, Query: "query", Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}, Limit: 3, Now: now,
	})
	if err != nil || len(items) != 1 || fallback.searchCalls != 0 || fallback.listCalls != 1 {
		t.Fatalf("fallback retrieve = %+v err=%v search=%d list=%d", items, err, fallback.searchCalls, fallback.listCalls)
	}

	missingProjection := &fakeMemoryRecallRepository{listItems: []model.AgentMemoryItem{semanticItem}}
	items, err = NewSemanticMemoryRetriever(missingProjection, fakeMemoryEmbedder{}).Retrieve(context.Background(), MemoryRetrieveRequest{
		UserID: 1, Query: "query", Scopes: []MemoryScope{{Type: model.MemoryScopeUser, ID: "1"}}, Limit: 3, Now: now,
	})
	if err != nil || len(items) != 1 || missingProjection.searchCalls != 1 || missingProjection.listCalls != 1 {
		t.Fatalf("missing projection fallback = %+v err=%v search=%d list=%d", items, err, missingProjection.searchCalls, missingProjection.listCalls)
	}
}

func TestExplicitPreferenceExtractorNormalizesAndRejectsCredentials(t *testing.T) {
	extractor := ExplicitPreferenceExtractor{}
	candidates, err := extractor.Extract(context.Background(), MemoryExtractionRequest{
		UserID: 1, SessionID: 1, UserText: "以后请简洁回答，不要复述这段原文", SourceRef: "message:1",
	})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("normalized extraction = %+v err=%v", candidates, err)
	}
	if candidates[0].Content != "回答风格：简洁" || strings.Contains(candidates[0].Content, "不要复述") {
		t.Fatalf("extractor retained raw text: %+v", candidates[0])
	}
	candidates, err = extractor.Extract(context.Background(), MemoryExtractionRequest{
		UserID: 1, SessionID: 1, UserText: "以后回答时带上我的 API Key sk-abcdefghijklmnopqrstuvwxyz", SourceRef: "message:2",
	})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("credential-bearing preference was extracted: %+v err=%v", candidates, err)
	}
}

type recordingMemoryEmbeddingStore struct {
	model  string
	vector []float32
}

func (s *recordingMemoryEmbeddingStore) UpsertEmbedding(_ context.Context, item model.AgentMemoryItem, modelName string, vector []float32) (string, error) {
	s.model = modelName
	s.vector = append([]float32(nil), vector...)
	return "fake-vector:" + item.ID, nil
}

func TestAsyncMemoryCaptureAndEmbeddingUseFakeSeams(t *testing.T) {
	candidate := MemoryCandidate{UserID: 1, SessionID: 1, Scope: MemoryScope{Type: model.MemoryScopeUser, ID: "1"}, Kind: "preference", Content: "concise", SourceType: "user_message", SourceRef: "message:1", Importance: .6}
	recordingWriter := &recordingMemoryWriter{}
	capture := NewAsyncMemoryCapture(fakeMemoryExtractor{candidates: []MemoryCandidate{candidate}}, recordingWriter, 2)
	defer capture.Close(context.Background())
	if result := capture.EnqueueExtraction(MemoryExtractionRequest{UserID: 1, SessionID: 1, UserText: "please be concise", SourceRef: "message:1"}); !result.Accepted {
		t.Fatalf("EnqueueExtraction() = %+v", result)
	}
	waitForMemoryCondition(t, func() bool {
		recordingWriter.mu.Lock()
		defer recordingWriter.mu.Unlock()
		return len(recordingWriter.candidates) == 1
	})

	embeddingStore := &recordingMemoryEmbeddingStore{}
	projector := NewRepositoryMemoryProjector(fakeMemoryEmbedder{}, embeddingStore)
	ref, err := projector.Project(context.Background(), model.AgentMemoryItem{ID: "memory-1", UserID: 1, Content: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "fake-vector:memory-1" || embeddingStore.model != "fake-embedding" || !reflect.DeepEqual(embeddingStore.vector, []float32{3, 1}) {
		t.Fatalf("fake embedding projection = ref:%s model:%s vector:%v", ref, embeddingStore.model, embeddingStore.vector)
	}
}

func TestAsyncMemoryWriterRequiresSourceAndEmbeddingFailureKeepsRelationalItem(t *testing.T) {
	store := &fakeMemoryWriteStore{}
	writer := NewAsyncMemoryWriter(store, fakeMemoryAuthorizer{}, failingMemoryProjector{}, 4)
	defer writer.Close(context.Background())
	if got := writer.Enqueue(MemoryCandidate{UserID: 1, SessionID: 1, Scope: MemoryScope{Type: model.MemoryScopeUser, ID: "1"}, Kind: "preference", Content: "中文", SourceType: "user_message", Importance: .5}); got.Accepted {
		t.Fatalf("sourceless candidate accepted: %+v", got)
	}
	got := writer.Enqueue(MemoryCandidate{UserID: 1, SessionID: 1, Scope: MemoryScope{Type: model.MemoryScopeUser, ID: "1"}, Kind: "preference", Content: "中文", SourceType: "user_message", SourceRef: "message:1", Importance: .5})
	if !got.Accepted {
		t.Fatalf("candidate rejected: %+v", got)
	}
	waitForMemoryCondition(t, func() bool {
		_, failed, _, _ := writer.Stats()
		return failed == 1
	})
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.items) != 1 || store.items[0].SourceRef == "" || store.items[0].EmbeddingRef != "" {
		t.Fatalf("relational item after embedding failure = %+v", store.items)
	}
}

func TestAsyncMemoryWriterRejectsUnverifiedAgentAnswerAsVideoMemory(t *testing.T) {
	writer := NewAsyncMemoryWriter(&fakeMemoryWriteStore{}, fakeMemoryAuthorizer{}, nil, 1)
	defer writer.Close(context.Background())
	result := writer.Enqueue(MemoryCandidate{
		UserID: 1, SessionID: 1, Scope: MemoryScope{Type: model.MemoryScopeVideo, ID: "10"}, Kind: "fact", Content: "模型猜测",
		SourceType: "agent_answer", SourceRef: "message:9", Importance: .8,
	})
	if result.Accepted || !strings.Contains(result.Reason, "不能写入") {
		t.Fatalf("unverified video memory result = %+v", result)
	}
}

type failingLongTermMemoryProvider struct{}

func (failingLongTermMemoryProvider) Snapshot(context.Context, MemorySnapshotRequest) (MemorySnapshot, error) {
	return MemorySnapshot{}, errors.New("memory retrieval unavailable")
}

type failingMemoryCapture struct{}

func (failingMemoryCapture) EnqueueExtraction(MemoryExtractionRequest) MemoryEnqueueResult {
	return MemoryEnqueueResult{Reason: "async write unavailable"}
}

type staticMemoryProvider struct{ snapshot MemorySnapshot }

func (p staticMemoryProvider) Snapshot(context.Context, MemorySnapshotRequest) (MemorySnapshot, error) {
	return p.snapshot, nil
}

func TestVideoAgentInjectsMemoryBelowCurrentEvidenceAndPersistsSnapshotIdentity(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	policyService := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policyService.UpdateSessionPolicy(context.Background(), session.UserID, session.ID, model.MemorySessionPolicyEnabled, 0); err != nil {
		t.Fatal(err)
	}
	memory := MemorySnapshot{
		SchemaVersion: MemorySnapshotSchemaVersion,
		Version:       MemorySnapshotSchemaVersion + ":stable",
		MemoryIDs:     []string{"memory-1"},
		Items: []MemorySnapshotItem{{
			ID: "memory-1", ScopeType: model.MemoryScopeVideo, ScopeID: fmt.Sprint(task.ID), Kind: "topic",
			Content: "历史记忆说旧主题", SourceType: "user_confirmation", SourceRef: "message:1", Version: 2,
		}},
	}
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-current", ChunkID: 1, ChunkIndex: 0, Score: .9, Content: "当前视频证据说新主题",
	}}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: .3}, ChatDependencies{LongTermMemory: staticMemoryProvider{snapshot: memory}, MemoryCapture: failingMemoryCapture{}, MemoryPolicy: policyService})
	client := &scriptedChatClient{responses: []string{"not-json", "以当前证据为准 [C1]"}}
	result, err := NewVideoAgentService(chatSvc).Ask(context.Background(), VideoAgentRequest{
		UserID: session.UserID, SessionID: session.ID, Question: "主题是什么？", TopK: 1,
	}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Memory == nil || !reflect.DeepEqual(result.Memory.MemoryIDs, []string{"memory-1"}) {
		t.Fatalf("result memory = %+v", result.Memory)
	}
	finalMessages := client.messages[len(client.messages)-1]
	joined := ""
	for _, message := range finalMessages {
		joined += message.Content + "\n"
	}
	if !strings.Contains(joined, "记忆作为 Claim 或引用证据") || !strings.Contains(joined, "当前视频片段冲突，以当前视频片段为准") || !strings.Contains(joined, "当前视频证据说新主题") {
		t.Fatalf("final prompt does not preserve evidence priority: %s", joined)
	}
	messages, err := repos.Chat.ListMessages(session.UserID, session.ID)
	if err != nil || len(messages) != 2 || messages[1].RetrievalSnapshot == nil {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	snapshot, err := DecodeAgentSnapshot(*messages[1].RetrievalSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Memory == nil || snapshot.Memory.Version != memory.Version || !reflect.DeepEqual(snapshot.Memory.MemoryIDs, memory.MemoryIDs) {
		t.Fatalf("persisted memory snapshot = %+v", snapshot.Memory)
	}
	if strings.Contains(*messages[1].RetrievalSnapshot, "历史记忆说旧主题") || strings.Contains(*messages[1].RetrievalSnapshot, "message:1") {
		t.Fatalf("chat history retained memory content/source: %s", *messages[1].RetrievalSnapshot)
	}
}

func TestVideoAgentSucceedsWhenMemoryRecallAndAsyncWriteFail(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	policyService := NewMemoryPolicyService(repos.Memory, true)
	if _, err := policyService.UpdateSessionPolicy(context.Background(), session.UserID, session.ID, model.MemorySessionPolicyEnabled, 0); err != nil {
		t.Fatal(err)
	}
	chatSvc := NewChatServiceWithDependencies(repos, &fakeRetriever{results: []RetrievedChunk{{
		TaskID: task.ID, EvidenceID: "ev-memory-fail-open", ChunkID: 1, ChunkIndex: 0, Score: .9, Content: "当前视频证据",
	}}}, ChatConfig{TopK: 5, CandidateK: 5, MinScore: .3}, ChatDependencies{LongTermMemory: failingLongTermMemoryProvider{}, MemoryCapture: failingMemoryCapture{}, MemoryPolicy: policyService})
	agent := NewVideoAgentService(chatSvc)
	result, err := agent.Ask(context.Background(), VideoAgentRequest{UserID: session.UserID, SessionID: session.ID, Question: "请回答", TopK: 1},
		&fakeEmbeddingClient{dim: 3}, &scriptedChatClient{responses: []string{"not-json", "主回答成功 [C1]"}}, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "主回答成功" || len(result.Citations) != 1 || result.Memory != nil {
		t.Fatalf("result = %+v", result)
	}
}

func waitForMemoryCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for memory worker")
}
