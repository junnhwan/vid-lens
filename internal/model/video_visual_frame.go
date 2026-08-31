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
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID          int64     `gorm:"index;uniqueIndex:idx_task_visual_frame;not null" json:"task_id"`
	FrameIndex      int       `gorm:"uniqueIndex:idx_task_visual_frame;not null" json:"frame_index"`
	FrameKey        string    `gorm:"type:varchar(160);index;not null;default:''" json:"frame_key"`
	TimeMs          int64     `gorm:"index;not null" json:"time_ms"`
	StartMS         int64     `gorm:"index;not null;default:0" json:"start_ms"`
	EndMS           int64     `gorm:"index;not null;default:0" json:"end_ms"`
	TimeStatus      string    `gorm:"type:varchar(20);index;not null;default:'unknown'" json:"time_status"`
	ObjectKey       string    `gorm:"type:varchar(500)" json:"object_key"`
	OCRText         string    `gorm:"type:text" json:"ocr_text"`
	VisionCaption   string    `gorm:"type:text" json:"vision_caption"`
	Phash           string    `gorm:"type:varchar(64);index" json:"phash"`
	Source          string    `gorm:"type:varchar(30);not null" json:"source"` // scene | interval | manual
	SamplingVersion string    `gorm:"type:varchar(50);index;not null;default:''" json:"sampling_version"`
	CaptionMethod   string    `gorm:"type:varchar(20)" json:"caption_method"` // legacy projection: vision | ocr | vision+ocr
	OCRStatus       string    `gorm:"type:varchar(30);index;not null;default:'pending'" json:"ocr_status"`
	VisionStatus    string    `gorm:"type:varchar(30);index;not null;default:'pending'" json:"vision_status"`
	OCRError        string    `gorm:"type:varchar(500)" json:"ocr_error"`
	VisionError     string    `gorm:"type:varchar(500)" json:"vision_error"`
	Status          string    `gorm:"type:varchar(30);index;not null" json:"status"`
	ErrorMsg        string    `gorm:"type:varchar(500)" json:"error_msg"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (VideoVisualFrame) TableName() string {
	return "video_visual_frames"
}
