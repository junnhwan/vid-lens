package model

import "time"

const MemoryExtractorVersion = "explicit-preferences/v2"

// MemoryCaptureJob references the original user message; it never duplicates
// conversation text. A completed job remains an idempotency tombstone.
type MemoryCaptureJob struct {
	ExtractorVersion  string    `gorm:"type:varchar(80);not null;default:'explicit-preferences/v2'"`
	ProjectionPending bool      `gorm:"not null;default:false"`
	MessageID         int64     `gorm:"primaryKey;autoIncrement:false"`
	UserID            int64     `gorm:"not null;index"`
	SessionID         int64     `gorm:"not null;index"`
	Status            string    `gorm:"type:varchar(20);not null;index"`
	Attempts          int       `gorm:"not null;default:0"`
	LeaseToken        string    `gorm:"type:varchar(36);not null;default:''"`
	AvailableAt       time.Time `gorm:"not null;index"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
