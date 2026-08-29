package model

import (
	"time"
)

// AISummary AI 分析结果表
// AI output is stored separately from the base transcription record.
// 选用 TEXT 类型存储 Markdown 格式的分析结果，方便前端直接渲染
//
// file_md5 列承担内容+目标级去重（spec 03）：与 task_id 的 1:1 唯一索引解耦，
// 同一内容指纹跨 task/跨用户只允许一个成功摘要结果行（行存在即 Completed 语义）。
// 三层幂等分工见 VideoTranscription 注释。
type AISummary struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	FileMD5   string    `gorm:"type:char(32);not null;uniqueIndex:uk_ai_summaries_file_md5" json:"file_md5"` // 内容指纹，跨 task 去重键
	Content   string    `gorm:"type:text" json:"content"`                                                    // AI 总结（Markdown 格式）
	ModelName string    `gorm:"type:varchar(100)" json:"model_name"`                                         // 使用的模型名称
	CreatedAt time.Time `json:"created_at"`
}

func (AISummary) TableName() string {
	return "ai_summaries"
}
