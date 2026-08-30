package model

import "time"

type VideoChunk struct {
	ID                  int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID              int64     `gorm:"index;not null" json:"user_id"`
	TaskID              int64     `gorm:"index;uniqueIndex:idx_task_chunk_model;not null" json:"task_id"`
	ChunkIndex          int       `gorm:"uniqueIndex:idx_task_chunk_model;not null" json:"chunk_index"`
	Content             string    `gorm:"type:text;not null" json:"content"`
	ContentHash         string    `gorm:"type:char(32);not null;index" json:"content_hash"`
	TokenCount          int       `gorm:"default:0" json:"token_count"`
	EmbeddingModel      string    `gorm:"type:varchar(100);uniqueIndex:idx_task_chunk_model;not null" json:"embedding_model"`
	EmbeddingDim        int       `gorm:"not null" json:"embedding_dim"`
	VectorID            string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"vector_id"`
	Modality            string    `gorm:"type:varchar(30);not null;default:'unknown';index" json:"modality"`
	StartMS             int64     `gorm:"not null;default:0;index" json:"start_ms"`
	EndMS               int64     `gorm:"not null;default:0;index" json:"end_ms"`
	TimeRangeStatus     string    `gorm:"type:varchar(20);not null;default:'unknown';index" json:"time_range_status"`
	SourceMappingStatus string    `gorm:"type:varchar(20);not null;default:'unmapped';index" json:"source_mapping_status"`
	SourceRefs          string    `gorm:"column:source_refs;type:text;not null;default:'[]'" json:"source_refs"`
	ChunkerStrategy     string    `gorm:"type:varchar(50);not null;default:''" json:"chunker_strategy"`
	ChunkerVersion      string    `gorm:"type:varchar(50);not null;default:''" json:"chunker_version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

const (
	ChunkModalityTranscript    = "transcript"
	ChunkModalityVisualOCR     = "visual_ocr"
	ChunkModalityVisualCaption = "visual_caption"
	ChunkModalityUnknown       = "unknown"

	ChunkTimeRangeExact   = "exact"
	ChunkTimeRangeCoarse  = "coarse"
	ChunkTimeRangeUnknown = "unknown"

	ChunkSourceMapped   = "mapped"
	ChunkSourcePartial  = "partial"
	ChunkSourceUnmapped = "unmapped"
)

func (VideoChunk) TableName() string {
	return "video_chunks"
}
