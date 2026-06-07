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
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int64     `gorm:"index;not null" json:"user_id"`
	TaskID      int64     `gorm:"index" json:"task_id,omitempty"`
	SessionID   int64     `gorm:"index" json:"session_id,omitempty"`
	Kind        string    `gorm:"type:varchar(30);index;not null" json:"kind"`
	Provider    string    `gorm:"type:varchar(50);index" json:"provider"`
	ModelName   string    `gorm:"type:varchar(100);index" json:"model"`
	Status      string    `gorm:"type:varchar(30);index;not null" json:"status"`
	DurationMs  int64     `gorm:"default:0" json:"duration_ms"`
	InputChars  int       `gorm:"default:0" json:"input_chars"`
	OutputChars int       `gorm:"default:0" json:"output_chars"`
	ErrorCode   string    `gorm:"type:varchar(100)" json:"error_code,omitempty"`
	ErrorMsg    string    `gorm:"type:varchar(500)" json:"error_msg,omitempty"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

func (AICallLog) TableName() string {
	return "ai_call_logs"
}

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

func (UserUsageDaily) TableName() string {
	return "user_usage_daily"
}
