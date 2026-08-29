package model

import "time"

type VideoChunk struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         int64     `gorm:"index;not null" json:"user_id"`
	TaskID         int64     `gorm:"index;uniqueIndex:idx_task_chunk_model;not null" json:"task_id"`
	ChunkIndex     int       `gorm:"uniqueIndex:idx_task_chunk_model;not null" json:"chunk_index"`
	Content        string    `gorm:"type:text;not null" json:"content"`
	ContentHash    string    `gorm:"type:char(32);not null;index" json:"content_hash"`
	TokenCount     int       `gorm:"default:0" json:"token_count"`
	EmbeddingModel string    `gorm:"type:varchar(100);uniqueIndex:idx_task_chunk_model;not null" json:"embedding_model"`
	EmbeddingDim   int       `gorm:"not null" json:"embedding_dim"`
	VectorID       string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"vector_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (VideoChunk) TableName() string {
	return "video_chunks"
}
