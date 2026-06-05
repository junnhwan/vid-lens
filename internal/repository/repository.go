package repository

import "gorm.io/gorm"

// Repositories 所有 Repository 的聚合
type Repositories struct {
	User         *UserRepository
	Task         *TaskRepository
	Transcription *TranscriptionRepository
	Summary      *SummaryRepository
}

// NewRepositories 创建所有 Repository 实例
func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		User:         NewUserRepository(db),
		Task:         NewTaskRepository(db),
		Transcription: NewTranscriptionRepository(db),
		Summary:      NewSummaryRepository(db),
	}
}
