package model

import "time"

const (
	RAGIndexStatusNotIndexed = "not_indexed"
	RAGIndexStatusIndexing   = "indexing"
	RAGIndexStatusIndexed    = "indexed"
	RAGIndexStatusFailed     = "failed"
)

type VideoRAGIndex struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"user_id"`
	TaskID         int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"task_id"`
	EmbeddingModel string     `gorm:"type:varchar(100);uniqueIndex:idx_user_task_model;not null" json:"embedding_model"`
	EmbeddingDim   int        `gorm:"not null" json:"embedding_dim"`
	Status         string     `gorm:"type:varchar(30);index;not null" json:"status"`
	ChunkCount     int        `gorm:"default:0" json:"chunk_count"`
	LastError      string     `gorm:"type:varchar(500)" json:"last_error"`
	BuildVersion   int        `gorm:"default:1" json:"build_version"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (VideoRAGIndex) TableName() string {
	return "video_rag_indexes"
}
