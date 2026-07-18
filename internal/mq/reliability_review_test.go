package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"

	"github.com/segmentio/kafka-go"
)

type leaseCapturingRetryProducer struct {
	tokens  []string
	budgets []string
	tasks   []int64
	err     error
}

func (p *leaseCapturingRetryProducer) capture(ctx context.Context, taskID int64) error {
	p.tokens = append(p.tokens, claimTokenFromContext(ctx))
	p.budgets = append(p.budgets, retryBudgetIDFromContext(ctx))
	p.tasks = append(p.tasks, taskID)
	return p.err
}
func (p *leaseCapturingRetryProducer) EnqueueAnalyze(ctx context.Context, taskID int64, _ string) error {
	return p.capture(ctx, taskID)
}
func (p *leaseCapturingRetryProducer) EnqueueTranscribe(ctx context.Context, taskID int64, _ string) error {
	return p.capture(ctx, taskID)
}
func (p *leaseCapturingRetryProducer) EnqueueDownload(ctx context.Context, taskID int64, _ string) error {
	return p.capture(ctx, taskID)
}
func (p *leaseCapturingRetryProducer) EnqueueRAGIndex(ctx context.Context, taskID int64) error {
	return p.capture(ctx, taskID)
}

func TestRetrySchedulerPersistsMatchingDispatchLeaseBeforeEnqueue(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)
	task := createDueRetryTask(t, repos, now, "60606060606060606060606060606060")
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return now }, DispatchLease: 2 * time.Minute,
		NewToken: func() string { return "dispatch-token" },
	})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(producer.tokens) != 1 || producer.tokens[0] != "dispatch-token" {
		t.Fatalf("producer tokens = %#v, want dispatch-token", producer.tokens)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	wantExpiry := now.Add(2 * time.Minute)
	if current.Status != model.TaskStatusQueued || current.ProcessingToken != "dispatch-token" || current.LeaseKind != model.TaskLeaseKindDispatch || current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.Equal(wantExpiry) || current.LeaseVersion != 1 {
		t.Fatalf("task dispatch lease = %+v", current)
	}
	if job == nil || job.ProcessingToken != "dispatch-token" || job.LeaseKind != model.TaskLeaseKindDispatch || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.Equal(wantExpiry) || job.LeaseVersion != current.LeaseVersion {
		t.Fatalf("job dispatch lease = %+v", job)
	}
}

