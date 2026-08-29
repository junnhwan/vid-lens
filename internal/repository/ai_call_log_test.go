package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestAICallLogRepositoryCreatesLogAndRollsUpDailyUsage(t *testing.T) {
	repo := newAICallLogTestRepo(t)

	if err := repo.Create(&model.AICallLog{
		UserID:      7,
		TaskID:      42,
		SessionID:   99,
		Kind:        model.AICallKindLLM,
		Provider:    "openai_compatible",
		ModelName:   "chat-model",
		Status:      model.AICallStatusSuccess,
		DurationMs:  123,
		InputChars:  20,
		OutputChars: 12,
	}); err != nil {
		t.Fatalf("create log: %v", err)
	}
	if err := repo.IncrementDailyUsage(7, "2026-06-07", model.AICallKindLLM, model.AICallStatusSuccess, 20, 12, 0); err != nil {
		t.Fatalf("increment llm usage: %v", err)
	}
	if err := repo.IncrementDailyUsage(7, "2026-06-07", model.AICallKindEmbedding, model.AICallStatusFailed, 8, 0, 0); err != nil {
		t.Fatalf("increment embedding usage: %v", err)
	}

	logs, err := repo.ListByUserID(7, 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(logs))
	}
	if logs[0].Provider != "openai_compatible" || logs[0].ModelName != "chat-model" {
		t.Fatalf("log = %+v", logs[0])
	}

	usage, err := repo.FindDailyUsage(7, "2026-06-07")
	if err != nil {
		t.Fatalf("find usage: %v", err)
	}
	if usage == nil {
		t.Fatal("expected usage row")
	}
	if usage.LLMRequests != 1 || usage.EmbeddingRequests != 1 || usage.FailedRequests != 1 {
		t.Fatalf("usage request counts = %+v", usage)
	}
	if usage.InputChars != 28 || usage.OutputChars != 12 {
		t.Fatalf("usage chars = %+v", usage)
	}
}

func newAICallLogTestRepo(t *testing.T) *AICallLogRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AICallLog{}, &model.UserUsageDaily{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewAICallLogRepository(db)
}
