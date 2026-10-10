package model

import "time"

// SummaryTagIntent is publication state owned by the existing summary job.
// It never acquires a separate execution lease.
type SummaryTagIntent struct {
	UserID             int64  `gorm:"primaryKey"`
	TaskID             int64  `gorm:"primaryKey"`
	GenerationID       string `gorm:"type:varchar(36);primaryKey"`
	GeneratedVersion   int64  `gorm:"primaryKey"`
	SourceDigest       string `gorm:"type:char(64);not null"`
	ExpectedTagVersion int64  `gorm:"not null"`
	Enabled            bool   `gorm:"not null"`
	CandidatesJSON     string `gorm:"type:text;not null"`
	CandidateDigest    string `gorm:"type:char(64);not null"`
	Status             string `gorm:"type:varchar(20);not null;index"`
	ErrorCode          string `gorm:"type:varchar(80)"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
