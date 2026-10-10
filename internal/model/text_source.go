package model

import "time"

const (
	TextSourcePlatformSubtitle = "platform_subtitle"
	TextSourceASR              = "asr"
	TextSourceLegacy           = "legacy"
	SummaryHashMarkdown        = "markdown-v1"
	SummaryHashDocument        = "summary-json-v2"
)

// VideoTextSource is an immutable, task-owned source. File hashes identify
// media; they never authorize reuse of platform subtitles or private intent.
type VideoTextSource struct {
	ID               string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID           int64     `gorm:"not null;uniqueIndex:idx_text_source_digest,priority:1" json:"-"`
	TaskID           int64     `gorm:"not null;index;uniqueIndex:idx_text_source_digest,priority:2" json:"task_id"`
	Kind             string    `gorm:"type:varchar(32);not null" json:"kind"`
	IdentityJSON     string    `gorm:"type:text;not null" json:"-"`
	MediaFingerprint string    `gorm:"type:varchar(128);not null" json:"media_fingerprint"`
	Language         string    `gorm:"type:varchar(64)" json:"language"`
	TrackKey         string    `gorm:"type:varchar(255)" json:"track_key,omitempty"`
	SubtitleKind     string    `gorm:"type:varchar(16)" json:"subtitle_kind,omitempty"`
	KindBasis        string    `gorm:"type:varchar(255)" json:"kind_basis,omitempty"`
	RawObjectKey     string    `gorm:"type:varchar(1000)" json:"-"`
	RawHash          string    `gorm:"type:char(64)" json:"-"`
	CanonicalText    string    `gorm:"type:text;not null" json:"-"`
	CanonicalHash    string    `gorm:"type:char(64);not null" json:"-"`
	SourceDigest     string    `gorm:"type:char(64);not null;uniqueIndex:idx_text_source_digest,priority:3" json:"source_digest"`
	ParserVersion    string    `gorm:"type:varchar(64);not null" json:"parser_version"`
	Quality          string    `gorm:"type:varchar(16);not null" json:"quality"`
	WarningsJSON     string    `gorm:"type:text;not null;default:'[]'" json:"-"`
	CreatedAt        time.Time `gorm:"not null" json:"created_at"`
}

func (VideoTextSource) TableName() string { return "video_text_sources" }

// Subtitle cues are not ASR chunks: no audio object, provider usage, or
// alignment is fabricated. Unknown timing stays null in storage and responses.
type VideoTextCue struct {
	JoinBefore   *string `gorm:"type:text" json:"join_before,omitempty"`
	ID           int64   `gorm:"primaryKey;autoIncrement" json:"-"`
	SourceID     string  `gorm:"type:varchar(36);not null;uniqueIndex:idx_text_cue_id,priority:1;index:idx_text_cue_order,priority:1" json:"source_id"`
	CueID        string  `gorm:"type:varchar(128);not null;uniqueIndex:idx_text_cue_id,priority:2" json:"cue_id"`
	Order        int     `gorm:"not null;index:idx_text_cue_order,priority:2" json:"order"`
	RawText      string  `gorm:"type:text;not null" json:"-"`
	Text         string  `gorm:"type:text;not null" json:"text"`
	StartMS      *int64  `json:"start_ms"`
	EndMS        *int64  `json:"end_ms"`
	TimingMethod string  `gorm:"type:varchar(32);not null" json:"timing_method"`
	RawRefsJSON  string  `gorm:"type:text;not null;default:'[]'" json:"-"`
}

func (VideoTextCue) TableName() string { return "video_text_cues" }
