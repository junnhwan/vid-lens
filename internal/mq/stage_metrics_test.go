package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

func TestCompleteTaskProcessingRecordsSuccessFromPersistedStageStart(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	started := time.Now().Add(-2 * time.Minute)
	finished := started.Add(90 * time.Second)
	task := &model.VideoTask{UserID: 9, FileMD5: "90909090909090909090909090909090", Filename: "metrics.mp4", Status: model.TaskStatusQueued, Stage: model.TaskStageUploaded, TraceID: "trace-metrics", MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: task.ID, JobType: TaskJobTranscribe, Stage: model.TaskStageTranscribing, Now: started, LeaseUntil: started.Add(time.Hour), NewToken: "metrics-token"})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	registry := prometheus.NewRegistry()
	metrics, _ := observability.NewMetrics(registry)
	previous := observability.DefaultMetrics()
	observability.SetDefaultMetrics(metrics)
	t.Cleanup(func() { observability.SetDefaultMetrics(previous) })
	consumer := &Consumer{repo: repos, now: func() time.Time { return finished }}
	completed, err := consumer.completeTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: TaskJobTranscribe, JobStage: model.TaskStageTranscribing, Token: claim.Token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: finished})
	if err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	count, sum := taskStageHistogram(t, registry, model.TaskStageTranscribing)
	if count != 1 || sum < 89 || sum > 91 {
		t.Fatalf("count=%d sum=%f", count, sum)
	}
}

func TestLeasedFailureRecordsDurationFromPersistedStageStart(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	started := time.Now().Add(-2 * time.Minute)
	finished := started.Add(75 * time.Second)
	task := &model.VideoTask{UserID: 9, FileMD5: "91919191919191919191919191919191", Filename: "failure.mp4", Status: model.TaskStatusQueued, Stage: model.TaskStageUploaded, TraceID: "trace-failure", MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: task.ID, JobType: TaskJobTranscribe, Stage: model.TaskStageTranscribing, Now: started, LeaseUntil: started.Add(time.Hour), NewToken: "failure-token"})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	registry := prometheus.NewRegistry()
	metrics, _ := observability.NewMetrics(registry)
	previous := observability.DefaultMetrics()
	observability.SetDefaultMetrics(metrics)
	t.Cleanup(func() { observability.SetDefaultMetrics(previous) })
	consumer := &Consumer{repo: repos, retryPolicy: TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{1}, Now: func() time.Time { return finished }}}
	if err := consumer.recordTaskFailure(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, errors.New("context deadline exceeded"), claim.Token); err != nil {
		t.Fatal(err)
	}
	count, sum := taskStageHistogram(t, registry, model.TaskStageTranscribing)
	if count != 1 || sum < 74 || sum > 76 {
		t.Fatalf("count=%d sum=%f", count, sum)
	}
}

func taskStageHistogram(t *testing.T, registry *prometheus.Registry, stage string) (uint64, float64) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "vidlens_task_stage_duration_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "stage" && label.GetValue() == stage {
					return metric.GetHistogram().GetSampleCount(), metric.GetHistogram().GetSampleSum()
				}
			}
		}
	}
	return 0, 0
}

func TestTransitionTaskStageRecordsPreviousStageSuccess(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	started := time.Now().Add(-40 * time.Second)
	finished := started.Add(30 * time.Second)
	task := &model.VideoTask{UserID: 9, FileMD5: "93939393939393939393939393939393", Filename: "transition.mp4", Status: model.TaskStatusRunning, Stage: model.TaskStageTranscribing, StageStartedAt: &started}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	registry := prometheus.NewRegistry()
	metrics, _ := observability.NewMetrics(registry)
	previous := observability.DefaultMetrics()
	observability.SetDefaultMetrics(metrics)
	t.Cleanup(func() { observability.SetDefaultMetrics(previous) })
	consumer := &Consumer{repo: repos, now: func() time.Time { return finished }}
	if err := consumer.transitionTaskStage(context.Background(), task.ID, model.TaskStageSummarizing); err != nil {
		t.Fatal(err)
	}
	count, sum := taskStageHistogram(t, registry, model.TaskStageTranscribing)
	if count != 1 || sum < 29 || sum > 31 {
		t.Fatalf("count=%d sum=%f", count, sum)
	}
}
