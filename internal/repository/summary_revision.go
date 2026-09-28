package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

type EffectiveSummary struct {
	Generated            *model.AISummary
	Revision             *model.SummaryRevision
	Version              int64
	Content              string
	BaseHash             string
	CurrentGeneratedHash string
	SourceStatus         string
}

type SummaryRevisionRepository struct{ db *gorm.DB }

func NewSummaryRevisionRepository(db *gorm.DB) *SummaryRevisionRepository {
	return &SummaryRevisionRepository{db: db}
}

func summaryTask(tx *gorm.DB, owner, taskID int64, lock bool) (*model.VideoTask, error) {
	var task model.VideoTask
	if lock {
		tx = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := tx.Where("id = ? AND user_id = ?", taskID, owner).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, artifact.Err("not_found", 404)
		}
		return nil, err
	}
	return &task, nil
}

func generatedSummary(tx *gorm.DB, task *model.VideoTask) (*model.AISummary, error) {
	var row model.AISummary
	err := tx.Where("task_id = ?", task.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && task.FileMD5 != "" {
		err = tx.Where("file_md5 = ?", task.FileMD5).First(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func effectiveSummary(tx *gorm.DB, task *model.VideoTask) (*EffectiveSummary, error) {
	generated, err := generatedSummary(tx, task)
	if err != nil {
		return nil, err
	}
	out := &EffectiveSummary{Generated: generated, SourceStatus: "current"}
	if generated != nil {
		out.Content = generated.Content
		out.CurrentGeneratedHash = artifact.Hash(generated.Content)
		out.BaseHash = out.CurrentGeneratedHash
	}
	var head model.SummaryRevisionHead
	err = tx.Where("user_id = ? AND task_id = ?", task.UserID, task.ID).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var revision model.SummaryRevision
	if err = tx.Where("id = ? AND user_id = ? AND task_id = ?", head.CurrentRevisionID, task.UserID, task.ID).First(&revision).Error; err != nil {
		return nil, err
	}
	out.Revision, out.Version, out.Content, out.BaseHash = &revision, head.Version, revision.Content, revision.BaseGeneratedHash
	if generated == nil {
		out.SourceStatus = "generated_missing"
	} else if out.BaseHash != out.CurrentGeneratedHash {
		out.SourceStatus = "needs_merge"
	}
	return out, nil
}

func (r *SummaryRevisionRepository) Effective(ctx context.Context, owner, taskID int64) (*EffectiveSummary, error) {
	var out *EffectiveSummary
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := summaryTask(tx, owner, taskID, false)
		if err != nil {
			return err
		}
		out, err = effectiveSummary(tx, task)
		return err
	})
	return out, err
}

// Presence keeps lightweight task lists consistent even when a generated
// cache row is temporarily unavailable. The caller supplies already
// owner-scoped, non-deleted task IDs.
func (r *SummaryRevisionRepository) Presence(ctx context.Context, owner int64, taskIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	var heads []model.SummaryRevisionHead
	if err := r.db.WithContext(ctx).Where("user_id = ? AND task_id IN ?", owner, taskIDs).Find(&heads).Error; err != nil {
		return nil, err
	}
	for _, head := range heads {
		out[head.TaskID] = true
	}
	return out, nil
}

func (r *SummaryRevisionRepository) Operation(ctx context.Context, owner, taskID int64, id string) (*model.SummaryEditOperation, error) {
	if _, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false); err != nil {
		return nil, err
	}
	var op model.SummaryEditOperation
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ? AND task_id = ?", id, owner, taskID).First(&op).Error; err != nil {
		return nil, hideMissing(err)
	}
	return &op, nil
}

func (r *SummaryRevisionRepository) LatestOperation(ctx context.Context, owner, taskID int64) (*model.SummaryEditOperation, error) {
	if _, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false); err != nil {
		return nil, err
	}
	var op model.SummaryEditOperation
	err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ?", owner, taskID).Order("created_at DESC").First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *SummaryRevisionRepository) OperationByKey(ctx context.Context, owner int64, key, hash string) (*model.SummaryEditOperation, error) {
	var op model.SummaryEditOperation
	err := r.db.WithContext(ctx).Where("user_id = ? AND key = ?", owner, key).First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if op.RequestHash != hash {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	if _, err = summaryTask(r.db.WithContext(ctx), owner, op.TaskID, false); err != nil {
		return nil, err
	}
	return &op, nil
}

