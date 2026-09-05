package model

import "time"

const (
	ClaimStatusHypothesized = "hypothesized"
	ClaimStatusVerified     = "verified"
	ClaimStatusCorrected    = "corrected"
	ClaimStatusUnsupported  = "unsupported"
	ClaimStatusUncertain    = "uncertain"
)

const (
	EvidenceTimeRangeKnown   = "known"
	EvidenceTimeRangeUnknown = "unknown"
)

const (
	EvidenceSourceRevisionAvailable   = "available"
	EvidenceSourceRevisionUnavailable = "unavailable"
)

const (
	ClaimEvidenceSupports    = "supports"
	ClaimEvidenceContradicts = "contradicts"
	ClaimEvidenceContext     = "context"
)

// AgentClaim is one auditable factual statement extracted from an Agent
// answer. Corrections append another row under RootClaimID with a higher
// Revision; existing rows are never rewritten into a new meaning.
type AgentClaim struct {
	Inspection        *ClaimInspection `gorm:"serializer:json;type:text" json:"inspection,omitempty"`
	ID                string           `gorm:"type:varchar(36);primaryKey" json:"id"`
	RootClaimID       string           `gorm:"type:varchar(36);not null;uniqueIndex:idx_agent_claim_root_revision,priority:1;index" json:"root_claim_id"`
	Revision          int              `gorm:"not null;default:1;uniqueIndex:idx_agent_claim_root_revision,priority:2" json:"revision"`
	SupersedesClaimID string           `gorm:"type:varchar(36);not null;default:'';index" json:"supersedes_claim_id,omitempty"`
	UserID            int64            `gorm:"not null;index:idx_agent_claim_owner_run,priority:1;index:idx_agent_claim_owner_session,priority:1" json:"user_id"`
	SessionID         int64            `gorm:"not null;index:idx_agent_claim_owner_session,priority:2" json:"session_id"`
	MessageID         int64            `gorm:"not null;index" json:"message_id"`
	RunID             string           `gorm:"type:varchar(36);not null;index:idx_agent_claim_owner_run,priority:2;index" json:"run_id"`
	Kind              string           `gorm:"type:varchar(40);not null;default:'answer_fact';index" json:"kind"`
	Text              string           `gorm:"type:text;not null" json:"text"`
	Status            string           `gorm:"type:varchar(20);not null;index;check:chk_agent_claim_status,status IN ('hypothesized','verified','corrected','unsupported','uncertain')" json:"status"`
	Confidence        float64          `gorm:"type:double precision;not null;default:0;check:chk_agent_claim_confidence,confidence >= 0 AND confidence <= 1" json:"confidence"`
	ValidationNote    string           `gorm:"type:text;not null;default:''" json:"validation_note,omitempty"`
	CreatedAt         time.Time        `gorm:"not null;index" json:"created_at"`
}

// ClaimInspection is an immutable semantic check of this specific claim revision.
// Evidence snapshots preserve what was checked even after retrieval reindexing.
type ClaimInspection struct {
	CandidateHash   string              `json:"candidate_hash"`
	Version         string              `json:"version"`
	Model           string              `json:"model"`
	Claim           string              `json:"claim"`
	Result          string              `json:"result"`
	Reason          string              `json:"reason"`
	CounterQuery    string              `json:"counter_query"`
	SearchCompleted bool                `json:"search_completed"`
	Evidence        []InspectedEvidence `json:"evidence"`
	CheckedAt       time.Time           `json:"checked_at"`
}

type InspectedEvidence struct {
	AnchorQuote    string `json:"anchor_quote,omitempty"`
	SourceRef      string `json:"source_ref"`
	Content        string `json:"content"`
	ContentHash    string `json:"content_hash"`
	SourceRevision string `json:"source_revision"`
	SourceRefs     string `json:"source_refs"`
	Modality       string `json:"modality"`
	StartMS        int64  `json:"start_ms"`
	EndMS          int64  `json:"end_ms"`
	Cited          bool   `json:"cited"`
	Relation       string `json:"relation"`
	Reason         string `json:"reason"`
}

