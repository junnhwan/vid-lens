package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func (c *Consumer) handleTranscriptSummary(ctx context.Context, payload AnalyzePayload) error {
	claim, err := c.claimTaskForMessage(payload.TaskID, model.TaskJobTypeSummary, model.TaskStageSummarizing, payload.ClaimToken)
	if err != nil {
		return err
	}
	if claim.Outcome == repository.TaskLeaseStale || claim.Outcome == repository.TaskLeaseTerminal {
		return nil
	}
	if claim.Outcome != repository.TaskLeaseAcquired {
		return fmt.Errorf("摘要正在处理中")
	}
	ctx, stop := c.startProcessingLeaseHeartbeat(ctx, payload.TaskID, model.TaskJobTypeSummary, claim.Token)
	defer stop()
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}
	ctx = c.contextForTaskJob(ctx, task, model.TaskJobTypeSummary, payload.BudgetID)
	if err := c.summarizeTask(ctx, task); err != nil {
		return c.recordTaskFailure(task.ID, model.TaskJobTypeSummary, model.TaskStageSummarizing, err, claim.Token)
	}
	_, err = c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{TaskID: task.ID, JobType: model.TaskJobTypeSummary, JobStage: model.TaskStageSummarizing, Token: claim.Token, TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime()})
	return err
}

func (c *Consumer) summarySourceChunks(ctx context.Context, taskID int64) ([]model.VideoTranscriptionChunk, error) {
	if owner := processingLeaseOwnerFromContext(ctx); owner != nil && owner.jobType == model.TaskJobTypeSummary {
		job, err := c.repo.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeSummary)
		if err != nil {
			return nil, err
		}
		if job == nil {
			return nil, fmt.Errorf("摘要输入快照不存在")
		}
		var rows []model.VideoTranscriptionChunk
		if job.InputChunksJSON != "" {
			err = json.Unmarshal([]byte(job.InputChunksJSON), &rows)
		}
		return rows, err
	}
	return c.repo.TranscriptionChunk.ListByTaskID(taskID)
}
