package repository

import (
	"errors"

	"vid-lens/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
// task_id 标识生成结果所属视频；file_md5 用于未强制生成的视频复用已有结果。
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
		"file_md5":   s.FileMD5,
	}).Error
}

// FindByMD5 按内容指纹查找可复用的已完成摘要。多个任务强制生成后，
// 优先选择最早保存的结果，避免随机挑选其他任务的个性化重跑版本。
func (r *SummaryRepository) FindByMD5(fileMD5 string) (*model.AISummary, error) {
	var s model.AISummary
	err := r.db.Where("file_md5 = ?", fileMD5).Order("id ASC").First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SummaryRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.AISummary{}).Error
}

// RehomeOrDeleteByTaskID preserves a shared generated cache for another
// still-active task with the same content. It runs in the cleanup transaction
// after the deleting task has been hidden, and never copies user revisions.
func (r *SummaryRepository) RehomeOrDeleteByTaskID(taskID int64) error {
	var summary model.AISummary
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).First(&summary).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var successor model.VideoTask
	err = r.db.Where("file_md5 = ? AND id <> ? AND id NOT IN (SELECT task_id FROM ai_summaries)", summary.FileMD5, taskID).Order("id ASC").First(&successor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Where("id = ?", summary.ID).Delete(&model.AISummary{}).Error
	}
	if err != nil {
		return err
	}
	return r.db.Model(&summary).Update("task_id", successor.ID).Error
}
