package model

import "time"

// SummaryRevisionHead is scoped to the viewer's task, never to the shared
// file_md5 cache row. A revision remains available when that row is regenerated.
type SummaryRevisionHead struct {
	UserID            int64  `gorm:"primaryKey"`
	TaskID            int64  `gorm:"primaryKey"`
	Version           int64  `gorm:"not null"`
	CurrentRevisionID string `gorm:"type:varchar(36);not null"`
}

type SummaryRevision struct {
	ID                string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID            int64     `gorm:"not null;uniqueIndex:idx_summary_revision_scope,priority:1" json:"-"`
	TaskID            int64     `gorm:"not null;uniqueIndex:idx_summary_revision_scope,priority:2" json:"task_id"`
	Version           int64     `gorm:"not null;uniqueIndex:idx_summary_revision_scope,priority:3" json:"version"`
	Content           string    `gorm:"type:text;not null" json:"content"`
	BaseGeneratedHash string    `gorm:"type:char(64);not null" json:"base_generated_hash"`
	Origin            string    `gorm:"type:varchar(24);not null" json:"origin"`
	OperationID       *string   `gorm:"type:varchar(36);uniqueIndex" json:"operation_id,omitempty"`
	ParentRevisionID  *string   `gorm:"type:varchar(36)" json:"parent_revision_id,omitempty"`
	CreatedAt         time.Time `gorm:"not null" json:"created_at"`
}

type SummaryRevisionState struct {
	Version              int64  `json:"version"`
	RevisionID           string `json:"revision_id"`
	BaseGeneratedHash    string `json:"base_generated_hash"`
	CurrentGeneratedHash string `json:"current_generated_hash"`
	SourceStatus         string `json:"source_status"`
	Origin               string `json:"origin"`
}

// SummaryEditOperation is both the idempotency record and the durable diff.
// Public reads must first check that the owning VideoTask is still readable.
type SummaryEditOperation struct {
	ID                 string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID             int64      `gorm:"not null;uniqueIndex:idx_summary_edit_key,priority:1" json:"-"`
	TaskID             int64      `gorm:"not null;index" json:"task_id"`
	Key                string     `gorm:"type:varchar(128);not null;uniqueIndex:idx_summary_edit_key,priority:2" json:"-"`
	RequestHash        string     `gorm:"type:char(64);not null" json:"-"`
	RunID              string     `gorm:"type:varchar(36);not null;uniqueIndex" json:"run_id"`
	Mode               string     `gorm:"type:varchar(16);not null" json:"mode"`
	Status             string     `gorm:"type:varchar(20);not null" json:"status"`
	Instruction        string     `gorm:"type:text;not null" json:"instruction"`
	BaseVersion        int64      `gorm:"not null" json:"base_version"`
	BaseContentHash    string     `gorm:"type:char(64);not null" json:"base_content_hash"`
	BaseContent        string     `gorm:"type:text;not null" json:"-"`
	BaseGeneratedHash  string     `gorm:"type:char(64);not null" json:"base_generated_hash"`
	RuleVersion        int64      `gorm:"not null;default:0" json:"rule_version"`
	RuleDigest         string     `gorm:"type:char(64);not null" json:"rule_digest"`
	RuleSnapshotJSON   string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ProfileID          int64      `gorm:"not null" json:"-"`
	ProfileFingerprint string     `gorm:"not null" json:"-"`
	PatchJSON          string     `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	ResultRevisionID   *string    `gorm:"type:varchar(36)" json:"result_revision_id,omitempty"`
	UndoRevisionID     *string    `gorm:"type:varchar(36)" json:"undo_revision_id,omitempty"`
	ErrorCode          string     `gorm:"type:varchar(80);not null;default:''" json:"error_code,omitempty"`
	CreatedAt          time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null" json:"updated_at"`
	CommittedAt        *time.Time `json:"committed_at,omitempty"`
}

// SummaryEditDispatch is a separate outbox/queue identity. Legacy artifact
// workers cannot accidentally acknowledge a summary edit as an artifact edit.
type SummaryEditDispatch struct {
	ID            string    `gorm:"type:varchar(36);primaryKey"`
	RunID         string    `gorm:"index;not null"`
	NextAttemptAt time.Time `gorm:"index"`
	PublishedAt   *time.Time
	LeaseToken    string
	LeaseUntil    *time.Time `gorm:"index"`
	CreatedAt     time.Time
}

type VideoTermRuleHead struct {
	UserID  int64  `gorm:"primaryKey"`
	TaskID  int64  `gorm:"primaryKey"`
	Version int64  `gorm:"not null"`
	Digest  string `gorm:"type:char(64);not null"`
}

// RulesJSON is a complete immutable snapshot, so an in-flight run can load
// exactly the version it froze even after a rule is disabled or edited.
type VideoTermRuleVersion struct {
	ID                string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID            int64     `gorm:"not null;uniqueIndex:idx_term_rule_version_scope,priority:1" json:"-"`
	TaskID            int64     `gorm:"not null;uniqueIndex:idx_term_rule_version_scope,priority:2" json:"task_id"`
	Version           int64     `gorm:"not null;uniqueIndex:idx_term_rule_version_scope,priority:3" json:"version"`
	Digest            string    `gorm:"type:char(64);not null" json:"digest"`
	RulesJSON         string    `gorm:"type:jsonb;not null" json:"-"`
	LinkedOperationID *string   `gorm:"type:varchar(36);index" json:"linked_operation_id,omitempty"`
	CreatedAt         time.Time `gorm:"not null" json:"created_at"`
}
