package model

import "time"

type ChatSession struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    int64     `gorm:"index;not null" json:"user_id"`
	TaskID    int64     `gorm:"index;not null" json:"task_id"`
	Title     string    `gorm:"type:varchar(200)" json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ChatSession) TableName() string {
	return "chat_sessions"
}

type ChatMessage struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID         int64     `gorm:"index;not null" json:"session_id"`
	UserID            int64     `gorm:"index;not null" json:"user_id"`
	Role              string    `gorm:"type:varchar(20);not null" json:"role"`
	Content           string    `gorm:"type:longtext;not null" json:"content"`
	RetrievalSnapshot *string   `gorm:"type:json" json:"retrieval_snapshot,omitempty"`
	ModelName         string    `gorm:"type:varchar(100)" json:"model_name,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}
