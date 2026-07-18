package model

import (
	"time"
)

// AISummary AI 分析结果表
// 面试亮点：独立存储 AI 结构化输出，与基础转录业务解耦
// 选用 TEXT 类型存储 Markdown 格式的分析结果，方便前端直接渲染
type AISummary struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
	Content   string    `gorm:"type:text" json:"content"`            // AI 总结（Markdown 格式）
	ModelName string    `gorm:"type:varchar(100)" json:"model_name"` // 使用的模型名称
	CreatedAt time.Time `json:"created_at"`
}

func (AISummary) TableName() string {
	return "ai_summaries"
}
