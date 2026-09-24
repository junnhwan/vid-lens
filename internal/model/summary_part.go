package model

import "time"

// SummaryPart is a durable checkpoint for one leaf or merge call. Content is
// never exposed by the task detail API until every leaf has been covered.
type SummaryPart struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	TaskID     int64  `gorm:"not null;uniqueIndex:uk_summary_part"`
	Level      int    `gorm:"not null;uniqueIndex:uk_summary_part"`
	PartIndex  int    `gorm:"not null;uniqueIndex:uk_summary_part"`
	InputHash  string `gorm:"type:char(64);not null"`
	InputLimit int    `gorm:"default:0"`
	ModelName  string `gorm:"type:varchar(100)"`
	StartMS    int64  `gorm:"default:0"`
	EndMS      int64  `gorm:"default:0"`
	Status     string `gorm:"type:varchar(20);not null"`
	Content    string `gorm:"type:text"`
	ErrorMsg   string `gorm:"type:varchar(500)"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (SummaryPart) TableName() string { return "summary_parts" }

type SummaryProgress struct {
	Phase      string `json:"phase"`
	Completed  int    `json:"completed"`
	Total      int    `json:"total"`
	Current    int    `json:"current"`
	StartMS    int64  `json:"start_ms"`
	EndMS      int64  `json:"end_ms"`
	FailedPart int    `json:"failed_part,omitempty"`
}
