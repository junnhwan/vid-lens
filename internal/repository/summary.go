package repository

import (
	"errors"

	"vid-lens/internal/model"

	"gorm.io/gorm"
)

type SummaryRepository struct {
	db *gorm.DB
}

func NewSummaryRepository(db *gorm.DB) *SummaryRepository {
	return &SummaryRepository{db: db}
}

// Create 创建 AI 总结记录
func (r *SummaryRepository) Create(s *model.AISummary) error {
	return r.db.Create(s).Error
}

// FindByTaskID 根据任务 ID 查找总结
func (r *SummaryRepository) FindByTaskID(taskID int64) (*model.AISummary, error) {
	var s model.AISummary
	err := r.db.Where("task_id = ?", taskID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert 创建或更新总结记录
func (r *SummaryRepository) Upsert(s *model.AISummary) error {
	var existing model.AISummary
	err := r.db.Where("task_id = ?", s.TaskID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(s).Error
	}
	if err != nil {
		return err
	}
	return r.db.Model(&existing).Updates(map[string]interface{}{
		"content":    s.Content,
		"model_name": s.ModelName,
	}).Error
}
