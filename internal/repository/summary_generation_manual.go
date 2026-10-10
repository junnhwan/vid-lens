package repository

import (
	"context"
	"time"

	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

type ManualSummaryGenerationRequest struct {
	UserID, TaskID                                               int64
	SourceID, SourceDigest, MediaFingerprint, ExpectedIntentJSON string
	Intent                                                       processing.Intent
	Force                                                        bool
	Token                                                        string
	Now, LeaseUntil                                              time.Time
}

// Explicit regeneration accepts a new immutable generation; scheduled retries
// only redispatch their existing frozen job and never call this method.
func (r *Repositories) PrepareManualSummaryGeneration(ctx context.Context, req ManualSummaryGenerationRequest) (InitialTaskDispatch, error) {
	var prepared InitialTaskDispatch
	if _, err := processing.Decode(artifact.JSON(req.Intent)); err != nil {
		return prepared, artifact.Err("invalid_generation_snapshot", 400)
	}
	err := r.TransactionContext(ctx, func(tx *Repositories) error {
		task, err := summaryTask(tx.db, req.UserID, req.TaskID, true)
		if err != nil {
			return err
		}
		if task.FileMD5 != req.MediaFingerprint || task.ProcessingIntentJSON != req.ExpectedIntentJSON {
			return artifact.Err("generation_changed", 409)
		}
		source, err := tx.TextSource.LockSource(ctx, req.UserID, req.TaskID, req.SourceID, req.SourceDigest)
		if err != nil {
			return err
		}
		job, err := tx.TaskJob.FindByTaskAndType(req.TaskID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if SummaryJobActive(job) {
			return artifact.Err("summary_generation_active", 409)
		}
		base, err := tx.Summary.FindByTaskID(task.ID)
		if err != nil {
			return err
		}
		snapshot := processing.GenerationSnapshot{Intent: req.Intent, SourceID: source.ID, SourceDigest: source.SourceDigest, ExpectedGeneratedHashKind: model.SummaryHashMarkdown}
		if base != nil {
			snapshot.ExpectedGeneratedVersion = base.GeneratedVersion
			snapshot.ExpectedGeneratedHashKind = base.ContentHashKind
			if snapshot.ExpectedGeneratedHashKind == "" {
				snapshot.ExpectedGeneratedHashKind = model.SummaryHashMarkdown
			}
			snapshot.ExpectedGeneratedHash = base.ContentDigest
			if snapshot.ExpectedGeneratedHash == "" && snapshot.ExpectedGeneratedHashKind == model.SummaryHashMarkdown {
				snapshot.ExpectedGeneratedHash = artifact.Hash(base.Content)
			}
		}
		prepared, err = tx.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusDead}, JobType: model.TaskJobTypeSummary, Stage: model.TaskStageSummarizing, SummaryForce: req.Force, Token: req.Token, Now: req.Now, LeaseUntil: req.LeaseUntil})
		if err != nil {
			return err
		}
		if err = tx.TaskJob.FreezeSummaryInput(task.ID, req.Intent.GenerationID, source.ID, artifact.JSON(snapshot)); err != nil {
			return err
		}
		raw := artifact.JSON(req.Intent)
		if err = tx.db.Model(task).Update("processing_intent_json", raw).Error; err != nil {
			return err
		}
		prepared.Task.ProcessingIntentJSON = raw
		return nil
	})
	return prepared, err
}
