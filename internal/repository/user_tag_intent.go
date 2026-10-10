package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type PrepareTagIntentRequest = PublishTagCandidatesRequest

// PrepareTagIntent must be called on the transaction-scoped UserTag repository
// that publishes the summary. It freezes recipe output without touching names.
func (r *UserTagRepository) PrepareTagIntent(ctx context.Context, req PrepareTagIntentRequest) (*model.SummaryTagIntent, error) {
	if req.GenerationID == "" || req.SourceDigest == "" || req.GeneratedVersion <= 0 || req.ExpectedTagVersion < 0 || len(req.Candidates) > r.limits.Candidates {
		return nil, artifact.Err("invalid_request", 400)
	}
	tx := r.db.WithContext(ctx)
	if _, err := currentTagGeneration(tx, req.UserID, req.TaskID, req.SourceDigest, req.GenerationID, req.GeneratedVersion); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := "pending"
	if !req.Enabled {
		status = "completed"
	}
	row := model.SummaryTagIntent{UserID: req.UserID, TaskID: req.TaskID, GenerationID: req.GenerationID, GeneratedVersion: req.GeneratedVersion, SourceDigest: req.SourceDigest, ExpectedTagVersion: req.ExpectedTagVersion, Enabled: req.Enabled, CandidatesJSON: artifact.JSON(req.Candidates), CandidateDigest: artifact.Hash(artifact.JSON(req.Candidates)), Status: status, CreatedAt: now, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	var prior model.SummaryTagIntent
	if err := tx.Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=?", req.UserID, req.TaskID, req.GenerationID, req.GeneratedVersion).First(&prior).Error; err != nil {
		return nil, err
	}
	if prior.SourceDigest != row.SourceDigest || prior.CandidateDigest != row.CandidateDigest || prior.Enabled != row.Enabled || prior.ExpectedTagVersion != row.ExpectedTagVersion {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	if err := tx.Model(&model.SummaryTagIntent{}).Where("user_id=? AND task_id=? AND (generation_id<>? OR generated_version<?) AND status IN ?", req.UserID, req.TaskID, req.GenerationID, req.GeneratedVersion, []string{"pending", "failed"}).Updates(map[string]any{"status": "cancelled", "error_code": "superseded", "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return &prior, nil
}

func tagWorkerFence(tx *gorm.DB, req PublishTagCandidatesRequest, allowCancelled ...bool) error {
	if req.LeaseToken == "" {
		return artifact.Err("generation_stale", 409)
	}
	var job model.TaskJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id=? AND user_id=? AND job_type=?", req.TaskID, req.UserID, model.TaskJobTypeSummary).First(&job).Error; err != nil {
		return artifact.Err("generation_stale", 409)
	}
	if job.GenerationID != req.GenerationID || job.Status != model.TaskStatusRunning || !ownsSummaryJob(&job, req.LeaseToken, model.TaskLeaseKindProcessing, time.Now()) {
		return artifact.Err("generation_stale", 409)
	}
	var run model.AgentRun
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND task_id=? AND subject_kind=? AND subject_id=?", req.GenerationID, req.UserID, req.TaskID, model.AgentRunSubjectSummaryGeneration, req.GenerationID).First(&run).Error; err != nil {
		return artifact.Err("generation_stale", 409)
	}
	if (len(allowCancelled) == 0 || !allowCancelled[0]) && (run.CancelRequestedAt != nil || (run.Status != model.AgentRunStatusRunning && run.Status != model.AgentRunStatusCompleted)) {
		return artifact.Err("generation_cancelled", 409)
	}
	return nil
}

// PublishPendingCandidates uses only the frozen durable candidates. All worker
// acknowledgments, classification receipts and tag mutations share one commit.
func (r *UserTagRepository) PublishPendingCandidates(ctx context.Context, req PublishTagCandidatesRequest) (TaskTagState, error) {
	var out TaskTagState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockVocabulary(tx, req.UserID); err != nil {
			return err
		}
		if _, err := currentTagGeneration(tx, req.UserID, req.TaskID, req.SourceDigest, req.GenerationID, req.GeneratedVersion); err != nil {
			return err
		}
		if err := tagWorkerFence(tx, req); err != nil {
			return err
		}
		var intent model.SummaryTagIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=?", req.UserID, req.TaskID, req.GenerationID, req.GeneratedVersion).First(&intent).Error; err != nil {
			return hideMissing(err)
		}
		if intent.SourceDigest != req.SourceDigest || intent.ExpectedTagVersion != req.ExpectedTagVersion {
			return artifact.Err("tag_generation_stale", 409)
		}
		if intent.Status == "completed" {
			var err error
			out, err = r.taskState(tx, req.UserID, req.TaskID)
			return err
		}
		if intent.Status != "pending" && intent.Status != "failed" {
			return artifact.Err("generation_cancelled", 409)
		}
		if err := json.Unmarshal([]byte(intent.CandidatesJSON), &req.Candidates); err != nil || artifact.Hash(intent.CandidatesJSON) != intent.CandidateDigest {
			return artifact.Err("invalid_tag_intent", 409)
		}
		req.Enabled = intent.Enabled
		bound := NewUserTagRepository(tx, r.limits)
		var err error
		out, err = bound.PublishCandidates(ctx, req)
		if err != nil {
			return err
		}
		if err := tx.Model(&intent).Updates(map[string]any{"status": "completed", "error_code": "", "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		out, err = bound.taskState(tx, req.UserID, req.TaskID)
		return err
	})
	return out, err
}

// MarkTagIntentFailed preserves the already published summary and leaves a
// bounded resumable failure receipt; cancellation uses the same fenced write.
func (r *UserTagRepository) MarkTagIntentFailed(ctx context.Context, req PublishTagCandidatesRequest, code string, cancelled bool) error {
	if len(code) > 80 {
		return artifact.Err("invalid_request", 400)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := currentTagGeneration(tx, req.UserID, req.TaskID, req.SourceDigest, req.GenerationID, req.GeneratedVersion); err != nil {
			return err
		}
		// Cancellation can be requested on the run while the job still owns its lease.
		if err := tagWorkerFence(tx, req, cancelled); err != nil {
			return err
		}
		status := "failed"
		if cancelled {
			status = "cancelled"
		}
		res := tx.Model(&model.SummaryTagIntent{}).Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=? AND source_digest=? AND expected_tag_version=? AND status IN ?", req.UserID, req.TaskID, req.GenerationID, req.GeneratedVersion, req.SourceDigest, req.ExpectedTagVersion, []string{"pending", "failed"}).Updates(map[string]any{"status": status, "error_code": code, "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return artifact.Err("tag_generation_stale", 409)
		}
		return nil
	})
}

// ReadGenerationIntent returns only a still-current owner's durable intent.
func (r *UserTagRepository) ReadGenerationIntent(ctx context.Context, owner, taskID int64, generation string, version int64) (*model.SummaryTagIntent, error) {
	var row model.SummaryTagIntent
	err := r.db.WithContext(ctx).Where("user_id=? AND task_id=? AND generation_id=? AND generated_version=?", owner, taskID, generation, version).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = currentTagGeneration(r.db.WithContext(ctx), owner, taskID, row.SourceDigest, generation, version); err != nil {
		return nil, err
	}
	return &row, nil
}
