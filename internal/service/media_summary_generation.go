package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
	"vid-lens/internal/textsource"
)

func (s *MediaService) requestSourceSummary(ctx context.Context, task *model.VideoTask, force bool) error {
	if task.ActiveTextSourceID == "" {
		return artifact.Err("source_not_ready", 422)
	}
	if s.mq == nil {
		return ErrTaskDispatchUnavailable
	}
	job, err := s.repo.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
	if err != nil {
		return err
	}
	if repository.SummaryJobActive(job) {
		return artifact.Err("summary_generation_active", 409)
	}
	if !force {
		base, findErr := s.repo.Summary.FindByTaskID(task.ID)
		if findErr != nil {
			return findErr
		}
		if base != nil {
			return artifact.Err("summary_available", 409)
		}
	}
	source, err := s.repo.TextSource.Read(ctx, task.UserID, task.ID, task.ActiveTextSourceID)
	if err != nil {
		return err
	}
	options := processing.Options{AutoSummary: true}
	if task.ProcessingIntentJSON != "" {
		prior, err := processing.Decode(task.ProcessingIntentJSON)
		if err != nil {
			return artifact.Err("invalid_generation_snapshot", 409)
		}
		options = prior.Options
		options.ProfileID = prior.ProfileID
	} else if source.Kind == textsource.KindASR {
		options.TextSourcePolicy = "force_asr"
	}
	options.AutoSummary = true
	options, err = processing.Normalize(options, false)
	if err != nil {
		return artifact.Err("invalid_processing_options", 400)
	}
	request := &preparedImport{options: options}
	if err = s.freezeImport(task.UserID, request); err != nil {
		return err
	}
	now := time.Now()
	prepared, err := s.repo.PrepareManualSummaryGeneration(ctx, repository.ManualSummaryGenerationRequest{UserID: task.UserID, TaskID: task.ID, SourceID: source.ID, SourceDigest: source.SourceDigest, MediaFingerprint: task.FileMD5, ExpectedIntentJSON: task.ProcessingIntentJSON, Intent: request.intent, Force: force, Token: uuid.NewString(), Now: now, LeaseUntil: now.Add(initialDispatchLease)})
	if errors.Is(err, repository.ErrSummaryAvailable) {
		return artifact.Err("summary_available", 409)
	}
	if errors.Is(err, repository.ErrInitialTaskDispatchConflict) {
		return artifact.Err("summary_generation_active", 409)
	}
	if err != nil {
		return err
	}
	enqueueCtx := mq.ContextWithClaimToken(mq.ContextWithRetryBudgetID(mq.ContextWithTraceID(ctx, prepared.Task.TraceID), prepared.RetryBudgetID), prepared.Token)
	if err = s.mq.EnqueueSummary(enqueueCtx, prepared.Task.ID, prepared.Task.FileMD5); err != nil {
		_, restoreErr := s.repo.RestoreRetryDispatch(repository.TaskDispatchRestoreRequest{TaskID: task.ID, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, Token: prepared.Token, NextRetryAt: time.Now().Add(initialDispatchFailureBackoff), ErrorMessage: initialDispatchErrorMessage(err)})
		return publicInitialDispatchError(ctx, prepared.Task, model.TaskJobTypeSummary, model.TaskStageSummarizing, errors.Join(err, restoreErr))
	}
	return nil
}
