package repository

import (
	"context"
	"strings"

	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VideoVisualObservationRepository struct {
	db *gorm.DB
}

func NewVideoVisualObservationRepository(db *gorm.DB) *VideoVisualObservationRepository {
	return &VideoVisualObservationRepository{db: db}
}

func (r *VideoVisualObservationRepository) FindByID(ctx context.Context, userID, taskID int64, id string) (*model.VideoVisualObservation, error) {
	if r == nil || r.db == nil || userID <= 0 || taskID <= 0 || id == "" {
		return nil, gorm.ErrInvalidData
	}
	var row model.VideoVisualObservation
	err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ? AND id = ?", userID, taskID, id).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *VideoVisualObservationRepository) Append(ctx context.Context, observation *model.VideoVisualObservation) error {
	if r == nil || r.db == nil || observation == nil {
		return gorm.ErrInvalidData
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(observation).Error
}

func (r *VideoVisualObservationRepository) FindByCacheKey(ctx context.Context, userID, taskID int64, cacheKey string) (*model.VideoVisualObservation, error) {
	if r == nil || r.db == nil || userID <= 0 || taskID <= 0 || strings.TrimSpace(cacheKey) == "" {
		return nil, gorm.ErrInvalidData
	}
	var observation model.VideoVisualObservation
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND task_id = ? AND cache_key = ?", userID, taskID, strings.TrimSpace(cacheKey)).
		First(&observation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &observation, nil
}

func (r *VideoVisualObservationRepository) ListByTaskID(ctx context.Context, userID, taskID int64) ([]model.VideoVisualObservation, error) {
	if r == nil || r.db == nil || userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrInvalidData
	}
	var observations []model.VideoVisualObservation
	err := r.db.WithContext(ctx).Where("user_id = ? AND task_id = ?", userID, taskID).
		Order("created_at asc, id asc").Find(&observations).Error
	return observations, err
}

func (r *VideoVisualObservationRepository) ListObjectKeysByTaskID(taskID int64) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var keys []string
	err := r.db.Model(&model.VideoVisualObservation{}).
		Where("task_id = ? AND object_key <> ''", taskID).
		Pluck("object_key", &keys).Error
	return keys, err
}

func (r *VideoVisualObservationRepository) DeleteByTaskID(taskID int64) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoVisualObservation{}).Error
}
