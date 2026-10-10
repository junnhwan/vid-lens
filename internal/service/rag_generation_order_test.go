package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// This fixture models the external store's physical rows, including orphans.
// Search selects current authorized relational IDs before ranking/TopK, exactly
// as the pgvector EXISTS authority query does.
type orderedGenerationStore struct {
	repos        *repository.Repositories
	mu           sync.Mutex
	rows         map[string]RAGVector
	calls        int
	deleteCalls  int
	replaceCalls int
	firstEntered chan struct{}
	releaseFirst chan struct{}
	oldIDs       map[string]bool
}

func (s *orderedGenerationStore) UpsertChunks(ctx context.Context, vectors []RAGVector) error {
	return s.write(ctx, 0, 0, "", vectors, false)
}

func (s *orderedGenerationStore) write(ctx context.Context, userID, taskID int64, embeddingModel string, vectors []RAGVector, replace bool) error {
	s.mu.Lock()
	s.calls++
	if replace {
		s.replaceCalls++
	}
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		close(s.firstEntered)
		select {
		case <-s.releaseFirst:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if replace {
		for id, row := range s.rows {
			if row.UserID == userID && row.TaskID == taskID && row.EmbeddingModel == embeddingModel {
				delete(s.rows, id)
			}
		}
	}
	for _, vector := range vectors {
		s.rows[vector.VectorID] = vector
		if first {
			s.oldIDs[vector.VectorID] = true
		}
	}
	return nil
}
func (s *orderedGenerationStore) DeleteTaskChunks(_ context.Context, userID, taskID int64, embeddingModel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	for id, row := range s.rows {
		if row.UserID == userID && row.TaskID == taskID && row.EmbeddingModel == embeddingModel {
			delete(s.rows, id)
		}
	}
	return nil
}
func (s *orderedGenerationStore) ReplaceTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string, vectors []RAGVector) error {
	return s.write(ctx, userID, taskID, embeddingModel, vectors, true)
}
func (s *orderedGenerationStore) Search(_ context.Context, _ []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	ids := req.TaskIDs
	if len(ids) == 0 {
		ids = []int64{req.TaskID}
	}
	allowed := map[int64]model.VideoChunk{}
	for _, id := range ids {
		rows, err := s.repos.VideoChunk.ListByTaskID(req.UserID, id, req.EmbeddingModel)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			allowed[row.ID] = row
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var hits []RetrievedChunk
	for _, vector := range s.rows {
		current, ok := allowed[vector.ChunkID]
		if !ok || current.VectorID != vector.VectorID || vector.UserID != req.UserID || vector.EmbeddingModel != req.EmbeddingModel {
			continue
		}
		score := float32(0.8)
		if s.oldIDs[vector.VectorID] {
			score = 1
		}
		hits = append(hits, RetrievedChunk{TaskID: vector.TaskID, ChunkID: vector.ChunkID, ChunkIndex: vector.ChunkIndex, EvidenceID: vector.VectorID, Content: vector.Content, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if req.TopK > 0 && len(hits) > req.TopK {
		hits = hits[:req.TopK]
	}
	return hits, nil
}

func TestRAGLateOldProjectionWriteCannotEraseCompletedCurrentSource(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "source-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded, ProcessingIntentJSON: "{}", MediaIdentityJSON: `{"platform":"bilibili","bvid":"BV-source","cid":123,"part_index":2,"media_fingerprint":"source-media"}`}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	task = publishReadFixture(t, repos, task, 10)
	store := &orderedGenerationStore{repos: repos, rows: map[string]RAGVector{}, oldIDs: map[string]bool{}, firstEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 800, EmbeddingDim: 3})
	profile := ai.Profile{EmbeddingModel: "source-model", EmbeddingDim: 3}
	oldDone := make(chan error, 1)
	go func() {
		_, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, profile)
		oldDone <- err
	}()
	select {
	case <-store.firstEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("old write did not pass preflight")
	}
	oldChunks, err := repos.VideoChunk.ListByTaskID(task.UserID, task.ID, profile.EmbeddingModel)
	if err != nil || len(oldChunks) == 0 {
		close(store.releaseFirst)
		t.Fatalf("old fixture source rows missing: %+v %v", oldChunks, err)
	}
	// Identical wording with changed subtitle timing changes source identity;
	// content-only vector IDs would collide here.
	task = publishReadFixture(t, repos, task, 12)
	current, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, profile)
	if err != nil || !current.Indexed {
		close(store.releaseFirst)
		t.Fatalf("new build did not complete: %+v %v", current, err)
	}
	fresh, _ := repos.VideoChunk.ListByTaskID(task.UserID, task.ID, profile.EmbeddingModel)
	if len(fresh) == 0 {
		close(store.releaseFirst)
		t.Fatal("new build has no source rows")
	}
	close(store.releaseFirst)
	select {
	case err := <-oldDone:
		if !errors.Is(err, repository.ErrRAGSourceChanged) {
			t.Fatalf("old completion=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old write did not finish")
	}
	if fresh[0].ContentHash != oldChunks[0].ContentHash || fresh[0].VectorID == oldChunks[0].VectorID {
		t.Fatal("same text from different generations must use distinct vector identities")
	}
	index, err := repos.RAGIndex.FindByTaskAndModel(task.UserID, task.ID, profile.EmbeddingModel)
	if err != nil || index.Status != model.RAGIndexStatusIndexed {
		t.Fatalf("old build invalidated new publication %+v %v", index, err)
	}
	store.mu.Lock()
	physical, old, deletes, replaces := len(store.rows), len(store.oldIDs), store.deleteCalls, store.replaceCalls
	store.mu.Unlock()
	if physical <= len(fresh) || old == 0 {
		t.Fatal("fixture did not retain both late old and new remote generations")
	}
	if deletes != 0 || replaces != 0 {
		t.Fatalf("build used destructive scope operations: deletes=%d replaces=%d", deletes, replaces)
	}
	hits, err := store.Search(context.Background(), []float32{1, 2, 3}, RetrievalRequest{UserID: task.UserID, TaskID: task.ID, EmbeddingModel: profile.EmbeddingModel, TopK: 1})
	if err != nil || len(hits) != 1 || hits[0].ChunkID != fresh[0].ID || hits[0].EvidenceID != fresh[0].VectorID {
		t.Fatalf("current generation lost to late old write: %+v %v", hits, err)
	}
	oldRead, err := repos.VideoChunk.FindByIdentity(task.UserID, task.ID, oldChunks[0].ID, oldChunks[0].VectorID)
	if err != nil || oldRead != nil {
		t.Fatalf("source_read accepted obsolete projection identity: %+v %v", oldRead, err)
	}
	newRead, err := repos.VideoChunk.FindByIdentity(task.UserID, task.ID, fresh[0].ID, fresh[0].VectorID)
	if err != nil || newRead == nil {
		t.Fatalf("source_read lost current projection identity: %+v %v", newRead, err)
	}
	pipeline := NewRetrievalPipeline(repos, store, NoopQueryRewriter{}, nil, nil, 1, 0)
	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: task.UserID, TaskID: task.ID, EmbeddingModel: profile.EmbeddingModel, Embedding: &fakeEmbeddingClient{dim: 3}, Question: "字幕说明什么缓存策略", TopK: 1})
	if err != nil || len(result.Citations) != 1 || result.Citations[0].ChunkID != fresh[0].ID {
		t.Fatalf("latest retrieval is unreadable: %+v %v", result, err)
	}
	for _, ref := range result.Citations[0].SourceRefs {
		if ref.SourceID != task.ActiveTextSourceID {
			t.Fatalf("retrieval mixed old source: %+v", ref)
		}
	}
}