func (AgentClaim) TableName() string { return "agent_claims" }

// AgentEvidence is a stable evidence artifact. SourceRef is the existing RAG
// EvidenceID and selects an observed retrieval artifact; it is not a source
// processing revision. StableLocator records the relational coordinates needed
// to locate the source again without depending on the vector index.
type AgentEvidence struct {
	ID                   string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID               int64     `gorm:"not null;uniqueIndex:idx_agent_evidence_owner_run_ref,priority:1;index:idx_agent_evidence_owner_run,priority:1;index" json:"user_id"`
	RunID                string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_agent_evidence_owner_run_ref,priority:2;index:idx_agent_evidence_owner_run,priority:2;index" json:"run_id"`
	SourceRef            string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_agent_evidence_owner_run_ref,priority:3;index" json:"source_ref"`
	SourceType           string    `gorm:"type:varchar(40);not null;index" json:"source_type"`
	TaskID               int64     `gorm:"not null;index" json:"task_id"`
	DocumentID           string    `gorm:"type:varchar(100);not null;index" json:"document_id"`
	StartSecond          int64     `gorm:"not null;default:0;check:chk_agent_evidence_start,start_second >= 0" json:"start_second"`
	EndSecond            int64     `gorm:"not null;default:0;check:chk_agent_evidence_range,end_second >= start_second" json:"end_second"`
	StartMS              int64     `gorm:"not null;default:0;index" json:"start_ms"`
	EndMS                int64     `gorm:"not null;default:0;index" json:"end_ms"`
	TimeRangeStatus      string    `gorm:"type:varchar(20);not null;check:chk_agent_evidence_time_status,time_range_status IN ('known','unknown')" json:"time_range_status"`
	QuoteText            string    `gorm:"type:text;not null" json:"quote_text"`
	ContentHash          string    `gorm:"type:char(64);not null;index" json:"content_hash"`
	StableLocator        string    `gorm:"type:text;not null" json:"stable_locator"`
	SourceRevision       string    `gorm:"type:varchar(255);not null;default:''" json:"source_revision,omitempty"`
	SourceRevisionStatus string    `gorm:"type:varchar(20);not null;default:'unavailable';check:chk_agent_evidence_revision_status,source_revision_status IN ('available','unavailable')" json:"source_revision_status"`
	CreatedAt            time.Time `gorm:"not null;index" json:"created_at"`
	UpdatedAt            time.Time `gorm:"not null" json:"updated_at"`
}

func (AgentEvidence) TableName() string { return "agent_evidence" }

// AgentClaimEvidence explicitly records how an evidence artifact relates to a
// claim. VerificationStatus describes this binding, not a hidden model trace.
type AgentClaimEvidence struct {
	ClaimID            string        `gorm:"type:varchar(36);primaryKey" json:"claim_id"`
	EvidenceID         string        `gorm:"type:varchar(36);primaryKey;index" json:"evidence_id"`
	Relation           string        `gorm:"type:varchar(20);not null;check:chk_agent_claim_evidence_relation,relation IN ('supports','contradicts','context')" json:"relation"`
	VerificationStatus string        `gorm:"type:varchar(20);not null;check:chk_agent_claim_evidence_status,verification_status IN ('verified','unsupported','uncertain')" json:"verification_status"`
	ValidationReason   string        `gorm:"type:text;not null;default:''" json:"validation_reason,omitempty"`
	CreatedAt          time.Time     `gorm:"not null;index" json:"created_at"`
	Claim              AgentClaim    `gorm:"foreignKey:ClaimID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Evidence           AgentEvidence `gorm:"foreignKey:EvidenceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (AgentClaimEvidence) TableName() string { return "agent_claim_evidence" }
