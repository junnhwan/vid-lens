package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type fakeEmbeddingClient struct {
	dim    int
	inputs []string
}

func (c *fakeEmbeddingClient) Embed(_ context.Context, input string) ([]float32, error) {
	c.inputs = append(c.inputs, input)
	vector := make([]float32, c.dim)
	for i := range vector {
		vector[i] = float32(i)
	}
	return vector, nil
}

type fakeVectorStore struct {
	upserts []RAGVector
}

func (s *fakeVectorStore) UpsertChunks(_ context.Context, vectors []RAGVector) error {
	s.upserts = append([]RAGVector(nil), vectors...)
	return nil
}

func TestRAGIndexServiceBuildTaskIndexCreatesChunksAndVectors(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "video.mp4", FileURL: "videos/a.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{
		TaskID:  task.ID,
		Content: "abcdefghijklmnopqrstuvwxyz",
		Words:   26,
	}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	embedding := &fakeEmbeddingClient{dim: 3}
	store := &fakeVectorStore{}
	svc := NewRAGIndexService(repos, store, RAGIndexConfig{
		ChunkSize:      10,
		ChunkOverlap:   2,
		EmbeddingDim:   3,
		CollectionName: "vidlens_video_chunks",
	})

	result, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, embedding, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err != nil {
		t.Fatalf("BuildTaskIndex() error = %v", err)
	}
	if result.Chunks != 4 {
		t.Fatalf("result.Chunks = %d, want 4", result.Chunks)
	}
	if len(embedding.inputs) != 4 {
		t.Fatalf("embedding calls = %d, want 4", len(embedding.inputs))
	}
	if len(store.upserts) != 4 {
		t.Fatalf("vector upserts = %d, want 4", len(store.upserts))
	}

	chunks, err := repos.VideoChunk.ListByTaskID(7, task.ID, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("ListByTaskID() error = %v", err)
	}
	if len(chunks) != 4 {
		t.Fatalf("stored chunks = %d, want 4", len(chunks))
	}
	if chunks[0].VectorID == "" || store.upserts[0].VectorID != chunks[0].VectorID {
		t.Fatalf("vector id mismatch: chunk=%q vector=%q", chunks[0].VectorID, store.upserts[0].VectorID)
	}
	if store.upserts[0].Content != chunks[0].Content {
		t.Fatalf("vector content = %q, want %q", store.upserts[0].Content, chunks[0].Content)
	}
}

func TestRAGIndexServiceRejectsEmbeddingDimMismatch(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "video.mp4", FileURL: "videos/b.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 1536})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded with mismatched embedding dim")
	}
}

func TestRAGIndexServiceRejectsProfileDimDifferentFromCollectionDim(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "99999999999999999999999999999999", Filename: "video.mp4", FileURL: "videos/g.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 1536})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 1024}, ai.Profile{
		EmbeddingModel: "custom-embedding",
		EmbeddingDim:   1024,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded with profile dim different from collection dim")
	}
}

func TestRAGIndexServiceRejectsMissingVectorStore(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "ffffffffffffffffffffffffffffffff", Filename: "video.mp4", FileURL: "videos/f.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "abcdefghij", Words: 10}); err != nil {
		t.Fatalf("upsert transcription: %v", err)
	}

	svc := NewRAGIndexService(repos, nil, RAGIndexConfig{ChunkSize: 10, EmbeddingDim: 3})
	_, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   3,
	})
	if err == nil {
		t.Fatal("BuildTaskIndex() succeeded without vector store")
	}
}

func TestRAGIndexServiceGetTaskIndexStatusUsesStoredChunks(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "12121212121212121212121212121212", Filename: "video.mp4", FileURL: "videos/status.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "chunk 0", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "vector-0"},
		{UserID: 7, TaskID: task.ID, ChunkIndex: 1, Content: "chunk 1", ContentHash: "hash1", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "vector-1"},
	}); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	svc := NewRAGIndexService(repos, nil, RAGIndexConfig{CollectionName: "vidlens_video_chunks"})
	status, err := svc.GetTaskIndexStatus(context.Background(), 7, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus() error = %v", err)
	}
	if !status.Indexed || status.Chunks != 2 {
		t.Fatalf("status = %+v, want indexed with 2 chunks", status)
	}

	otherUserStatus, err := svc.GetTaskIndexStatus(context.Background(), 8, task.ID, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
	})
	if err != nil {
		t.Fatalf("GetTaskIndexStatus(wrong user) error = %v", err)
	}
	if otherUserStatus.Indexed || otherUserStatus.Chunks != 0 {
		t.Fatalf("wrong user status = %+v, want not indexed", otherUserStatus)
	}
}

func newRAGIndexTestRepositories(t *testing.T) *repository.Repositories {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return repository.NewRepositories(db)
}
