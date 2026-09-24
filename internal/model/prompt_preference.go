package model

import "time"

// UserPromptPreference contains optional user instructions for one AI function.
// Product constraints stay in code and are never replaced by this text.
type UserPromptPreference struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	UserID    int64  `gorm:"uniqueIndex:idx_user_prompt_function;not null"`
	Function  string `gorm:"type:varchar(32);uniqueIndex:idx_user_prompt_function;not null"`
	Text      string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}
