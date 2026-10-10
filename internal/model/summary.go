package model

import (
	"time"
)

// AISummary AI 分析结果表
// AI output is stored separately from the base transcription record.
// 选用 TEXT 类型存储 Markdown 格式的分析结果，方便前端直接渲染
//
// file_md5 支持跨任务复用已有结果。强制重新生成的任务可保存自己的摘要，
// 而不会改写相同文件的其他任务正在使用的摘要。
// 三层幂等分工见 VideoTranscription 注释。
type AISummary struct {
	DocumentJSON     string    `gorm:"type:text;not null;default:''" json:"-"`
	SchemaVersion    string    `gorm:"type:varchar(32);not null;default:''" json:"schema_version,omitempty"`
	SourceID         string    `gorm:"type:varchar(36);not null;default:'';index" json:"source_id,omitempty"`
	SourceDigest     string    `gorm:"type:char(64);not null;default:''" json:"source_digest,omitempty"`
	ContentDigest    string    `gorm:"type:char(64);not null;default:''" json:"content_digest,omitempty"`
	ContentHashKind  string    `gorm:"type:varchar(32);not null;default:'markdown-v1'" json:"content_hash_kind"`
	GeneratedVersion int64     `gorm:"not null;default:0" json:"generated_version"`
	GenerationID     string    `gorm:"type:varchar(36);not null;default:''" json:"generation_id,omitempty"`
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID           int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	FileMD5          string    `gorm:"type:char(32);not null;index:idx_ai_summaries_file_md5" json:"file_md5"`
	Content          string    `gorm:"type:text" json:"content"`            // AI 总结（Markdown 格式）
	ModelName        string    `gorm:"type:varchar(100)" json:"model_name"` // 使用的模型名称
	CreatedAt        time.Time `json:"created_at"`
}

func (AISummary) TableName() string {
	return "ai_summaries"
}
