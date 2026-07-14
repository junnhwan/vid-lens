package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestClaimTaskProcessingReclaimsExpiredRunningLeaseAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	task := &model.VideoTask{
		UserID: 1, FileMD5: "10101010101010101010101010101010", Filename: "expired.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		ProcessingToken: "old-token", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expired, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatalf("create job: %v", err)
	}

	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(10 * time.Minute), NewToken: "new-token",
	})
	if err != nil {
		t.Fatalf("claim processing: %v", err)
	}
	if claim.Outcome != TaskLeaseAcquired || claim.Token != "new-token" {
		t.Fatalf("claim = %+v, want acquired new-token", claim)
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.ProcessingToken != "new-token" || current.LeaseVersion != 2 || current.Status != model.TaskStatusRunning {
		t.Fatalf("task lease = %+v", current)
	}
	if job == nil || job.ProcessingToken != "new-token" || job.LeaseVersion != 2 || job.Status != model.TaskStatusRunning {
		t.Fatalf("job lease = %+v", job)
	}
}

func TestClaimTaskProcessingDoesNotStealActiveLease(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	activeUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 1, FileMD5: "20202020202020202020202020202020", Filename: "active.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex,
		ProcessingToken: "active-token", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &activeUntil, LeaseVersion: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, Stage: model.TaskStageIndexing,
		Now: now, LeaseUntil: now.Add(10 * time.Minute), NewToken: "steal-token",
	})
	if err != nil {
		t.Fatalf("claim processing: %v", err)
	}
	if claim.Outcome != TaskLeaseBusy {
		t.Fatalf("claim outcome = %s, want busy", claim.Outcome)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.ProcessingToken != "active-token" || current.LeaseVersion != 3 {
		t.Fatalf("active lease changed: %+v", current)
	}
}

func TestClaimRetryDispatchUsesVersionCASAndUpdatesTaskAndJob(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID: 2, FileMD5: "30303030303030303030303030303030", Filename: "retry.mp4",
		Status: model.TaskStatusFailed, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		NextRetryAt: &due, RetryCount: 1, MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("create job: %v", err)
	}

	claimed, err := repos.ClaimRetryDispatch(TaskDispatchClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: "dispatch-1",
	})
	if err != nil || !claimed {
		t.Fatalf("first dispatch claim = %v/%v", claimed, err)
	}
	claimed, err = repos.ClaimRetryDispatch(TaskDispatchClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: "dispatch-2",
	})
	if err != nil {
		t.Fatalf("second dispatch claim: %v", err)
	}
	if claimed {
		t.Fatal("stale version claimed task twice")
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.ProcessingToken != "dispatch-1" || current.LeaseKind != model.TaskLeaseKindDispatch || current.LeaseVersion != 1 || current.NextRetryAt != nil {
		t.Fatalf("task dispatch state = %+v", current)
	}
	if job == nil || job.ProcessingToken != "dispatch-1" || job.LeaseKind != model.TaskLeaseKindDispatch || job.LeaseVersion != 1 {
		t.Fatalf("job dispatch state = %+v", job)
	}
}

