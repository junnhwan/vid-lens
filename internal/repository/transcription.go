package repository

import (
	"errors"

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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Upsert 创建或更新转录记录
// 写入时带 file_md5，使 (file_md5) 唯一约束成为内容+目标级去重的持久兜底
// （docs/architecture/data-model.md）：同内容重复 ASR 写入会撞唯一约束，调用方据此识别已有结果。
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
		"content":   t.Content,
		"words":     t.Words,
		"file_md5":  t.FileMD5,
	}).Error
}

// FindByMD5 按内容指纹查找已完成的转写结果（跨 task、跨用户）。
// 行存在即转写已成功完成（转写表无 status 列），用于内容+目标级去重命中判定（docs/architecture/data-model.md）。
func (r *TranscriptionRepository) FindByMD5(fileMD5 string) (*model.VideoTranscription, error) {
	var t model.VideoTranscription
	err := r.db.Where("file_md5 = ?", fileMD5).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TranscriptionRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoTranscription{}).Error
}