// Begin records the frozen target before the provider call. A retry with the
// same key reads this row; a conflicting key cannot borrow its run identity.
func (r *SummaryRevisionRepository) Begin(ctx context.Context, op *model.SummaryEditOperation, run *model.AgentRun) (*model.SummaryEditOperation, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := summaryTask(tx, op.UserID, op.TaskID, true)
		if err != nil {
			return err
		}
		var prior model.SummaryEditOperation
		err = tx.Where("user_id = ? AND key = ?", op.UserID, op.Key).First(&prior).Error
		if err == nil {
			if prior.RequestHash != op.RequestHash {
				return artifact.Err("idempotency_conflict", 409)
			}
			*op = prior
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current, err := effectiveSummary(tx, task)
		if err != nil {
			return err
		}
		if current.Content == "" {
			return artifact.Err("source_not_ready", 422)
		}
		if current.Version != op.BaseVersion || artifact.Hash(current.Content) != op.BaseContentHash {
			return artifact.Err("version_conflict", 409)
		}
		if err = tx.Create(run).Error; err != nil {
			return err
		}
		if err = tx.Create(op).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Create(&model.SummaryEditDispatch{ID: uuid.NewString(), RunID: run.ID, NextAttemptAt: now, CreatedAt: now}).Error
	})
	return op, err
}

// Commit checks the latest effective text and writes the immutable revision,
// head, operation, and run outcome in one database transaction.
func (r *SummaryRevisionRepository) Commit(ctx context.Context, opID string, content, patchJSON string, leaseToken ...string) (*model.SummaryRevision, error) {
	var result *model.SummaryRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op model.SummaryEditOperation
		if err := tx.Where("id = ?", opID).First(&op).Error; err != nil {
			return hideMissing(err)
		}
		task, err := summaryTask(tx, op.UserID, op.TaskID, true)
		if err != nil {
			return err
		}
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", opID).First(&op).Error; err != nil {
			return err
		}
		if op.ResultRevisionID != nil {
			var existing model.SummaryRevision
			if err = tx.Where("id = ?", *op.ResultRevisionID).First(&existing).Error; err != nil {
				return err
			}
			result = &existing
			return nil
		}
		if op.Status != "running" && op.Status != "proposed" {
			return artifact.Err("version_conflict", 409)
		}
		if len(leaseToken) > 0 {
			if err = summaryLeaseValid(tx, op.RunID, leaseToken[0]); err != nil {
				return err
			}
		}
		current, err := effectiveSummary(tx, task)
		if err != nil {
			return err
		}
		if current.Version != op.BaseVersion || artifact.Hash(current.Content) != op.BaseContentHash {
			return artifact.Err("version_conflict", 409)
		}
		if content == current.Content {
			return artifact.Err("nothing_to_change", 422)
		}
		var head model.SummaryRevisionHead
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", op.UserID, op.TaskID).First(&head).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			head = model.SummaryRevisionHead{UserID: op.UserID, TaskID: op.TaskID}
		} else if err != nil {
			return err
		}
		if head.Version != op.BaseVersion {
			return artifact.Err("version_conflict", 409)
		}
		var parent *string
		if current.Revision != nil {
			parent = &current.Revision.ID
		}
		now := time.Now().UTC()
		revision := &model.SummaryRevision{ID: uuid.NewString(), UserID: op.UserID, TaskID: op.TaskID, Version: head.Version + 1, Content: content, BaseGeneratedHash: op.BaseGeneratedHash, Origin: "agent", OperationID: &op.ID, ParentRevisionID: parent, CreatedAt: now}
		if err = tx.Create(revision).Error; err != nil {
			return err
		}
		if head.Version == 0 {
			head.Version, head.CurrentRevisionID = revision.Version, revision.ID
			err = tx.Create(&head).Error
		} else {
			err = tx.Model(&head).Updates(map[string]any{"version": revision.Version, "current_revision_id": revision.ID}).Error
		}
		if err != nil {
			return err
		}
		if err = tx.Model(&op).Updates(map[string]any{"status": "committed", "patch_json": patchJSON, "result_revision_id": revision.ID, "committed_at": now, "updated_at": now, "error_code": ""}).Error; err != nil {
			return err
		}
		if err = tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ?", op.RunID, op.UserID).Updates(map[string]any{"status": model.AgentRunStatusCompleted, "stage": "saved", "result_version_id": revision.ID, "finished_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		result = revision
		return nil
	})
	return result, err
}