type refreshAfterGenerationSearch struct {
	store   *orderedGenerationStore
	refresh func()
}

func (r *refreshAfterGenerationSearch) Search(ctx context.Context, query []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	hits, err := r.store.Search(ctx, query, req)
	if r.refresh != nil {
		r.refresh()
		r.refresh = nil
	}
	return hits, err
}

func TestRAGSourceRefreshAfterRecallDropsRetiredProjectionFromContext(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "source-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded, ProcessingIntentJSON: "{}", MediaIdentityJSON: `{"platform":"bilibili","bvid":"BV-source","cid":123,"part_index":2,"media_fingerprint":"source-media"}`}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	task = publishReadFixture(t, repos, task, 10)
	store := &orderedGenerationStore{repos: repos, rows: map[string]RAGVector{}, oldIDs: map[string]bool{}, calls: 1}
	profile := ai.Profile{EmbeddingModel: "source-model", EmbeddingDim: 3}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 800, EmbeddingDim: 3})
	if _, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, profile); err != nil {
		t.Fatal(err)
	}
	retriever := &refreshAfterGenerationSearch{store: store, refresh: func() { task = publishReadFixture(t, repos, task, 12) }}
	pipeline := NewRetrievalPipeline(repos, retriever, NoopQueryRewriter{}, nil, nil, 1, 0)
	result, err := pipeline.Retrieve(context.Background(), RetrievalPipelineRequest{UserID: task.UserID, TaskID: task.ID, EmbeddingModel: profile.EmbeddingModel, Embedding: &fakeEmbeddingClient{dim: 3}, Question: "缓存策略", TopK: 1})
	if err != nil || len(result.Citations) != 0 {
		t.Fatalf("retired vector/keyword generation entered context: %+v %v", result, err)
	}
}

func TestRAGIndexV7RequiresRebuildIntoIsolatedProjectionContract(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "v7-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, FileMD5: task.FileMD5, Content: "既有可重建的文字"}); err != nil {
		t.Fatal(err)
	}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 800, EmbeddingDim: 3})
	profile := ai.Profile{EmbeddingModel: "source-model", EmbeddingDim: 3}
	if _, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, profile); err != nil {
		t.Fatal(err)
	}
	status, err := svc.GetTaskIndexStatus(context.Background(), task.UserID, task.ID, profile)
	if err != nil || !status.Indexed || status.NeedsRebuild {
		t.Fatalf("current contract unexpectedly stale: %+v %v", status, err)
	}
	index, err := repos.RAGIndex.FindByTaskAndModel(task.UserID, task.ID, profile.EmbeddingModel)
	if err != nil {
		t.Fatal(err)
	}
	index.BuildVersion = 7
	if err := repos.RAGIndex.Upsert(index); err != nil {
		t.Fatal(err)
	}
	status, err = svc.GetTaskIndexStatus(context.Background(), task.UserID, task.ID, profile)
	if err != nil || status.Indexed || !status.NeedsRebuild || status.Status != model.RAGIndexStatusNeedsRebuild {
		t.Fatalf("v7 unsafe projection was reused: %+v %v", status, err)
	}
	if _, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, profile); err != nil {
		t.Fatal(err)
	}
	status, err = svc.GetTaskIndexStatus(context.Background(), task.UserID, task.ID, profile)
	if err != nil || !status.Indexed || status.NeedsRebuild {
		t.Fatalf("v7 index did not recover into v8: %+v %v", status, err)
	}
}
