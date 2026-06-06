package repository

import "gorm.io/gorm"

// Repositories 所有 Repository 的聚合
type Repositories struct {
	User          *UserRepository
	Asset         *AssetRepository
	Task          *TaskRepository
	Transcription *TranscriptionRepository
	Summary       *SummaryRepository
	AIProfile     *AIProfileRepository
	VideoChunk    *VideoChunkRepository
	Chat          *ChatRepository
}

// NewRepositories 创建所有 Repository 实例
func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		User:          NewUserRepository(db),
		Asset:         NewAssetRepository(db),
		Task:          NewTaskRepository(db),
		Transcription: NewTranscriptionRepository(db),
		Summary:       NewSummaryRepository(db),
		AIProfile:     NewAIProfileRepository(db),
		VideoChunk:    NewVideoChunkRepository(db),
		Chat:          NewChatRepository(db),
	}
}
