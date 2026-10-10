package repository

import (
	"context"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
)

type PrepareSourceRefreshRequest struct {
	UserID, TaskID                                         int64
	ExpectedSourceID, ExpectedIntentJSON, MediaFingerprint string
	Snapshot                                               processing.SourceRefreshSnapshot
	Token                                                  string
	Now, LeaseUntil                                        time.Time
}

func (r *Repositories) PrepareSourceRefresh(ctx context.Context, req PrepareSourceRefreshRequest) (InitialTaskDispatch, error) {
	var out InitialTaskDispatch
	if req.Snapshot.Operation != processing.OperationSourceRefresh || req.Snapshot.ExpectedActiveSourceID != req.ExpectedSourceID {
		return out, artifact.Err("invalid_source_refresh", 400)
	}
	if _, err := processing.Decode(artifact.JSON(req.Snapshot.Intent)); err != nil {
		return out, artifact.Err("invalid_generation_snapshot", 400)
	}
	err := r.TransactionContext(ctx, func(tx *Repositories) error {
		task, err := summaryTask(tx.db, req.UserID, req.TaskID, true)
		if err != nil {
			return err
		}
		if task.ActiveTextSourceID != req.ExpectedSourceID || task.ProcessingIntentJSON != req.ExpectedIntentJSON || task.FileMD5 != req.MediaFingerprint {
			return artifact.Err("source_changed", 409)
		}
		if task.FileURL == "" || task.FileMD5 == "" {
			return artifact.Err("media_not_ready", 422)
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			return artifact.Err("task_processing_active", 409)
		}
		summaryJob, err := tx.TaskJob.FindByTaskAndType(task.ID, model.TaskJobTypeSummary)
		if err != nil {
			return err
		}
		if SummaryJobActive(summaryJob) {
			return artifact.Err("summary_generation_active", 409)
		}
		if req.ExpectedSourceID != "" {
			if _, err = tx.TextSource.Active(ctx, req.UserID, req.TaskID); err != nil {
				return err
			}
		}
		kind, stage := model.TaskJobTypeTextSource, model.TaskStageTextSource
		if req.Snapshot.Intent.Options.TextSourcePolicy == "force_asr" {
			kind, stage = model.TaskJobTypeTranscribe, model.TaskStageTranscribing
		}
		out, err = tx.PrepareInitialTaskDispatch(InitialTaskDispatchRequest{Task: task, AllowedStatuses: []int8{model.TaskStatusPending, model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusDead}, JobType: kind, Stage: stage, Token: req.Token, Now: req.Now, LeaseUntil: req.LeaseUntil})
		if err != nil {
			return err
		}
		if err = tx.FreezeSourceRefreshJob(ctx, task.ID, kind, req.Snapshot); err != nil {
			return err
		}
		raw := artifact.JSON(req.Snapshot.Intent)
		if err = tx.db.Model(task).Update("processing_intent_json", raw).Error; err != nil {
			return err
		}
		out.Task.ProcessingIntentJSON = raw
		return nil
	})
	return out, err
}

func (r *Repositories) FreezeSourceRefreshJob(ctx context.Context, taskID int64, kind string, frozen processing.SourceRefreshSnapshot) error {
	if kind != model.TaskJobTypeTextSource && kind != model.TaskJobTypeTranscribe {
		return artifact.Err("invalid_source_job", 400)
	}
	result := r.db.WithContext(ctx).Model(&model.TaskJob{}).Where("task_id=? AND job_type=?", taskID, kind).Updates(map[string]any{"generation_id": frozen.Intent.GenerationID, "input_source_id": frozen.ExpectedActiveSourceID, "input_snapshot_json": artifact.JSON(frozen)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return artifact.Err("source_job_missing", 409)
	}
	return nil
}

func (r *Repositories) RecordUnchangedSourceRefresh(ctx context.Context, taskID int64, kind, gen string) error {
	result := r.db.WithContext(ctx).Model(&model.TaskJob{}).Where("task_id=? AND job_type=? AND generation_id=? AND status=? AND processing_token=''", taskID, kind, gen, model.TaskStatusCompleted).Updates(map[string]any{"last_error_code": "source_unchanged", "last_error_msg": "source and accepted processing configuration unchanged; retained existing summary"})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return artifact.Err("source_job_changed", 409)
	}
	return nil
}
