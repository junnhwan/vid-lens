package repository

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestSummaryEditOutboxLeaseFencesStaleCommitAndRecovers(t *testing.T) {
	runSummaryEditDispatchTest(t, summaryRevisionDB(t))
}

func TestPostgresSummaryEditOutboxLeaseFencesStaleCommitAndRecovers(t *testing.T) {
	runSummaryEditDispatchTest(t, openPostgresRepositoryTestDB(t).db)
}

func runSummaryEditDispatchTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	task := model.VideoTask{ID: 61, UserID: 17, FileMD5: "88888888888888888888888888888888", Filename: "edit.mp4", Status: model.TaskStatusCompleted}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	base := "旧名称。"
	if err := db.Create(&model.AISummary{TaskID: 61, FileMD5: task.FileMD5, Content: base}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewSummaryRevisionRepository(db)
	op, run := summaryEditFixture(17, 61, base)
	if _, err := repo.Begin(ctx, op, run); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Dispatches(ctx)
	if err != nil || len(rows) != 1 || rows[0].RunID != run.ID {
		t.Fatalf("durable dispatch = %+v, %v", rows, err)
	}
	if err = repo.DispatchResult(ctx, rows[0].SummaryEditDispatch, true); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := repo.ClaimRun(ctx, run.ID, "worker-one", time.Minute); err != nil || !claimed {
		t.Fatalf("first claim=%v, %v", claimed, err)
	}
	if _, claimed, err := repo.ClaimRun(ctx, run.ID, "worker-two", time.Minute); err != nil || claimed {
		t.Fatalf("second active claim=%v, %v", claimed, err)
	}
	old := time.Now().UTC().Add(-2 * time.Minute)
	if err = db.Model(&model.AgentRun{}).Where("id = ?", run.ID).Update("run_lease_until", old).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.SummaryEditDispatch{}).Where("run_id = ?", run.ID).Updates(map[string]any{"created_at": old, "published_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.Dispatches(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID == "" {
		t.Fatalf("recovery dispatch = %+v, %v", rows, err)
	}
	if _, claimed, err := repo.ClaimRun(ctx, run.ID, "worker-two", time.Minute); err != nil || !claimed {
		t.Fatalf("replacement claim=%v, %v", claimed, err)
	}
	if _, err = repo.Commit(ctx, op.ID, "新名称。", `{}`, "worker-one"); err == nil {
		t.Fatal("stale worker committed")
	}
	if committed, err := repo.Commit(ctx, op.ID, "新名称。", `{}`, "worker-two"); err != nil || committed.Version != 1 {
		t.Fatalf("replacement commit=%+v, %v", committed, err)
	}
	if err = repo.ReleaseRun(ctx, run.ID, "worker-two"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := repo.ClaimRun(ctx, run.ID, "worker-three", time.Minute); err != nil || claimed {
		t.Fatalf("completed run reclaimed=%v, %v", claimed, err)
	}
}
