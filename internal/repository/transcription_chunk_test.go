package repository

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestTranscriptionChunkRepositoryPersistsTimingAndClearsSupersededTiming(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VideoTranscriptionChunk{}); err != nil {
		t.Fatal(err)
	}
	repo := NewTranscriptionChunkRepository(db)
	timeline := TranscriptionChunkTimeline{WindowStartMS: 18000, WindowEndMS: 42000, CoreStartMS: 20000, CoreEndMS: 40000}
	want := []model.TranscriptionSegment{{Text: "原句", StartMS: 19000, EndMS: 23000}}
	if err := repo.UpsertCompletedWithTimedSegments(99, 0, "audio.mp3", "原句", timeline, want); err != nil {
		t.Fatal(err)
	}
	row, err := repo.FindByTaskAndIndex(99, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []model.TranscriptionSegment
	if err := json.Unmarshal([]byte(row.TimedSegments), &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("timing=%+v err=%v", got, err)
	}
	if err := repo.UpsertCompletedWithTimedSegments(99, 0, "audio.mp3", "不合法", timeline, []model.TranscriptionSegment{{Text: "越界", StartMS: 19000, EndMS: 43000}}); err == nil {
		t.Fatal("out-of-window absolute timing persisted")
	}
	row, err = repo.FindByTaskAndIndex(99, 0)
	if err != nil || row.Content != "原句" {
		t.Fatalf("invalid write replaced source=%+v err=%v", row, err)
	}
	if err := repo.UpsertCompletedWithTimeline(99, 0, "audio.mp3", "新原文", timeline); err != nil {
		t.Fatal(err)
	}
	row, err = repo.FindByTaskAndIndex(99, 0)
	if err != nil || row.TimedSegments != "" || row.Content != "新原文" {
		t.Fatalf("stale timing=%+v err=%v", row, err)
	}
}

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

func TestTranscriptionChunkRepositoryTracksRetryWaitAndNextAttempt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VideoTranscriptionChunk{}); err != nil {
		t.Fatal(err)
	}
	repo := NewTranscriptionChunkRepository(db)
	if err := repo.UpsertPendingWithTimeline(9, 0, "private.mp3", TranscriptionChunkTimeline{}); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Minute)
	if err := repo.MarkRetryWait(9, 0, 1, "local_admission", until); err != nil {
		t.Fatal(err)
	}
	chunk, err := repo.FindByTaskAndIndex(9, 0)
	if err != nil || chunk == nil || chunk.Status != model.TranscriptionChunkStatusRetryWait || chunk.RetryCount != 1 || chunk.WaitReason != "local_admission" || chunk.NextRetryAt == nil {
		t.Fatalf("retry wait chunk = %+v, error = %v", chunk, err)
	}
	if err := repo.MarkAttempt(9, 0, 1); err != nil {
		t.Fatal(err)
	}
	chunk, err = repo.FindByTaskAndIndex(9, 0)
	if err != nil || chunk == nil || chunk.Status != model.TranscriptionChunkStatusRunning || chunk.NextRetryAt != nil || chunk.WaitReason != "" {
		t.Fatalf("next attempt chunk = %+v, error = %v", chunk, err)
	}
}
