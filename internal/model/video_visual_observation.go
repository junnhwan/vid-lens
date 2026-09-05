package model

import "time"

const (
	VisualObservationStatusCaptured = "captured"
	VisualObservationStatusObserved = "observed"
	VisualObservationStatusFailed   = "failed"
)

// VideoVisualObservation is an append-only query-time observation. It is
// deliberately separate from VideoVisualFrame: offline indexing can replace
// its projection, while an investigation must remain replayable.
type VideoVisualObservation struct {
	ID                   string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID               int64     `gorm:"index;not null" json:"user_id"`
	TaskID               int64     `gorm:"index;not null" json:"task_id"`
	TraceRef             string    `gorm:"type:varchar(80);index;not null" json:"trace_ref"`
	CacheKey             string    `gorm:"type:char(64);uniqueIndex;not null" json:"cache_key"`
	VideoRevision        string    `gorm:"type:varchar(255);index;not null" json:"video_revision"`
	FrameRef             string    `gorm:"type:varchar(160);index;not null" json:"frame_ref"`
	FFmpegArgs           string    `gorm:"type:text;not null;default:'[]'" json:"ffmpeg_args"`
	ObjectKey            string    `gorm:"type:varchar(500);not null" json:"object_key"`
	StartMS              int64     `gorm:"index;not null" json:"start_ms"`
	EndMS                int64     `gorm:"index;not null" json:"end_ms"`
	Source               string    `gorm:"type:varchar(40);not null" json:"source"`
	CapturePolicyVersion string    `gorm:"type:varchar(80);not null" json:"capture_policy_version"`
	Model                string    `gorm:"type:varchar(160);not null;default:''" json:"model,omitempty"`
	PromptVersion        string    `gorm:"type:varchar(80);not null" json:"prompt_version"`
	Observation          string    `gorm:"type:text;not null;default:''" json:"observation,omitempty"`
	StructuredFacts      string    `gorm:"type:text;not null;default:'[]'" json:"structured_facts"`
	StructuredGaps       string    `gorm:"type:text;not null;default:'[]'" json:"structured_gaps"`
	RawResponseHash      string    `gorm:"type:char(64);not null;default:''" json:"raw_response_hash,omitempty"`
	Status               string    `gorm:"type:varchar(20);index;not null" json:"status"`
	ErrorMsg             string    `gorm:"type:varchar(500);not null;default:''" json:"error_msg,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

func (VideoVisualObservation) TableName() string { return "video_visual_observations" }
