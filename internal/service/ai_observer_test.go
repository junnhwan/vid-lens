package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestAIObserverRecordsSuccessWithoutPromptOrResponseContent(t *testing.T) {
	repos := newAIObserverTestRepositories(t)
	observer := NewAIObserver(repos)

	err := observer.RecordAICall(context.Background(), ai.CallRecord{
		UserID:      7,
		TaskID:      42,
		SessionID:   99,
		Kind:        model.AICallKindLLM,
		Provider:    "openai_compatible",
		Model:       "chat-model",
		DurationMs:  123,
		InputChars:  100,
		OutputChars: 40,
		Status:      model.AICallStatusSuccess,
		ErrorCode:   "",
		ErrorMsg:    "",
	})
	if err != nil {
		t.Fatalf("RecordAICall() error = %v", err)
	}

	logs, err := repos.AICallLog.ListByUserID(7, 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(logs))
	}
	if logs[0].InputChars != 100 || logs[0].OutputChars != 40 {
		t.Fatalf("log chars = %+v", logs[0])
	}
	if strings.Contains(logs[0].ErrorMsg, "完整 prompt") || strings.Contains(logs[0].ErrorMsg, "完整响应") {
		t.Fatalf("log should not contain prompt/response content: %+v", logs[0])
	}

	usage, err := repos.AICallLog.FindDailyUsage(7, logs[0].CreatedAt.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("find usage: %v", err)
	}
	if usage == nil || usage.LLMRequests != 1 || usage.InputChars != 100 || usage.OutputChars != 40 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestObservedChatClientRecordsFailure(t *testing.T) {
	repos := newAIObserverTestRepositories(t)
	observer := NewAIObserver(repos)
	client := ai.NewObservedChatClient(&failingChatClient{}, observer, ai.CallContext{
		UserID:    7,
		TaskID:    42,
		SessionID: 99,
		Provider:  "openai_compatible",
		Model:     "chat-model",
	})

	_, err := client.Chat(context.Background(), []ai.ChatMessage{{Role: "user", Content: "完整 prompt 不应入库"}})
	if err == nil {
		t.Fatal("Chat() succeeded, want failure")
	}

	logs, listErr := repos.AICallLog.ListByUserID(7, 10)
	if listErr != nil {
		t.Fatalf("list logs: %v", listErr)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(logs))
	}
	if logs[0].Status != model.AICallStatusFailed || logs[0].Kind != model.AICallKindLLM {
		t.Fatalf("log = %+v, want failed llm", logs[0])
	}
	if logs[0].ErrorMsg == "" || strings.Contains(logs[0].ErrorMsg, "完整 prompt") {
		t.Fatalf("error msg should be summarized without prompt content: %q", logs[0].ErrorMsg)
	}
}

type failingChatClient struct{}

func (f *failingChatClient) Chat(context.Context, []ai.ChatMessage) (string, error) {
	return "", errors.New("provider timeout")
}

func newAIObserverTestRepositories(t *testing.T) *repository.Repositories {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.NewRepositories(db)
}
