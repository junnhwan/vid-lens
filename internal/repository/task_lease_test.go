package repository

import (
	"testing"
	"time"

	"vid-lens/internal/model"
)

func TestPrepareInitialTaskDispatchPersistsRecoverableLeaseAndBudgetAtomically(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "01010101010101010101010101010101", Filename: "initial.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-initial", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	prepared, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "initial-dispatch-token",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}
	if prepared.RetryBudgetID == "" || prepared.Token != "initial-dispatch-token" {
		t.Fatalf("prepared dispatch = %+v", prepared)
	}

	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find task job: %v", err)
	}
	if current.Status != model.TaskStatusQueued || current.Stage != model.TaskStageTranscribing || current.LastJobType != model.TaskJobTypeTranscribe || current.ProcessingToken != prepared.Token || current.LeaseKind != model.TaskLeaseKindDispatch || current.LeaseExpiresAt == nil || current.LeaseVersion != 1 {
		t.Fatalf("prepared task = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusQueued || job.ProcessingToken != prepared.Token || job.LeaseKind != model.TaskLeaseKindDispatch || job.LeaseExpiresAt == nil || job.LeaseVersion != current.LeaseVersion || job.RetryBudgetID != prepared.RetryBudgetID {
		t.Fatalf("prepared task job = %+v", job)
	}
	if _, err := repos.RetryBudget.Get(prepared.RetryBudgetID); err != nil {
		t.Fatalf("load retry budget: %v", err)
	}

	due, err := repos.Task.FindDueRetryTasks(now.Add(2*time.Minute+time.Millisecond), 10)
	if err != nil {
		t.Fatalf("find expired initial dispatch: %v", err)
	}
	if len(due) != 1 || due[0].ID != task.ID {
		t.Fatalf("expired initial dispatches = %+v, want task %d", due, task.ID)
	}
}

func TestPreparedInitialDispatchHandsOffOnlyMatchingToken(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 17, 9, 15, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "03030303030303030303030303030303", Filename: "handoff.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-handoff", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	prepared, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "handoff-dispatch-token",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}

	stale, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		MessageToken: "wrong-token", Now: now.Add(time.Second), LeaseUntil: now.Add(time.Hour), NewToken: "worker-stale",
	})
	if err != nil || stale.Outcome != TaskLeaseStale {
		t.Fatalf("stale handoff = %+v/%v", stale, err)
	}
	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		MessageToken: prepared.Token, Now: now.Add(time.Second), LeaseUntil: now.Add(time.Hour), NewToken: "worker-current",
	})
	if err != nil || claim.Outcome != TaskLeaseAcquired {
		t.Fatalf("current handoff = %+v/%v", claim, err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if current.ProcessingToken != "worker-current" || current.LeaseKind != model.TaskLeaseKindProcessing || current.LeaseVersion != 2 {
		t.Fatalf("processing task = %+v", current)
	}
	if job == nil || job.ProcessingToken != "worker-current" || job.LeaseKind != model.TaskLeaseKindProcessing || job.LeaseVersion != current.LeaseVersion {
		t.Fatalf("processing job = %+v", job)
	}
}

func TestPrepareInitialTaskDispatchRollsBackWhenBudgetPersistenceFails(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 17, 9, 30, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 7, FileMD5: "02020202020202020202020202020202", Filename: "rollback.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-rollback", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.db.Migrator().DropTable(&model.AIRetryBudget{}); err != nil {
		t.Fatalf("drop retry budget table: %v", err)
	}

	_, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "rollback-token",
	})
	if err == nil {
		t.Fatal("prepare initial dispatch error = nil, want budget persistence failure")
	}

	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task after rollback: %v", findErr)
	}
	if current.Status != model.TaskStatusPending || current.Stage != model.TaskStageUploaded || current.ProcessingToken != "" || current.LastJobType != "" || current.LeaseVersion != 0 {
		t.Fatalf("task changed despite rollback: %+v", current)
	}
	job, findErr := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if findErr != nil {
		t.Fatalf("find task job after rollback: %v", findErr)
	}
	if job != nil {
		t.Fatalf("task job survived rollback: %+v", job)
	}
}

func TestPrepareInitialTaskDispatchRollsBackNewTaskWhenBudgetPersistenceFails(t *testing.T) {
	repos := newTestRepositories(t)
	now := time.Date(2026, 7, 17, 9, 45, 0, 0, time.UTC)
	if err := repos.db.Migrator().DropTable(&model.AIRetryBudget{}); err != nil {
		t.Fatalf("drop retry budget table: %v", err)
	}
	task := &model.VideoTask{
		UserID: 7, FileMD5: "04040404040404040404040404040404", Filename: "new-rollback.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageDownloading, TraceID: "trace-new-rollback", MaxRetries: 3,
	}

	_, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, CreateTask: true,
		JobType: model.TaskJobTypeDownload, Stage: model.TaskStageDownloading,
		Now: now, LeaseUntil: now.Add(2 * time.Minute), Token: "new-rollback-token",
	})
	if err == nil {
		t.Fatal("prepare new initial dispatch error = nil, want budget persistence failure")
	}
	if task.ID != 0 {
		t.Fatalf("caller task id = %d after rolled-back create, want 0", task.ID)
	}
	var taskCount, jobCount int64
	if err := repos.db.Model(&model.VideoTask{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := repos.db.Model(&model.TaskJob{}).Count(&jobCount).Error; err != nil {
		t.Fatalf("count task jobs: %v", err)
	}
	if taskCount != 0 || jobCount != 0 {
		t.Fatalf("rolled-back rows = tasks:%d jobs:%d", taskCount, jobCount)
	}
}

func TestClaimTaskProcessingSetsTaskStartedAtOnWorkerHandoff(t *testing.T) {
	repos := newTestRepositories(t)
	queuedAt := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	processingAt := queuedAt.Add(time.Second)
	task := &model.VideoTask{
		UserID: 8, FileMD5: "05050505050505050505050505050505", Filename: "start-time.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-start-time", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	prepared, err := repos.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: queuedAt, LeaseUntil: queuedAt.Add(2 * time.Minute), Token: "start-time-dispatch",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}
	if task.StartedAt != nil {
		t.Fatalf("queued task started_at = %v, want nil before worker handoff", task.StartedAt)
	}

	claim, err := repos.ClaimTaskProcessing(TaskProcessingClaimRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		MessageToken: prepared.Token, Now: processingAt, LeaseUntil: processingAt.Add(time.Hour), NewToken: "start-time-worker",
	})
	if err != nil || claim.Outcome != TaskLeaseAcquired {
		t.Fatalf("claim processing = %+v/%v", claim, err)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find processing task: %v", err)
	}
	if current.StartedAt == nil || !current.StartedAt.Equal(processingAt) {
		t.Fatalf("processing task started_at = %v, want %v", current.StartedAt, processingAt)
	}
}

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
