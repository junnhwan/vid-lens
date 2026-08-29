package model

import "time"

const (
	AICallKindASR       = "asr"
	AICallKindLLM       = "llm"
	AICallKindEmbedding = "embedding"
)

const (
	AICallStatusSuccess = "success"
	AICallStatusFailed  = "failed"
)

type AICallLog struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID            int64     `gorm:"index;not null" json:"user_id"`
	TaskID            int64     `gorm:"index;index:idx_ai_call_task_stage,priority:1" json:"task_id,omitempty"`
	JobID             int64     `gorm:"index" json:"job_id,omitempty"`
	SessionID         int64     `gorm:"index" json:"session_id,omitempty"`
	TraceID           string    `gorm:"type:varchar(64);index:idx_ai_call_trace_created,priority:1" json:"trace_id,omitempty"`
	JobType           string    `gorm:"type:varchar(30);index" json:"job_type,omitempty"`
	Stage             string    `gorm:"type:varchar(50);index:idx_ai_call_task_stage,priority:2" json:"stage,omitempty"`
	Attempt           int       `gorm:"default:0" json:"attempt,omitempty"`
	Kind              string    `gorm:"type:varchar(30);index;not null" json:"kind"`
	Provider          string    `gorm:"type:varchar(50);index" json:"provider"`
	ModelName         string    `gorm:"type:varchar(100);index" json:"model"`
	Status            string    `gorm:"type:varchar(30);index;not null" json:"status"`
	DurationMs        int64     `gorm:"default:0" json:"duration_ms"`
	InputChars        int       `gorm:"default:0" json:"input_chars"`
	OutputChars       int       `gorm:"default:0" json:"output_chars"`
	PromptTokens      *int64    `json:"prompt_tokens,omitempty"`
	CompletionTokens  *int64    `json:"completion_tokens,omitempty"`
	TotalTokens       *int64    `json:"total_tokens,omitempty"`
	EstimatedCost     *float64  `gorm:"type:decimal(18,8)" json:"estimated_cost,omitempty"`
	TokenEstimated    bool      `gorm:"default:false" json:"token_estimated"`
	Currency          string    `gorm:"type:varchar(10)" json:"currency,omitempty"`
	PriceVersion      string    `gorm:"type:varchar(30)" json:"price_version,omitempty"`
	ProviderRequestID string    `gorm:"type:varchar(100);index" json:"provider_request_id,omitempty"`
	ErrorCode         string    `gorm:"type:varchar(100)" json:"error_code,omitempty"`
	ErrorMsg          string    `gorm:"type:varchar(500)" json:"error_msg,omitempty"`
	CreatedAt         time.Time `gorm:"index;index:idx_ai_call_trace_created,priority:2" json:"created_at"`
}

func (AICallLog) TableName() string { return "ai_call_logs" }

type UserUsageDaily struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID            int64     `gorm:"uniqueIndex:idx_user_usage_daily;not null" json:"user_id"`
	Date              string    `gorm:"type:char(10);uniqueIndex:idx_user_usage_daily;not null" json:"date"`
	ASRSeconds        int       `gorm:"default:0" json:"asr_seconds"`
	ASRRequests       int       `gorm:"default:0" json:"asr_requests"`
	LLMRequests       int       `gorm:"default:0" json:"llm_requests"`
	EmbeddingRequests int       `gorm:"default:0" json:"embedding_requests"`
	FailedRequests    int       `gorm:"default:0" json:"failed_requests"`
	InputChars        int       `gorm:"default:0" json:"input_chars"`
	OutputChars       int       `gorm:"default:0" json:"output_chars"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (UserUsageDaily) TableName() string { return "user_usage_daily" }
