package model

import "time"

const (
	RetryAttemptLayerProvider  = "provider"
	RetryAttemptLayerScheduler = "scheduler"
	RetryBudgetReasonExhausted = "exhausted"
	RetryBudgetReasonDeadline  = "deadline_exceeded"
)

// RetryBudgetDecision is shared by repositories and provider decorators without
// coupling the AI client package to GORM.
type RetryBudgetDecision struct {
	Allowed      bool
	Duplicate    bool
	Reason       string
	AttemptCount int
	MaxAttempts  int
	Deadline     time.Time
}

// AIRetryBudget is the durable, shared attempt budget for one logical AI operation.
// Provider and task-scheduler retries consume the same counter, so restarts cannot
// silently create a fresh retry allowance.
type AIRetryBudget struct {
	BudgetID       string     `gorm:"type:varchar(64);primaryKey" json:"budget_id"`
	TaskID         int64      `gorm:"index" json:"task_id,omitempty"`
	JobID          int64      `gorm:"index" json:"job_id,omitempty"`
	Operation      string     `gorm:"type:varchar(40);not null;index" json:"operation"`
	MaxAttempts    int        `gorm:"not null" json:"max_attempts"`
	AttemptCount   int        `gorm:"not null;default:0" json:"attempt_count"`
	FirstAttemptAt *time.Time `json:"first_attempt_at,omitempty"`
	Deadline       time.Time  `gorm:"index;not null" json:"deadline"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (AIRetryBudget) TableName() string { return "ai_retry_budgets" }

type AIRetryAttempt struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	BudgetID   string    `gorm:"type:varchar(64);not null;index;uniqueIndex:uk_ai_retry_attempt" json:"budget_id"`
	AttemptKey string    `gorm:"type:varchar(128);not null;uniqueIndex:uk_ai_retry_attempt" json:"attempt_key"`
	Layer      string    `gorm:"type:varchar(20);not null;index" json:"layer"`
	CreatedAt  time.Time `json:"created_at"`
}

func (AIRetryAttempt) TableName() string { return "ai_retry_attempts" }
