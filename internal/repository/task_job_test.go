package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestTaskJobRepositoryTracksLifecycleAndRetryMetadata(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "abababababababababababababababab",
		Filename:   "video.mp4",
		FileURL:    "videos/video.mp4",
		Status:     model.TaskStatusPending,
		Stage:      model.TaskStageUploaded,
		TraceID:    "trace-job",
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := repos.TaskJob.UpsertQueued(task, "transcribe", model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("upsert queued job: %v", err)
	}

	job, err := repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find queued job: %v", err)
	}
	if job == nil {
		t.Fatal("expected queued task job")
	}
	if job.UserID != task.UserID || job.TraceID != task.TraceID || job.Status != model.TaskStatusQueued || job.Stage != model.TaskStageTranscribing {
		t.Fatalf("queued job = %+v", job)
	}

	if err := repos.TaskJob.MarkRunning(task.ID, "transcribe", model.TaskStageTranscribing); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	job, err = repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find running job: %v", err)
	}
	if job.Status != model.TaskStatusRunning || job.StartedAt == nil {
		t.Fatalf("running job = %+v, want running with started_at", job)
	}

	nextRetryAt := time.Date(2026, 6, 6, 12, 1, 0, 0, time.UTC)
	if err := repos.TaskJob.RecordRetryableFailure(task.ID, "transcribe", model.TaskStageTranscribing, "network timeout", 2, 3, nextRetryAt); err != nil {
		t.Fatalf("record retryable failure: %v", err)
	}
	job, err = repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find failed job: %v", err)
	}
	if job.Status != model.TaskStatusFailed || job.RetryCount != 2 || job.MaxRetries != 3 || job.NextRetryAt == nil || !job.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("failed job retry metadata = %+v", job)
	}
	if job.LastErrorCode != "retryable_error" || job.LastErrorMsg != "network timeout" {
		t.Fatalf("failed job error fields = %q/%q", job.LastErrorCode, job.LastErrorMsg)
	}

	if err := repos.TaskJob.UpsertQueued(task, "transcribe", model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("requeue job: %v", err)
	}
	job, err = repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find requeued job: %v", err)
	}
	if job.Status != model.TaskStatusQueued || job.NextRetryAt != nil || job.LastErrorMsg != "" {
		t.Fatalf("requeued job = %+v, want queued with cleared retry fields", job)
	}

	if err := repos.TaskJob.MarkCompleted(task.ID, "transcribe", model.TaskStageTranscribing); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	job, err = repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find completed job: %v", err)
	}
	if job.Status != model.TaskStatusCompleted || job.FinishedAt == nil {
		t.Fatalf("completed job = %+v, want completed with finished_at", job)
	}
}

func TestTaskJobRepositoryUpsertQueuedResetsRetryCountOnInsert(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{
		UserID:     7,
		FileMD5:    "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		Filename:   "failed-video.mp4",
		Status:     model.TaskStatusFailed,
		Stage:      model.TaskStageTranscribing,
		TraceID:    "trace-requeue",
		RetryCount: 2,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := repos.TaskJob.UpsertQueued(task, "transcribe", model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("upsert queued job: %v", err)
	}

	job, err := repos.TaskJob.FindByTaskAndType(task.ID, "transcribe")
	if err != nil {
		t.Fatalf("find queued job: %v", err)
	}
	if job == nil {
		t.Fatal("expected queued job")
	}
	if job.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want reset to 0", job.RetryCount)
	}
}
