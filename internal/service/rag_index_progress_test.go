package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type rateLimitedEmbedding struct{ calls int }

func (e *rateLimitedEmbedding) Embed(context.Context, string) ([]float32, error) {
	e.calls++
	if e.calls == 1 {
		return nil, &ai.ProviderError{Class: ai.ErrorRateLimited, StatusCode: 429, RetryAfter: time.Millisecond}
	}
	return []float32{1, 2, 3}, nil
}

func TestIndexEmbeddingReportsProviderRateLimitWait(t *testing.T) {
	embedding := &rateLimitedEmbedding{}
	reason := ""
	vector, err := embedWithAdmissionProgress(context.Background(), embedding, "source", func(value string, retryAt time.Time) error {
		reason = value
		if !retryAt.After(time.Now()) {
			t.Fatal("retry time must be in the future")
		}
		return nil
	})
	if err != nil || len(vector) != 3 || embedding.calls != 2 || reason != "provider_rate_limit" {
		t.Fatalf("retry = %v, calls %d, reason %q, error %v", vector, embedding.calls, reason, err)
	}
}

func TestRAGIndexBuildRejectsExistingClaimBeforeEmbedding(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "12121212121212121212121212121212", Filename: "video.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source text"}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	claimed, err := repos.RAGIndex.ClaimBuild(&model.VideoRAGIndex{
		UserID: 7, TaskID: task.ID, FileMD5: task.FileMD5, EmbeddingModel: "embed", EmbeddingDim: 3,
		Status: model.RAGIndexStatusIndexing, BuildPhase: "embedding", StartedAt: &started,
	})
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	embedding := &fakeEmbeddingClient{dim: 3}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 20, EmbeddingDim: 3})
	_, err = svc.BuildTaskIndex(context.Background(), 7, task.ID, embedding, ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3})
	if err == nil || !strings.Contains(err.Error(), "正在构建") {
		t.Fatalf("duplicate build error = %v", err)
	}
	if len(embedding.inputs) != 0 {
		t.Fatalf("duplicate build called embedding %d times", len(embedding.inputs))
	}
	index, err := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "embed")
	if err != nil || index == nil || index.Status != model.RAGIndexStatusIndexing {
		t.Fatalf("index = %+v, %v", index, err)
	}
}

func TestStaleIndexBuildCanBeRetried(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "56565656565656565656565656565656", Filename: "video.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source text"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID: 7, TaskID: task.ID, FileMD5: task.FileMD5, EmbeddingModel: "embed", EmbeddingDim: 3,
		Status: model.RAGIndexStatusIndexing, StartedAt: &old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	profile := ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 20, EmbeddingDim: 3})
	status, err := svc.GetTaskIndexStatus(context.Background(), 7, task.ID, profile)
	if err != nil || status.Status != model.RAGIndexStatusFailed || status.LastError == "" {
		t.Fatalf("stale status = %+v, error = %v", status, err)
	}
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, profile); err != nil {
		t.Fatalf("retry stale build: %v", err)
	}
}

func TestRAGIndexStatusReportsQueuedAndCompletedChunkCounts(t *testing.T) {
	repos := newRAGIndexTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "34343434343434343434343434343434", Filename: "video.mp4", Status: model.TaskStatusQueued, Stage: model.TaskStageIndexing}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	svc := NewRAGIndexService(repos, &fakeVectorStore{}, RAGIndexConfig{ChunkSize: 20, EmbeddingDim: 3})
	profile := ai.Profile{EmbeddingModel: "embed", EmbeddingDim: 3}
	queued, err := svc.GetTaskIndexStatus(context.Background(), 7, task.ID, profile)
	if err != nil || queued.Status != "queued" || queued.BuildPhase != "queued" {
		t.Fatalf("queued = %+v, %v", queued, err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source text"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BuildTaskIndex(context.Background(), 7, task.ID, &fakeEmbeddingClient{dim: 3}, profile); err != nil {
		t.Fatal(err)
	}
	// The parent task can still be indexing until its worker commits completion.
	index, err := repos.RAGIndex.FindByTaskAndModel(7, task.ID, "embed")
	if err != nil || index == nil || index.TotalChunks == 0 || index.CompletedChunks != index.TotalChunks || index.BuildPhase != "completed" {
		t.Fatalf("completed progress = %+v, %v", index, err)
	}
}
