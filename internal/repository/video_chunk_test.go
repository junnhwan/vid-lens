package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestVideoChunkRepositoryReplaceTaskChunks(t *testing.T) {
	repo := newVideoChunkTestRepo(t)

	err := repo.ReplaceTaskChunks(1, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: 1, ChunkIndex: 0, Content: "old", ContentHash: "old", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "old"},
	})
	if err != nil {
		t.Fatalf("ReplaceTaskChunks(old) error = %v", err)
	}

	err = repo.ReplaceTaskChunks(1, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: 1, ChunkIndex: 0, Content: "new-0", ContentHash: "new0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "new-0"},
		{UserID: 7, TaskID: 1, ChunkIndex: 1, Content: "new-1", ContentHash: "new1", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "new-1"},
	})
	if err != nil {
		t.Fatalf("ReplaceTaskChunks(new) error = %v", err)
	}

	chunks, err := repo.ListByTaskID(7, 1, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("ListByTaskID() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if chunks[0].Content != "new-0" || chunks[1].Content != "new-1" {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
}

func newVideoChunkTestRepo(t *testing.T) *VideoChunkRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.VideoChunk{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return NewVideoChunkRepository(db)
}
