package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestRenewTaskProcessingExtendsTaskAndJobLeaseAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	oldExpiry := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 10, FileMD5: "c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1", Filename: "heartbeat.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		ProcessingToken: "worker-1", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &oldExpiry, LeaseVersion: 2,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}

	newExpiry := now.Add(10 * time.Minute)
	renewed, err := repos.RenewTaskProcessing(TaskProcessingLeaseRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: "worker-1", Now: now, LeaseUntil: newExpiry,
	})
	if err != nil || !renewed {
		t.Fatalf("renew = %v/%v", renewed, err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.Equal(newExpiry) {
		t.Fatalf("task expiry = %v", current.LeaseExpiresAt)
	}
	if job == nil || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.Equal(newExpiry) {
		t.Fatalf("job expiry = %+v", job)
	}

	stale, err := repos.RenewTaskProcessing(TaskProcessingLeaseRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Token: "worker-old", Now: now, LeaseUntil: now.Add(time.Hour),
	})
	if err != nil || stale {
		t.Fatalf("stale renew = %v/%v", stale, err)
	}
}

func TestTaskProcessingOwnershipExpiresEvenBeforeAnotherWorkerClaims(t *testing.T) {
	repos := newTestRepositories(t)
	expiry := time.Date(2026, 7, 14, 10, 1, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 11, FileMD5: "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1", Filename: "expired-owner.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing, LastJobType: model.TaskJobTypeAnalyze,
		ProcessingToken: "old-worker", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expiry, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeAnalyze, model.TaskStatusRunning, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}

	owned, err := repos.OwnsTaskProcessing(TaskProcessingLeaseRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeAnalyze, Token: "old-worker", Now: expiry.Add(time.Nanosecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatal("expired lease must not authorize a business side effect")
	}

	completed, err := repos.CompleteTaskProcessing(TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeAnalyze, JobStage: model.TaskStageSummarizing,
		Token: "old-worker", TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: expiry.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("expired owner completed task")
	}
}

func TestFailTaskProcessingHandoffPersistsRAGRetryAndCompletesCurrentJobAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	nextRetryAt := now.Add(30 * time.Second)
	task := &model.VideoTask{
		UserID: 12, FileMD5: "e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1", Filename: "rag-handoff.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		RetryCount: 3, MaxRetries: 3, ProcessingToken: "transcribe-worker", LeaseKind: model.TaskLeaseKindProcessing,
		LeaseExpiresAt: &leaseUntil, LeaseVersion: 4,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}

	updated, err := repos.FailTaskProcessingHandoff(TaskProcessingHandoffFailureRequest{
		TaskID: task.ID, CurrentJobType: model.TaskJobTypeTranscribe, CurrentStage: model.TaskStageTranscribing,
		NextJobType: model.TaskJobTypeRAGIndex, NextStage: model.TaskStageIndexing, Token: "transcribe-worker",
		Status: model.TaskStatusFailed, ErrorCode: "enqueue_failed", ErrorMessage: "kafka unavailable",
		RetryCount: 1, MaxRetries: 3, NextRetryAt: &nextRetryAt, Now: now,
	})
	if err != nil || !updated {
		t.Fatalf("handoff = %v/%v", updated, err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	transcribeJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	ragJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if current.Status != model.TaskStatusFailed || current.LastJobType != model.TaskJobTypeRAGIndex || current.RetryCount != 1 || current.NextRetryAt == nil || !current.NextRetryAt.Equal(nextRetryAt) || current.ProcessingToken != "" {
		t.Fatalf("parent = %+v", current)
	}
	if transcribeJob == nil || transcribeJob.Status != model.TaskStatusCompleted || transcribeJob.RetryCount != 0 || transcribeJob.ProcessingToken != "" {
		t.Fatalf("current job = %+v", transcribeJob)
	}
	if ragJob == nil || ragJob.Status != model.TaskStatusFailed || ragJob.RetryCount != 1 || ragJob.MaxRetries != 3 || ragJob.NextRetryAt == nil || !ragJob.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("rag job = %+v", ragJob)
	}
}

func TestNewTaskJobDoesNotInheritParentRetryCount(t *testing.T) {
	repos := newTestRepositories(t)
	task := &model.VideoTask{
		UserID: 13, FileMD5: "f2f2f2f2f2f2f2f2f2f2f2f2f2f2f2f2", Filename: "stage-budget.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, RetryCount: 3, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.RetryCount != 0 {
		t.Fatalf("new stage retry_count = %+v, want 0", job)
	}
}
