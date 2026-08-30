package model

import "time"

const (
	TranscriptionChunkStatusPending   = "pending"
	TranscriptionChunkStatusRunning   = "running"
	TranscriptionChunkStatusCompleted = "completed"
	TranscriptionChunkStatusFailed    = "failed"
)

type VideoTranscriptionChunk struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID           int64     `gorm:"index;uniqueIndex:idx_task_transcription_chunk;not null" json:"task_id"`
	ChunkIndex       int       `gorm:"uniqueIndex:idx_task_transcription_chunk;not null" json:"chunk_index"`
	AudioObject      string    `gorm:"type:varchar(500)" json:"audio_object"`
	SegmentKey       string    `gorm:"type:varchar(200);index" json:"segment_key"`
	SegmenterVersion string    `gorm:"type:varchar(50)" json:"segmenter_version"`
	WindowStartMS    int64     `gorm:"default:0" json:"window_start_ms"`
	WindowEndMS      int64     `gorm:"default:0" json:"window_end_ms"`
	CoreStartMS      int64     `gorm:"default:0" json:"core_start_ms"`
	CoreEndMS        int64     `gorm:"default:0" json:"core_end_ms"`
	StartSecond      int       `gorm:"default:0" json:"start_second"`
	EndSecond        int       `gorm:"default:0" json:"end_second"`
	Status           string    `gorm:"type:varchar(30);index;not null" json:"status"`
	Content          string    `gorm:"type:text" json:"content"`
	Chars            int       `gorm:"default:0" json:"chars"`
	ErrorMsg         string    `gorm:"type:varchar(500)" json:"error_msg"`
	RetryCount       int       `gorm:"default:0" json:"retry_count"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (VideoTranscriptionChunk) TableName() string {
	return "video_transcription_chunks"
}
