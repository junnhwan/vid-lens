package model

import "time"

type UserTag struct {
	ID              string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID          int64     `gorm:"not null;index" json:"-"`
	DisplayName     string    `gorm:"type:varchar(320);not null" json:"display_name"`
	CreationOrigin  string    `gorm:"type:varchar(16);not null" json:"creation_origin"`
	ProtectedByUser bool      `gorm:"not null;default:false" json:"protected_by_user"`
	Version         int64     `gorm:"not null" json:"version"`
	Status          string    `gorm:"type:varchar(16);not null" json:"status"`
	MergedIntoID    *string   `gorm:"type:varchar(36)" json:"merged_into_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type UserTagName struct {
	UserID        int64  `gorm:"primaryKey" json:"-"`
	NormalizedKey string `gorm:"type:varchar(320);primaryKey" json:"-"`
	TagID         string `gorm:"type:varchar(36);not null;index" json:"tag_id"`
	DisplayName   string `gorm:"type:varchar(320);not null" json:"display_name"`
	Kind          string `gorm:"type:varchar(16);not null" json:"kind"`
}

// The head serializes writes to one user's shared canonical/alias namespace.
type UserTagVocabularyHead struct {
	UserID  int64 `gorm:"primaryKey"`
	Version int64 `gorm:"not null;default:0"`
}
type VideoTagSetHead struct {
	UserID  int64 `gorm:"primaryKey" json:"-"`
	TaskID  int64 `gorm:"primaryKey" json:"task_id"`
	Version int64 `gorm:"not null;default:0" json:"version"`
}
type VideoTagAssignment struct {
	UserID           int64     `gorm:"primaryKey" json:"-"`
	TaskID           int64     `gorm:"primaryKey" json:"task_id"`
	TagID            string    `gorm:"type:varchar(36);primaryKey" json:"tag_id"`
	Origin           string    `gorm:"type:varchar(16);not null" json:"origin"`
	GenerationID     string    `gorm:"type:varchar(36)" json:"generation_id,omitempty"`
	GeneratedVersion int64     `gorm:"not null;default:0" json:"generated_version,omitempty"`
	SourceDigest     string    `gorm:"type:char(64)" json:"source_digest,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
type VideoTagDecision struct {
	UserID        int64     `gorm:"primaryKey" json:"-"`
	TaskID        int64     `gorm:"primaryKey" json:"task_id"`
	DecisionKey   string    `gorm:"type:varchar(400);primaryKey" json:"-"`
	TagID         string    `gorm:"type:varchar(36);index" json:"tag_id,omitempty"`
	NormalizedKey string    `gorm:"type:varchar(320);index" json:"normalized_key,omitempty"`
	Decision      string    `gorm:"type:varchar(16);not null" json:"decision"`
	Version       int64     `gorm:"not null" json:"version"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type VideoTagSuggestion struct {
	UserDecision        string `gorm:"type:varchar(16);not null;default:''" json:"-"`
	DecisionBaseVersion int64  `gorm:"not null;default:0" json:"-"`
	ID                  string `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID              int64  `gorm:"not null;uniqueIndex:idx_tag_suggestion_identity,priority:1" json:"-"`
	TaskID              int64  `gorm:"not null;uniqueIndex:idx_tag_suggestion_identity,priority:2" json:"task_id"`
	GenerationID        string `gorm:"type:varchar(36);not null;uniqueIndex:idx_tag_suggestion_identity,priority:3" json:"generation_id"`
	GeneratedVersion    int64  `gorm:"not null;uniqueIndex:idx_tag_suggestion_identity,priority:4" json:"generated_version"`
	NormalizedKey       string `gorm:"type:varchar(320);not null;uniqueIndex:idx_tag_suggestion_identity,priority:5" json:"-"`
	DisplayName         string `gorm:"type:varchar(320);not null" json:"display_name"`
	TagID               string `gorm:"type:varchar(36)" json:"tag_id,omitempty"`
	SourceDigest        string `gorm:"type:char(64);not null" json:"source_digest"`
	Reason              string `gorm:"type:text;not null" json:"reason"`
	Status              string `gorm:"type:varchar(16);not null" json:"status"`
	// EffectiveStatus projects the current task decision without rewriting the
	// historical generation candidate or its recorded acceptance status.
	EffectiveStatus string    `gorm:"-" json:"effective_status,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type UserTagMergeRecord struct {
	UserID      int64  `gorm:"primaryKey"`
	Key         string `gorm:"type:varchar(128);primaryKey"`
	RequestHash string `gorm:"type:char(64);not null"`
	ResultJSON  string `gorm:"type:text;not null"`
	CreatedAt   time.Time
}
