package repository

import (
	"context"
	"gorm.io/gorm"
	"testing"
	"time"
	"vid-lens/internal/model"
)

func TestSummaryScreenshotRegistersOnlyInspectedOwnedSourceFrames(t *testing.T) {
	runSummaryScreenshotRegistration(t, summaryRevisionDB(t))
}
func TestPostgresSummaryScreenshotRegistersOnlyInspectedOwnedSourceFrames(t *testing.T) {
	runSummaryScreenshotRegistration(t, openPostgresRepositoryTestDB(t).db)
}

func runSummaryScreenshotRegistration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	repos := NewRepositories(db)
	task := createSourceTask(t, db, 501, 17)
	source, err := repos.PublishTextSource(ctx, PublishTextSourceRequest{UserID: 17, TaskID: task.ID, Snapshot: sourceFixture(t, "配置连接池。", 3000)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Minute)
	job := model.TaskJob{TaskID: task.ID, UserID: 17, JobType: model.TaskJobTypeSummary, GenerationID: "generation-image", InputSourceID: source.ID, Status: model.TaskStatusRunning, ProcessingToken: "image-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expiry}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	observation := model.VideoVisualObservation{ID: "observed-frame", UserID: 17, TaskID: task.ID, VideoRevision: task.FileMD5, ObjectKey: "visual-investigations/private.jpg", StartMS: 1500, EndMS: 1501, Status: model.VisualObservationStatusCaptured, RawResponseHash: "observed-hash", CacheKey: "frame-key"}
	if err := db.Create(&observation).Error; err != nil {
		t.Fatal(err)
	}
	req := RegisterSummaryScreenshotRequest{UserID: 17, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken, BlockID: "config", ObservationID: observation.ID, CueIDs: []string{"cue-1"}}
	if _, err := repos.RegisterSummaryScreenshot(ctx, req); err == nil {
		t.Fatal("captured-only frame accepted")
	}
	db.Model(&observation).Update("status", model.VisualObservationStatusObserved)
	for _, bad := range []RegisterSummaryScreenshotRequest{
		{UserID: 18, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken, BlockID: "config", ObservationID: observation.ID, CueIDs: []string{"cue-1"}},
		{UserID: 17, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: "stale-worker", BlockID: "config", ObservationID: observation.ID, CueIDs: []string{"cue-1"}},
		{UserID: 17, TaskID: task.ID, GenerationID: job.GenerationID, SourceID: source.ID, SourceDigest: source.SourceDigest, LeaseToken: job.ProcessingToken, BlockID: "config", ObservationID: observation.ID, CueIDs: []string{"missing-cue"}},
	} {
		if _, err := repos.RegisterSummaryScreenshot(ctx, bad); err == nil {
			t.Fatalf("bad registration accepted=%+v", bad)
		}
	}
	registered, err := repos.RegisterSummaryScreenshot(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repos.RegisterSummaryScreenshot(ctx, req)
	if err != nil || registered.ID != replayed.ID {
		t.Fatalf("registration replay=%+v %v", replayed, err)
	}
	if registered.ObjectKey != observation.ObjectKey || registered.CaptureMS != 1500 || !registered.Inspected {
		t.Fatalf("frame provenance=%+v", registered)
	}
	if _, err := repos.ReadSummaryScreenshot(ctx, 18, task.ID, registered.ID); err == nil {
		t.Fatal("foreign image readable")
	}
	if _, err := repos.ReadSummaryScreenshot(ctx, 17, task.ID, registered.ID); err != nil {
		t.Fatal(err)
	}
	db.Model(&observation).Update("start_ms", 4500)
	req.BlockID = "other-block"
	if _, err := repos.RegisterSummaryScreenshot(ctx, req); err == nil {
		t.Fatal("out-of-source-window frame accepted")
	}
	if err := repos.Task.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.ReadSummaryScreenshot(ctx, 17, task.ID, registered.ID); err == nil {
		t.Fatal("image bypasses deleted task")
	}
}
