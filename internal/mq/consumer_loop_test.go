package mq

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// scriptedAmqpReader is the test fake for messageReader. It serves a scripted
// slice of deliveries over a channel and records Ack/Nack calls. When the
// script is exhausted it blocks on the context like a real idle consumer; tests
// end the loop by canceling the context.
type scriptedAmqpReader struct {
	mu       sync.Mutex
	fetches  []amqp.Delivery
	acks     []uint64
	nacks    []uint64
	consumed int
	closed   bool
	// closeImmediately makes Consume return a channel that is already closed,
	// simulating an infra error (e.g. connection reset) so the outer loop
	// rebuilds the reader.
	closeImmediately bool
}

func (r *scriptedAmqpReader) Consume(ctx context.Context) (<-chan amqp.Delivery, error) {
	deliveries := make(chan amqp.Delivery, 8)
	if r.closeImmediately {
		close(deliveries)
		return deliveries, nil
	}
	go func() {
		defer close(deliveries)
		for {
			r.mu.Lock()
			if len(r.fetches) > 0 {
				next := r.fetches[0]
				r.fetches = r.fetches[1:]
				r.consumed++
				r.mu.Unlock()
				select {
				case deliveries <- next:
				case <-ctx.Done():
					return
				}
				continue
			}
			r.mu.Unlock()
			// Script exhausted: close the channel deterministically so the
			// consume loop returns instead of blocking on a 20ms sleep + cancel
			// (which flakes on loaded CI). Tests that need a still-running reader
			// set closeImmediately=false and rely on ctx cancellation elsewhere.
			return
		}
	}()
	return deliveries, nil
}

// stubDelivery makes a delivery whose Ack/Nack record into this reader.
func (r *scriptedAmqpReader) stubDelivery(tag uint64, body []byte) amqp.Delivery {
	d := amqp.Delivery{DeliveryTag: tag, Body: body}
	d.Acknowledger = &recordingAcknowledger{reader: r}
	return d
}

type recordingAcknowledger struct {
	reader *scriptedAmqpReader
}

func (a *recordingAcknowledger) Ack(tag uint64, multiple bool) error {
	a.reader.mu.Lock()
	a.reader.acks = append(a.reader.acks, tag)
	a.reader.mu.Unlock()
	return nil
}

func (a *recordingAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	a.reader.mu.Lock()
	a.reader.nacks = append(a.reader.nacks, tag)
	a.reader.mu.Unlock()
	return nil
}

func (a *recordingAcknowledger) Reject(tag uint64, requeue bool) error { return nil }

func (r *scriptedAmqpReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *scriptedAmqpReader) snapshot() (consumed int, acks []uint64, nacks []uint64, closed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.consumed, append([]uint64(nil), r.acks...), append([]uint64(nil), r.nacks...), r.closed
}

func TestConsumeMessagesAcksHandledDelivery(t *testing.T) {
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{reader.stubDelivery(7, []byte("payload"))}
	handled := 0

	err := consumeMessages(context.Background(), reader, func(_ context.Context, got amqp.Delivery) error {
		handled++
		if got.DeliveryTag != 7 {
			t.Fatalf("handled tag = %d, want 7", got.DeliveryTag)
		}
		return nil
	})
	_ = err

	consumed, acks, nacks, closed := reader.snapshot()
	if handled != 1 || consumed != 1 {
		t.Fatalf("handled/consumed = %d/%d, want 1/1", handled, consumed)
	}
	if len(acks) != 1 || acks[0] != 7 || len(nacks) != 0 {
		t.Fatalf("acks/nacks = %v/%v, want ack 7", acks, nacks)
	}
	if !closed {
		t.Fatal("reader was not closed after loop exit")
	}
}

func TestConsumeMessagesNacksOnHandlerError(t *testing.T) {
	handleErr := errors.New("failure state persistence failed")
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{reader.stubDelivery(10, nil)}

	err := consumeMessages(context.Background(), reader, func(context.Context, amqp.Delivery) error {
		return handleErr
	})
	_ = err

	_, acks, nacks, closed := reader.snapshot()
	if len(acks) != 0 || len(nacks) != 1 || nacks[0] != 10 {
		t.Fatalf("acks/nacks = %v/%v, want nack 10", acks, nacks)
	}
	if !closed {
		t.Fatal("reader was not closed after handler error")
	}
}

func TestConsumeMessagesClosesOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &scriptedAmqpReader{}

	err := consumeMessages(ctx, reader, func(context.Context, amqp.Delivery) error {
		t.Fatal("handler must not run after context cancellation")
		return nil
	})
	_ = err

	_, acks, _, closed := reader.snapshot()
	if len(acks) != 0 || !closed {
		t.Fatalf("acks/closed = %d/%v, want 0/true", len(acks), closed)
	}
}

func TestStartGroupConsumerStopsAndWaitsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &scriptedAmqpReader{}
	readerStarted := make(chan struct{})
	consumer := &Consumer{
		newMessageReader: func(queue, groupID string) messageReader {
			close(readerStarted)
			return reader
		},
	}

	consumer.startGroupConsumer(ctx, "test", []string{"broker"}, "topic", "group", func(context.Context, amqp.Delivery) error {
		return nil
	})
	select {
	case <-readerStarted:
	case <-time.After(time.Second):
		t.Fatal("consumer did not create a reader")
	}
	cancel()

	done := make(chan struct{})
	go func() {
		consumer.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after context cancellation")
	}

	_, _, _, closed := reader.snapshot()
	if !closed {
		t.Fatal("reader was not closed after consumer shutdown")
	}
}

func TestRunGroupConsumerRebuildsReaderAfterInfrastructureError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &scriptedAmqpReader{closeImmediately: true} // closed channel = infra error
	second := &scriptedAmqpReader{}
	created := 0
	consumer := &Consumer{
		newMessageReader: func(queue, groupID string) messageReader {
			created++
			if created == 1 {
				return first
			}
			cancel()
			return second
		},
		readerRestartBackoff: time.Millisecond,
	}

	consumer.runGroupConsumer(ctx, "test", "topic", "group", func(context.Context, amqp.Delivery) error {
		return nil
	})

	_, _, _, firstClosed := first.snapshot()
	_, _, _, secondClosed := second.snapshot()
	if created != 2 {
		t.Fatalf("reader factory calls = %d, want 2", created)
	}
	if !firstClosed || !secondClosed {
		t.Fatalf("reader closed states = %v/%v, want true/true", firstClosed, secondClosed)
	}
}

func TestConsumeMessagesAcksAfterBusinessFailureIsHandedToRetryScheduler(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID:     11,
		FileMD5:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa01",
		Filename:   "rag-failure.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageIndexing,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source transcript"}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}
	consumer := &Consumer{
		repo: repos,
		ragIndex: func(context.Context, *model.VideoTask) error {
			return fmt.Errorf("network timeout")
		},
		retryPolicy:  TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now }},
		idempotency:  noOpIdempotencyChecker{},
	}
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{reader.stubDelivery(1, ragIndexMessage(task.ID, "trace-transfer").Body)}

	err := consumeMessages(context.Background(), reader, consumer.handleRAGIndex)
	_ = err

	_, acks, _, _ := reader.snapshot()
	if len(acks) != 1 {
		t.Fatalf("ack count = %d, want 1 after durable failure handoff", len(acks))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || !current.NextRetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("failure handoff state = %+v, want failed with scheduler due time", current)
	}
}

func TestConsumeMessagesNacksWhenFailurePersistenceIsNotAtomic(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID:     12,
		FileMD5:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa02",
		Filename:   "rag-persistence-failure.mp4",
		Status:     model.TaskStatusRunning,
		Stage:      model.TaskStageIndexing,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source transcript"}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}
	if err := repos.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, 3); err != nil {
		t.Fatalf("create task job: %v", err)
	}
	if err := db.Exec(
		"CREATE TRIGGER fail_retry_job_update BEFORE UPDATE OF last_error_code ON task_jobs " +
			"WHEN NEW.last_error_code = 'retryable_error' BEGIN SELECT RAISE(ABORT, 'task job write failed'); END",
	).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	consumer := &Consumer{
		repo: repos,
		ragIndex: func(context.Context, *model.VideoTask) error {
			return fmt.Errorf("network timeout")
		},
		retryPolicy: TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now }},
		idempotency: noOpIdempotencyChecker{},
	}
	reader := &scriptedAmqpReader{}
	reader.fetches = []amqp.Delivery{reader.stubDelivery(1, ragIndexMessage(task.ID, "trace-persist").Body)}

	_ = consumeMessages(context.Background(), reader, consumer.dedupHandler("video-rag-index", consumer.handleRAGIndex))
	_, acks, nacks, _ := reader.snapshot()
	// Handler failed because the failure-handoff transaction aborted (task job
	// write trigger); the delivery is Nacked for redelivery, not Acked — so the
	// task is not silently completed and RetryScheduler can still drive recovery.
	if len(acks) != 0 || len(nacks) != 1 {
		t.Fatalf("ack/nack = %d/%d, want 0/1 when failure handoff is not durable", len(acks), len(nacks))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusRunning || current.RetryCount != 0 || current.NextRetryAt != nil {
		t.Fatalf("task state = %+v, want transaction rollback to running without retry", current)
	}
}

