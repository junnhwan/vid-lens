package model

import (
	"time"

	"gorm.io/gorm"
)

// TaskStatus 任务状态枚举
// 面试亮点：严格的状态机设计，防止非法状态流转
const (
	TaskStatusPending   int8 = 0 // 待处理（文件已上传，等待分析）
	TaskStatusQueued    int8 = 1 // 排队中（已投递消息队列）
	TaskStatusRunning   int8 = 2 // 处理中（消费者正在执行）
	TaskStatusCompleted int8 = 3 // 已完成
	TaskStatusFailed    int8 = 4 // 失败
)

// VideoTask 视频任务记录 —— 整个异步架构的枢纽表
// 面试亮点：
//   1. file_md5 字段实现内容级去重 + 秒传
//   2. status 字段严格定义任务生命周期
//   3. (status, created_at) 联合索引供调度器捞取积压任务
type VideoTask struct {
	ID        int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    int64          `gorm:"index;not null" json:"user_id"`
	FileMD5   string         `gorm:"type:char(32);uniqueIndex:idx_file_md5;not null" json:"file_md5"`
	Filename  string         `gorm:"type:varchar(255);not null" json:"filename"`
	FileURL   string         `gorm:"type:varchar(500)" json:"file_url"`          // MinIO 存储路径
	FileSize  int64          `gorm:"default:0" json:"file_size"`                 // 文件大小（字节）
	Status    int8           `gorm:"type:tinyint;default:0;index:idx_status_time" json:"status"`
	ErrorMsg  string         `gorm:"type:varchar(500)" json:"error_msg"`         // 失败原因
	CreatedAt time.Time      `gorm:"index:idx_status_time" json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联（不存储在数据库）
	Transcription *VideoTranscription `gorm:"foreignKey:TaskID;references:ID" json:"transcription,omitempty"`
	Summary       *AISummary          `gorm:"foreignKey:TaskID;references:ID" json:"summary,omitempty"`
}

func (VideoTask) TableName() string {
	return "video_tasks"
}
