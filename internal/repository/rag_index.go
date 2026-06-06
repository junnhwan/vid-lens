package repository

import (
	"errors"

	"gorm.io/gorm"
	"vid-lens/internal/model"
)

type RAGIndexRepository struct {
	db *gorm.DB
}

func NewRAGIndexRepository(db *gorm.DB) *RAGIndexRepository {
	return &RAGIndexRepository{db: db}
}

func (r *RAGIndexRepository) Upsert(index *model.VideoRAGIndex) error {
	var existing model.VideoRAGIndex
	err := r.db.Where("user_id = ? AND task_id = ? AND embedding_model = ?", index.UserID, index.TaskID, index.EmbeddingModel).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if index.BuildVersion <= 0 {
			index.BuildVersion = 1
		}
		return r.db.Create(index).Error
	}
	if err != nil {
		return err
	}

	index.ID = existing.ID
	if index.BuildVersion <= 0 {
		index.BuildVersion = existing.BuildVersion
	}
	return r.db.Model(&existing).Updates(map[string]interface{}{
		"embedding_dim": index.EmbeddingDim,
		"status":        index.Status,
		"chunk_count":   index.ChunkCount,
		"last_error":    index.LastError,
		"build_version": index.BuildVersion,
		"started_at":    index.StartedAt,
		"finished_at":   index.FinishedAt,
	}).Error
}

func (r *RAGIndexRepository) FindByTaskAndModel(userID, taskID int64, embeddingModel string) (*model.VideoRAGIndex, error) {
	var index model.VideoRAGIndex
	err := r.db.Where("user_id = ? AND task_id = ? AND embedding_model = ?", userID, taskID, embeddingModel).
		First(&index).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &index, nil
}
