package repository

import (
	"errors"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type TranscriptionChunkRepository struct {
	db *gorm.DB
}

func NewTranscriptionChunkRepository(db *gorm.DB) *TranscriptionChunkRepository {
	return &TranscriptionChunkRepository{db: db}
}

func (r *TranscriptionChunkRepository) FindByTaskAndIndex(taskID int64, chunkIndex int) (*model.VideoTranscriptionChunk, error) {
	var chunk model.VideoTranscriptionChunk
	err := r.db.Where("task_id = ? AND chunk_index = ?", taskID, chunkIndex).First(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &chunk, nil
}

func (r *TranscriptionChunkRepository) ListByTaskID(taskID int64) ([]model.VideoTranscriptionChunk, error) {
	var chunks []model.VideoTranscriptionChunk
	err := r.db.Where("task_id = ?", taskID).Order("chunk_index ASC").Find(&chunks).Error
	return chunks, err
}

func (r *TranscriptionChunkRepository) UpsertRunning(taskID int64, chunkIndex int, audioObject string) error {
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			Status:      model.TranscriptionChunkStatusRunning,
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object": audioObject,
		"status":       model.TranscriptionChunkStatusRunning,
		"error_msg":    "",
	}).Error
}

func (r *TranscriptionChunkRepository) UpsertCompleted(taskID int64, chunkIndex int, audioObject, content string) error {
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	chars := len([]rune(content))
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			Status:      model.TranscriptionChunkStatusCompleted,
			Content:     content,
			Chars:       chars,
			ErrorMsg:    "",
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object": audioObject,
		"status":       model.TranscriptionChunkStatusCompleted,
		"content":      content,
		"chars":        chars,
		"error_msg":    "",
	}).Error
}

func (r *TranscriptionChunkRepository) UpsertFailed(taskID int64, chunkIndex int, audioObject, errMsg string) error {
	existing, err := r.FindByTaskAndIndex(taskID, chunkIndex)
	if err != nil {
		return err
	}
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	if existing == nil {
		return r.db.Create(&model.VideoTranscriptionChunk{
			TaskID:      taskID,
			ChunkIndex:  chunkIndex,
			AudioObject: audioObject,
			Status:      model.TranscriptionChunkStatusFailed,
			ErrorMsg:    errMsg,
			RetryCount:  1,
		}).Error
	}
	return r.db.Model(existing).Updates(map[string]interface{}{
		"audio_object": audioObject,
		"status":       model.TranscriptionChunkStatusFailed,
		"error_msg":    errMsg,
		"retry_count":  existing.RetryCount + 1,
	}).Error
}