func TestFindDueRetryTasksIncludesExpiredLease(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	active := now.Add(time.Minute)
	expiredTask := &model.VideoTask{UserID: 3, FileMD5: "40404040404040404040404040404040", Filename: "expired.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, ProcessingToken: "expired", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &expired, LeaseVersion: 2, MaxRetries: 3}
	activeTask := &model.VideoTask{UserID: 3, FileMD5: "50505050505050505050505050505050", Filename: "active.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, ProcessingToken: "active", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &active, LeaseVersion: 2, MaxRetries: 3}
	if err := repos.Task.Create(expiredTask); err != nil {
		t.Fatal(err)
	}
	if err := repos.Task.Create(activeTask); err != nil {
		t.Fatal(err)
	}

	tasks, err := repos.Task.FindDueRetryTasks(now, 10)
	if err != nil {
		t.Fatalf("find due: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != expiredTask.ID {
		t.Fatalf("due tasks = %+v, want only expired lease", tasks)
	}
}

func TestRestoreRetryDispatchRequiresTokenAndUpdatesTaskAndJobAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	task := &model.VideoTask{UserID: 4, FileMD5: "90909090909090909090909090909090", Filename: "restore.mp4", Status: model.TaskStatusFailed, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe, NextRetryAt: &due, RetryCount: 1, MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.ClaimRetryDispatch(TaskDispatchClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: "dispatch-token"})
	if err != nil || !claimed {
		t.Fatalf("claim = %v/%v", claimed, err)
	}
	next := now.Add(2 * time.Minute)

	restored, err := repos.RestoreRetryDispatch(TaskDispatchRestoreRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Token: "stale-token", ErrorMessage: "kafka unavailable", NextRetryAt: next})
	if err != nil {
		t.Fatalf("stale restore: %v", err)
	}
	if restored {
		t.Fatal("stale token restored current dispatch")
	}
	restored, err = repos.RestoreRetryDispatch(TaskDispatchRestoreRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Token: "dispatch-token", ErrorMessage: "kafka unavailable", NextRetryAt: next})
	if err != nil || !restored {
		t.Fatalf("restore = %v/%v", restored, err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || !current.NextRetryAt.Equal(next) || current.ProcessingToken != "" || current.LeaseExpiresAt != nil || current.LeaseVersion != 2 {
		t.Fatalf("task restored = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || !job.NextRetryAt.Equal(next) || job.ProcessingToken != "" || job.LeaseExpiresAt != nil || job.LeaseVersion != current.LeaseVersion {
		t.Fatalf("job restored = %+v", job)
	}
}

func TestCompleteTaskProcessingUpdatesTaskAndJobAtomicallyAndRejectsStaleOwner(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Hour)
	task := &model.VideoTask{UserID: 5, FileMD5: "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1", Filename: "complete.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, ProcessingToken: "worker-new", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}

	completed, err := repos.CompleteTaskProcessing(TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, JobStage: model.TaskStageIndexing, Token: "worker-old", TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: now})
	if err != nil {
		t.Fatalf("stale complete: %v", err)
	}
	if completed {
		t.Fatal("stale worker completed current lease")
	}
	completed, err = repos.CompleteTaskProcessing(TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, JobStage: model.TaskStageIndexing, Token: "worker-new", TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: now})
	if err != nil || !completed {
		t.Fatalf("complete = %v/%v", completed, err)
	}

	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeRAGIndex)
	if current.Status != model.TaskStatusCompleted || current.Stage != model.TaskStageNone || current.ProcessingToken != "" || current.LeaseKind != "" || current.LeaseExpiresAt != nil || current.LeaseVersion != 4 || current.FinishedAt == nil {
		t.Fatalf("task = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusCompleted || job.ProcessingToken != "" || job.LeaseKind != "" || job.LeaseExpiresAt != nil || job.LeaseVersion != 4 || job.FinishedAt == nil {
		t.Fatalf("job = %+v", job)
	}
}

func TestFailTaskProcessingUpdatesTaskAndJobAtomicallyAndRejectsStaleOwner(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Hour)
	next := now.Add(time.Minute)
	task := &model.VideoTask{UserID: 6, FileMD5: "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1", Filename: "failure.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe, ProcessingToken: "worker-new", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &leaseUntil, LeaseVersion: 2, MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusRunning, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}
	req := TaskProcessingFailureRequest{TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing, Token: "worker-old", Status: model.TaskStatusFailed, ErrorCode: "retryable_error", ErrorMessage: "timeout", RetryCount: 1, MaxRetries: 3, NextRetryAt: &next, Now: now}
	failed, err := repos.FailTaskProcessing(req)
	if err != nil || failed {
		t.Fatalf("stale fail = %v/%v", failed, err)
	}
	req.Token = "worker-new"
	failed, err = repos.FailTaskProcessing(req)
	if err != nil || !failed {
		t.Fatalf("fail = %v/%v", failed, err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || !current.NextRetryAt.Equal(next) || current.ProcessingToken != "" || current.LeaseVersion != 3 {
		t.Fatalf("task = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || !job.NextRetryAt.Equal(next) || job.ProcessingToken != "" || job.LeaseVersion != 3 {
		t.Fatalf("job = %+v", job)
	}
}
