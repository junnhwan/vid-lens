package model

import "time"

const (
	TaskJobTypeAnalyze    = "analyze"
	TaskJobTypeTranscribe = "transcribe"
	TaskJobTypeDownload   = "download"
	TaskJobTypeRAGIndex   = "rag_index"
)

// TaskJob records the state of one processing action under a video task.
// video_tasks remains the compatibility status source; task_jobs separates
// download/transcribe/analyze/rag_index progress for backend observability.
type TaskJob struct {
	ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID        int64      `gorm:"not null;uniqueIndex:uk_task_jobs_task_type;index" json:"task_id"`
	UserID        int64      `gorm:"not null;index" json:"user_id"`
	JobType       string     `gorm:"type:varchar(30);not null;uniqueIndex:uk_task_jobs_task_type;index" json:"job_type"`
	Status        int8       `gorm:"type:tinyint;default:0;index" json:"status"`
	Stage         string     `gorm:"type:varchar(50);default:'none';index" json:"stage"`
	TraceID       string     `gorm:"type:varchar(64);index" json:"trace_id"`
	RetryCount    int        `gorm:"default:0" json:"retry_count"`
	MaxRetries    int        `gorm:"default:3" json:"max_retries"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	LastErrorCode string     `gorm:"type:varchar(100)" json:"last_error_code"`
	LastErrorMsg  string     `gorm:"type:varchar(500)" json:"last_error_msg"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (TaskJob) TableName() string {
	return "task_jobs"
}
