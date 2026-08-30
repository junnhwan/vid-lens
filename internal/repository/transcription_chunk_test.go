package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestTranscriptionChunkRepositoryPersistsOverlapTimeline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VideoTranscriptionChunk{}); err != nil {
		t.Fatal(err)
	}
	repo := NewTranscriptionChunkRepository(db)
	timeline := TranscriptionChunkTimeline{
		SegmentKey: "overlap_windows_v1:295000:605000:300000:600000", SegmenterVersion: "overlap_windows_v1",
		WindowStartMS: 295_000, WindowEndMS: 605_000, CoreStartMS: 300_000, CoreEndMS: 600_000,
	}
	if err := repo.UpsertRunningWithTimeline(42, 1, "chunk.mp3", timeline); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertCompletedWithTimeline(42, 1, "chunk.mp3", "转录文本", timeline); err != nil {
		t.Fatal(err)
	}
	chunk, err := repo.FindByTaskAndIndex(42, 1)
	if err != nil {
		t.Fatal(err)
	}
	if chunk == nil || chunk.SegmentKey != timeline.SegmentKey || chunk.WindowStartMS != 295_000 || chunk.WindowEndMS != 605_000 || chunk.CoreStartMS != 300_000 || chunk.CoreEndMS != 600_000 || chunk.StartSecond != 295 || chunk.EndSecond != 605 {
		t.Fatalf("chunk = %+v", chunk)
	}
}

func TestTranscriptionChunkRepositoryRejectsCoreOutsideWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewTranscriptionChunkRepository(db)
	err = repo.UpsertRunningWithTimeline(42, 0, "chunk.mp3", TranscriptionChunkTimeline{
		WindowStartMS: 5_000, WindowEndMS: 10_000, CoreStartMS: 0, CoreEndMS: 10_000,
	})
	if err == nil {
		t.Fatal("expected invalid timeline error")
	}
}