func TestInitialDispatchLeaseExpiryFlowsThroughSchedulerToConsumerHandoff(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	preparedAt := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	initialLeaseUntil := preparedAt.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 26, FileMD5: "12121212121212121212121212121212", Filename: "initial-recovery.mp4",
		Status: model.TaskStatusPending, Stage: model.TaskStageUploaded, TraceID: "trace-initial-recovery", MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	prepared, err := repos.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
		Task: task, AllowedStatuses: []int8{model.TaskStatusPending},
		JobType: model.TaskJobTypeTranscribe, Stage: model.TaskStageTranscribing,
		Now: preparedAt, LeaseUntil: initialLeaseUntil, Token: "abandoned-initial-token",
	})
	if err != nil {
		t.Fatalf("prepare initial dispatch: %v", err)
	}

	schedulerNow := initialLeaseUntil.Add(time.Second)
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return schedulerNow }, DispatchLease: 2 * time.Minute,
		NewToken: func() string { return "scheduler-dispatch-token" },
	})
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("scheduler RunOnce: %v", err)
	}
	if len(producer.tasks) != 1 || producer.tasks[0] != task.ID || len(producer.tokens) != 1 || producer.tokens[0] != "scheduler-dispatch-token" {
		t.Fatalf("scheduler publish correlation = tasks:%v tokens:%v", producer.tasks, producer.tokens)
	}
	if len(producer.budgets) != 1 || producer.budgets[0] != prepared.RetryBudgetID {
		t.Fatalf("scheduler retry budget = %v, want %q", producer.budgets, prepared.RetryBudgetID)
	}

	consumer := &Consumer{
		repo: repos, processingLease: time.Hour,
		now:      func() time.Time { return schedulerNow.Add(time.Second) },
		newToken: func() string { return "consumer-processing-token" },
	}
	claim, err := consumer.claimTaskForMessage(task.ID, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, producer.tokens[0])
	if err != nil {
		t.Fatalf("consumer handoff: %v", err)
	}
	if claim.Outcome != repository.TaskLeaseAcquired || claim.Token != "consumer-processing-token" {
		t.Fatalf("consumer claim = %+v", claim)
	}
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatalf("find processing task: %v", err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	if err != nil {
		t.Fatalf("find processing job: %v", err)
	}
	if current.Status != model.TaskStatusRunning || current.LeaseKind != model.TaskLeaseKindProcessing || current.ProcessingToken != claim.Token {
		t.Fatalf("processing task = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusRunning || job.LeaseKind != model.TaskLeaseKindProcessing || job.ProcessingToken != claim.Token || job.RetryBudgetID != prepared.RetryBudgetID {
		t.Fatalf("processing job = %+v", job)
	}
}
func TestRetrySchedulerClaimRollsBackWhenTaskJobLeaseWriteFails(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	now := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	task := createDueRetryTask(t, repos, now, "70707070707070707070707070707070")
	if err := repos.TaskJob.UpsertQueued(task, model.TaskJobTypeTranscribe, model.TaskStageTranscribing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}
	if err := db.Exec("CREATE TRIGGER fail_job_dispatch_lease BEFORE UPDATE OF processing_token ON task_jobs BEGIN SELECT RAISE(ABORT, 'job lease failed'); END").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return now }, NewToken: func() string { return "dispatch-token" },
	})

	err := scheduler.RunOnce(context.Background())
	if err == nil || !containsAll(err.Error(), "job lease failed") {
		t.Fatalf("RunOnce error = %v, want job lease failure", err)
	}
	if len(producer.tasks) != 0 {
		t.Fatalf("producer calls = %#v, want none", producer.tasks)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || current.ProcessingToken != "" || current.LeaseVersion != 0 {
		t.Fatalf("task claim was not rolled back: %+v", current)
	}
}

func TestRetrySchedulerRecoversExpiredDispatchLeaseAfterCrash(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID: 8, FileMD5: "80808080808080808080808080808080", Filename: "dispatch-crash.mp4",
		Status: model.TaskStatusQueued, Stage: model.TaskStageTranscribing, LastJobType: model.TaskJobTypeTranscribe,
		RetryCount: 1, MaxRetries: 3, ProcessingToken: "abandoned", LeaseKind: model.TaskLeaseKindDispatch,
		LeaseExpiresAt: &expired, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeTranscribe, model.TaskStatusQueued, model.TaskStageTranscribing); err != nil {
		t.Fatal(err)
	}
	producer := &leaseCapturingRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return now }, NewToken: func() string { return "recovered" },
	})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(producer.tokens) != 1 || producer.tokens[0] != "recovered" {
		t.Fatalf("tokens = %#v", producer.tokens)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.ProcessingToken != "recovered" || current.LeaseVersion != 2 || current.Status != model.TaskStatusQueued {
		t.Fatalf("recovered task = %+v", current)
	}
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}

var _ retryProducer = (*leaseCapturingRetryProducer)(nil)

func TestRetrySchedulerProducerFailureRestoresDispatchLeaseTransactionally(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 7, 30, 0, 0, time.UTC)
	task := createDueRetryTask(t, repos, now, "91919191919191919191919191919191")
	producer := &leaseCapturingRetryProducer{err: fmt.Errorf("kafka unavailable")}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10, Now: func() time.Time { return now },
		NewToken: func() string { return "dispatch-restore-token" },
	})

	err := scheduler.RunOnce(context.Background())
	if err == nil || !containsAll(err.Error(), "kafka unavailable") {
		t.Fatalf("RunOnce error = %v, want producer failure", err)
	}
	current, _ := repos.Task.FindByID(task.ID)
	job, _ := repos.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeTranscribe)
	wantRetry := now.Add(time.Minute)
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || !current.NextRetryAt.Equal(wantRetry) || current.ProcessingToken != "" || current.LeaseKind != "" || current.LeaseExpiresAt != nil || current.LeaseVersion != 2 {
		t.Fatalf("task restore state = %+v", current)
	}
	if job == nil || job.Status != model.TaskStatusFailed || job.NextRetryAt == nil || !job.NextRetryAt.Equal(wantRetry) || job.ProcessingToken != "" || job.LeaseKind != "" || job.LeaseExpiresAt != nil || job.LeaseVersion != current.LeaseVersion {
		t.Fatalf("job restore state = %+v", job)
	}
}

