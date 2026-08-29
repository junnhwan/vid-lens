package mq

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"

	"github.com/google/uuid"
)

func (c *Consumer) currentTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *Consumer) claimTaskForMessage(taskID int64, jobType, stage, messageToken string) (repository.TaskLeaseClaim, error) {
	if c == nil || c.repo == nil {
		return repository.TaskLeaseClaim{}, fmt.Errorf("任务仓储未初始化")
	}
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	lease := c.processingLease
	if lease <= 0 {
		lease = 30 * time.Minute
	}
	newToken := uuid.NewString()
	if c.newToken != nil {
		newToken = c.newToken()
	}
	return c.repo.ClaimTaskProcessing(repository.TaskProcessingClaimRequest{
		TaskID: taskID, JobType: jobType, Stage: stage, MessageToken: messageToken,
		Now: now, LeaseUntil: now.Add(lease), NewToken: newToken,
	})
}

func truncateError(err error) string {
	errMsg := err.Error()
	if len(errMsg) > 500 {
		return errMsg[:500]
	}
	return errMsg
}

func traceIDForTask(payloadTraceID string, task *model.VideoTask) string {
	if payloadTraceID != "" {
		return payloadTraceID
	}
	if task != nil {
		return task.TraceID
	}
	return ""
}

func (c *Consumer) contextForTaskJob(ctx context.Context, task *model.VideoTask, jobType string, payloadBudgetID ...string) context.Context {
	if c == nil || c.repo == nil || c.repo.TaskJob == nil || task == nil {
		return contextForTaskJob(ctx, task, nil)
	}
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, jobType)
	if err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "load task job correlation failed",
			slog.String("job_type", jobType), slog.String("error", observability.SafeError(err)))
		return contextForTaskJob(ctx, task, nil)
	}
	ctx = contextForTaskJob(ctx, task, job)
	budgetID := ""
	if job != nil {
		budgetID = strings.TrimSpace(job.RetryBudgetID)
	}
	payloadID := ""
	if len(payloadBudgetID) > 0 {
		payloadID = strings.TrimSpace(payloadBudgetID[0])
	}
	if budgetID == "" {
		budgetID = payloadID
	} else if payloadID != "" && payloadID != budgetID {
		// The database binding is authoritative. A stale Kafka delivery must not
		// replace the retry cycle currently owned by the task job.
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "stale retry budget in Kafka payload ignored",
			slog.String("job_type", jobType))
	}
	return ai.WithGovernanceContext(ctx, ai.GovernanceContext{
		RetryBudgetID: budgetID,
		Subject:       fmt.Sprintf("user:%d", task.UserID),
	})
}

func (c *Consumer) analyzeInitialStage(task *model.VideoTask) string {
	if task == nil {
		return model.TaskStageTranscribing
	}
	if task.Stage == model.TaskStageTranscribing || task.Stage == model.TaskStageSummarizing {
		return task.Stage
	}
	if c != nil && c.repo != nil && c.repo.Transcription != nil {
		if transcription, err := c.repo.Transcription.FindByTaskID(task.ID); err == nil && transcription != nil && strings.TrimSpace(transcription.Content) != "" {
			return model.TaskStageSummarizing
		}
	}
	return model.TaskStageTranscribing
}

func (c *Consumer) transitionTaskStage(ctx context.Context, taskID int64, nextStage string) error {
	if c == nil || c.repo == nil || c.repo.Task == nil {
		return fmt.Errorf("任务仓储未初始化")
	}
	task, err := c.repo.Task.FindByID(taskID)
	if err != nil {
		return err
	}
	if task.Stage == nextStage {
		return nil
	}
	finishedAt := c.currentTime()
	startedAt := finishedAt
	if task.StageStartedAt != nil {
		startedAt = *task.StageStartedAt
	}
	if err := c.runLeasedSideEffect(ctx, func(repos *repository.Repositories) error {
		return repos.Task.UpdateStatusAndStage(taskID, model.TaskStatusRunning, nextStage, "")
	}); err != nil {
		return err
	}
	if task.Stage != "" && task.Stage != model.TaskStageNone && task.Stage != model.TaskStageUploaded {
		if startedAt.After(finishedAt) {
			startedAt = finishedAt
		}
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveTaskStage(task.Stage, "success", finishedAt.Sub(startedAt))
		}
	}
	return nil
}

func (c *Consumer) completeTaskProcessing(req repository.TaskProcessingCompleteRequest) (bool, error) {
	if c == nil || c.repo == nil {
		return false, fmt.Errorf("任务仓储未初始化")
	}
	startedAt := req.Now
	if task, err := c.repo.Task.FindByID(req.TaskID); err == nil && task != nil && task.StageStartedAt != nil {
		startedAt = *task.StageStartedAt
	}
	completed, err := c.repo.CompleteTaskProcessing(req)
	if err != nil || !completed {
		return completed, err
	}
	finishedAt := req.Now
	if finishedAt.IsZero() {
		finishedAt = c.currentTime()
	}
	if startedAt.IsZero() || startedAt.After(finishedAt) {
		startedAt = finishedAt
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		metrics.ObserveTaskStage(req.JobStage, "success", finishedAt.Sub(startedAt))
	}
	return true, nil
}
