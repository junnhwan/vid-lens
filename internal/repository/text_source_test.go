package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/textsource"
)

func sourceFixture(t *testing.T, text string, end int64) textsource.Snapshot {
	t.Helper()
	start := int64(0)
	snapshot, err := textsource.Canonicalize(textsource.Snapshot{
		Kind: textsource.KindSubtitle, Identity: textsource.Identity{Platform: "bilibili", BVID: "BV1fixture", CID: 123, PartIndex: 2, MediaFingerprint: "11111111111111111111111111111111"},
		Language: "zh-CN", TrackKey: "zh-CN", SubtitleKind: "unknown", ParserVersion: textsource.SRTParserVersion,
		Cues: []textsource.Cue{{ID: "cue-1", Order: 1, RawText: text, Text: text, StartMS: &start, EndMS: &end, TimingMethod: textsource.TimingSubtitle}},
	}, textsource.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func createSourceTask(t *testing.T, db *gorm.DB, id, owner int64) model.VideoTask {
	t.Helper()
	task := model.VideoTask{ID: id, UserID: owner, FileMD5: "11111111111111111111111111111111", Filename: "lesson.mp4", FileURL: "videos/lesson.mp4", Status: model.TaskStatusCompleted, Stage: model.TaskStageUploaded}
	identity, _ := json.Marshal(textsource.Identity{Platform: "bilibili", BVID: "BV1fixture", CID: 123, PartIndex: 2, MediaFingerprint: task.FileMD5})
	task.MediaIdentityJSON = string(identity)
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTextSourcePublicationIsAtomicAndScoped(t *testing.T) {
	runTextSourcePublication(t, summaryRevisionDB(t))
}
func TestPostgresTextSourcePublicationIsAtomicAndScoped(t *testing.T) {
	runTextSourcePublication(t, openPostgresRepositoryTestDB(t).db)
}

func runTextSourcePublication(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 61, 17)
	createSourceTask(t, db, 62, 18)
	snapshot := sourceFixture(t, "先检查连接池配置。", 3000)
	req := PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: snapshot}
	wantErr := errors.New("cannot persist next job")
	_, err := repos.PublishTextSource(ctx, req, func(tx *Repositories, source *model.VideoTextSource) error {
		job := model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, InputSourceID: source.ID, Status: model.TaskStatusQueued}
		if err := tx.db.Create(&job).Error; err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("advance rollback error = %v", err)
	}
	for _, row := range []any{&model.VideoTextSource{}, &model.VideoTextCue{}, &model.VideoTranscription{}, &model.TaskJob{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("publication leaked %T: %d, %v", row, count, err)
		}
	}
	var index model.VideoRAGIndex
	index.TaskID = task.ID
	index.UserID = task.UserID
	index.Status = model.RAGIndexStatusIndexed
	if err := db.Create(&index).Error; err != nil {
		t.Fatal(err)
	}
	published, err := repos.PublishTextSource(ctx, req, func(tx *Repositories, source *model.VideoTextSource) error {
		return tx.db.Create(&model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeSummary, InputSourceID: source.ID, Status: model.TaskStatusQueued}).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := repos.TextSource.Active(ctx, task.UserID, task.ID)
	if err != nil || active == nil || active.ID != published.ID || active.SourceDigest != snapshot.SourceDigest || active.Cues[0].TimingMethod != textsource.TimingSubtitle {
		t.Fatalf("active = %+v,%v", active, err)
	}
	projection, err := repos.Transcription.FindByTaskID(task.ID)
	if err != nil || projection == nil || projection.SourceID != published.ID || projection.Content != snapshot.CanonicalText {
		t.Fatalf("projection = %+v,%v", projection, err)
	}
	if err := db.First(&index, index.ID).Error; err != nil || index.Status != model.RAGIndexStatusNeedsRebuild {
		t.Fatalf("index = %+v,%v", index, err)
	}
	var asrChunks int64
	if err := db.Model(&model.VideoTranscriptionChunk{}).Count(&asrChunks).Error; err != nil || asrChunks != 0 {
		t.Fatalf("subtitle became ASR chunk: %d,%v", asrChunks, err)
	}
	if _, err := repos.TextSource.Read(ctx, 18, task.ID, published.ID); err == nil {
		t.Fatal("source leaked across owner")
	}
	if _, err := repos.TextSource.Read(ctx, 18, 62, published.ID); err == nil {
		t.Fatal("source leaked across task")
	}
	// An acknowledgement lost after commit can safely replay the same snapshot.
	prior, err := repos.PublishTextSource(ctx, req, nil)
	if err != nil || prior.ID != published.ID {
		t.Fatalf("same source replay = %+v,%v", prior, err)
	}
	var count int64
	if err := db.Model(&model.VideoTextSource{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate immutable source: %d,%v", count, err)
	}
	changed := sourceFixture(t, "先检查连接池配置。", 4500)
	req.Snapshot = changed
	if _, err := repos.PublishTextSource(ctx, req, nil); err == nil {
		t.Fatal("stale pointer replaced different source")
	}
	req.ExpectedActiveSourceID = published.ID
	newSource, err := repos.PublishTextSource(ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if newSource.SourceDigest == published.SourceDigest || newSource.ID == published.ID {
		t.Fatal("changed time did not create new identity")
	}
	old, err := repos.TextSource.Read(ctx, task.UserID, task.ID, published.ID)
	if err != nil || *old.Cues[0].EndMS != 3000 {
		t.Fatalf("old immutable source changed: %+v,%v", old, err)
	}
	if err := db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repos.TextSource.Read(ctx, task.UserID, task.ID, published.ID); err == nil {
		t.Fatal("deleted task source readable")
	}
	if _, err := repos.PublishTextSource(ctx, req, nil); err == nil {
		t.Fatal("deleted task source republished")
	}
}

func TestTextSourceRejectsInvalidMediaAndStaleLease(t *testing.T) {
	ctx := context.Background()
	db := summaryRevisionDB(t)
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 71, 7)
	snapshot := sourceFixture(t, "检查参数。", 3000)
	req := PublishTextSourceRequest{UserID: task.UserID, TaskID: task.ID, Snapshot: snapshot}
	bad := req
	bad.Snapshot.Identity.CID = 0
	if _, err := repos.PublishTextSource(ctx, bad, nil); err == nil {
		t.Fatal("unresolved cid published")
	}
	bad = req
	bad.Snapshot.Quality = textsource.QualityUnusable
	if _, err := repos.PublishTextSource(ctx, bad, nil); err == nil {
		t.Fatal("unusable source published")
	}
	bad = req
	bad.Snapshot.Identity.MediaFingerprint = "different"
	if _, err := repos.PublishTextSource(ctx, bad, nil); err == nil {
		t.Fatal("wrong media published")
	}
	now := time.Now()
	expiry := now.Add(time.Minute)
	if err := db.Model(&task).Updates(map[string]any{"processing_token": "new-worker", "lease_kind": model.TaskLeaseKindProcessing, "lease_expires_at": expiry}).Error; err != nil {
		t.Fatal(err)
	}
	job := model.TaskJob{TaskID: task.ID, UserID: task.UserID, JobType: model.TaskJobTypeTranscribe, ProcessingToken: "new-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expiry}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	req.Lease = &TaskProcessingLeaseRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: "old-worker", Now: now}
	if _, err := repos.PublishTextSource(ctx, req, nil); err == nil {
		t.Fatal("stale worker published source")
	}
	req.Lease.Token = "new-worker"
	source, err := repos.PublishTextSource(ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.VideoTextCue{}).Where("source_id = ?", source.ID).Update("text", "corrupted").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repos.TextSource.Active(ctx, task.UserID, task.ID); err == nil {
		t.Fatal("source corruption not detected")
	}
}

func TestTextSourceSchemaMigrationPreservesLegacyRows(t *testing.T) {
	db := summaryRevisionDB(t)
	task := createSourceTask(t, db, 81, 8)
	legacy := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "已有摘要"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&legacy, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.Content != "已有摘要" || legacy.ContentHashKind != model.SummaryHashMarkdown || legacy.DocumentJSON != "" {
		t.Fatalf("legacy migration = %+v", legacy)
	}
}
