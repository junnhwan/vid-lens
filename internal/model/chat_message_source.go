package model

import "time"

// ChatMessageSource stores normalized task provenance for an assistant message.
// RetrievalSnapshot remains the public citation snapshot; this table exists for
// membership filtering and task cleanup.
type ChatMessageSource struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MessageID int64     `gorm:"not null;index:idx_chat_message_sources_message_id;uniqueIndex:uidx_chat_message_sources_message_task,priority:1" json:"message_id"`
	SessionID int64     `gorm:"not null;index:idx_chat_message_sources_session_id" json:"session_id"`
	TaskID    int64     `gorm:"not null;index:idx_chat_message_sources_task_id;uniqueIndex:uidx_chat_message_sources_message_task,priority:2" json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (ChatMessageSource) TableName() string {
	return "chat_message_sources"
}