func TestRedeliveryDoesNotRunRAGWhileRetrySchedulerOwnsTask(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	nextRetryAt := time.Date(2026, 7, 13, 12, 5, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID:       13,
		FileMD5:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa03",
		Filename:     "rag-redelivery.mp4",
		Status:       model.TaskStatusFailed,
		Stage:        model.TaskStageIndexing,
		RetryCount:   1,
		MaxRetries:   3,
		NextRetryAt:  &nextRetryAt,
		LastJobType:  TaskJobRAGIndex,
		LastErrorMsg: "network timeout",
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repos.Transcription.Upsert(&model.VideoTranscription{TaskID: task.ID, Content: "source transcript"}); err != nil {
		t.Fatalf("create transcription: %v", err)
	}
	calls := 0
	consumer := &Consumer{
		repo: repos,
		ragIndex: func(context.Context, *model.VideoTask) error {
			calls++
			return nil
		},
		idempotency: noOpIdempotencyChecker{},
	}
	reader := &scriptedAmqpReader{}
	redelivery := reader.stubDelivery(1, ragIndexMessage(task.ID, "trace-redelivery").Body)
	redelivery.MessageId = "rag_index:1"
	reader.fetches = []amqp.Delivery{redelivery}

	_ = consumeMessages(context.Background(), reader, consumer.dedupHandler("video-rag-index", consumer.handleRAGIndex))

	if calls != 0 {
		t.Fatalf("RAG index calls = %d, want 0 while RetryScheduler owns next retry", calls)
	}
	_, acks, _, _ := reader.snapshot()
	if len(acks) != 1 {
		t.Fatalf("ack count = %d, want 1 after stale message acknowledged", len(acks))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusFailed || current.RetryCount != 1 || current.NextRetryAt == nil || !current.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("scheduler-owned task changed on redelivery: %+v", current)
	}
}

func newConsumerLoopTestRepositories(t *testing.T) (*repository.Repositories, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return repository.NewRepositories(db), db
}

func TestRetrySchedulerReturnsTaskRestoreErrorAfterProducerFailure(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	now := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	task := createDueRetryTask(t, repos, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa04")
	if err := db.Exec(
		"CREATE TRIGGER fail_task_retry_restore BEFORE UPDATE OF last_error_code ON video_tasks " +
			"WHEN NEW.last_error_code = 'retry_enqueue_failed' BEGIN SELECT RAISE(ABORT, 'task restore failed'); END",
	).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	scheduler := NewRetryScheduler(repos, &recordingRetryProducer{err: fmt.Errorf("mq unavailable")}, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	err := scheduler.RunOnce(context.Background())

	if err == nil || !strings.Contains(err.Error(), "mq unavailable") || !strings.Contains(err.Error(), "task restore failed") {
		t.Fatalf("RunOnce error = %v, want producer and task restore errors", err)
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusQueued || current.NextRetryAt != nil || current.ProcessingToken == "" || current.LeaseKind != model.TaskLeaseKindDispatch || current.LeaseExpiresAt == nil || current.LeaseVersion != 1 {
		t.Fatalf("task restore transaction did not roll back: %+v", current)
	}
}

func TestRetrySchedulerReturnsTaskJobRestoreErrorAfterProducerFailure(t *testing.T) {
	repos, db := newConsumerLoopTestRepositories(t)
	now := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	task := createDueRetryTask(t, repos, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa05")
	if err := db.Exec(
		"CREATE TRIGGER fail_job_retry_restore BEFORE UPDATE OF last_error_code ON task_jobs " +
			"WHEN NEW.last_error_code = 'retry_enqueue_failed' BEGIN SELECT RAISE(ABORT, 'task job restore failed'); END",
	).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	scheduler := NewRetryScheduler(repos, &recordingRetryProducer{err: fmt.Errorf("mq unavailable")}, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	err := scheduler.RunOnce(context.Background())

	if err == nil || !strings.Contains(err.Error(), "mq unavailable") || !strings.Contains(err.Error(), "task job restore failed") {
		t.Fatalf("RunOnce error = %v, want producer and task job restore errors", err)
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusQueued || current.NextRetryAt != nil || current.ProcessingToken == "" || current.LeaseKind != model.TaskLeaseKindDispatch || current.LeaseExpiresAt == nil || current.LeaseVersion != 1 {
		t.Fatalf("task/job restore transaction did not roll back: %+v", current)
	}
}

func TestRetrySchedulerDoesNotRedispatchUntilProducerFailureBackoffExpires(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	createDueRetryTask(t, repos, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa06")
	producer := &recordingRetryProducer{err: fmt.Errorf("mq unavailable")}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("first RunOnce expected producer error")
	}
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce before restored due time: %v", err)
	}
	if len(producer.transcribes) != 1 {
		t.Fatalf("dispatch attempts before due = %d, want 1", len(producer.transcribes))
	}

	producer.err = nil
	now = now.Add(time.Minute)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce after restored due time: %v", err)
	}
	if len(producer.transcribes) != 2 {
		t.Fatalf("dispatch attempts after due = %d, want 2", len(producer.transcribes))
	}
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("duplicate RunOnce after successful claim: %v", err)
	}
	if len(producer.transcribes) != 2 {
		t.Fatalf("duplicate successful dispatch count = %d, want 2", len(producer.transcribes))
	}
}

func createDueRetryTask(t *testing.T, repos *repository.Repositories, now time.Time, md5 string) *model.VideoTask {
	t.Helper()
	dueAt := now.Add(-time.Second)
	task := &model.VideoTask{
		UserID:      14,
		FileMD5:     md5,
		Filename:    "retry.mp4",
		Status:      model.TaskStatusFailed,
		Stage:       model.TaskStageTranscribing,
		RetryCount:  1,
		MaxRetries:  3,
		NextRetryAt: &dueAt,
		LastJobType: TaskJobTranscribe,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create retry task: %v", err)
	}
	return task
}
