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

func TestVideoChunkRepositorySearchByBM25RanksKeywordMatches(t *testing.T) {
	repo := newVideoChunkTestRepo(t)
	chunks := []model.VideoChunk{
		{UserID: 7, TaskID: 1, ChunkIndex: 0, Content: "Redis 分布式锁释放时必须校验 owner，避免删掉别人的锁", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v0"},
		{UserID: 7, TaskID: 1, ChunkIndex: 1, Content: "WatchDog 会在长任务运行时自动续期分布式锁", ContentHash: "hash1", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v1"},
		{UserID: 7, TaskID: 1, ChunkIndex: 2, Content: "AI 总结会复用已有转写文本", ContentHash: "hash2", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v2"},
	}
	if err := repo.ReplaceTaskChunks(1, "text-embedding-3-small", chunks); err != nil {
		t.Fatalf("ReplaceTaskChunks() error = %v", err)
	}

	results, err := repo.SearchByBM25(7, 1, "text-embedding-3-small", []string{"分布式锁", "owner", "校验"}, 5)
	if err != nil {
		t.Fatalf("SearchByBM25() error = %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("results = %+v, want at least two keyword matches", results)
	}
	if results[0].Chunk.ChunkIndex != 0 {
		t.Fatalf("top chunk index = %d, want owner chunk first: %+v", results[0].Chunk.ChunkIndex, results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("top BM25 score should be greater than second: %+v", results)
	}
	if results[0].Rank != 1 || results[1].Rank != 2 {
		t.Fatalf("ranks = %+v, want 1-based ranks", results)
	}
}

func TestVideoChunkRepositoryListByIndexRangeWindowPreservesOrderAndScope(t *testing.T) {
	repo := newVideoChunkTestRepo(t)
	chunks := []model.VideoChunk{
		{UserID: 7, TaskID: 1, ChunkIndex: 0, Content: "target-0", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v0"},
		{UserID: 7, TaskID: 1, ChunkIndex: 1, Content: "target-1", ContentHash: "hash1", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v1"},
		{UserID: 7, TaskID: 1, ChunkIndex: 2, Content: "target-2", ContentHash: "hash2", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v2"},
		{UserID: 8, TaskID: 3, ChunkIndex: 1, Content: "wrong-user", ContentHash: "hash3", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v3"},
		{UserID: 7, TaskID: 2, ChunkIndex: 1, Content: "wrong-task", ContentHash: "hash4", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "v4"},
		{UserID: 7, TaskID: 1, ChunkIndex: 1, Content: "wrong-model", ContentHash: "hash5", EmbeddingModel: "other-model", EmbeddingDim: 1536, VectorID: "v5"},
	}
	for _, chunk := range chunks {
		if err := repo.db.Create(&chunk).Error; err != nil {
			t.Fatalf("create chunk %+v: %v", chunk, err)
		}
	}

	window, err := repo.ListByIndexRange(7, 1, "text-embedding-3-small", 0, 2)
	if err != nil {
		t.Fatalf("ListByIndexRange() error = %v", err)
	}
	if len(window) != 3 {
		t.Fatalf("len(window) = %d, want 3: %+v", len(window), window)
	}
	for i, chunk := range window {
		if chunk.ChunkIndex != i {
			t.Fatalf("window[%d].ChunkIndex = %d, want %d: %+v", i, chunk.ChunkIndex, i, window)
		}
		if chunk.UserID != 7 || chunk.TaskID != 1 || chunk.EmbeddingModel != "text-embedding-3-small" {
			t.Fatalf("window[%d] escaped scope: %+v", i, chunk)
		}
	}

	wrongUserWindow, err := repo.ListByIndexRange(7, 3, "text-embedding-3-small", 1, 1)
	if err != nil {
		t.Fatalf("ListByIndexRange(wrong user scope) error = %v", err)
	}
	if len(wrongUserWindow) != 0 {
		t.Fatalf("wrong user window = %+v, want empty", wrongUserWindow)
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