func TestConsumerProcessingLeaseKeepsActiveOwnerAndReclaimsExpiredOwner(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	activeUntil := now.Add(time.Minute)
	task := &model.VideoTask{UserID: 8, FileMD5: "c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1", Filename: "lease.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, ProcessingToken: "active", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &activeUntil, LeaseVersion: 1}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}
	consumer := &Consumer{repo: repos, processingLease: time.Hour, now: func() time.Time { return now }, newToken: func() string { return "replacement" }}

	claim, err := consumer.claimTaskForMessage(task.ID, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, "")
	if err != nil || claim.Outcome != repository.TaskLeaseBusy {
		t.Fatalf("active claim = %+v/%v", claim, err)
	}
	now = activeUntil.Add(time.Second)
	claim, err = consumer.claimTaskForMessage(task.ID, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, "")
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired || claim.Token != "replacement" {
		t.Fatalf("expired claim = %+v/%v", claim, err)
	}
}

func TestConsumerProcessingLeaseRejectsOldMessageAfterSchedulerClaimAndAcceptsMatchingToken(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	task := &model.VideoTask{UserID: 8, FileMD5: "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1", Filename: "dispatch.mp4", Status: model.TaskStatusFailed, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, NextRetryAt: &due, MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, Stage: model.TaskStageIndexing, ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: "dispatch-current"})
	if err != nil || !claimed {
		t.Fatalf("dispatch claim = %v/%v", claimed, err)
	}
	consumer := &Consumer{repo: repos, processingLease: time.Hour, now: func() time.Time { return now }, newToken: func() string { return "worker" }}

	oldClaim, err := consumer.claimTaskForMessage(task.ID, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, "")
	if err != nil || oldClaim.Outcome != repository.TaskLeaseStale {
		t.Fatalf("old message = %+v/%v", oldClaim, err)
	}
	newClaim, err := consumer.claimTaskForMessage(task.ID, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, "dispatch-current")
	if err != nil || newClaim.Outcome != repository.TaskLeaseAcquired || newClaim.Token != "worker" {
		t.Fatalf("new message = %+v/%v", newClaim, err)
	}
	lateClaim, err := consumer.claimTaskForMessage(task.ID, model.TaskJobTypeRAGIndex, model.TaskStageIndexing, "dispatch-current")
	if err != nil || lateClaim.Outcome != repository.TaskLeaseStale {
		t.Fatalf("late old token = %+v/%v", lateClaim, err)
	}
}