func (r *SummaryRevisionRepository) Propose(ctx context.Context, opID, patchJSON string, leaseToken ...string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op model.SummaryEditOperation
		if err := tx.Where("id = ?", opID).First(&op).Error; err != nil {
			return hideMissing(err)
		}
		task, err := summaryTask(tx, op.UserID, op.TaskID, true)
		if err != nil {
			return err
		}
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", opID).First(&op).Error; err != nil {
			return err
		}
		if op.Status == "proposed" && op.PatchJSON == patchJSON {
			return nil
		}
		if op.Status != "running" {
			return artifact.Err("version_conflict", 409)
		}
		if len(leaseToken) > 0 {
			if err = summaryLeaseValid(tx, op.RunID, leaseToken[0]); err != nil {
				return err
			}
		}
		current, err := effectiveSummary(tx, task)
		if err != nil {
			return err
		}
		if current.Version != op.BaseVersion || artifact.Hash(current.Content) != op.BaseContentHash {
			return artifact.Err("version_conflict", 409)
		}
		now := time.Now().UTC()
		if err = tx.Model(&op).Updates(map[string]any{"status": "proposed", "patch_json": patchJSON, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ?", op.RunID, op.UserID).Updates(map[string]any{"status": model.AgentRunStatusCompleted, "stage": "proposal", "finished_at": now, "updated_at": now}).Error
	})
}

func (r *SummaryRevisionRepository) Undo(ctx context.Context, owner, taskID int64, opID string, expected int64, currentHash, content string) (*model.SummaryRevision, error) {
	var result *model.SummaryRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := summaryTask(tx, owner, taskID, true)
		if err != nil {
			return err
		}
		var op model.SummaryEditOperation
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND task_id = ?", opID, owner, taskID).First(&op).Error; err != nil {
			return hideMissing(err)
		}
		if op.ResultRevisionID == nil || op.Status != "committed" || op.UndoRevisionID != nil {
			return artifact.Err("undo_conflict", 409)
		}
		current, err := effectiveSummary(tx, task)
		if err != nil {
			return err
		}
		if current.Version != expected || artifact.Hash(current.Content) != currentHash {
			return artifact.Err("version_conflict", 409)
		}
		if current.Content == content {
			return artifact.Err("nothing_to_change", 422)
		}
		var head model.SummaryRevisionHead
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error; err != nil {
			return err
		}
		if head.Version != expected {
			return artifact.Err("version_conflict", 409)
		}
		now := time.Now().UTC()
		result = &model.SummaryRevision{ID: uuid.NewString(), UserID: owner, TaskID: taskID, Version: expected + 1, Content: content, BaseGeneratedHash: current.BaseHash, Origin: "undo", ParentRevisionID: &head.CurrentRevisionID, CreatedAt: now}
		if err = tx.Create(result).Error; err != nil {
			return err
		}
		if err = tx.Model(&head).Updates(map[string]any{"version": result.Version, "current_revision_id": result.ID}).Error; err != nil {
			return err
		}
		return tx.Model(&op).Updates(map[string]any{"undo_revision_id": result.ID, "updated_at": now}).Error
	})
	return result, err
}

