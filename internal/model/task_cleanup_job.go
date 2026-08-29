package model

import "time"

const (
	TaskCleanupStatusPending   = "pending"
	TaskCleanupStatusRunning   = "running"
	TaskCleanupStatusFailed    = "failed"
	TaskCleanupStatusCompleted = "completed"
)

// TaskCleanupJob is the durable intent for removing one task's projections and
// external resources. PostgreSQL owns this lifecycle; MinIO, Redis, and the
// vector backend are idempotent cleanup targets.
type TaskCleanupJob struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID         int64      `gorm:"not null;uniqueIndex" json:"task_id"`
	UserID         int64      `gorm:"not null;index" json:"user_id"`
	AssetID        *int64     `gorm:"index" json:"asset_id,omitempty"`
	ObjectName     string     `gorm:"type:varchar(500)" json:"object_name"`
	FileMD5        string     `gorm:"type:char(32);index" json:"file_md5"`
	Status         string     `gorm:"type:varchar(20);not null;index" json:"status"`
	Attempts       int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt    *time.Time `gorm:"index" json:"next_retry_at,omitempty"`
	LeaseToken     string     `gorm:"type:varchar(64);index" json:"-"`
	LeaseExpiresAt *time.Time `gorm:"index" json:"-"`
	LastError      string     `gorm:"type:varchar(1000)" json:"last_error"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (TaskCleanupJob) TableName() string {
	return "task_cleanup_jobs"
}
