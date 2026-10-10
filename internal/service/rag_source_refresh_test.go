package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type sourceRefreshEmbedding struct {
	once  func()
	calls int
}

func (e *sourceRefreshEmbedding) Embed(context.Context, string) ([]float32, error) {
	e.calls++
	if e.once != nil {
		fn := e.once
		e.once = nil
		fn()
	}
	return []float32{1, 2, 3}, nil
}

type sourceRefreshVectorStore struct {
	fakeVectorStore
	once func()
}

func (s *sourceRefreshVectorStore) UpsertChunks(ctx context.Context, vectors []RAGVector) error {
	if err := s.fakeVectorStore.UpsertChunks(ctx, vectors); err != nil {
		return err
	}
	if s.once != nil {
		fn := s.once
		s.once = nil
		fn()
	}
	return nil
}

// A source refresh inside a real build stage exercises both fences: no stale
// source rows may be written after embedding, and vectors written before a
// refresh can never make the old source searchable at completion.
func TestRAGIndexSourceRefreshDuringBuildCannotPublishStaleIndex(t *testing.T) {
	for _, stage := range []string{"embedding", "vector_projection"} {
		t.Run(stage, func(t *testing.T) {
			repos := newRAGIndexTestRepositories(t)
			task := &model.VideoTask{UserID: 7, FileMD5: "source-media", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded, ProcessingIntentJSON: "{}", MediaIdentityJSON: `{"platform":"bilibili","bvid":"BV-source","cid":123,"part_index":2,"media_fingerprint":"source-media"}`}
			if err := repos.Task.Create(task); err != nil {
				t.Fatal(err)
			}
			task = publishReadFixture(t, repos, task, 10)
			oldSource := task.ActiveTextSourceID
			refresh := func() { task = publishReadFixture(t, repos, task, 12) }
			embedding := &sourceRefreshEmbedding{}
			store := &sourceRefreshVectorStore{}
			if stage == "embedding" {
				embedding.once = refresh
			} else {
				store.once = refresh
			}
			svc := NewRAGIndexService(repos, store, RAGIndexConfig{ChunkSize: 800, EmbeddingDim: 3})
			_, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, embedding, ai.Profile{EmbeddingModel: "source-model", EmbeddingDim: 3})
			if !errors.Is(err, repository.ErrRAGSourceChanged) {
				t.Fatalf("build error=%v, want source change", err)
			}
			if task.ActiveTextSourceID == oldSource {
				t.Fatal("fixture did not refresh source")
			}
			row, err := repos.RAGIndex.FindByTaskAndModel(task.UserID, task.ID, "source-model")
			if err != nil || row == nil || row.Status != model.RAGIndexStatusNeedsRebuild {
				t.Fatalf("stale build overwrote invalidation: %+v %v", row, err)
			}
			chunks, err := repos.VideoChunk.ListByTaskID(task.UserID, task.ID, "source-model")
			if err != nil || len(chunks) != 0 {
				t.Fatalf("stale source chunks survived: %+v %v", chunks, err)
			}
			if stage == "embedding" && len(store.upserts) != 0 {
				t.Fatal("stale embedding reached vector projection")
			}
			if stage == "vector_projection" && len(store.upserts) == 0 {
				t.Fatal("fixture missed completion race")
			}
			// The new source can subsequently complete a clean build.
			result, err := svc.BuildTaskIndex(context.Background(), task.UserID, task.ID, &fakeEmbeddingClient{dim: 3}, ai.Profile{EmbeddingModel: "source-model", EmbeddingDim: 3})
			if err != nil || result == nil || !result.Indexed {
				t.Fatalf("replacement build=%+v %v", result, err)
			}
			fresh, err := repos.VideoChunk.ListByTaskID(task.UserID, task.ID, "source-model")
			if err != nil || len(fresh) == 0 {
				t.Fatalf("replacement chunks=%+v %v", fresh, err)
			}
			for _, chunk := range fresh {
				refs, err := ParseChunkSourceRefs(chunk.SourceRefs)
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range refs {
					if ref.SourceID != task.ActiveTextSourceID {
						t.Fatalf("replacement used old source: %+v", ref)
					}
				}
			}
		})
	}
}
