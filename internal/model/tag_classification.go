package model

import "time"

// An immutable receipt makes retries of one generation's candidate publication
// harmless after the user changes their decisions; it is not another workflow.
type VideoTagClassification struct {
	UserID            int64  `gorm:"primaryKey"`
	TaskID            int64  `gorm:"primaryKey"`
	GenerationID      string `gorm:"type:varchar(36);primaryKey"`
	GeneratedVersion  int64  `gorm:"primaryKey"`
	SourceDigest      string `gorm:"type:char(64);not null"`
	CandidateDigest   string `gorm:"type:char(64);not null"`
	AppliedTagVersion int64  `gorm:"not null"`
	CreatedAt         time.Time
}
