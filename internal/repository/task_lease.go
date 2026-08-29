package repository

import (
	"time"

	"vid-lens/internal/model"
)

// 任务 lease 状态、请求对象和跨模块共享结果类型。
// 具体状态转换按 processing、dispatch、terminal 和 ownership 能力拆分。
type TaskLeaseOutcome string

const (
	TaskLeaseAcquired TaskLeaseOutcome = "acquired"
	TaskLeaseBusy     TaskLeaseOutcome = "busy"
	TaskLeaseStale    TaskLeaseOutcome = "stale"
	TaskLeaseTerminal TaskLeaseOutcome = "terminal"
)

type TaskLeaseClaim struct {
	Outcome TaskLeaseOutcome
	Token   string
	Version int64
}

type TaskProcessingClaimRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	MessageToken string
	Now          time.Time
	LeaseUntil   time.Time
	NewToken     string
}

// InitialTaskDispatchRequest atomically prepares the durable owner used by an
// HTTP-triggered first Kafka publish. CreateTask is used by URL download tasks;
// existing uploaded tasks use AllowedStatuses as their compare-and-swap guard.
type InitialTaskDispatchRequest struct {
	Task            *model.VideoTask
	CreateTask      bool
	AllowedStatuses []int8
	JobType         string
	Stage           string
	Now             time.Time
	LeaseUntil      time.Time
	Token           string
}

// InitialTaskDispatch is the complete correlation state that must be copied to
// the first Kafka payload. If publishing fails, Token owns the compensating
// RestoreRetryDispatch transition.
type InitialTaskDispatch struct {
	Task          model.VideoTask
	RetryBudgetID string
	Token         string
}

type TaskDispatchClaimRequest struct {
	TaskID          int64
	JobType         string
	Stage           string
	ExpectedVersion int64
	Now             time.Time
	LeaseUntil      time.Time
	Token           string
}

type TaskDispatchRestoreRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	Token        string
	ErrorMessage string
	NextRetryAt  time.Time
}

// TaskProcessingCompleteRequest completes the current processing owner using its lease token.
type TaskProcessingCompleteRequest struct {
	TaskID     int64
	JobType    string
	JobStage   string
	Token      string
	TaskStatus int8
	TaskStage  string
	TaskFields map[string]interface{}
	Now        time.Time
}

// TaskProcessingFailureRequest records a durable failure for the current processing owner.
type TaskProcessingFailureRequest struct {
	TaskID       int64
	JobType      string
	Stage        string
	Token        string
	Status       int8
	ErrorCode    string
	ErrorMessage string
	RetryCount   int
	MaxRetries   int
	NextRetryAt  *time.Time
	Now          time.Time
}

// TaskProcessingLeaseRequest identifies the current owner of one processing stage.
type TaskProcessingLeaseRequest struct {
	TaskID     int64
	JobType    string
	Token      string
	Now        time.Time
	LeaseUntil time.Time
}

// TaskProcessingHandoffFailureRequest atomically completes the current stage and
// makes the next stage retryable when its Kafka handoff cannot be published.
type TaskProcessingHandoffFailureRequest struct {
	TaskID         int64
	CurrentJobType string
	CurrentStage   string
	NextJobType    string
	NextStage      string
	Token          string
	Status         int8
	ErrorCode      string
	ErrorMessage   string
	RetryCount     int
	MaxRetries     int
	NextRetryAt    *time.Time
	Now            time.Time
}