func (r *SummaryRevisionRepository) ResolveBase(ctx context.Context, owner, taskID, expected int64, choice string) (*model.SummaryRevision, error) {
	var result *model.SummaryRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := summaryTask(tx, owner, taskID, true)
		if err != nil {
			return err
		}
		current, err := effectiveSummary(tx, task)
		if err != nil {
			return err
		}
		if current.Version != expected || current.Revision == nil || current.Generated == nil {
			return artifact.Err("version_conflict", 409)
		}
		if current.SourceStatus != "needs_merge" {
			return artifact.Err("nothing_to_change", 422)
		}
		content := current.Content
		if choice == "use_generated" {
			content = current.Generated.Content
		} else if choice != "keep_revision" {
			return artifact.Err("invalid_request", 400)
		}
		var head model.SummaryRevisionHead
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error; err != nil {
			return err
		}
		if head.Version != expected {
			return artifact.Err("version_conflict", 409)
		}
		result = &model.SummaryRevision{ID: uuid.NewString(), UserID: owner, TaskID: taskID, Version: expected + 1, Content: content, BaseGeneratedHash: current.CurrentGeneratedHash, Origin: choice, ParentRevisionID: &head.CurrentRevisionID, CreatedAt: time.Now().UTC()}
		if err = tx.Create(result).Error; err != nil {
			return err
		}
		return tx.Model(&head).Updates(map[string]any{"version": result.Version, "current_revision_id": result.ID}).Error
	})
	return result, err
}

func (r *SummaryRevisionRepository) Fail(ctx context.Context, opID, code string, leaseToken ...string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op model.SummaryEditOperation
		if err := tx.Where("id = ?", opID).First(&op).Error; err != nil {
			return err
		}
		if _, err := summaryTask(tx, op.UserID, op.TaskID, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", opID).First(&op).Error; err != nil {
			return err
		}
		if op.Status != "running" {
			return nil
		}
		if len(leaseToken) > 0 {
			if err := summaryLeaseValid(tx, op.RunID, leaseToken[0]); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		if err := tx.Model(&op).Updates(map[string]any{"status": "failed", "error_code": code, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.AgentRun{}).Where("id = ? AND user_id = ?", op.RunID, op.UserID).Updates(map[string]any{"status": model.AgentRunStatusFailed, "error_code": code, "finished_at": now, "updated_at": now}).Error
	})
}

// RevokeSource runs in the task deletion transaction. Historical identities
// remain for audit, but source-derived text and replay checkpoints are wiped
// immediately; the soft-deleted task also blocks every public read/write.
func (r *SummaryRevisionRepository) RevokeSource(taskID int64) error {
	if !r.db.Migrator().HasTable(&model.SummaryEditOperation{}) {
		return nil
	}
	now := time.Now().UTC()
	runs := r.db.Model(&model.AgentRun{}).Select("id").Where("subject_kind = ? AND task_id = ?", model.AgentRunSubjectSummaryEdit, taskID)
	if err := r.db.Model(&model.AgentToolCall{}).Where("run_id IN (?) AND status = ?", runs, model.AgentToolCallStatusRunning).Updates(map[string]any{"status": model.AgentToolCallStatusFailed, "error_code": "source_deleted", "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentStep{}).Where("run_id IN (?) AND status = ?", runs, model.AgentStepStatusRunning).Updates(map[string]any{"status": model.AgentStepStatusFailed, "error_code": "source_deleted", "lease_token": "", "lease_expires_at": nil, "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentToolCall{}).Where("run_id IN (?)", runs).Updates(map[string]any{"result_checkpoint": "", "input_summary": "{}", "output_ref": "", "error_message": ""}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentStep{}).Where("run_id IN (?)", runs).Updates(map[string]any{"result_checkpoint": "", "input_summary": "{}", "output_ref": "", "error_message": ""}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentRun{}).Where("subject_kind = ? AND task_id = ?", model.AgentRunSubjectSummaryEdit, taskID).Updates(map[string]any{"goal": "", "cancel_requested_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.AgentRun{}).Where("subject_kind = ? AND task_id = ? AND status IN ?", model.AgentRunSubjectSummaryEdit, taskID, []string{model.AgentRunStatusPending, model.AgentRunStatusRunning}).Updates(map[string]any{"status": model.AgentRunStatusCancelled, "stage": "source_deleted", "run_lease_token": "", "run_lease_until": nil, "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.SummaryEditOperation{}).Where("task_id = ?", taskID).Updates(map[string]any{"instruction": "", "base_content": "", "patch_json": "{}", "rule_snapshot_json": "{}", "error_code": "source_deleted", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.SummaryRevision{}).Where("task_id = ?", taskID).Update("content", "").Error; err != nil {
		return err
	}
	if err := r.db.Model(&model.VideoTermRuleVersion{}).Where("task_id = ?", taskID).Update("rules_json", "[]").Error; err != nil {
		return err
	}
	return nil
}
