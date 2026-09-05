package repository

import (
	"context"
	"vid-lens/internal/model"

	"gorm.io/gorm"
)

func (r *VideoVisualFrameRepository) FindForUser(ctx context.Context, userID, taskID, frameID int64) (*model.VideoVisualFrame, error) {
	var row model.VideoVisualFrame
	err := r.db.WithContext(ctx).Where("task_id = ? AND id = ? AND task_id IN (SELECT id FROM video_tasks WHERE user_id = ?)", taskID, frameID, userID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type VideoVisualFrameRepository struct {
	db *gorm.DB
}

func NewVideoVisualFrameRepository(db *gorm.DB) *VideoVisualFrameRepository {
	return &VideoVisualFrameRepository{db: db}
}

func (r *VideoVisualFrameRepository) ReplaceTaskFrames(taskID int64, frames []model.VideoVisualFrame) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&model.VideoVisualFrame{}).Error; err != nil {
			return err
		}
		if len(frames) == 0 {
			return nil
		}
		return tx.Create(&frames).Error
	})
}

func (r *VideoVisualFrameRepository) ListByTaskID(taskID int64) ([]model.VideoVisualFrame, error) {
	var frames []model.VideoVisualFrame
	err := r.db.Where("task_id = ?", taskID).
		Order("frame_index asc").
		Find(&frames).Error
	return frames, err
}

func (r *VideoVisualFrameRepository) ListCompletedWithText(taskID int64) ([]model.VideoVisualFrame, error) {
	var frames []model.VideoVisualFrame
	err := r.db.Where("task_id = ? AND status = ? AND (ocr_text <> '' OR vision_caption <> '')", taskID, model.VisualFrameStatusCompleted).
		Order("time_ms asc, frame_index asc").
		Find(&frames).Error
	return frames, err
}

func (r *VideoVisualFrameRepository) ListCompletedInRange(taskID, startMS, endMS int64, limit int) ([]model.VideoVisualFrame, error) {
	var frames []model.VideoVisualFrame
	query := r.db.Where("task_id = ? AND status = ? AND time_ms >= ? AND time_ms < ? AND (ocr_text <> '' OR vision_caption <> '')",
		taskID, model.VisualFrameStatusCompleted, startMS, endMS).
		Order("time_ms asc, frame_index asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return frames, query.Find(&frames).Error
}

func (r *VideoVisualFrameRepository) ListObjectKeysByTaskID(taskID int64) ([]string, error) {
	var keys []string
	err := r.db.Model(&model.VideoVisualFrame{}).
		Where("task_id = ? AND object_key <> ''", taskID).
		Pluck("object_key", &keys).Error
	return keys, err
}

func (r *VideoVisualFrameRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoVisualFrame{}).Error
}
