package mq

import (
	"context"
	"fmt"

	"vid-lens/internal/model"
	"vid-lens/internal/pkg/processingguard"
	"vid-lens/internal/repository"
)

// handleVisualBuild reuses the media lease and indexing queue without invoking ASR.
func (c *Consumer) handleVisualBuild(ctx context.Context, payload RAGIndexPayload) error {
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return err
	}
	ctx = ContextWithTraceID(ctx, traceIDForTask(payload.TraceID, task))
	stage := model.TaskStageVisual
	// A retry after publication resumes embedding rather than paying for OCR/VLM again.
	// An explicit new build resets the child job's stage and retry count at dispatch.
	job, err := c.repo.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeVisual)
	if err != nil {
		return err
	}
	if job != nil && job.Stage == model.TaskStageIndexing {
		stage = model.TaskStageIndexing
	}
	claim, err := c.claimTaskForMessage(task.ID, model.TaskJobTypeVisual, stage, payload.ClaimToken)
	if err != nil {
		return err
	}
	switch claim.Outcome {
	case repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseStale:
		return errStaleDispatch
	case repository.TaskLeaseBusy:
		return fmt.Errorf("画面构建正由其他消费者处理")
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知画面构建租约状态")
	}
	ctx, stop := c.startProcessingLeaseHeartbeat(ctx, task.ID, model.TaskJobTypeVisual, claim.Token)
	defer stop()
	ctx = c.contextForTaskJob(ctx, task, model.TaskJobTypeVisual, payload.BudgetID)
	ctx = processingguard.With(ctx, requireProcessingLease)
	fail := func(err error) error {
		return c.recordTaskFailure(task.ID, model.TaskJobTypeVisual, stage, err, claim.Token)
	}
	if c.visualIndex == nil || task.EffectiveVisualMode() == model.VisualModeOff {
		return fail(fmt.Errorf("画面处理未启用"))
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if stage == model.TaskStageVisual {
		outcome := c.startVisualIndexBranch(ctx, task)()
		if err := requireProcessingLease(ctx); err != nil {
			return err
		}
		if outcome.err != nil {
			return fail(outcome.err)
		}
		if outcome.count == 0 {
			return fail(fmt.Errorf("未生成可用画面证据，请确认所选 OCR 或视觉模型已配置且视频有可识别内容"))
		}
	}
	if c.ragIndex != nil {
		if err := c.transitionTaskStage(ctx, task.ID, model.TaskStageIndexing); err != nil {
			return err
		}
		stage = model.TaskStageIndexing
		if err := c.ragIndex(ctx, task); err != nil {
			return fail(err)
		}
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	_, err = c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: model.TaskJobTypeVisual, JobStage: model.TaskStageVisual, Token: claim.Token,
		TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime(),
	})
	return err
}
