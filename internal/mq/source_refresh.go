package mq

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

func sourceRefreshSnapshot(task *model.VideoTask, job *model.TaskJob) (*processing.SourceRefreshSnapshot, error) {
	if job == nil || job.InputSnapshotJSON == "" {
		return nil, nil
	}
	var frozen processing.SourceRefreshSnapshot
	if json.Unmarshal([]byte(job.InputSnapshotJSON), &frozen) != nil {
		return nil, artifact.Err("invalid_source_refresh_snapshot", 409)
	}
	if frozen.Operation != processing.OperationSourceRefresh {
		return nil, nil
	}
	if _, err := processing.Decode(artifact.JSON(frozen.Intent)); err != nil {
		return nil, artifact.Err("invalid_source_refresh_snapshot", 409)
	}
	if job.GenerationID != frozen.Intent.GenerationID || task.ProcessingIntentJSON != artifact.JSON(frozen.Intent) || job.InputSourceID != frozen.ExpectedActiveSourceID || task.ActiveTextSourceID != frozen.ExpectedActiveSourceID {
		return nil, artifact.Err("source_refresh_changed", 409)
	}
	return &frozen, nil
}

func (c *Consumer) unchangedSourceRefresh(ctx context.Context, tx *repository.Repositories, task *model.VideoTask, intent processing.Intent, source *model.VideoTextSource, kind string) (bool, error) {
	job, err := tx.TaskJob.FindByTaskAndType(task.ID, kind)
	if err != nil {
		return false, err
	}
	// The source pointer may already have advanced inside publication; only the
	// accepted expected pointer and stable input fingerprint decide this reuse.
	if job == nil || job.InputSnapshotJSON == "" {
		return false, nil
	}
	var frozen processing.SourceRefreshSnapshot
	if json.Unmarshal([]byte(job.InputSnapshotJSON), &frozen) != nil {
		return false, artifact.Err("invalid_source_refresh_snapshot", 409)
	}
	if frozen.Operation != processing.OperationSourceRefresh {
		return false, nil
	}
	if frozen.Intent.GenerationID != intent.GenerationID || frozen.ExpectedActiveSourceID != source.ID || frozen.PreviousInputFingerprint != processing.SourceSummaryInputFingerprint(intent) {
		return false, nil
	}
	base, err := tx.Summary.FindByTaskID(task.ID)
	if err != nil {
		return false, err
	}
	return base != nil && base.SourceID == source.ID && base.SourceDigest == source.SourceDigest && base.DocumentJSON != "", nil
}

func (c *transcriptionWorkflow) sourceASRAttempt(ctx context.Context, taskID int64) (string, error) {
	if taskID <= 0 || c.repo == nil || c.repo.Task == nil {
		return "", nil
	}
	task, err := c.repo.Task.FindByID(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	} // Retains legacy/test-only window workflows.
	if task.ProcessingIntentJSON == "" {
		return "", nil
	}
	intent, err := processing.Decode(task.ProcessingIntentJSON)
	if err != nil {
		return "", err
	}
	job, err := c.repo.TaskJob.FindByTaskAndType(taskID, model.TaskJobTypeTranscribe)
	if err != nil {
		return "", err
	}
	frozen, err := sourceRefreshSnapshot(task, job)
	if err != nil {
		return "", err
	}
	if frozen == nil || frozen.ReuseUnscopedASRWindows {
		return "", nil
	}
	if frozen != nil && frozen.ASRCheckpointGenerationID != "" {
		return frozen.ASRCheckpointGenerationID, nil
	}
	return intent.GenerationID, nil
}
