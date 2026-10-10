package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

func TestTaskCleanupPreservesPrivateSourceUntilRawDeletionSucceeds(t *testing.T) {
	repos, db := newMediaTestRepositoriesAndDB(t)
	ctx := context.Background()
	asset := createMediaTestAsset(t, repos, "12345678901234567890123456789012", "videos/source-cleanup.mp4")
	task := createMediaTestTask(t, repos, 7, asset, "source-cleanup.mp4")
	snapshot, err := textsource.Canonicalize(textsource.Snapshot{Kind: textsource.KindASR, Identity: textsource.Identity{Platform: "local", MediaFingerprint: task.FileMD5}, ParserVersion: "asr-fixture", RawObjectKey: "sources/7/private.srt", Cues: []textsource.Cue{{ID: "cue-1", Order: 1, Text: "私有来源", RawText: "私有来源", TimingMethod: textsource.TimingUnknown}}}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	source, err := repos.PublishTextSource(ctx, repository.PublishTextSourceRequest{UserID: 7, TaskID: task.ID, Snapshot: snapshot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SummaryScreenshotRef{ID: "registered-image", UserID: 7, TaskID: task.ID, GenerationID: "gen-1", SourceID: source.ID, SourceDigest: source.SourceDigest, MediaRevision: task.FileMD5, BlockID: "config", Status: "ready", Inspected: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	storage := &flakyCleanupObjectStorage{err: errors.New("object deletion failed")}
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{Now: func() time.Time { return now }, RetryBackoff: time.Second})
	job, err := cleanup.RequestDelete(ctx, 7, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repos.TextSource.Read(ctx, 7, task.ID, source.ID); err == nil {
		t.Fatal("deleted task private source still readable")
	}
	if err := cleanup.ExecuteJob(ctx, job.ID); err == nil {
		t.Fatal("failed raw deletion completed cleanup")
	}
	var count int64
	db.Model(&model.VideoTextSource{}).Where("task_id = ?", task.ID).Count(&count)
	if count != 1 {
		t.Fatal("raw recovery facts lost")
	}
	storage.err = nil
	now = now.Add(2 * time.Second)
	if err := cleanup.ExecuteJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.VideoTextSource{}, &model.VideoTextCue{}, &model.SummaryScreenshotRef{}} {
		query := db.Model(row)
		if _, ok := row.(*model.VideoTextCue); ok {
			query = query.Where("source_id = ?", source.ID)
		} else {
			query = query.Where("task_id = ?", task.ID)
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("owned source rows remain %T count=%d err=%v", row, count, err)
		}
	}
	if len(storage.deleted) != 3 || storage.deleted[0] != snapshot.RawObjectKey || storage.deleted[1] != snapshot.RawObjectKey || storage.deleted[2] != asset.ObjectName {
		t.Fatalf("cleanup order/retry=%v", storage.deleted)
	}
}
