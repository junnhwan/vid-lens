package model

import "time"

// UserAIProfile stores user-owned AI provider credentials.
// API keys are encrypted before persistence; never store plaintext keys here.
type UserAIProfile struct {
	ID                        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID                    int64     `gorm:"index;not null" json:"user_id"`
	Name                      string    `gorm:"type:varchar(100);not null" json:"name"`
	LLMProvider               string    `gorm:"type:varchar(50);not null" json:"llm_provider"`
	LLMBaseURL                string    `gorm:"type:varchar(500);not null" json:"llm_base_url"`
	LLMAPIKeyCiphertext       string    `gorm:"type:text;not null" json:"-"`
	LLMModel                  string    `gorm:"type:varchar(100);not null" json:"llm_model"`
	ASRProvider               string    `gorm:"type:varchar(50);not null" json:"asr_provider"`
	ASRBaseURL                string    `gorm:"type:varchar(500);not null" json:"asr_base_url"`
	ASRAPIKeyCiphertext       string    `gorm:"type:text;not null" json:"-"`
	ASRModel                  string    `gorm:"type:varchar(100);not null" json:"asr_model"`
	EmbeddingProvider         string    `gorm:"type:varchar(50);not null" json:"embedding_provider"`
	EmbeddingEndpoint         string    `gorm:"type:varchar(500);not null" json:"embedding_endpoint"`
	EmbeddingAPIKeyCiphertext string    `gorm:"type:text;not null" json:"-"`
	EmbeddingModel            string    `gorm:"type:varchar(100);not null" json:"embedding_model"`
	EmbeddingDim              int       `gorm:"not null" json:"embedding_dim"`
	IsDefault                 bool      `gorm:"default:false;index" json:"is_default"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

func (UserAIProfile) TableName() string {
	return "user_ai_profiles"
}
