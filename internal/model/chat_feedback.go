package model

import "time"

// ChatFeedback is a user's assessment, never a correctness label or model fact.
type ChatFeedback struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `gorm:"not null;uniqueIndex:idx_feedback_owner_message,priority:1;index" json:"user_id"`
	SessionID int64     `gorm:"not null;index" json:"session_id"`
	MessageID int64     `gorm:"not null;uniqueIndex:idx_feedback_owner_message,priority:2" json:"message_id"`
	RunID     string    `gorm:"type:varchar(36);not null;default:''" json:"run_id,omitempty"`
	Rating    string    `gorm:"type:varchar(16);not null;check:chk_feedback_rating,rating IN ('helpful','problem')" json:"rating"`
	Category  string    `gorm:"type:varchar(16);not null;default:''" json:"category"`
	Note      string    `gorm:"type:text;not null;default:''" json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ChatFeedback) TableName() string { return "chat_feedback" }
