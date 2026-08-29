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
// 写入时带 file_md5，使 (file_md5) 唯一约束成为内容+目标级去重的持久兜底
// （docs/architecture/data-model.md）：同内容重复摘要写入会撞唯一约束，调用方据此识别已有结果。
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

// FindByMD5 按内容指纹查找已完成的摘要结果（跨 task、跨用户）。
// 行存在即摘要已成功完成（摘要表无 status 列），用于内容+目标级去重命中判定（docs/architecture/data-model.md）。
func (r *SummaryRepository) FindByMD5(fileMD5 string) (*model.AISummary, error) {
	var s model.AISummary
	err := r.db.Where("file_md5 = ?", fileMD5).First(&s).Error
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
