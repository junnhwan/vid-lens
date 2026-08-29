package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// KnowledgeBase groups videos owned by one user for cross-video retrieval.
type KnowledgeBase struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int64     `gorm:"index;not null" json:"user_id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	Description string    `gorm:"type:varchar(500)" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (KnowledgeBase) TableName() string {
	return "knowledge_bases"
}

// BeforeSave normalizes presentation-only whitespace. Business constraints
// such as non-empty names and length limits remain in the service layer.
func (k *KnowledgeBase) BeforeSave(_ *gorm.DB) error {
	k.Name = strings.TrimSpace(k.Name)
	return nil
}

// KnowledgeBaseVideo is a membership edge. A video may belong to multiple
// knowledge bases, but duplicate membership in one knowledge base is ignored.
type KnowledgeBaseVideo struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	KnowledgeBaseID int64     `gorm:"not null;index:idx_knowledge_base_videos_knowledge_base_id;uniqueIndex:uidx_knowledge_base_videos_base_task,priority:1" json:"knowledge_base_id"`
	TaskID          int64     `gorm:"not null;index:idx_knowledge_base_videos_task_id;uniqueIndex:uidx_knowledge_base_videos_base_task,priority:2" json:"task_id"`
	CreatedAt       time.Time `json:"created_at"`
}

func (KnowledgeBaseVideo) TableName() string {
	return "knowledge_base_videos"
}
