package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/processingguard"
)

type cancelAfterEmbeddingClient struct {
	cancel context.CancelFunc
	calls  int
	dim    int
}

func (c *cancelAfterEmbeddingClient) Embed(_ context.Context, _ string) ([]float32, error) {
	c.calls++
	c.cancel()
	return make([]float32, c.dim), nil
}

type cancelAfterDeleteStore struct {
	cancel      context.CancelFunc
	deleteCalls int
	upsertCalls int
}

func (s *cancelAfterDeleteStore) DeleteTaskChunks(_ context.Context, _, _ int64, _ string) error {
	s.deleteCalls++
	s.cancel()
	return nil
}

func (s *cancelAfterDeleteStore) UpsertChunks(_ context.Context, _ []RAGVector) error {
	s.upsertCalls++
	return nil
}

func TestRAGIndexServiceStopsAfterContextCancellationDuringEmbedding(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "video.mp4", FileURL: "videos/guard-embedding.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	embedding := &cancelAfterEmbeddingClient{cancel: cancel, dim: 3}
	store := &fakeVectorStore{}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})

	_, err := svc.BuildTaskIndex(ctx, 7, task.ID, embedding, ai.Profile{EmbeddingModel: "test-model", EmbeddingDim: 3})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildTaskIndex() error = %v, want context.Canceled", err)
	}
	if len(store.upserts) != 0 {
		t.Fatalf("vector upserts = %d, want 0 after cancellation", len(store.upserts))
	}
	chunks, listErr := repos.VideoChunk.ListByTaskID(7, task.ID, "test-model")
	if listErr != nil {
		t.Fatalf("list chunks: %v", listErr)
	}
	if len(chunks) != 0 {
		t.Fatalf("stored chunks = %d, want 0 after cancellation", len(chunks))
	}
	index, findErr := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "test-model")
	if findErr != nil {
		t.Fatalf("find index: %v", findErr)
	}
	if index == nil || index.Status != model.RAGIndexStatusIndexing {
		t.Fatalf("index status = %+v, want indexing without stale terminal write", index)
	}
}

func TestRAGIndexServiceStopsAfterContextCancellationDuringVectorDelete(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", Filename: "video.mp4", FileURL: "videos/guard-delete.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelAfterDeleteStore{cancel: cancel}
	embedding := &fakeEmbeddingClient{dim: 3}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})

	_, err := svc.BuildTaskIndex(ctx, 7, task.ID, embedding, ai.Profile{EmbeddingModel: "test-model", EmbeddingDim: 3})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildTaskIndex() error = %v, want context.Canceled", err)
	}
	if store.deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", store.deleteCalls)
	}
	if len(embedding.inputs) != 0 {
		t.Fatalf("embedding calls = %d, want 0 after cancellation", len(embedding.inputs))
	}
	if store.upsertCalls != 0 {
		t.Fatalf("upsert calls = %d, want 0", store.upsertCalls)
	}
}

type guardLossEmbeddingClient struct {
	lost *bool
	dim  int
}

func (c *guardLossEmbeddingClient) Embed(_ context.Context, _ string) ([]float32, error) {
	*c.lost = true
	return make([]float32, c.dim), nil
}

func TestRAGIndexServiceStopsWhenProcessingGuardIsLostDuringEmbedding(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "efefefefefefefefefefefefefefefef", Filename: "video.mp4", FileURL: "videos/guard-lease.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	leaseLost := errors.New("test processing lease lost")
	lost := false
	ctx := processingguard.With(context.Background(), func(context.Context) error {
		if lost {
			return leaseLost
		}
		return nil
	})
	store := &fakeVectorStore{}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})

	_, err := svc.BuildTaskIndex(ctx, 7, task.ID, &guardLossEmbeddingClient{lost: &lost, dim: 3}, ai.Profile{EmbeddingModel: "test-model", EmbeddingDim: 3})
	if !errors.Is(err, leaseLost) {
		t.Fatalf("BuildTaskIndex() error = %v, want lease guard error", err)
	}
	if len(store.upserts) != 0 {
		t.Fatalf("vector upserts = %d, want 0 after lease loss", len(store.upserts))
	}
	chunks, listErr := repos.VideoChunk.ListByTaskID(7, task.ID, "test-model")
	if listErr != nil {
		t.Fatalf("list chunks: %v", listErr)
	}
	if len(chunks) != 0 {
		t.Fatalf("stored chunks = %d, want 0 after lease loss", len(chunks))
	}
	index, findErr := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "test-model")
	if findErr != nil {
		t.Fatalf("find index: %v", findErr)
	}
	if index == nil || index.Status != model.RAGIndexStatusIndexing {
		t.Fatalf("index status = %+v, want indexing without stale terminal write", index)
	}
}
