package model

import "time"

const (
	TranscriptionChunkStatusPending   = "pending"
	TranscriptionChunkStatusRunning   = "running"
	TranscriptionChunkStatusRetryWait = "retry_wait"
	TranscriptionChunkStatusCompleted = "completed"
	TranscriptionChunkStatusFailed    = "failed"
)

type VideoTranscriptionChunk struct {
	ID               int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID           int64      `gorm:"index;uniqueIndex:idx_task_transcription_chunk;not null" json:"task_id"`
	ChunkIndex       int        `gorm:"uniqueIndex:idx_task_transcription_chunk;not null" json:"chunk_index"`
	AudioObject      string     `gorm:"type:varchar(500)" json:"audio_object"`
	SegmentKey       string     `gorm:"type:varchar(200);index" json:"segment_key"`
	SegmenterVersion string     `gorm:"type:varchar(50)" json:"segmenter_version"`
	WindowStartMS    int64      `gorm:"default:0" json:"window_start_ms"`
	WindowEndMS      int64      `gorm:"default:0" json:"window_end_ms"`
	CoreStartMS      int64      `gorm:"default:0" json:"core_start_ms"`
	CoreEndMS        int64      `gorm:"default:0" json:"core_end_ms"`
	StartSecond      int        `gorm:"default:0" json:"start_second"`
	EndSecond        int        `gorm:"default:0" json:"end_second"`
	Status           string     `gorm:"type:varchar(30);index;not null" json:"status"`
	Content          string     `gorm:"type:text" json:"content"`
	TimedSegments    string     `gorm:"type:text" json:"-"`
	Chars            int        `gorm:"default:0" json:"chars"`
	ErrorMsg         string     `gorm:"type:varchar(500)" json:"error_msg"`
	RetryCount       int        `gorm:"default:0" json:"retry_count"`
	WaitReason       string     `gorm:"type:varchar(40)" json:"wait_reason"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// TranscriptionSegment preserves provider-observed speech timing. Persisted
// segments use absolute video milliseconds; ASR adapters use local audio time
// until the consumer validates and offsets them by the audio window start.
type TranscriptionSegment struct {
	Text    string `json:"text"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	// Rune offsets are validated against the immutable ASR window text. They
	// distinguish repeated words and allow time ownership without estimating
	// timing from character positions. Legacy provider spans omit them.
	TextStart int    `json:"text_start,omitempty"`
	TextEnd   int    `json:"text_end,omitempty"`
	Method    string `json:"method,omitempty"`
}

func (VideoTranscriptionChunk) TableName() string {
	return "video_transcription_chunks"
}
