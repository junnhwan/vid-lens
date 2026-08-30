package model

import "time"

const (
	AgentRunStatusPending         = "pending"
	AgentRunStatusRunning         = "running"
	AgentRunStatusCompleted       = "completed"
	AgentRunStatusFailed          = "failed"
	AgentRunStatusCancelled       = "cancelled"
	AgentRunStatusBudgetExhausted = "budget_exhausted"
)

const (
	AgentStepStatusRunning   = "running"
	AgentStepStatusCompleted = "completed"
	AgentStepStatusFailed    = "failed"
	AgentStepStatusAmbiguous = "ambiguous"
)

const (
	AgentToolCallStatusRunning   = "running"
	AgentToolCallStatusCompleted = "completed"
	AgentToolCallStatusFailed    = "failed"
	AgentToolCallStatusAmbiguous = "ambiguous"
)

// AgentRun freezes the identity, scope, profile, policy, and budget of one
// Agent execution. Snapshot fields contain server-created JSON without API
// keys or prompt text. PostgreSQL is authoritative for recovery.
type AgentRun struct {
	ID                 string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	UserID             int64      `gorm:"not null;index:idx_agent_run_owner_status,priority:1;index:idx_agent_run_owner_session,priority:1" json:"user_id"`
	SessionID          int64      `gorm:"not null;index:idx_agent_run_owner_session,priority:2" json:"session_id"`
	ScopeType          string     `gorm:"type:varchar(30);not null;index" json:"scope_type"`
	TaskID             int64      `gorm:"not null;default:0;index" json:"task_id,omitempty"`
	KnowledgeBaseID    int64      `gorm:"not null;default:0;index" json:"knowledge_base_id,omitempty"`
	Goal               string     `gorm:"type:text;not null" json:"goal"`
	Mode               string     `gorm:"type:varchar(30);not null;index" json:"mode"`
	AgentProfile       string     `gorm:"type:varchar(80);not null;default:'default'" json:"agent_profile"`
	ProfileSnapshot    string     `gorm:"type:text;not null" json:"profile_snapshot"`
	PolicySnapshot     string     `gorm:"type:text;not null" json:"policy_snapshot"`
	BudgetSnapshot     string     `gorm:"type:text;not null" json:"budget_snapshot"`
	Status             string     `gorm:"type:varchar(24);not null;index:idx_agent_run_owner_status,priority:2;check:chk_agent_run_status,status IN ('pending','running','completed','failed','cancelled','budget_exhausted')" json:"status"`
	Version            int64      `gorm:"not null;default:1" json:"version"`
	MaxSteps           int        `gorm:"not null" json:"max_steps"`
	MaxToolCalls       int        `gorm:"not null" json:"max_tool_calls"`
	MaxLLMCalls        int        `gorm:"not null" json:"max_llm_calls"`
	MaxVisionCalls     int        `gorm:"not null" json:"max_vision_calls"`
	MaxAttemptsPerStep int        `gorm:"not null;default:1" json:"max_attempts_per_step"`
	StepsUsed          int        `gorm:"not null;default:0" json:"steps_used"`
	ToolCallsUsed      int        `gorm:"not null;default:0" json:"tool_calls_used"`
	LLMCallsUsed       int        `gorm:"not null;default:0" json:"llm_calls_used"`
	VisionCallsUsed    int        `gorm:"not null;default:0" json:"vision_calls_used"`
	StopReason         string     `gorm:"type:varchar(80);not null;default:''" json:"stop_reason,omitempty"`
	ErrorCode          string     `gorm:"type:varchar(80);not null;default:''" json:"error_code,omitempty"`
	ErrorMessage       string     `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	CreatedAt          time.Time  `gorm:"not null;index" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null" json:"updated_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
}

func (AgentRun) TableName() string { return "agent_runs" }

