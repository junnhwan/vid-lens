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
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type scriptedFetch struct {
	message kafka.Message
	err     error
}

type scriptedMessageReader struct {
	mu         sync.Mutex
	fetches    []scriptedFetch
	fetchCalls int
	commits    [][]kafka.Message
	commitErr  error
	closed     bool
}

func (r *scriptedMessageReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	r.mu.Lock()
	r.fetchCalls++
	if len(r.fetches) > 0 {
		result := r.fetches[0]
		r.fetches = r.fetches[1:]
		r.mu.Unlock()
		return result.message, result.err
	}
	r.mu.Unlock()

	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (r *scriptedMessageReader) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commits = append(r.commits, append([]kafka.Message(nil), messages...))
	return r.commitErr
}

func (r *scriptedMessageReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *scriptedMessageReader) snapshot() (fetchCalls int, commits [][]kafka.Message, closed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fetchCalls, append([][]kafka.Message(nil), r.commits...), r.closed
}

func TestConsumeReaderCommitsHandledMessage(t *testing.T) {
	message := kafka.Message{Topic: "analyze", Partition: 1, Offset: 7, Value: []byte("payload")}
	reader := &scriptedMessageReader{fetches: []scriptedFetch{
		{message: message},
		{err: context.Canceled},
	}}
	handled := 0

	err := consumeReader(context.Background(), reader, func(_ context.Context, got kafka.Message) error {
		handled++
		if got.Offset != message.Offset {
			t.Fatalf("handled offset = %d, want %d", got.Offset, message.Offset)
		}
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consumeReader error = %v, want context canceled", err)
	}
	fetchCalls, commits, closed := reader.snapshot()
	if handled != 1 || fetchCalls != 2 {
		t.Fatalf("handled/fetch calls = %d/%d, want 1/2", handled, fetchCalls)
	}
	if len(commits) != 1 || len(commits[0]) != 1 || commits[0][0].Offset != message.Offset {
		t.Fatalf("commits = %#v, want only offset %d", commits, message.Offset)
	}
	if !closed {
		t.Fatal("reader was not closed after loop exit")
	}
}

func TestConsumeReaderStopsWithoutCommitOnFetchError(t *testing.T) {
	fetchErr := errors.New("broker disconnected")
	reader := &scriptedMessageReader{fetches: []scriptedFetch{{err: fetchErr}}}
	handled := 0

	err := consumeReader(context.Background(), reader, func(context.Context, kafka.Message) error {
		handled++
		return nil
	})

	if !errors.Is(err, fetchErr) {
		t.Fatalf("consumeReader error = %v, want fetch error", err)
	}
	fetchCalls, commits, closed := reader.snapshot()
	if handled != 0 || fetchCalls != 1 || len(commits) != 0 {
		t.Fatalf("handled/fetch/commits = %d/%d/%d, want 0/1/0", handled, fetchCalls, len(commits))
	}
	if !closed {
		t.Fatal("reader was not closed after fetch error")
	}
}

func TestConsumeReaderStopsWithoutReadingNextMessageOnHandlerError(t *testing.T) {
	handleErr := errors.New("failure state persistence failed")
	reader := &scriptedMessageReader{fetches: []scriptedFetch{
		{message: kafka.Message{Offset: 10}},
		{message: kafka.Message{Offset: 11}},
	}}

	err := consumeReader(context.Background(), reader, func(context.Context, kafka.Message) error {
		return handleErr
	})

	if !errors.Is(err, handleErr) {
		t.Fatalf("consumeReader error = %v, want handler error", err)
	}
	fetchCalls, commits, closed := reader.snapshot()
	if fetchCalls != 1 || len(commits) != 0 {
		t.Fatalf("fetch/commit calls = %d/%d, want 1/0", fetchCalls, len(commits))
	}
	if !closed {
		t.Fatal("reader was not closed after handler error")
	}
}

func TestConsumeReaderStopsWithoutReadingNextMessageOnCommitError(t *testing.T) {
	commitErr := errors.New("commit coordinator unavailable")
	reader := &scriptedMessageReader{
		fetches: []scriptedFetch{
			{message: kafka.Message{Offset: 20}},
			{message: kafka.Message{Offset: 21}},
		},
		commitErr: commitErr,
	}
	handled := 0

	err := consumeReader(context.Background(), reader, func(context.Context, kafka.Message) error {
		handled++
		return nil
	})

	if !errors.Is(err, commitErr) {
		t.Fatalf("consumeReader error = %v, want commit error", err)
	}
	fetchCalls, commits, closed := reader.snapshot()
	if handled != 1 || fetchCalls != 1 || len(commits) != 1 {
		t.Fatalf("handled/fetch/commit calls = %d/%d/%d, want 1/1/1", handled, fetchCalls, len(commits))
	}
	if !closed {
		t.Fatal("reader was not closed after commit error")
	}
}

func TestConsumeReaderClosesOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &scriptedMessageReader{}

	err := consumeReader(ctx, reader, func(context.Context, kafka.Message) error {
		t.Fatal("handler must not run after context cancellation")
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consumeReader error = %v, want context canceled", err)
	}
	_, commits, closed := reader.snapshot()
	if len(commits) != 0 || !closed {
		t.Fatalf("commits/closed = %d/%v, want 0/true", len(commits), closed)
	}
}

func TestStartGroupConsumerStopsAndWaitsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &scriptedMessageReader{}
	readerStarted := make(chan struct{})
	consumer := &Consumer{
		newKafkaReader: func(kafka.ReaderConfig) kafkaMessageReader {
			close(readerStarted)
			return reader
		},
	}

	consumer.startGroupConsumer(ctx, "test", []string{"broker"}, "topic", "group", func(context.Context, kafka.Message) error {
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

	_, _, closed := reader.snapshot()
	if !closed {
		t.Fatal("reader was not closed after consumer shutdown")
	}
}

func TestRunGroupConsumerRebuildsReaderAfterInfrastructureError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &scriptedMessageReader{fetches: []scriptedFetch{{err: errors.New("connection reset")}}}
	second := &scriptedMessageReader{}
	created := 0
	consumer := &Consumer{
		newKafkaReader: func(kafka.ReaderConfig) kafkaMessageReader {
			created++
			if created == 1 {
				return first
			}
			cancel()
			return second
		},
		readerRestartBackoff: time.Millisecond,
	}

	consumer.runGroupConsumer(ctx, "test", kafka.ReaderConfig{}, func(context.Context, kafka.Message) error {
		return nil
	})

	_, _, firstClosed := first.snapshot()
	_, _, secondClosed := second.snapshot()
	if created != 2 {
		t.Fatalf("reader factory calls = %d, want 2", created)
	}
	if !firstClosed || !secondClosed {
		t.Fatalf("reader closed states = %v/%v, want true/true", firstClosed, secondClosed)
	}
}

func TestConsumeReaderCommitsAfterBusinessFailureIsHandedToRetryScheduler(t *testing.T) {
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
		retryPolicy: TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now }},
	}
	reader := &scriptedMessageReader{fetches: []scriptedFetch{
		{message: ragIndexMessage(task.ID, "trace-transfer")},
		{err: context.Canceled},
	}}

	err := consumeReader(context.Background(), reader, consumer.handleRAGIndex)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consumeReader error = %v, want context canceled after committed failure", err)
	}
	_, commits, _ := reader.snapshot()
	if len(commits) != 1 {
		t.Fatalf("commit count = %d, want 1 after durable failure handoff", len(commits))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusFailed || current.NextRetryAt == nil || !current.NextRetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("failure handoff state = %+v, want failed with scheduler due time", current)
	}
}

