package model

import "time"

const (
	TranscriptionChunkStatusPending   = "pending"
	TranscriptionChunkStatusRunning   = "running"
	TranscriptionChunkStatusCompleted = "completed"
	TranscriptionChunkStatusFailed    = "failed"
)

type VideoTranscriptionChunk struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID      int64     `gorm:"index;uniqueIndex:idx_task_transcription_chunk;not null" json:"task_id"`
	ChunkIndex  int       `gorm:"uniqueIndex:idx_task_transcription_chunk;not null" json:"chunk_index"`
	AudioObject string    `gorm:"type:varchar(500)" json:"audio_object"`
	StartSecond int       `gorm:"default:0" json:"start_second"`
	EndSecond   int       `gorm:"default:0" json:"end_second"`
	Status      string    `gorm:"type:varchar(30);index;not null" json:"status"`
	Content     string    `gorm:"type:longtext" json:"content"`
	Chars       int       `gorm:"default:0" json:"chars"`
	ErrorMsg    string    `gorm:"type:varchar(500)" json:"error_msg"`
	RetryCount  int       `gorm:"default:0" json:"retry_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (VideoTranscriptionChunk) TableName() string {
	return "video_transcription_chunks"
}
