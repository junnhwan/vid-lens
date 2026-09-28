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

type VideoTermRuleRepository struct{ db *gorm.DB }

func NewVideoTermRuleRepository(db *gorm.DB) *VideoTermRuleRepository {
	return &VideoTermRuleRepository{db: db}
}

func (r *VideoTermRuleRepository) Current(ctx context.Context, owner, taskID int64) (*model.VideoTermRuleVersion, error) {
	if _, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false); err != nil {
		return nil, err
	}
	var head model.VideoTermRuleHead
	err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.Version(ctx, owner, taskID, head.Version)
}

func (r *VideoTermRuleRepository) Version(ctx context.Context, owner, taskID, version int64) (*model.VideoTermRuleVersion, error) {
	if _, err := summaryTask(r.db.WithContext(ctx), owner, taskID, false); err != nil {
		return nil, err
	}
	var row model.VideoTermRuleVersion
	if err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ? AND version = ?", owner, taskID, version).First(&row).Error; err != nil {
		return nil, hideMissing(err)
	}
	return &row, nil
}

func (r *VideoTermRuleRepository) Save(ctx context.Context, owner, taskID, expected int64, digest, rulesJSON string, linkedOperationID *string) (*model.VideoTermRuleVersion, error) {
	var result *model.VideoTermRuleVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := summaryTask(tx, owner, taskID, true); err != nil {
			return err
		}
		var head model.VideoTermRuleHead
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND task_id = ?", owner, taskID).First(&head).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			head = model.VideoTermRuleHead{UserID: owner, TaskID: taskID}
		} else if err != nil {
			return err
		}
		if head.Version != expected {
			return artifact.Err("version_conflict", 409)
		}
		if linkedOperationID != nil {
			var operation model.SummaryEditOperation
			if err = tx.Where("id = ? AND user_id = ? AND task_id = ? AND status = ?", *linkedOperationID, owner, taskID, "committed").First(&operation).Error; err != nil {
				return artifact.Err("target_scope_mismatch", 409)
			}
		}
		if head.Version > 0 && head.Digest == digest {
			return artifact.Err("nothing_to_change", 422)
		}
		result = &model.VideoTermRuleVersion{ID: uuid.NewString(), UserID: owner, TaskID: taskID, Version: expected + 1, Digest: digest, RulesJSON: rulesJSON, LinkedOperationID: linkedOperationID, CreatedAt: time.Now().UTC()}
		if err = tx.Create(result).Error; err != nil {
			return err
		}
		if head.Version == 0 {
			head.Version, head.Digest = result.Version, digest
			return tx.Create(&head).Error
		}
		return tx.Model(&head).Updates(map[string]any{"version": result.Version, "digest": digest}).Error
	})
	return result, err
}
