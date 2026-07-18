package mq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestIsRetryableErrorUsesTypedProviderError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "rate limited",
			err:  &ai.ProviderError{Class: ai.ErrorRateLimited, StatusCode: 429, Retryable: true},
			want: true,
		},
		{
			name: "provider 5xx wrapped",
			err: fmt.Errorf("generate embedding: %w", &ai.ProviderError{
				Class:      ai.ErrorProvider5xx,
				StatusCode: 503,
				Retryable:  true,
			}),
			want: true,
		},
		{
			name: "authentication failure",
			err:  &ai.ProviderError{Class: ai.ErrorAuth, StatusCode: 401, Retryable: false},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Fatalf("isRetryableError() = %v, want %v for %v", got, tt.want, tt.err)
			}
		})
	}
}

func TestRecordTaskFailureUsesProviderRetryAfterAsLowerBound(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	task := &model.VideoTask{
		UserID: 31, FileMD5: "31313131313131313131313131313131", Filename: "retry-after.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageSummarizing,
		MaxRetries: 3,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}

	consumer := &Consumer{repo: repos}
	consumer.SetRetryPolicy(TaskRetryPolicy{
		MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now },
	})
	failure := fmt.Errorf("summary call failed: %w", &ai.ProviderError{
		Class: ai.ErrorRateLimited, StatusCode: 429, Retryable: true, RetryAfter: 7 * time.Minute,
	})
	if err := consumer.recordTaskFailure(task.ID, TaskJobAnalyze, model.TaskStageSummarizing, failure); err != nil {
		t.Fatal(err)
	}

	want := now.Add(7 * time.Minute)
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobAnalyze)
	if err != nil {
		t.Fatal(err)
	}
	if current.NextRetryAt == nil || !current.NextRetryAt.Equal(want) {
		t.Fatalf("task next_retry_at = %v, want %v", current.NextRetryAt, want)
	}
	if job == nil || job.NextRetryAt == nil || !job.NextRetryAt.Equal(want) {
		t.Fatalf("job next_retry_at = %v, want %v", job, want)
	}
}

func TestRecordLeasedTaskFailureUsesProviderRetryAfterAsLowerBound(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	now := time.Date(2026, 7, 14, 13, 30, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	task := &model.VideoTask{
		UserID: 32, FileMD5: "32323232323232323232323232323232", Filename: "leased-retry-after.mp4",
		Status: model.TaskStatusRunning, Stage: model.TaskStageIndexing, LastJobType: TaskJobRAGIndex,
		MaxRetries: 3, ProcessingToken: "rag-worker", LeaseKind: model.TaskLeaseKindProcessing,
		LeaseExpiresAt: &leaseUntil, LeaseVersion: 1,
	}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := repos.TaskJob.UpsertDispatching(task, TaskJobRAGIndex, model.TaskStatusRunning, model.TaskStageIndexing); err != nil {
		t.Fatal(err)
	}

	consumer := &Consumer{repo: repos, now: func() time.Time { return now }}
	consumer.SetRetryPolicy(TaskRetryPolicy{
		MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return now },
	})
	failure := &ai.ProviderError{
		Class: ai.ErrorProvider5xx, StatusCode: 503, Retryable: true, RetryAfter: 9 * time.Minute,
	}
	if err := consumer.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, failure, "rag-worker"); err != nil {
		t.Fatal(err)
	}

	want := now.Add(9 * time.Minute)
	current, err := repos.Task.FindByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobRAGIndex)
	if err != nil {
		t.Fatal(err)
	}
	if current.NextRetryAt == nil || !current.NextRetryAt.Equal(want) {
		t.Fatalf("task next_retry_at = %v, want %v", current.NextRetryAt, want)
	}
	if job == nil || job.NextRetryAt == nil || !job.NextRetryAt.Equal(want) {
		t.Fatalf("job next_retry_at = %v, want %v", job, want)
	}
}

func TestRetrySchedulerWaitsForStoppedWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scheduler := NewRetryScheduler(nil, nil, RetrySchedulerConfig{})
	scheduler.Start(ctx)
	done := make(chan struct{})
	go func() {
		scheduler.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry scheduler did not stop after context cancellation")
	}
}