func TestHandleRAGIndexDoesNotExecuteActiveLeaseAndReclaimsExpiredLease(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	activeUntil := now.Add(time.Minute)
	task := &model.VideoTask{UserID: 9, FileMD5: "e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1", Filename: "rag.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, ProcessingToken: "active", LeaseKind: model.TaskLeaseKindProcessing, LeaseExpiresAt: &activeUntil, LeaseVersion: 1}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, model.TaskJobTypeRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "lease recovery evidence", Words: 3}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	consumer := &Consumer{repo: repos, processingLease: time.Hour, now: func() time.Time { return now }, newToken: func() string { return "reclaimed-worker" }, ragIndex: func(context.Context, *model.VideoTask) error { calls++; return nil }}
	msg := ragIndexMessage(task.ID, "trace-lease")

	if err := consumer.handleRAGIndex(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "processing lease") {
		t.Fatalf("active lease error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("active owner allowed duplicate execution: %d", calls)
	}
	now = activeUntil.Add(time.Second)
	if err := consumer.handleRAGIndex(context.Background(), msg); err != nil {
		t.Fatalf("expired lease: %v", err)
	}
	if calls != 1 {
		t.Fatalf("reclaimed execution count = %d", calls)
	}
	current, _ := repos.Task.FindByID(task.ID)
	if current.Status != model.TaskStatusCompleted || current.Stage != model.TaskStageNone || current.ProcessingToken != "" {
		t.Fatalf("rag parent completion = %+v", current)
	}
}

func TestHandleRAGIndexSchedulerTokenMakesOldMessageStaleAndExecutesNewMessageOnce(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	task := &model.VideoTask{UserID: 9, FileMD5: "f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1", Filename: "rag-retry.mp4", Status: model.TaskStatusFailed, Stage: model.TaskStageIndexing, LastJobType: model.TaskJobTypeRAGIndex, NextRetryAt: &due, RetryCount: 1, MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "retry evidence", Words: 2}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{TaskID: task.ID, JobType: model.TaskJobTypeRAGIndex, Stage: model.TaskStageIndexing, ExpectedVersion: 0, Now: now, LeaseUntil: now.Add(time.Minute), Token: "dispatch-rag"})
	if err != nil || !claimed {
		t.Fatalf("dispatch = %v/%v", claimed, err)
	}
	calls := 0
	consumer := &Consumer{repo: repos, processingLease: time.Hour, now: func() time.Time { return now }, newToken: func() string { return "rag-worker" }, ragIndex: func(context.Context, *model.VideoTask) error { calls++; return nil }}
	if err := consumer.handleRAGIndex(context.Background(), ragIndexMessage(task.ID, "old")); err != nil {
		t.Fatalf("old message: %v", err)
	}
	payload, _ := json.Marshal(RAGIndexPayload{TaskID: task.ID, TraceID: "new", ClaimToken: "dispatch-rag"})
	message := kafka.Message{Value: payload}
	if err := consumer.handleRAGIndex(context.Background(), message); err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := consumer.handleRAGIndex(context.Background(), message); err != nil {
		t.Fatalf("late message: %v", err)
	}
	if calls != 1 {
		t.Fatalf("rag executions = %d, want 1", calls)
	}
}

func TestPoisonAwareHandlerPersistsBadJSONAndMissingTaskThenAllowsCommit(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	consumer := &Consumer{repo: repos}
	handler := consumer.poisonAwareHandler("rag_index", "rag-group", consumer.handleRAGIndex)
	messages := []kafka.Message{
		{Topic: "rag", Partition: 1, Offset: 10, Value: []byte("{bad")},
		{Topic: "rag", Partition: 1, Offset: 11, Value: mustJSON(t, RAGIndexPayload{TaskID: 999})},
	}
	for _, message := range messages {
		reader := &scriptedMessageReader{fetches: []scriptedFetch{{message: message}, {err: context.Canceled}}}
		err := consumeReader(context.Background(), reader, handler)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consume poison offset %d: %v", message.Offset, err)
		}
		_, commits, _ := reader.snapshot()
		if len(commits) != 1 || commits[0][0].Offset != message.Offset {
			t.Fatalf("commits for %d = %#v", message.Offset, commits)
		}
	}
	var failures []model.KafkaMessageFailure
	if err := db.Order("message_offset").Find(&failures).Error; err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 || failures[0].MessageOffset != 10 || failures[1].MessageOffset != 11 {
		t.Fatalf("poison failures = %+v", failures)
	}
}

func TestPoisonAwareHandlerReturnsErrorWhenQuarantineWriteFails(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	if err := db.Exec("CREATE TRIGGER fail_poison_insert BEFORE INSERT ON kafka_message_failures BEGIN SELECT RAISE(ABORT, 'quarantine unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	consumer := &Consumer{repo: repos}
	handler := consumer.poisonAwareHandler("rag_index", "rag-group", consumer.handleRAGIndex)
	err := handler(context.Background(), kafka.Message{Topic: "rag", Partition: 0, Offset: 1, Value: []byte("bad")})
	if err == nil || !strings.Contains(err.Error(), "quarantine unavailable") {
		t.Fatalf("handler error = %v", err)
	}
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
