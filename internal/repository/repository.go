package repository

import "gorm.io/gorm"

// Repositories 所有 Repository 的聚合
type Repositories struct {
	db                 *gorm.DB
	User               *UserRepository
	Asset              *AssetRepository
	Task               *TaskRepository
	TaskJob            *TaskJobRepository
	TaskMessageFailure *TaskMessageFailureRepository
	Transcription      *TranscriptionRepository
	TranscriptionChunk *TranscriptionChunkRepository
	Summary            *SummaryRepository
	AIProfile          *AIProfileRepository
	VideoChunk         *VideoChunkRepository
	RAGIndex           *RAGIndexRepository
	Chat               *ChatRepository
	AICallLog          *AICallLogRepository
	RetryBudget        *RetryBudgetRepository
	UsageLedger        *UsageLedgerRepository
	QuotaCompensation  *QuotaCompensationRepository
}

// NewRepositories 创建所有 Repository 实例
func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		db:                 db,
		User:               NewUserRepository(db),
		Asset:              NewAssetRepository(db),
		Task:               NewTaskRepository(db),
		TaskJob:            NewTaskJobRepository(db),
		TaskMessageFailure: NewTaskMessageFailureRepository(db),
		Transcription:      NewTranscriptionRepository(db),
		TranscriptionChunk: NewTranscriptionChunkRepository(db),
		Summary:            NewSummaryRepository(db),
		AIProfile:          NewAIProfileRepository(db),
		VideoChunk:         NewVideoChunkRepository(db),
		RAGIndex:           NewRAGIndexRepository(db),
		Chat:               NewChatRepository(db),
		AICallLog:          NewAICallLogRepository(db),
		RetryBudget:        NewRetryBudgetRepository(db),
		UsageLedger:        NewUsageLedgerRepository(db),
		QuotaCompensation:  NewQuotaCompensationRepository(db),
	}
}

func (r *Repositories) Transaction(fn func(*Repositories) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewRepositories(tx))
	})
}
