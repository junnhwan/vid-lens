package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

func TestASRTimeoutFaultDrillCorrelatesTaskJobAuditAndRetry(t *testing.T) {
	repos := newConsumerTestRepositories(t)
	started := time.Now().Add(-20 * time.Second)
	failedAt := started.Add(5 * time.Second)
	task := &model.VideoTask{UserID: 77, FileMD5: "94949494949494949494949494949494", Filename: "fault-drill.mp4", Status: model.TaskStatusQueued, Stage: model.TaskStageUploaded, TraceID: "trace-asr-timeout", MaxRetries: 3}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	claim, err := repos.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{TaskID: task.ID, JobType: TaskJobTranscribe, Stage: model.TaskStageTranscribing, Now: started, LeaseUntil: started.Add(time.Minute), NewToken: "fault-token"})
	if err != nil || claim.Outcome != repository.TaskLeaseAcquired {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	job, err := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	if err != nil || job == nil {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	ctx := contextForTaskJobStage(context.Background(), task, job, model.TaskStageTranscribing)
	recorder := &repositoryCallRecorder{repos: repos}
	strategy := ai.NewObservedStrategy(asrTimeoutStrategy{}, recorder, ai.CallContext{ASRProvider: "mock", ASRModel: "timeout-model"})
	consumer := &Consumer{repo: repos, ffmpegPath: "ffmpeg", splitAudio: func(context.Context, string, string, int) ([]string, error) {
		return []string{"chunk-timeout.mp3"}, nil
	}, retryPolicy: TaskRetryPolicy{MaxRetries: 3, BackoffSeconds: []int{60}, Now: func() time.Time { return failedAt }}, now: func() time.Time { return failedAt }}
	_, callErr := consumer.transcribeAudio(ctx, task.ID, "audio.mp3", strategy)
	if !errors.Is(callErr, context.DeadlineExceeded) {
		t.Fatalf("call error=%v", callErr)
	}
	if err := consumer.recordTaskFailure(task.ID, TaskJobTranscribe, model.TaskStageTranscribing, callErr, claim.Token); err != nil {
		t.Fatal(err)
	}

	currentTask, _ := repos.Task.FindByID(task.ID)
	currentJob, _ := repos.TaskJob.FindByTaskAndType(task.ID, TaskJobTranscribe)
	logs, err := repos.AICallLog.ListByTaskID(task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if currentTask.Status != model.TaskStatusFailed || currentTask.Stage != model.TaskStageTranscribing || currentTask.RetryCount != 1 || currentTask.NextRetryAt == nil {
		t.Fatalf("task=%+v", currentTask)
	}
	if currentJob == nil || currentJob.Status != model.TaskStatusFailed || currentJob.Stage != model.TaskStageTranscribing || currentJob.RetryCount != 1 || currentJob.NextRetryAt == nil {
		t.Fatalf("job=%+v", currentJob)
	}
	if len(logs) != 1 {
		t.Fatalf("audit logs=%d", len(logs))
	}
	log := logs[0]
	complete := []bool{log.TraceID == task.TraceID, log.TaskID == task.ID, log.JobID == job.ID, log.UserID == task.UserID, log.JobType == TaskJobTranscribe, log.Stage == model.TaskStageTranscribing, log.Attempt == 1, log.Kind == model.AICallKindASR, log.Status == model.AICallStatusFailed, log.ErrorCode == "timeout"}
	for i, ok := range complete {
		if !ok {
			t.Fatalf("correlation field %d incomplete: %+v", i, log)
		}
	}
}

type asrTimeoutStrategy struct{}

func (asrTimeoutStrategy) Transcribe(context.Context, string) (string, error) {
	return "", context.DeadlineExceeded
}
func (asrTimeoutStrategy) TranscribeChunks(context.Context, []string) (string, error) {
	return "", context.DeadlineExceeded
}
func (asrTimeoutStrategy) Summarize(context.Context, string) (string, error) { return "", nil }

type repositoryCallRecorder struct{ repos *repository.Repositories }

func (r *repositoryCallRecorder) RecordAICall(_ context.Context, record ai.CallRecord) error {
	return r.repos.AICallLog.Create(&model.AICallLog{UserID: record.UserID, TaskID: record.TaskID, JobID: record.JobID, TraceID: record.TraceID, JobType: record.JobType, Stage: record.Stage, Attempt: record.Attempt, Kind: record.Kind, Provider: record.Provider, ModelName: record.Model, Status: record.Status, DurationMs: record.DurationMs, InputChars: record.InputChars, OutputChars: record.OutputChars, ErrorCode: record.ErrorCode, ErrorMsg: observability.SafeError(errors.New(record.ErrorMsg))})
}
