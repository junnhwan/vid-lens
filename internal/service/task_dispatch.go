package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/observability"
	"vid-lens/internal/repository"
)

const (
	initialDispatchLease          = 2 * time.Minute
	initialDispatchFailureBackoff = time.Minute
)

type initialDispatchSpec struct {
	createTask      bool
	allowedStatuses []int8
	jobType         string
	stage           string
	enqueue         func(context.Context, model.VideoTask) error
}

// enqueueInitialTask is the only service-level path for a first Kafka publish.
// The database transaction commits a recoverable dispatch intent first; Kafka
// success is finalized by the consumer's dispatch-to-processing lease handoff.
func (s *MediaService) enqueueInitialTask(ctx context.Context, task *model.VideoTask, spec initialDispatchSpec) (repository.InitialTaskDispatch, error) {
	if s == nil || s.repo == nil || s.mq == nil || spec.enqueue == nil {
		return repository.InitialTaskDispatch{}, fmt.Errorf("initial task dispatch dependencies are unavailable")
	}
	now := time.Now()
	prepared, err := s.repo.PrepareInitialTaskDispatch(repository.InitialTaskDispatchRequest{
		Task: task, CreateTask: spec.createTask, AllowedStatuses: spec.allowedStatuses,
		JobType: spec.jobType, Stage: spec.stage,
		Now: now, LeaseUntil: now.Add(initialDispatchLease), Token: uuid.NewString(),
	})
	if err != nil {
		return repository.InitialTaskDispatch{}, err
	}

	enqueueCtx := mq.ContextWithClaimToken(
		mq.ContextWithRetryBudgetID(
			mq.ContextWithTraceID(ctx, prepared.Task.TraceID),
			prepared.RetryBudgetID,
		),
		prepared.Token,
	)
	if err := spec.enqueue(enqueueCtx, prepared.Task); err != nil {
		nextRetryAt := time.Now().Add(initialDispatchFailureBackoff)
		restored, restoreErr := s.repo.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{
			TaskID: prepared.Task.ID, JobType: spec.jobType, Stage: spec.stage,
			Token: prepared.Token, ErrorMessage: initialDispatchErrorMessage(err), NextRetryAt: nextRetryAt,
		})
		var ownershipErr error
		if restoreErr == nil && !restored {
			ownershipErr = errors.New("initial dispatch lease changed before failure recovery")
		}
		return prepared, errors.Join(
			fmt.Errorf("publish initial %s task: %w", spec.jobType, err),
			wrapInitialDispatchRestoreError(restoreErr),
			ownershipErr,
		)
	}
	return prepared, nil
}

// publicInitialDispatchError keeps transport-facing errors stable while the
// structured log retains enough safe context to diagnose publish or recovery
// failures. Callers must not wrap the returned sentinel with the internal error.
func publicInitialDispatchError(ctx context.Context, task model.VideoTask, jobType, stage string, err error) error {
	logCtx := observability.WithCorrelation(ctx, observability.Correlation{
		TraceID: task.TraceID,
		TaskID:  task.ID,
		UserID:  task.UserID,
		JobType: jobType,
		Stage:   stage,
	})
	observability.Log(logCtx, slog.Default(), slog.LevelError, "initial task dispatch failed",
		slog.String("error", observability.SafeError(err)))
	return ErrTaskDispatchUnavailable
}

func initialDispatchErrorMessage(err error) string {
	const maxLength = 500
	message := "Kafka 初次投递失败"
	if err != nil {
		message = err.Error()
	}
	if len(message) > maxLength {
		message = message[:maxLength]
	}
	return message
}

func wrapInitialDispatchRestoreError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore initial dispatch after publish failure: %w", err)
}
