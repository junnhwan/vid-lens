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
	TaskStatusDead      int8 = 5 // 死信（超过最大重试次数，需人工或用户重新触发）
)

const (
	TaskSourceTypeUpload  = "upload"
	TaskSourceTypeChunked = "chunked"
	TaskSourceTypeURL     = "url"
)

const (
	TaskLeaseKindProcessing = "processing"
	TaskLeaseKindDispatch   = "dispatch"
)

const (
	TaskStageNone         = "none"
	TaskStageDownloading  = "downloading"
	TaskStageUploaded     = "uploaded"
	TaskStageTranscribing = "transcribing"
	TaskStageVisual       = "visual_indexing" // keyframe OCR; best-effort after ASR
	TaskStageSummarizing  = "summarizing"
	TaskStageIndexing     = "indexing"
)

// VideoTask 视频任务记录 —— 整个异步架构的枢纽表
// 面试亮点：
//  1. file_md5 字段实现内容级去重 + 秒传
//  2. status 字段严格定义任务生命周期
//  3. (status, created_at) 联合索引供调度器捞取积压任务
type VideoTask struct {
	ID              int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID          int64          `gorm:"index;not null" json:"user_id"`
	AssetID         *int64         `gorm:"index" json:"asset_id"`
	FileMD5         string         `gorm:"type:char(32);index;not null" json:"file_md5"`
	Filename        string         `gorm:"type:varchar(255);not null" json:"filename"`
	Title           string         `gorm:"type:varchar(120);default:''" json:"title,omitempty"`
	FileURL         string         `gorm:"type:varchar(500)" json:"file_url"` // MinIO 存储路径
	FileSize        int64          `gorm:"default:0" json:"file_size"`        // 文件大小（字节）
	Status          int8           `gorm:"type:smallint;default:0;index:idx_status_time" json:"status"`
	Stage           string         `gorm:"type:varchar(50);default:'none';index" json:"stage"`
	TraceID         string         `gorm:"type:varchar(64);index" json:"trace_id"`
	SourceType      string         `gorm:"type:varchar(20);index" json:"source_type"`
	SourceURL       string         `gorm:"type:varchar(1000)" json:"source_url,omitempty"`
	RetryCount      int            `gorm:"default:0" json:"retry_count"`
	MaxRetries      int            `gorm:"default:3" json:"max_retries"`
	NextRetryAt     *time.Time     `json:"next_retry_at,omitempty"`
	LastErrorCode   string         `gorm:"type:varchar(100)" json:"last_error_code"`
	LastErrorMsg    string         `gorm:"type:varchar(500)" json:"last_error_msg"`
	LastJobType     string         `gorm:"type:varchar(30);index" json:"last_job_type"`
	ProcessingToken string         `gorm:"type:varchar(64);index" json:"-"`
	LeaseKind       string         `gorm:"type:varchar(20);index" json:"-"`
	LeaseExpiresAt  *time.Time     `gorm:"index" json:"-"`
	LeaseVersion    int64          `gorm:"default:0" json:"-"`
	StageStartedAt  *time.Time     `json:"stage_started_at,omitempty"`
	StageFinishedAt *time.Time     `json:"stage_finished_at,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	ErrorMsg        string         `gorm:"type:varchar(500)" json:"error_msg"` // 失败原因
	CreatedAt       time.Time      `gorm:"index:idx_status_time" json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联（不存储在数据库）
	Asset         *VideoAsset         `gorm:"foreignKey:AssetID;references:ID" json:"asset,omitempty"`
	Transcription *VideoTranscription `gorm:"foreignKey:TaskID;references:ID" json:"transcription,omitempty"`
	Summary       *AISummary          `gorm:"foreignKey:TaskID;references:ID" json:"summary,omitempty"`
	Jobs          []TaskJob           `gorm:"foreignKey:TaskID;references:ID" json:"jobs,omitempty"`

	// 列表用轻量标记（非 DB 列）：是否已有转写/总结，避免把大字段 content 塞进 list。
	// 用值类型 bool（不用 *bool + omitempty），保证 JSON 始终带上 true/false，前端可直接灰显主按钮。
	HasTranscription bool `gorm:"-" json:"has_transcription"`
	HasSummary       bool `gorm:"-" json:"has_summary"`
}

func (VideoTask) TableName() string {
	return "video_tasks"
}