// AgentStep is one immutable attempt identity. Status and lease fields change
// only through version/token CAS. ResultCheckpoint contains a safe, typed tool
// result needed to resume; it never contains provider prompts or planner drafts.
type AgentStep struct {
	ID               string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	RunID            string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_agent_step_attempt,priority:1;index:idx_agent_step_run_status,priority:1" json:"run_id"`
	StepID           string     `gorm:"type:varchar(80);not null;uniqueIndex:idx_agent_step_attempt,priority:2" json:"step_id"`
	Attempt          int        `gorm:"not null;uniqueIndex:idx_agent_step_attempt,priority:3" json:"attempt"`
	Sequence         int        `gorm:"not null;index" json:"sequence"`
	Kind             string     `gorm:"type:varchar(30);not null;index" json:"kind"`
	Action           string     `gorm:"type:varchar(80);not null;index" json:"action"`
	Status           string     `gorm:"type:varchar(20);not null;index:idx_agent_step_run_status,priority:2;check:chk_agent_step_status,status IN ('running','completed','failed','ambiguous')" json:"status"`
	SafeReason       string     `gorm:"type:text;not null;default:''" json:"safe_reason,omitempty"`
	InputSummary     string     `gorm:"type:text;not null;default:'{}'" json:"input_summary"`
	OutputRef        string     `gorm:"type:text;not null;default:''" json:"output_ref,omitempty"`
	ResultCheckpoint string     `gorm:"type:text;not null;default:''" json:"-"`
	ResultDigest     string     `gorm:"type:char(64);not null;default:'';index" json:"result_digest,omitempty"`
	ErrorCode        string     `gorm:"type:varchar(80);not null;default:''" json:"error_code,omitempty"`
	ErrorMessage     string     `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	ReplaySafe       bool       `gorm:"not null;default:false" json:"replay_safe"`
	LeaseToken       string     `gorm:"type:varchar(64);not null;default:'';index" json:"-"`
	LeaseExpiresAt   *time.Time `gorm:"index" json:"lease_expires_at,omitempty"`
	LeaseVersion     int64      `gorm:"not null;default:1" json:"lease_version"`
	StartedAt        time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	DurationMs       int64      `gorm:"not null;default:0" json:"duration_ms"`
	CreatedAt        time.Time  `gorm:"not null;index" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"not null" json:"updated_at"`
	Run              AgentRun   `gorm:"foreignKey:RunID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (AgentStep) TableName() string { return "agent_steps" }

// AgentToolCall records one allow-listed tool invocation for a step attempt.
// Arguments are represented by a digest and a safe summary; the checkpoint is
// the server-validated result used for recovery, not a prompt transcript.
type AgentToolCall struct {
	ID               string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	RunID            string     `gorm:"type:varchar(36);not null;uniqueIndex:idx_agent_tool_attempt,priority:1;index" json:"run_id"`
	StepID           string     `gorm:"type:varchar(80);not null;uniqueIndex:idx_agent_tool_attempt,priority:2" json:"step_id"`
	Attempt          int        `gorm:"not null;uniqueIndex:idx_agent_tool_attempt,priority:3" json:"attempt"`
	AgentStepID      string     `gorm:"type:varchar(36);not null;uniqueIndex;index" json:"agent_step_id"`
	ToolName         string     `gorm:"type:varchar(80);not null;index" json:"tool_name"`
	Status           string     `gorm:"type:varchar(20);not null;index;check:chk_agent_tool_status,status IN ('running','completed','failed','ambiguous')" json:"status"`
	InputSummary     string     `gorm:"type:text;not null;default:'{}'" json:"input_summary"`
	ArgumentsDigest  string     `gorm:"type:char(64);not null;index" json:"arguments_digest"`
	CallDigest       string     `gorm:"type:char(64);not null;index" json:"call_digest"`
	OutputRef        string     `gorm:"type:text;not null;default:''" json:"output_ref,omitempty"`
	ResultCheckpoint string     `gorm:"type:text;not null;default:''" json:"-"`
	ResultDigest     string     `gorm:"type:char(64);not null;default:'';index" json:"result_digest,omitempty"`
	EvidenceRefs     string     `gorm:"type:text;not null;default:'[]'" json:"evidence_refs"`
	ErrorCode        string     `gorm:"type:varchar(80);not null;default:''" json:"error_code,omitempty"`
	ErrorMessage     string     `gorm:"type:text;not null;default:''" json:"error_message,omitempty"`
	DurationMs       int64      `gorm:"not null;default:0" json:"duration_ms"`
	PromptTokens     int64      `gorm:"not null;default:0" json:"prompt_tokens"`
	CompletionTokens int64      `gorm:"not null;default:0" json:"completion_tokens"`
	CostMicros       int64      `gorm:"not null;default:0" json:"cost_micros"`
	StartedAt        time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	CreatedAt        time.Time  `gorm:"not null;index" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"not null" json:"updated_at"`
	Step             AgentStep  `gorm:"foreignKey:AgentStepID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (AgentToolCall) TableName() string { return "agent_tool_calls" }
