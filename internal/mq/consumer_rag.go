package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/processingguard"
	"vid-lens/internal/repository"

	"github.com/segmentio/kafka-go"
)

func (c *Consumer) handleRAGIndex(ctx context.Context, msg kafka.Message) error {
	var payload RAGIndexPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析 RAG 索引消息失败: %w", err)
	}

	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询 RAG 索引任务失败: %w", err)
	}
	traceID := traceIDForTask(payload.TraceID, task)
	ctx = ContextWithTraceID(ctx, traceID)
	claim, err := c.claimTaskForMessage(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, payload.ClaimToken)
	if err != nil {
		return fmt.Errorf("获取 RAG processing lease 失败: %w", err)
	}
	switch claim.Outcome {
	case repository.TaskLeaseBusy:
		return fmt.Errorf("RAG processing lease 正由其他消费者持有")
	case repository.TaskLeaseStale, repository.TaskLeaseTerminal:
		return nil
	case repository.TaskLeaseAcquired:
	default:
		return fmt.Errorf("未知 RAG processing lease 状态: %s", claim.Outcome)
	}
	ctx, stopLease := c.startProcessingLeaseHeartbeat(ctx, task.ID, TaskJobRAGIndex, claim.Token)
	defer stopLease()
	task.TraceID = traceID
	ctx = c.contextForTaskJob(ctx, task, TaskJobRAGIndex, payload.BudgetID)

	transcription, err := c.repo.Transcription.FindByTaskID(task.ID)
	if err != nil {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, err, claim.Token)
	}
	if transcription == nil || strings.TrimSpace(transcription.Content) == "" {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("缺少转录文本，无法构建 RAG 索引"), claim.Token)
	}
	if c.ragIndex == nil {
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, fmt.Errorf("RAG 索引器未初始化"), claim.Token)
	}

	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index started")
	ctx = processingguard.With(ctx, requireProcessingLease)
	ragErr := c.ragIndex(ctx, task)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if ragErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "rag index failed", slog.String("error", observability.SafeError(ragErr)))
		return c.recordTaskFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, ragErr, claim.Token)
	}
	completed, err := c.completeTaskProcessing(repository.TaskProcessingCompleteRequest{
		TaskID: task.ID, JobType: TaskJobRAGIndex, JobStage: model.TaskStageIndexing, Token: claim.Token,
		TaskStatus: model.TaskStatusCompleted, TaskStage: model.TaskStageNone, Now: c.currentTime(),
	})
	if err != nil {
		return fmt.Errorf("完成 RAG processing lease 失败: %w", err)
	}
	if !completed {
		return nil
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index completed")
	return nil
}

func (c *Consumer) indexAfterTranscription(ctx context.Context, task *model.VideoTask) error {
	if c.ragProducer == nil {
		return nil
	}
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	var budgetID string
	if c.repo != nil && c.repo.TaskJob != nil {
		if err := c.repo.TaskJob.UpsertQueued(task, TaskJobRAGIndex, model.TaskStageIndexing, task.MaxRetries); err != nil {
			observability.Log(ctx, slog.Default(), slog.LevelError, "persist rag index job state failed", slog.String("error", observability.SafeError(err)))
			return fmt.Errorf("persist rag index job state: %w", err)
		}
		if err := requireProcessingLease(ctx); err != nil {
			return err
		}
		var err error
		budgetID, err = c.repo.EnsureTaskJobRetryBudget(task.ID, TaskJobRAGIndex, c.currentTime())
		if err != nil {
			return fmt.Errorf("persist rag index retry budget: %w", err)
		}
	}
	ctx = ContextWithRetryBudgetID(ContextWithTraceID(ctx, task.TraceID), budgetID)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	enqueueErr := c.ragProducer.EnqueueRAGIndex(ctx, task.ID)
	if err := requireProcessingLease(ctx); err != nil {
		return err
	}
	if enqueueErr != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "enqueue rag index failed", slog.String("error", observability.SafeError(enqueueErr)))
		c.recordRAGIndexEnqueueFailure(task, enqueueErr)

		owner := processingLeaseOwnerFromContext(ctx)
		if owner != nil && c.repo != nil {
			now := c.currentTime()
			policy := c.retryPolicy.normalized()
			nextRetryAt := now.Add(policy.backoffForRetry(1))
			currentStage := task.Stage
			if job, findErr := c.repo.TaskJob.FindByTaskAndType(task.ID, owner.jobType); findErr == nil && job != nil && job.Stage != "" {
				currentStage = job.Stage
			}
			updated, handoffErr := c.repo.FailTaskProcessingHandoff(repository.TaskProcessingHandoffFailureRequest{
				TaskID: task.ID, CurrentJobType: owner.jobType, CurrentStage: currentStage,
				NextJobType: TaskJobRAGIndex, NextStage: model.TaskStageIndexing, Token: owner.token,
				Status: model.TaskStatusFailed, ErrorCode: "enqueue_failed", ErrorMessage: truncateError(enqueueErr),
				RetryCount: 1, MaxRetries: policy.MaxRetries, NextRetryAt: &nextRetryAt, Now: now,
			})
			if handoffErr != nil {
				return fmt.Errorf("persist rag enqueue failure handoff: %w", handoffErr)
			}
			if !updated {
				return ErrProcessingLeaseLost
			}
		} else if c.repo != nil && c.repo.TaskJob != nil {
			// Compatibility for direct/non-consumer invocations: persist the child
			// failure, but only a processing owner may mutate the parent workflow.
			_ = c.repo.TaskJob.RecordTerminalFailure(task.ID, TaskJobRAGIndex, model.TaskStageIndexing, "enqueue_failed", truncateError(enqueueErr), 1, policyMaxRetries(c.retryPolicy), model.TaskStatusFailed)
		}
		return fmt.Errorf("enqueue rag index: %w", enqueueErr)
	}
	observability.Log(ctx, slog.Default(), slog.LevelInfo, "rag index enqueued")
	return nil
}

func policyMaxRetries(policy TaskRetryPolicy) int {
	return policy.normalized().MaxRetries
}

func (c *Consumer) recordRAGIndexEnqueueFailure(task *model.VideoTask, err error) {
	if c.repo == nil || c.repo.RAGIndex == nil || c.profiles == nil || task == nil {
		return
	}
	profile, profileErr := c.profiles.GetDefaultAIProfile(task.UserID)
	if profileErr != nil || profile == nil || strings.TrimSpace(profile.EmbeddingModel) == "" {
		return
	}
	now := time.Now()
	errMsg := truncateError(fmt.Errorf("RAG 索引任务投递失败: %w", err))
	_ = c.repo.RAGIndex.Upsert(&model.VideoRAGIndex{
		UserID:         task.UserID,
		TaskID:         task.ID,
		EmbeddingModel: profile.EmbeddingModel,
		EmbeddingDim:   profile.EmbeddingDim,
		Status:         model.RAGIndexStatusFailed,
		ChunkCount:     0,
		LastError:      errMsg,
		BuildVersion:   1,
		StartedAt:      &now,
		FinishedAt:     &now,
	})
}
