package model

import "time"

const (
	VisualFrameStatusPending   = "pending"
	VisualFrameStatusCompleted = "completed"
	VisualFrameStatusFailed    = "failed"
	VisualFrameStatusSkipped   = "skipped"
)

// VideoVisualFrame stores one keyframe evidence row for a task.
// OCR text is the searchable fact; ObjectKey points at the MinIO evidence image.
// This table is task-owned and deleted with the task cleanup path.
type VideoVisualFrame struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID        int64     `gorm:"index;uniqueIndex:idx_task_visual_frame;not null" json:"task_id"`
	FrameIndex    int       `gorm:"uniqueIndex:idx_task_visual_frame;not null" json:"frame_index"`
	TimeMs        int64     `gorm:"index;not null" json:"time_ms"`
	ObjectKey     string    `gorm:"type:varchar(500)" json:"object_key"`
	OCRText       string    `gorm:"type:text" json:"ocr_text"`
	Phash         string    `gorm:"type:varchar(64);index" json:"phash"`
	Source        string    `gorm:"type:varchar(30);not null" json:"source"` // scene | interval | manual
	CaptionMethod string    `gorm:"type:varchar(20)" json:"caption_method"`  // vision | ocr
	Status        string    `gorm:"type:varchar(30);index;not null" json:"status"`
	ErrorMsg      string    `gorm:"type:varchar(500)" json:"error_msg"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (VideoVisualFrame) TableName() string {
	return "video_visual_frames"
}
