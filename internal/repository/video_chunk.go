package repository

import (
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type VideoChunkRepository struct {
	db *gorm.DB
}

func NewVideoChunkRepository(db *gorm.DB) *VideoChunkRepository {
	return &VideoChunkRepository{db: db}
}

func (r *VideoChunkRepository) ReplaceTaskChunks(taskID int64, embeddingModel string, chunks []model.VideoChunk) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ? AND embedding_model = ?", taskID, embeddingModel).
			Delete(&model.VideoChunk{}).Error; err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		return tx.Create(&chunks).Error
	})
}

func (r *VideoChunkRepository) ListByTaskID(userID, taskID int64, embeddingModel string) ([]model.VideoChunk, error) {
	var chunks []model.VideoChunk
	err := r.db.Where("user_id = ? AND task_id = ? AND embedding_model = ?", userID, taskID, embeddingModel).
		Order("chunk_index asc").
		Find(&chunks).Error
	return chunks, err
}
