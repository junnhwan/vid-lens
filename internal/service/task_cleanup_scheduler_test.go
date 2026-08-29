package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestTaskCleanupSchedulerRunOnceExecutesOnlyDueBatch(t *testing.T) {
	repos := newMediaTestRepositories(t)
	now := time.Date(2026, 7, 17, 23, 0, 0, 0, time.UTC)
	for taskID := int64(1); taskID <= 2; taskID++ {
		if err := repos.TaskCleanup.Create(&model.TaskCleanupJob{
			TaskID: taskID,
			UserID: 7,
			Status: model.TaskCleanupStatusPending,
		}); err != nil {
			t.Fatal(err)
		}
	}
	future := now.Add(time.Hour)
	futureJob := &model.TaskCleanupJob{
		TaskID:      3,
		UserID:      7,
		Status:      model.TaskCleanupStatusFailed,
		NextRetryAt: &future,
	}
	if err := repos.TaskCleanup.Create(futureJob); err != nil {
		t.Fatal(err)
	}
	token := 0
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{
		Now: func() time.Time { return now },
		NewToken: func() string {
			token++
			return fmt.Sprintf("scheduler-%d", token)
		},
	})
	scheduler := NewTaskCleanupScheduler(cleanup, TaskCleanupSchedulerConfig{
		Interval:  time.Minute,
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	for jobID := int64(1); jobID <= 2; jobID++ {
		assertCleanupJobStatus(t, repos, jobID, model.TaskCleanupStatusCompleted)
	}
	assertCleanupJobStatus(t, repos, futureJob.ID, model.TaskCleanupStatusFailed)
}

func TestTaskCleanupSchedulerRecoversExpiredRunningLease(t *testing.T) {
	repos := newMediaTestRepositories(t)
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	job := &model.TaskCleanupJob{
		TaskID:         11,
		UserID:         7,
		Status:         model.TaskCleanupStatusRunning,
		LeaseToken:     "stale-owner",
		LeaseExpiresAt: &expired,
	}
	if err := repos.TaskCleanup.Create(job); err != nil {
		t.Fatal(err)
	}
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{
		Now:      func() time.Time { return now },
		NewToken: func() string { return "new-owner" },
	})
	scheduler := NewTaskCleanupScheduler(cleanup, TaskCleanupSchedulerConfig{
		Interval:  time.Minute,
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	stored, err := repos.TaskCleanup.FindByID(job.ID)
	if err != nil || stored == nil || stored.Status != model.TaskCleanupStatusCompleted || stored.Attempts != 1 {
		t.Fatalf("recovered job = %+v, %v", stored, err)
	}
}

func TestTaskCleanupSchedulerContinuesAfterOneJobFails(t *testing.T) {
	repos := newMediaTestRepositories(t)
	now := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	asset := createMediaTestAsset(t, repos, "cccccccccccccccccccccccccccccccd", "videos/fail-first.mp4")
	failedJob := &model.TaskCleanupJob{TaskID: 21, UserID: 7, AssetID: &asset.ID, ObjectName: asset.ObjectName, FileMD5: asset.FileMD5, Status: model.TaskCleanupStatusPending}
	succeedingJob := &model.TaskCleanupJob{TaskID: 22, UserID: 7, Status: model.TaskCleanupStatusPending}
	if err := repos.TaskCleanup.Create(failedJob); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskCleanup.Create(succeedingJob); err != nil {
		t.Fatal(err)
	}
	storage := &flakyCleanupObjectStorage{err: fmt.Errorf("minio unavailable")}
	cleanup := NewTaskCleanupService(repos, storage, nil, TaskCleanupConfig{
		Now: func() time.Time { return now },
		NewToken: func() string {
			return fmt.Sprintf("batch-%d", now.UnixNano())
		},
	})
	scheduler := NewTaskCleanupScheduler(cleanup, TaskCleanupSchedulerConfig{
		Interval:  time.Minute,
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() should aggregate the failed job error")
	}
	assertCleanupJobStatus(t, repos, failedJob.ID, model.TaskCleanupStatusFailed)
	assertCleanupJobStatus(t, repos, succeedingJob.ID, model.TaskCleanupStatusCompleted)
}

func TestTaskCleanupSchedulerStartStopsOnContextCancellation(t *testing.T) {
	repos := newMediaTestRepositories(t)
	cleanup := NewTaskCleanupService(repos, nil, nil, TaskCleanupConfig{})
	scheduler := NewTaskCleanupScheduler(cleanup, TaskCleanupSchedulerConfig{
		Interval:  time.Hour,
		BatchSize: 10,
	})
	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)
	cancel()

	done := make(chan struct{})
	go func() {
		scheduler.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait() did not return after context cancellation")
	}
}
