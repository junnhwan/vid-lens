package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	MemoryScopeUser          = "user"
	MemoryScopeVideo         = "video"
	MemoryScopeKnowledgeBase = "knowledge_base"
	MemoryScopeRun           = "run"
)

const (
	MemoryStatusActive     = "active"
	MemoryStatusConflicted = "conflicted"
	MemoryStatusWithdrawn  = "withdrawn"
	MemoryStatusDeleted    = "deleted"
)

const (
	MemoryEventCreated    = "created"
	MemoryEventConflicted = "conflicted"
	MemoryEventWithdrawn  = "withdrawn"
	MemoryEventDeleted    = "deleted"
)

const (
	MemorySessionPolicyInherit  = "inherit"
	MemorySessionPolicyEnabled  = "enabled"
	MemorySessionPolicyDisabled = "disabled"
)

const (
	MemoryPolicyTargetUser    = "user"
	MemoryPolicyTargetSession = "session"
)

const (
	MemoryPolicyReasonCapabilityDisabled = "capability_disabled"
	MemoryPolicyReasonSessionDisabled    = "session_disabled"
	MemoryPolicyReasonSessionEnabled     = "session_enabled"
	MemoryPolicyReasonUserEnabled        = "user_enabled"
	MemoryPolicyReasonUserDisabled       = "user_disabled"
	MemoryPolicyReasonUnavailable        = "policy_unavailable"
)

// EffectiveMemoryPolicy is the one resolved policy consumed by chat and Agent
// callers. Raw inputs remain visible for UI state and optimistic concurrency.
type EffectiveMemoryPolicy struct {
	CapabilityEnabled     bool   `json:"capability_enabled"`
	UserEnabled           bool   `json:"user_enabled"`
	UserPreferenceVersion int64  `json:"user_preference_version"`
	SessionPolicy         string `json:"session_policy"`
	SessionPolicyVersion  int64  `json:"session_policy_version"`
	EffectiveEnabled      bool   `json:"effective_enabled"`
	Reason                string `json:"reason"`
}

// AgentMemoryPreference stores a user's default authorization. Absence of a
// row is intentionally equivalent to enabled=false, version=0.
type AgentMemoryPreference struct {
	UserID    int64     `gorm:"primaryKey;not null" json:"user_id"`
	Enabled   bool      `gorm:"not null;default:false" json:"enabled"`
	Version   int64     `gorm:"not null;default:1" json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AgentMemoryPreference) TableName() string { return "agent_memory_preferences" }

// AgentMemoryPolicyEvent is append-only policy audit metadata. It deliberately
// excludes prompts, answers and memory contents.
type AgentMemoryPolicyEvent struct {
	ID                string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID            int64     `gorm:"not null;index" json:"user_id"`
	ActorUserID       int64     `gorm:"not null;index" json:"actor_user_id"`
	TargetType        string    `gorm:"type:varchar(20);not null;index;check:chk_agent_memory_policy_target,target_type IN ('user','session')" json:"target_type"`
	TargetID          string    `gorm:"type:varchar(100);not null;index" json:"target_id"`
	PreviousValue     string    `gorm:"type:varchar(20);not null" json:"previous_value"`
	NewValue          string    `gorm:"type:varchar(20);not null" json:"new_value"`
	Version           int64     `gorm:"not null" json:"version"`
	CapabilityEnabled bool      `gorm:"not null" json:"capability_enabled"`
	EffectiveBefore   bool      `gorm:"not null" json:"effective_before"`
	EffectiveAfter    bool      `gorm:"not null" json:"effective_after"`
	OccurredAt        time.Time `gorm:"not null;index" json:"occurred_at"`
}

func (AgentMemoryPolicyEvent) TableName() string { return "agent_memory_policy_events" }

// AgentMemoryItem is the authoritative, owner-scoped memory record. Embeddings
// are a rebuildable projection referenced by EmbeddingRef; the item remains
// usable when that projection is unavailable.
type AgentMemoryItem struct {
	ID            string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID        int64          `gorm:"not null;index:idx_agent_memory_owner_scope,priority:1;index" json:"user_id"`
	ScopeType     string         `gorm:"type:varchar(30);not null;index:idx_agent_memory_owner_scope,priority:2;check:chk_agent_memory_scope_type,scope_type IN ('user','video','knowledge_base','run')" json:"scope_type"`
	ScopeID       string         `gorm:"type:varchar(100);not null;index:idx_agent_memory_owner_scope,priority:3" json:"scope_id"`
	Kind          string         `gorm:"type:varchar(80);not null;index:idx_agent_memory_kind" json:"kind"`
	Content       string         `gorm:"type:text;not null" json:"content"`
	SourceType    string         `gorm:"type:varchar(40);not null" json:"source_type"`
	SourceRef     string         `gorm:"type:varchar(255);not null;index" json:"source_ref"`
	Importance    float64        `gorm:"type:double precision;not null;default:0.5;index" json:"importance"`
	EmbeddingRef  string         `gorm:"type:varchar(255);not null;default:''" json:"embedding_ref"`
	Status        string         `gorm:"type:varchar(20);not null;default:'active';index;check:chk_agent_memory_status,status IN ('active','conflicted','withdrawn','deleted')" json:"status"`
	Version       int64          `gorm:"not null;default:1" json:"version"`
	LastUsedAt    *time.Time     `gorm:"index" json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt     time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
	SemanticScore *float64       `gorm:"column:semantic_score;->;-:migration" json:"-"`
}

func (AgentMemoryItem) TableName() string { return "agent_memory_items" }

// AgentMemoryEvent preserves lifecycle and conflict transitions even when the
// latest item projection is withdrawn or soft-deleted.
type AgentMemoryEvent struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	MemoryID   string    `gorm:"type:varchar(36);not null;index" json:"memory_id"`
	UserID     int64     `gorm:"not null;index" json:"user_id"`
	EventType  string    `gorm:"type:varchar(30);not null;index" json:"event_type"`
	Version    int64     `gorm:"not null" json:"version"`
	SourceRef  string    `gorm:"type:varchar(255);not null" json:"source_ref"`
	OccurredAt time.Time `gorm:"not null;index" json:"occurred_at"`
}

func (AgentMemoryEvent) TableName() string { return "agent_memory_events" }
