package model

import "time"

// HostedAIConfig is a single encrypted, centrally managed provider bundle.
// User profiles only refer to it; credentials are never copied to user rows.
type HostedAIConfig struct {
	ID         int64     `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Enabled    bool      `gorm:"not null;default:false" json:"-"`
	Ciphertext string    `gorm:"type:text;not null" json:"-"`
	UpdatedAt  time.Time `json:"-"`
}
