package model

import "time"

const (
	TaskJobTypeAnalyze    = "analyze"
	TaskJobTypeSummary    = "summary" // transcript-only summary; owns its own lease
	TaskJobTypeTranscribe = "transcribe"
	TaskJobTypeDownload   = "download"
	TaskJobTypeTextSource = "text_source" // platform subtitle selection or durable ASR delegation
	TaskJobTypeRAGIndex   = "rag_index"
	TaskJobTypeVisual     = "visual" // explicitly requested frame extraction; never runs ASR
)

// TaskJob records the state of one processing action under a video task.
// video_tasks remains the compatibility status source; task_jobs separates
// download/transcribe/analyze/rag_index progress for backend observability.
type TaskJob struct {
	// Durable intent: retrying alignment must never fall back to paid ASR.
	TranscriptAlignmentOnly bool       `gorm:"default:false" json:"-"`
	InputText               string     `gorm:"type:text" json:"-"`
	InputChunksJSON         string     `gorm:"type:text" json:"-"`
	GenerationID            string     `gorm:"type:varchar(36);not null;default:'';index" json:"generation_id,omitempty"`
	InputSourceID           string     `gorm:"type:varchar(36);not null;default:''" json:"-"`
	InputSnapshotJSON       string     `gorm:"type:text;not null;default:''" json:"-"`
	ID                      int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID                  int64      `gorm:"not null;uniqueIndex:uk_task_jobs_task_type;index" json:"task_id"`
	UserID                  int64      `gorm:"not null;index" json:"user_id"`
	JobType                 string     `gorm:"type:varchar(30);not null;uniqueIndex:uk_task_jobs_task_type;index" json:"job_type"`
	Status                  int8       `gorm:"type:smallint;default:0;index" json:"status"`
	Stage                   string     `gorm:"type:varchar(50);default:'none';index" json:"stage"`
	TraceID                 string     `gorm:"type:varchar(64);index" json:"trace_id"`
	RetryCount              int        `gorm:"default:0" json:"retry_count"`
	MaxRetries              int        `gorm:"default:3" json:"max_retries"`
	RetryBudgetID           string     `gorm:"type:varchar(64);index" json:"-"`
	RetryBudgetGeneration   int        `gorm:"default:0" json:"-"`
	NextRetryAt             *time.Time `json:"next_retry_at,omitempty"`
	LastErrorCode           string     `gorm:"type:varchar(100)" json:"last_error_code"`
	LastErrorMsg            string     `gorm:"type:varchar(500)" json:"last_error_msg"`
	ProcessingToken         string     `gorm:"type:varchar(64);index" json:"-"`
	LeaseKind               string     `gorm:"type:varchar(20);index" json:"-"`
	LeaseExpiresAt          *time.Time `gorm:"index" json:"-"`
	LeaseVersion            int64      `gorm:"default:0" json:"-"`
	StartedAt               *time.Time `json:"started_at,omitempty"`
	FinishedAt              *time.Time `json:"finished_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

func (TaskJob) TableName() string {
	return "task_jobs"
}

// KafkaMessageFailure is the durable quarantine record for a poison Kafka message.
// The consumer-group offset key makes repeated delivery idempotent.
type KafkaMessageFailure struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ConsumerGroup string    `gorm:"type:varchar(255);not null;uniqueIndex:uk_kafka_message_failures_offset" json:"consumer_group"`
	ConsumerName  string    `gorm:"type:varchar(50);not null" json:"consumer_name"`
	Topic         string    `gorm:"type:varchar(255);not null;uniqueIndex:uk_kafka_message_failures_offset" json:"topic"`
	Partition     int       `gorm:"not null;uniqueIndex:uk_kafka_message_failures_offset" json:"partition"`
	MessageOffset int64     `gorm:"not null;uniqueIndex:uk_kafka_message_failures_offset" json:"message_offset"`
	MessageKey    []byte    `json:"message_key"`
	Payload       []byte    `json:"payload"`
	ErrorMessage  string    `gorm:"type:varchar(1000);not null" json:"error_message"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (KafkaMessageFailure) TableName() string {
	return "kafka_message_failures"
}
