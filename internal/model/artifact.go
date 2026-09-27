package model

import "time"

type Artifact struct {
	ID               string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID           int64     `gorm:"not null;index" json:"-"`
	Kind             string    `gorm:"not null" json:"kind"`
	Title            string    `gorm:"not null" json:"title"`
	HeadVersion      int64     `gorm:"not null;default:0" json:"head_version"`
	CurrentVersionID *string   `json:"current_version_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type ArtifactVersion struct {
	ID                   string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	ArtifactID           string    `gorm:"not null;uniqueIndex:idx_artifact_revision,priority:1;index" json:"artifact_id"`
	Version              int64     `gorm:"not null;uniqueIndex:idx_artifact_revision,priority:2" json:"version"`
	BaseVersion          int64     `json:"base_version"`
	Origin               string    `json:"origin"`
	RunID                *string   `gorm:"uniqueIndex:idx_artifact_run_output,priority:1" json:"run_id"`
	OutputRole           string    `gorm:"not null;uniqueIndex:idx_artifact_run_output,priority:2" json:"-"`
	ManifestID           string    `gorm:"index" json:"manifest_id"`
	BodyJSON             string    `gorm:"type:jsonb;not null" json:"-"`
	Quality              string    `json:"quality"`
	WasCandidate         bool      `gorm:"not null;default:false" json:"was_candidate"`
	AdoptedFromVersionID *string   `gorm:"index" json:"adopted_from_version_id"`
	CreatedAt            time.Time `json:"created_at"`
}
type SourceManifest struct {
	ID          string     `gorm:"type:varchar(36);primaryKey" json:"manifest_id"`
	UserID      int64      `gorm:"not null;uniqueIndex:idx_source_generation,priority:1" json:"-"`
	SourceID    int64      `gorm:"not null;index;uniqueIndex:idx_source_generation,priority:2" json:"source_id"`
	ContentHash string     `gorm:"not null;uniqueIndex:idx_source_generation,priority:3" json:"-"`
	Title       string     `json:"title"`
	RevokedAt   *time.Time `json:"-"`
	CreatedAt   time.Time  `json:"-"`
}
type SourceSnapshotItem struct {
	ID              string `gorm:"type:varchar(36);primaryKey" json:"id"`
	ManifestID      string `gorm:"not null;index" json:"manifest_id"`
	SourceID        int64  `gorm:"not null;index" json:"source_id"`
	SourceTitle     string `json:"source_title"`
	SourceIdentity  string `json:"source_identity"`
	Modality        string `json:"modality"`
	Content         string `gorm:"type:text" json:"content"`
	ContentHash     string `json:"content_hash"`
	StartMS         *int64 `json:"start_ms"`
	EndMS           *int64 `json:"end_ms"`
	TimeRangeStatus string `json:"time_range_status"`
	Position        int    `json:"-"`
}
type ArtifactEvidenceRef struct {
	ID         uint64 `gorm:"primaryKey"`
	VersionID  string `gorm:"index;not null"`
	BlockID    string
	EvidenceID string `gorm:"index;not null"`
	Relation   string
}

// GenerationRequest contains immutable execution inputs, never execution state.
type GenerationRequest struct {
	ID                 string `gorm:"type:varchar(36);primaryKey"`
	RunID              string `gorm:"uniqueIndex;not null"`
	UserID             int64  `gorm:"not null;uniqueIndex:idx_generation_idempotency,priority:1"`
	IdempotencyKey     string `gorm:"not null;uniqueIndex:idx_generation_idempotency,priority:2"`
	RequestHash        string `gorm:"not null"`
	RequestJSON        string `gorm:"type:jsonb;not null"`
	ArtifactID         string `gorm:"index;not null"`
	ManifestID         string `gorm:"index;not null"`
	BaseVersion        int64
	Recipe             string
	ProfileID          int64
	ProfileFingerprint string
	ParentRunID        *string
	QueueDeadline      time.Time
	CreatedAt          time.Time
}
type GenerationDispatch struct {
	ID            string    `gorm:"type:varchar(36);primaryKey"`
	RunID         string    `gorm:"index;not null"`
	NextAttemptAt time.Time `gorm:"index"`
	PublishedAt   *time.Time
	LeaseToken    string
	LeaseUntil    *time.Time `gorm:"index"`
	CreatedAt     time.Time
}
type RunEvent struct {
	RunID     string    `gorm:"type:varchar(36);primaryKey" json:"run_id"`
	Seq       int64     `gorm:"primaryKey" json:"seq"`
	Type      string    `json:"type"`
	DataJSON  string    `gorm:"type:jsonb;not null" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
