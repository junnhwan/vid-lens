package model

import "time"

const (
	ChatScopeVideo         = "video"
	ChatScopeKnowledgeBase = "knowledge_base"
)

type ChatSession struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID          int64     `gorm:"index;not null" json:"user_id"`
	TaskID          int64     `gorm:"index;not null" json:"task_id"`
	ScopeType       string    `gorm:"type:varchar(30);not null;default:'video';index:idx_chat_sessions_scope_knowledge_base,priority:1;check:chk_chat_sessions_scope,((scope_type = 'video' AND task_id > 0 AND knowledge_base_id = 0) OR (scope_type = 'knowledge_base' AND task_id = 0 AND knowledge_base_id > 0))" json:"scope_type"`
	KnowledgeBaseID int64     `gorm:"not null;default:0;index:idx_chat_sessions_scope_knowledge_base,priority:2" json:"knowledge_base_id"`
	Title           string    `gorm:"type:varchar(200)" json:"title"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (ChatSession) TableName() string {
	return "chat_sessions"
}

// LegacyChatSession is the historical MySQL migration contract. It must stay
// free of online-only scope columns so --upgrade-source-schema never requires
// the retired source database to adopt the knowledge-base schema.
type LegacyChatSession struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    int64     `gorm:"index;not null" json:"user_id"`
	TaskID    int64     `gorm:"index;not null" json:"task_id"`
	Title     string    `gorm:"type:varchar(200)" json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LegacyChatSession) TableName() string {
	return "chat_sessions"
}

type ChatMessage struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID         int64     `gorm:"index;not null" json:"session_id"`
	UserID            int64     `gorm:"index;not null" json:"user_id"`
	Role              string    `gorm:"type:varchar(20);not null" json:"role"`
	Content           string    `gorm:"type:text;not null" json:"content"`
	RetrievalSnapshot *string   `gorm:"type:json" json:"retrieval_snapshot,omitempty"`
	ModelName         string    `gorm:"type:varchar(100)" json:"model_name,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}
