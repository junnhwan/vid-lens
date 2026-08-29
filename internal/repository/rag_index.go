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
		"file_md5":              index.FileMD5,
		"embedding_dim":         index.EmbeddingDim,
		"status":                index.Status,
		"chunk_count":           index.ChunkCount,
		"chunker_strategy":      index.ChunkerStrategy,
		"chunker_version":       index.ChunkerVersion,
		"chunk_size":            index.ChunkSize,
		"chunk_overlap":         index.ChunkOverlap,
		"chunk_manifest_sha256": index.ChunkManifestSHA256,
		"last_error":            index.LastError,
		"build_version":         index.BuildVersion,
		"started_at":            index.StartedAt,
		"finished_at":           index.FinishedAt,
	}).Error
}

// FindByMD5AndModel 按内容指纹 + embedding 模型查找已成功索引的 RAG 索引行
// （跨 task、跨用户）。仅复用 status=indexed 的成功结果：索引重建（分块/embedding
// 模型变更）后旧索引行 status 会被 Upsert 改写，不再挡住重索引（docs/$1）。
func (r *RAGIndexRepository) FindByMD5AndModel(fileMD5, embeddingModel string) (*model.VideoRAGIndex, error) {
	var index model.VideoRAGIndex
	err := r.db.Where("file_md5 = ? AND embedding_model = ? AND status = ?", fileMD5, embeddingModel, model.RAGIndexStatusIndexed).
		First(&index).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &index, nil
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

func (r *RAGIndexRepository) ListByTaskIDsAndModel(userID int64, taskIDs []int64, embeddingModel string) ([]model.VideoRAGIndex, error) {
	if len(taskIDs) == 0 {
		return []model.VideoRAGIndex{}, nil
	}
	var indexes []model.VideoRAGIndex
	err := r.db.Where("user_id = ? AND embedding_model = ? AND task_id IN ?", userID, embeddingModel, taskIDs).
		Order("task_id ASC").Find(&indexes).Error
	return indexes, err
}

func (r *RAGIndexRepository) ListEmbeddingModelsByTask(userID, taskID int64) ([]string, error) {
	var models []string
	err := r.db.Model(&model.VideoRAGIndex{}).
		Where("user_id = ? AND task_id = ?", userID, taskID).
		Distinct("embedding_model").
		Order("embedding_model asc").
		Pluck("embedding_model", &models).Error
	return models, err
}

func (r *RAGIndexRepository) DeleteByTaskID(taskID int64) error {
	return r.db.Where("task_id = ?", taskID).Delete(&model.VideoRAGIndex{}).Error
}
