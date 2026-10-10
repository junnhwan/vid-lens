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
	"vid-lens/internal/processing"
)

type ImportRequestRepository struct{ db *gorm.DB }

func (r *ImportRequestRepository) Lookup(ctx context.Context, owner int64, action, key, hash string) (*model.VideoTask, error) {
	if owner <= 0 || action == "" || len(hash) != 64 {
		return nil, artifact.Err("invalid_request", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return nil, err
	}
	var request model.ImportRequest
	err := r.db.WithContext(ctx).Where("user_id = ? AND action = ? AND key = ?", owner, action, key).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if request.RequestHash != hash {
		return nil, artifact.Err("idempotency_conflict", 409)
	}
	var task model.VideoTask
	err = r.db.WithContext(ctx).Where("id = ? AND user_id = ?", request.TaskID, owner).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, artifact.Err("task_deleted", 404)
	}
	return &task, err
}

// AcceptImport serializes concurrent same-key acceptance through a unique
// insert. The callback may write DB intent; remote work must follow commit.
func (r *Repositories) AcceptImport(ctx context.Context, owner int64, action, key, hash string, fn func(*Repositories) (*model.VideoTask, error)) (*model.VideoTask, bool, error) {
	if prior, err := r.ImportRequest.Lookup(ctx, owner, action, key, hash); err != nil || prior != nil {
		return prior, false, err
	}
	var result *model.VideoTask
	created := false
	err := r.TransactionContext(ctx, func(tx *Repositories) error {
		row := model.ImportRequest{ID: uuid.NewString(), UserID: owner, Action: action, Key: key, RequestHash: hash, CreatedAt: time.Now().UTC()}
		insert := tx.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "action"}, {Name: "key"}}, DoNothing: true}).Create(&row)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			prior, err := tx.ImportRequest.Lookup(ctx, owner, action, key, hash)
			if err != nil {
				return err
			}
			result = prior
			return nil
		}
		task, err := fn(tx)
		if err != nil {
			return err
		}
		if task == nil || task.ID <= 0 || task.UserID != owner {
			return artifact.Err("invalid_import_result", 500)
		}
		initialGenerationID := ""
		if task.ProcessingIntentJSON != "" {
			intent, decodeErr := processing.Decode(task.ProcessingIntentJSON)
			if decodeErr != nil {
				return artifact.Err("invalid_import_result", 500)
			}
			initialGenerationID = intent.GenerationID
		}
		if err := tx.db.Model(&row).Updates(map[string]any{"task_id": task.ID, "initial_generation_id": initialGenerationID}).Error; err != nil {
			return err
		}
		result = task
		created = true
		return nil
	})
	return result, created, err
}

// The original HTTP receipt is immutable when an explicit regeneration later
// changes the task's current workflow intent. Empty historical values stay empty.
func (r *ImportRequestRepository) ReadAcceptedGeneration(ctx context.Context, owner int64, action, key string) (string, error) {
	if owner <= 0 || action == "" {
		return "", artifact.Err("invalid_request", 400)
	}
	if err := artifact.ValidateKey(key); err != nil {
		return "", err
	}
	var request model.ImportRequest
	if err := r.db.WithContext(ctx).Where("user_id=? AND action=? AND key=?", owner, action, key).First(&request).Error; err != nil {
		return "", hideMissing(err)
	}
	if _, err := summaryTask(r.db.WithContext(ctx), owner, request.TaskID, false); err != nil {
		return "", err
	}
	return request.InitialGenerationID, nil
}
