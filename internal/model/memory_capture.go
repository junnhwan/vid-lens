package model

import "time"

// MemoryCaptureJob references the original user message; it never duplicates
// conversation text. A completed job remains an idempotency tombstone.
type MemoryCaptureJob struct {
	MessageID   int64     `gorm:"primaryKey;autoIncrement:false"`
	UserID      int64     `gorm:"not null;index"`
	SessionID   int64     `gorm:"not null;index"`
	Status      string    `gorm:"type:varchar(20);not null;index"`
	Attempts    int       `gorm:"not null;default:0"`
	LeaseToken  string    `gorm:"type:varchar(36);not null;default:''"`
	AvailableAt time.Time `gorm:"not null;index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
