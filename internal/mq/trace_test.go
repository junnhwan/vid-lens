package mq

import (
	"context"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

func TestLegacyTraceAPIUsesBusinessCorrelationContext(t *testing.T) {
	ctx := ContextWithTraceID(context.Background(), "trace-legacy")
	if got := TraceIDFromContext(ctx); got != "trace-legacy" {
		t.Fatalf("trace=%q", got)
	}
	if got := observability.CorrelationFromContext(ctx).TraceID; got != "trace-legacy" {
		t.Fatalf("correlation trace=%q", got)
	}
}

func TestContextForTaskJobCarriesCompleteCorrelation(t *testing.T) {
	task := &model.VideoTask{ID: 42, UserID: 7, TraceID: "trace-1", RetryCount: 1}
	job := &model.TaskJob{ID: 8, TaskID: 42, UserID: 7, JobType: TaskJobAnalyze, Stage: model.TaskStageSummarizing, TraceID: "trace-1", RetryCount: 1}
	got := observability.CorrelationFromContext(contextForTaskJob(context.Background(), task, job))
	want := (observability.Correlation{TraceID: "trace-1", TaskID: 42, JobID: 8, UserID: 7, JobType: TaskJobAnalyze, Stage: model.TaskStageSummarizing, Attempt: 2})
	if got != want {
		t.Fatalf("correlation=%+v want=%+v", got, want)
	}
}

func TestRetrySchedulerRestoresCorrelationBeforeEnqueue(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	task := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "retry.mp4", Status: model.TaskStatusFailed, Stage: model.TaskStageSummarizing, TraceID: "trace-retry", RetryCount: 1, MaxRetries: 3, NextRetryAt: &due, LastJobType: TaskJobAnalyze}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	producer := &correlationRetryProducer{}
	scheduler := NewRetryScheduler(repos, producer, RetrySchedulerConfig{BatchSize: 10, Now: func() time.Time { return now }})
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := producer.correlation
	if got.TraceID != "trace-retry" || got.TaskID != task.ID || got.JobID == 0 || got.UserID != 7 || got.JobType != TaskJobAnalyze || got.Stage != model.TaskStageSummarizing || got.Attempt != 1 {
		t.Fatalf("retry correlation=%+v", got)
	}
}

type correlationRetryProducer struct{ correlation observability.Correlation }

func (p *correlationRetryProducer) capture(ctx context.Context) {
	p.correlation = observability.CorrelationFromContext(ctx)
}
func (p *correlationRetryProducer) EnqueueAnalyze(ctx context.Context, _ int64, _ string) error {
	p.capture(ctx)
	return nil
}
func (p *correlationRetryProducer) EnqueueTranscribe(ctx context.Context, _ int64, _ string) error {
	p.capture(ctx)
	return nil
}
func (p *correlationRetryProducer) EnqueueDownload(ctx context.Context, _ int64, _ string) error {
	p.capture(ctx)
	return nil
}
func (p *correlationRetryProducer) EnqueueRAGIndex(ctx context.Context, _ int64) error {
	p.capture(ctx)
	return nil
}

func TestContextForTaskJobStageOverridesPersistedStageWithoutChangingAttempt(t *testing.T) {
	task := &model.VideoTask{ID: 42, UserID: 7, TraceID: "trace-stage", RetryCount: 1}
	job := &model.TaskJob{ID: 8, TaskID: 42, UserID: 7, JobType: TaskJobAnalyze, Stage: model.TaskStageSummarizing, TraceID: "trace-stage", RetryCount: 1}
	ctx := contextForTaskJobStage(context.Background(), task, job, model.TaskStageTranscribing)
	got := observability.CorrelationFromContext(ctx)
	if got.Stage != model.TaskStageTranscribing || got.Attempt != 2 {
		t.Fatalf("correlation=%+v", got)
	}
}

func TestContextForTaskJobUsesExplicitAttemptSnapshot(t *testing.T) {
	task := &model.VideoTask{ID: 42, UserID: 7, TraceID: "trace-attempt", RetryCount: 2}
	job := &model.TaskJob{ID: 8, TaskID: 42, UserID: 7, JobType: TaskJobAnalyze, Stage: model.TaskStageSummarizing, TraceID: "trace-attempt", RetryCount: 2}
	ctx := contextForTaskJobAttempt(context.Background(), task, job, 2)
	got := observability.CorrelationFromContext(ctx)
	if got.Attempt != 2 {
		t.Fatalf("attempt=%d want=2", got.Attempt)
	}
}
