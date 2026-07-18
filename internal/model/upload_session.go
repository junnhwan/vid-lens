package model

import "time"

const (
	UploadSessionStatusActive     = "active"
	UploadSessionStatusCompleting = "completing"
	UploadSessionStatusCompleted  = "completed"
	UploadSessionStatusFailed     = "failed"
	UploadSessionStatusExpired    = "expired"
)

// UploadSession is the durable owner of one authenticated user's immutable
// chunk-upload manifest and completion lifecycle. PostgreSQL, not Redis, is the
// source of truth for resumability and idempotency.
type UploadSession struct {
	ID                       string     `gorm:"type:varchar(36);primaryKey" json:"session_id"`
	UserID                   int64      `gorm:"not null;index" json:"user_id"`
	Filename                 string     `gorm:"type:varchar(255);not null" json:"filename"`
	FileSize                 int64      `gorm:"not null" json:"file_size"`
	ChunkSize                int64      `gorm:"not null" json:"chunk_size"`
	TotalChunks              int        `gorm:"not null" json:"total_chunks"`
	ExpectedMD5              string     `gorm:"type:char(32);not null;index" json:"expected_md5"`
	VerifiedMD5              string     `gorm:"type:char(32)" json:"verified_md5,omitempty"`
	ManifestFingerprint      string     `gorm:"type:char(64);not null" json:"-"`
	ActiveKey                *string    `gorm:"type:varchar(160);uniqueIndex" json:"-"`
	Status                   string     `gorm:"type:varchar(20);not null;index" json:"status"`
	FinalObjectName          string     `gorm:"type:varchar(500)" json:"final_object_name,omitempty"`
	AssetID                  *int64     `gorm:"index" json:"asset_id,omitempty"`
	TaskID                   *int64     `gorm:"index" json:"task_id,omitempty"`
	CompletionToken          string     `gorm:"type:varchar(64);index" json:"-"`
	CompletionLeaseExpiresAt *time.Time `gorm:"index" json:"-"`
	ExpiresAt                time.Time  `gorm:"not null;index" json:"expires_at"`
	LastError                string     `gorm:"type:varchar(1000)" json:"last_error,omitempty"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

func (UploadSession) TableName() string {
	return "upload_sessions"
}
