package model

import "time"

const (
	VisualProgressQueued    = "queued"
	VisualProgressRunning   = "running"
	VisualProgressCompleted = "completed"
	VisualProgressSkipped   = "skipped"
	VisualProgressFailed    = "failed"
	VisualProgressCanceled  = "canceled"
)

// VideoVisualProgress is transient work state, separate from the published
// VideoVisualFrame evidence set. One row records the latest processing attempt.
type VideoVisualProgress struct {
	TaskID       int64     `gorm:"primaryKey" json:"task_id"`
	AttemptToken string    `gorm:"type:varchar(64);not null" json:"-"`
	Status       string    `gorm:"type:varchar(20);not null" json:"status"`
	Phase        string    `gorm:"type:varchar(30);not null" json:"phase"`
	TotalKnown   bool      `gorm:"not null;default:false" json:"total_known"`
	TotalFrames  int       `gorm:"not null;default:0" json:"total_frames"`
	Processed    int       `gorm:"not null;default:0" json:"processed_frames"`
	Failed       int       `gorm:"not null;default:0" json:"failed_frames"`
	OCRFailed    int       `gorm:"not null;default:0" json:"ocr_failed_frames"`
	VisionFailed int       `gorm:"not null;default:0" json:"vision_failed_frames"`
	ErrorCode    string    `gorm:"type:varchar(50)" json:"error_code,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (VideoVisualProgress) TableName() string { return "video_visual_progress" }
