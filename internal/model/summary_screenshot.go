package model

import "time"

// SummaryScreenshotRef authorizes a real, inspected observation for one
// generation and block. Documents expose only its opaque ID, never object keys.
type SummaryScreenshotRef struct {
	ID            string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID        int64     `gorm:"not null;index" json:"-"`
	TaskID        int64     `gorm:"not null;index" json:"-"`
	GenerationID  string    `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_summary_image_registration,priority:1" json:"-"`
	SourceID      string    `gorm:"type:varchar(36);not null;index" json:"-"`
	SourceDigest  string    `gorm:"type:char(64);not null" json:"-"`
	MediaRevision string    `gorm:"type:varchar(128);not null" json:"-"`
	BlockID       string    `gorm:"type:varchar(128);not null;uniqueIndex:idx_summary_image_registration,priority:2" json:"block_id"`
	ObservationID string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_summary_image_registration,priority:3" json:"-"`
	ObjectKey     string    `gorm:"type:varchar(1000);not null" json:"-"`
	CaptureMS     int64     `gorm:"not null" json:"capture_ms"`
	Inspected     bool      `gorm:"not null;default:false" json:"-"`
	Status        string    `gorm:"type:varchar(16);not null" json:"-"`
	CreatedAt     time.Time `gorm:"not null" json:"created_at"`
}

func (SummaryScreenshotRef) TableName() string { return "summary_screenshot_refs" }
