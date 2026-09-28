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
	EditOperationID      *string   `gorm:"type:varchar(36);uniqueIndex" json:"edit_operation_id"`
	OutputRole           string    `gorm:"not null;uniqueIndex:idx_artifact_run_output,priority:2" json:"-"`
	ManifestID           string    `gorm:"index" json:"manifest_id"`
	BodyJSON             string    `gorm:"type:jsonb;not null" json:"-"`
	Quality              string    `json:"quality"`
	WasCandidate         bool      `gorm:"not null;default:false" json:"was_candidate"`
	AdoptedFromVersionID *string   `gorm:"index" json:"adopted_from_version_id"`
	CreatedAt            time.Time `json:"created_at"`
}

const (
	AgentRunSubjectGeneration   = "generation_request"
	AgentRunSubjectArtifactEdit = "artifact_edit_request"
	AgentRunSubjectSummaryEdit  = "summary_edit_request"
)

// ArtifactEditRequest is the immutable, owner-scoped authority for one edit
// run. Execution progress belongs to AgentRun; these fields never change.
type ArtifactEditRequest struct {
	ID                   string    `gorm:"type:varchar(36);primaryKey"`
	RunID                string    `gorm:"type:varchar(36);uniqueIndex;not null"`
	UserID               int64     `gorm:"not null;uniqueIndex:idx_artifact_edit_request_idem,priority:1;index"`
	IdempotencyKey       string    `gorm:"type:varchar(128);not null;uniqueIndex:idx_artifact_edit_request_idem,priority:2"`
	RequestHash          string    `gorm:"type:char(64);not null"`
	RequestJSON          string    `gorm:"type:jsonb;not null"`
	ArtifactID           string    `gorm:"type:varchar(36);not null;index"`
	BaseVersionID        string    `gorm:"type:varchar(36);not null;index"`
	BaseVersion          int64     `gorm:"not null"`
	ManifestID           string    `gorm:"type:varchar(36);not null;index"`
	Instruction          string    `gorm:"type:text;not null"`
	Mode                 string    `gorm:"type:varchar(16);not null"`
	SelectedBlockIDsJSON string    `gorm:"type:jsonb;not null"`
	Recipe               string    `gorm:"type:varchar(40);not null"`
	ProfileID            int64     `gorm:"not null;default:0"`
	ProfileFingerprint   string    `gorm:"not null"`
	BudgetJSON           string    `gorm:"type:jsonb;not null;default:'{}'"`
	ToolPolicyJSON       string    `gorm:"type:jsonb;not null"`
	QueueDeadline        time.Time `gorm:"not null;index"`
	CreatedAt            time.Time `gorm:"not null"`
}

const (
	ArtifactEditOperationKindEdit = "edit"
	ArtifactEditOperationKindUndo = "undo"

	ArtifactEditOperationProposed  = "proposed"
	ArtifactEditOperationCommitted = "committed"
)

// ArtifactEditOperation is the durable proposal/commit identity. Patch and
// authorization JSON are canonical server-owned data, never raw model text.
type ArtifactEditOperation struct {
	ID                 string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID             int64      `gorm:"not null;index" json:"-"`
	ArtifactID         string     `gorm:"type:varchar(36);not null;index" json:"artifact_id"`
	RequestID          *string    `gorm:"type:varchar(36);index" json:"-"`
	RunID              *string    `gorm:"type:varchar(36);uniqueIndex;index" json:"-"`
	Kind               string     `gorm:"type:varchar(16);not null" json:"-"`
	ParentOperationID  *string    `gorm:"type:varchar(36);index" json:"-"`
	BaseVersionID      string     `gorm:"type:varchar(36);not null;index" json:"base_version_id"`
	BaseVersion        int64      `gorm:"not null" json:"base_version"`
	ManifestID         string     `gorm:"type:varchar(36);not null;index" json:"-"`
	CanonicalPatchJSON string     `gorm:"type:jsonb;not null" json:"-"`
	PatchHash          string     `gorm:"type:char(64);not null" json:"-"`
	AuthorizationJSON  string     `gorm:"type:jsonb;not null" json:"-"`
	ScopeJSON          string     `gorm:"type:jsonb;not null" json:"-"`
	ToolSchemaDigest   string     `gorm:"type:char(64);not null" json:"-"`
	Basis              string     `gorm:"type:varchar(32);not null" json:"basis"`
	EvidenceIDsJSON    string     `gorm:"type:jsonb;not null" json:"-"`
	Status             string     `gorm:"type:varchar(16);not null;index" json:"status"`
	Summary            string     `gorm:"type:text;not null" json:"summary"`
	CountsJSON         string     `gorm:"type:jsonb;not null" json:"-"`
	ChangesJSON        string     `gorm:"type:jsonb;not null" json:"-"`
	BlockMappingsJSON  string     `gorm:"type:jsonb;not null" json:"-"`
	ResultVersionID    *string    `gorm:"type:varchar(36);uniqueIndex" json:"result_version_id"`
	UndoVersionID      *string    `gorm:"type:varchar(36);index" json:"undo_version_id"`
	RunFinalizedAt     *time.Time `json:"-"`
	CreatedAt          time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null" json:"updated_at"`
	CommittedAt        *time.Time `json:"committed_at,omitempty"`
}

// ArtifactEditOutcome gives apply and undo their own owner-scoped idempotency
// namespace. It is inserted in the same transaction as the immutable version.
type ArtifactEditOutcome struct {
	UserID         int64     `gorm:"primaryKey"`
	IdempotencyKey string    `gorm:"type:varchar(128);primaryKey"`
	RequestHash    string    `gorm:"type:char(64);not null"`
	Action         string    `gorm:"type:varchar(16);not null"`
	OperationID    string    `gorm:"type:varchar(36);not null;index"`
	VersionID      string    `gorm:"type:varchar(36);not null"`
	CreatedAt      time.Time `gorm:"not null"`
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
	CitationID string
}

// LearningPosition is one durable resume target per user. Revision is a CAS
// token shared by tabs; stale writes cannot replace a newer target.
type LearningPosition struct {
	UserID     int64     `gorm:"primaryKey" json:"-"`
	Revision   int64     `gorm:"not null" json:"revision"`
	TaskID     int64     `gorm:"not null" json:"task_id"`
	ArtifactID string    `gorm:"type:varchar(36)" json:"artifact_id"`
	VersionID  string    `gorm:"type:varchar(36)" json:"version_id"`
	BlockID    string    `gorm:"type:varchar(100)" json:"block_id"`
	TimeMS     int64     `gorm:"not null" json:"time_ms"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AnswerImport struct {
	UserID      int64  `gorm:"primaryKey"`
	Key         string `gorm:"type:varchar(128);primaryKey"`
	RequestHash string `gorm:"not null"`
	ArtifactID  string `gorm:"not null"`
	VersionID   string `gorm:"not null"`
	CreatedAt   time.Time
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

// ArtifactEditDispatch is intentionally a separate durable outbox from
// GenerationDispatch. During a rolling deployment legacy artifact workers can
// only lease the generation table, so they can never publish an edit run onto
// the legacy generation queue.
type ArtifactEditDispatch struct {
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
