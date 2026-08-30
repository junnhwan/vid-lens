package model

import "time"

const (
	RAGIndexStatusNotIndexed   = "not_indexed"
	RAGIndexStatusIndexing     = "indexing"
	RAGIndexStatusIndexed      = "indexed"
	RAGIndexStatusFailed       = "failed"
	RAGIndexStatusNeedsRebuild = "needs_rebuild"

	CurrentRAGIndexBuildVersion    = 2
	CurrentRAGSourceMappingVersion = "source-map-v1"
	CurrentRAGChunkerVersion       = "recursive-sentence-source-v2"
)

type VideoRAGIndex struct {
	ID                   int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID               int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"user_id"`
	TaskID               int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"task_id"`
	FileMD5              string     `gorm:"type:char(32);not null;uniqueIndex:uk_rag_file_md5_model" json:"file_md5"`                                            // 内容指纹，跨 task 去重键
	EmbeddingModel       string     `gorm:"type:varchar(100);uniqueIndex:idx_user_task_model;not null;uniqueIndex:uk_rag_file_md5_model" json:"embedding_model"` // 内容+目标级去重第二维（模型），与 file_md5 组合唯一
	EmbeddingDim         int        `gorm:"not null" json:"embedding_dim"`
	Status               string     `gorm:"type:varchar(30);index;not null" json:"status"`
	ChunkCount           int        `gorm:"default:0" json:"chunk_count"`
	ChunkerStrategy      string     `gorm:"type:varchar(50)" json:"chunker_strategy"`
	ChunkerVersion       string     `gorm:"type:varchar(50)" json:"chunker_version"`
	ChunkSize            int        `gorm:"default:0" json:"chunk_size"`
	ChunkOverlap         int        `gorm:"default:0" json:"chunk_overlap"`
	ChunkManifestSHA256  string     `gorm:"type:varchar(64)" json:"chunk_manifest_sha256"`
	SourceMappingVersion string     `gorm:"type:varchar(50);not null;default:''" json:"source_mapping_version"`
	LastError            string     `gorm:"type:varchar(500)" json:"last_error"`
	BuildVersion         int        `gorm:"default:1" json:"build_version"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (VideoRAGIndex) TableName() string {
	return "video_rag_indexes"
}
