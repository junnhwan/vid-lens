package repository

import (
	"vid-lens/internal/model"

	"gorm.io/gorm"
)

type TranscriptionRepository struct {
	db *gorm.DB
}

func NewTranscriptionRepository(db *gorm.DB) *TranscriptionRepository {
	return &TranscriptionRepository{db: db}
}

// Create 创建转录记录
func (r *TranscriptionRepository) Create(t *model.VideoTranscription) error {
	return r.db.Create(t).Error
}

// FindByTaskID 根据任务 ID 查找转录
func (r *TranscriptionRepository) FindByTaskID(taskID int64) (*model.VideoTranscription, error) {
	var t model.VideoTranscription
	err := r.db.Where("task_id = ?", taskID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Upsert 创建或更新转录记录
func (r *TranscriptionRepository) Upsert(t *model.VideoTranscription) error {
	var existing model.VideoTranscription
	err := r.db.Where("task_id = ?", t.TaskID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(t).Error
	}
	if err != nil {
		return err
	}
	return r.db.Model(&existing).Updates(map[string]interface{}{
		"content": t.Content,
		"words":   t.Words,
	}).Error
}
