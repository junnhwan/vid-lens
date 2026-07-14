package model

import "time"

const (
	UsageLedgerReserved = "reserved"
	UsageLedgerSettled  = "settled"
	UsageLedgerReleased = "released"

	UsageUnitToken  = "token"
	UsageUnitSecond = "second"
	UsageUnitCall   = "call"

	UsageSourceActual    = "actual"
	UsageSourceEstimated = "estimated"
	UsageSourceUnknown   = "unknown"

	CompensationPending    = "pending"
	CompensationProcessing = "processing"
	CompensationCompleted  = "completed"
	CompensationDead       = "dead"
)

// AIUsageLedger is the auditable source of truth for quota accounting. Unknown
// provider measurements intentionally remain NULL rather than being reported as 0.
type AIUsageLedger struct {
	ID                int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	IdempotencyKey    string     `gorm:"type:varchar(128);not null;uniqueIndex" json:"idempotency_key"`
	UserID            int64      `gorm:"not null;index:idx_usage_user_date,priority:1" json:"user_id"`
	TaskID            int64      `gorm:"index" json:"task_id,omitempty"`
	JobID             int64      `gorm:"index" json:"job_id,omitempty"`
	Kind              string     `gorm:"type:varchar(30);not null;index" json:"kind"`
	Operation         string     `gorm:"type:varchar(40);index" json:"operation"`
	Provider          string     `gorm:"type:varchar(50);index" json:"provider"`
	ModelName         string     `gorm:"type:varchar(100);index" json:"model"`
	UsageDate         string     `gorm:"type:char(10);not null;index:idx_usage_user_date,priority:2" json:"usage_date"`
	Unit              string     `gorm:"type:varchar(20);not null" json:"unit"`
	Status            string     `gorm:"type:varchar(20);not null;index" json:"status"`
	ReservedUnits     float64    `gorm:"type:decimal(20,6);not null" json:"reserved_units"`
	ActualUnits       *float64   `gorm:"type:decimal(20,6)" json:"actual_units,omitempty"`
	UsageSource       string     `gorm:"type:varchar(20);not null;default:'unknown'" json:"usage_source"`
	PromptTokens      *int64     `json:"prompt_tokens,omitempty"`
	CompletionTokens  *int64     `json:"completion_tokens,omitempty"`
	TotalTokens       *int64     `json:"total_tokens,omitempty"`
	ASRSeconds        *float64   `gorm:"type:decimal(20,6)" json:"asr_seconds,omitempty"`
	EstimatedCost     *float64   `gorm:"type:decimal(18,8)" json:"estimated_cost,omitempty"`
	Currency          string     `gorm:"type:varchar(10)" json:"currency,omitempty"`
	PriceVersion      string     `gorm:"type:varchar(30)" json:"price_version,omitempty"`
	ProviderRequestID string     `gorm:"type:varchar(100);index" json:"provider_request_id,omitempty"`
	ReleaseReason     string     `gorm:"type:varchar(255)" json:"release_reason,omitempty"`
	ReservedAt        time.Time  `gorm:"not null" json:"reserved_at"`
	SettledAt         *time.Time `json:"settled_at,omitempty"`
	ReleasedAt        *time.Time `json:"released_at,omitempty"`
	ExpiresAt         time.Time  `gorm:"index;not null" json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (AIUsageLedger) TableName() string { return "ai_usage_ledgers" }

// QuotaCompensation is a durable outbox item used to reconcile the Redis daily
// cache with the MySQL usage ledger. EventKey is also the Redis idempotency key.
type QuotaCompensation struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	EventKey       string     `gorm:"type:varchar(160);not null;uniqueIndex" json:"event_key"`
	LedgerID       int64      `gorm:"not null;index" json:"ledger_id"`
	UserID         int64      `gorm:"not null;index" json:"user_id"`
	UsageDate      string     `gorm:"type:char(10);not null;index" json:"usage_date"`
	Kind           string     `gorm:"type:varchar(30);not null" json:"kind"`
	Unit           string     `gorm:"type:varchar(20);not null" json:"unit"`
	Action         string     `gorm:"type:varchar(20);not null" json:"action"`
	DeltaUnits     float64    `gorm:"type:decimal(20,6);not null" json:"delta_units"`
	Status         string     `gorm:"type:varchar(20);not null;index" json:"status"`
	AttemptCount   int        `gorm:"not null;default:0" json:"attempt_count"`
	NextAttemptAt  *time.Time `gorm:"index" json:"next_attempt_at,omitempty"`
	LeaseToken     string     `gorm:"type:varchar(64);index" json:"-"`
	LeaseExpiresAt *time.Time `gorm:"index" json:"-"`
	LastError      string     `gorm:"type:varchar(500)" json:"last_error,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (QuotaCompensation) TableName() string { return "quota_compensations" }
