package model

import "time"

// ImportRequest is HTTP request identity, separate from result caching and the
// existing TaskJob lease. Task creation and this record commit together.
type ImportRequest struct {
	ID                  string `gorm:"type:varchar(36);primaryKey"`
	UserID              int64  `gorm:"not null;uniqueIndex:idx_import_request_key,priority:1"`
	Action              string `gorm:"type:varchar(40);not null;uniqueIndex:idx_import_request_key,priority:2"`
	Key                 string `gorm:"type:varchar(128);not null;uniqueIndex:idx_import_request_key,priority:3"`
	RequestHash         string `gorm:"type:varchar(64);not null"`
	TaskID              int64  `gorm:"not null;index"`
	InitialGenerationID string `gorm:"type:varchar(36);not null;default:''" json:"-"`
	CreatedAt           time.Time
}

func (ImportRequest) TableName() string { return "import_requests" }
