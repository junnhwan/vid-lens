package repository

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestTaskCleanupJobClaimRecoversExpiredLeaseAndRejectsStaleOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TaskCleanupJob{}); err != nil {
		t.Fatal(err)
	}
	repo := NewTaskCleanupJobRepository(db)
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	job := &model.TaskCleanupJob{TaskID: 11, UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: model.TaskCleanupStatusPending}
	if err := repo.Create(job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	due, err := repo.FindDue(now, 10)
	if err != nil || len(due) != 1 || due[0].ID != job.ID {
		t.Fatalf("FindDue() = %+v, %v, want job %d", due, err, job.ID)
	}
	claimed, err := repo.Claim(TaskCleanupClaimRequest{JobID: job.ID, Token: "owner-1", Now: now, LeaseUntil: now.Add(time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("first Claim() = %v, %v", claimed, err)
	}
	claimed, err = repo.Claim(TaskCleanupClaimRequest{JobID: job.ID, Token: "owner-2", Now: now.Add(30 * time.Second), LeaseUntil: now.Add(2 * time.Minute)})
	if err != nil || claimed {
		t.Fatalf("unexpired Claim() = %v, %v, want false", claimed, err)
	}
	claimed, err = repo.Claim(TaskCleanupClaimRequest{JobID: job.ID, Token: "owner-2", Now: now.Add(time.Minute), LeaseUntil: now.Add(2 * time.Minute)})
	if err != nil || !claimed {
		t.Fatalf("expired Claim() = %v, %v, want true", claimed, err)
	}

	updated, err := repo.MarkFailed(job.ID, "owner-1", "stale worker", now.Add(3*time.Minute))
	if err != nil || updated {
		t.Fatalf("stale MarkFailed() = %v, %v, want false", updated, err)
	}
	updated, err = repo.MarkCompleted(job.ID, "owner-1", now.Add(90*time.Second))
	if err != nil || updated {
		t.Fatalf("stale MarkCompleted() = %v, %v, want false", updated, err)
	}
	updated, err = repo.MarkFailed(job.ID, "owner-2", "minio unavailable", now.Add(3*time.Minute))
	if err != nil || !updated {
		t.Fatalf("owner MarkFailed() = %v, %v, want true", updated, err)
	}
	if due, err = repo.FindDue(now.Add(2*time.Minute), 10); err != nil || len(due) != 0 {
		t.Fatalf("FindDue() before backoff = %+v, %v", due, err)
	}
	if due, err = repo.FindDue(now.Add(3*time.Minute), 10); err != nil || len(due) != 1 {
		t.Fatalf("FindDue() after backoff = %+v, %v", due, err)
	}
}

func TestTaskCleanupJobCreateIsUniquePerTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TaskCleanupJob{}); err != nil {
		t.Fatal(err)
	}
	repo := NewTaskCleanupJobRepository(db)
	first := &model.TaskCleanupJob{TaskID: 12, UserID: 7, Status: model.TaskCleanupStatusPending}
	if err := repo.Create(first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(&model.TaskCleanupJob{TaskID: 12, UserID: 7, Status: model.TaskCleanupStatusPending}); err == nil {
		t.Fatal("second cleanup intent for the same task should fail")
	}
	found, err := repo.FindByTaskID(12)
	if err != nil || found == nil || found.ID != first.ID {
		t.Fatalf("FindByTaskID() = %+v, %v", found, err)
	}
}

func TestRepositoriesIncludesTaskCleanupJobs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if repos := NewRepositories(db); repos.TaskCleanup == nil {
		t.Fatal("NewRepositories() must wire TaskCleanup")
	}
}
