package model

import "time"

// ArtifactCanvasLayout stores immutable visual revisions. Content remains in ArtifactVersion.
type ArtifactCanvasLayout struct {
	ID               string    `gorm:"type:varchar(36);primaryKey"`
	UserID           int64     `gorm:"not null;index;uniqueIndex:idx_canvas_revision,priority:1;uniqueIndex:idx_canvas_key,priority:1"`
	ArtifactID       string    `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_canvas_revision,priority:2;uniqueIndex:idx_canvas_key,priority:2"`
	ContentVersionID string    `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_canvas_revision,priority:3"`
	ViewID           string    `gorm:"type:varchar(32);not null;uniqueIndex:idx_canvas_revision,priority:4"`
	Revision         int64     `gorm:"not null;uniqueIndex:idx_canvas_revision,priority:5"`
	IdempotencyKey   string    `gorm:"type:varchar(128);not null;uniqueIndex:idx_canvas_key,priority:3"`
	RequestHash      string    `gorm:"type:varchar(64);not null"`
	LayoutJSON       string    `gorm:"type:jsonb;not null"`
	CreatedAt        time.Time `json:"created_at"`
}