func TestConsumeReaderDoesNotCommitWhenFailurePersistenceIsNotAtomic(t *testing.T) {
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
	}
	reader := &scriptedMessageReader{fetches: []scriptedFetch{
		{message: ragIndexMessage(task.ID, "trace-persist")},
		{err: context.Canceled},
	}}

	err := consumeReader(context.Background(), reader, consumer.handleRAGIndex)

	if err == nil || !strings.Contains(err.Error(), "task job write failed") {
		t.Fatalf("consumeReader error = %v, want task job persistence error", err)
	}
	_, commits, _ := reader.snapshot()
	if len(commits) != 0 {
		t.Fatalf("commit count = %d, want 0 when failure handoff is not durable", len(commits))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusRunning || current.RetryCount != 0 || current.NextRetryAt != nil {
		t.Fatalf("task state = %+v, want transaction rollback to running without retry", current)
	}
}

func TestKafkaRedeliveryDoesNotRunRAGWhileRetrySchedulerOwnsTask(t *testing.T) {
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
	}
	reader := &scriptedMessageReader{fetches: []scriptedFetch{
		{message: ragIndexMessage(task.ID, "trace-redelivery")},
		{err: context.Canceled},
	}}

	err := consumeReader(context.Background(), reader, consumer.handleRAGIndex)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consumeReader error = %v, want context canceled after duplicate commit", err)
	}
	if calls != 0 {
		t.Fatalf("RAG index calls = %d, want 0 while RetryScheduler owns next retry", calls)
	}
	_, commits, _ := reader.snapshot()
	if len(commits) != 1 {
		t.Fatalf("commit count = %d, want duplicate Kafka message acknowledged", len(commits))
	}
	current, findErr := repos.Task.FindByID(task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if current.Status != model.TaskStatusFailed || current.RetryCount != 1 || current.NextRetryAt == nil || !current.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("scheduler-owned task changed on Kafka redelivery: %+v", current)
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
	scheduler := NewRetryScheduler(repos, &recordingRetryProducer{err: fmt.Errorf("kafka unavailable")}, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	err := scheduler.RunOnce(context.Background())

	if err == nil || !strings.Contains(err.Error(), "kafka unavailable") || !strings.Contains(err.Error(), "task restore failed") {
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
	scheduler := NewRetryScheduler(repos, &recordingRetryProducer{err: fmt.Errorf("kafka unavailable")}, RetrySchedulerConfig{
		BatchSize: 10,
		Now:       func() time.Time { return now },
	})

	err := scheduler.RunOnce(context.Background())

	if err == nil || !strings.Contains(err.Error(), "kafka unavailable") || !strings.Contains(err.Error(), "task job restore failed") {
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
	producer := &recordingRetryProducer{err: fmt.Errorf("kafka unavailable")}
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
