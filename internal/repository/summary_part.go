package repository

import (
	"errors"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type SummaryPartRepository struct{ db *gorm.DB }

func NewSummaryPartRepository(db *gorm.DB) *SummaryPartRepository {
	return &SummaryPartRepository{db: db}
}

func (r *SummaryPartRepository) Find(taskID int64, level, index int) (*model.SummaryPart, error) {
	var part model.SummaryPart
	err := r.db.Where("task_id = ? AND level = ? AND part_index = ?", taskID, level, index).First(&part).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &part, nil
}

func (r *SummaryPartRepository) List(taskID int64) ([]model.SummaryPart, error) {
	var parts []model.SummaryPart
	err := r.db.Where("task_id = ?", taskID).Order("level ASC, part_index ASC").Find(&parts).Error
	return parts, err
}

func (r *SummaryPartRepository) Upsert(part *model.SummaryPart) error {
	existing, err := r.Find(part.TaskID, part.Level, part.PartIndex)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.db.Create(part).Error
	}
	return r.db.Model(existing).Updates(map[string]any{
		"input_hash": part.InputHash, "model_name": part.ModelName, "start_ms": part.StartMS,
		"end_ms": part.EndMS, "status": part.Status, "content": part.Content, "error_msg": part.ErrorMsg,
	}).Error
}

func (r *SummaryPartRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.SummaryPart{}).Error
}
