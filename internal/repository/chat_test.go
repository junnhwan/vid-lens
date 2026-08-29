package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestChatRepositoryCreatesSessionAndListsMessages(t *testing.T) {
	repo := newChatTestRepo(t)

	session := &model.ChatSession{UserID: 7, TaskID: 2, Title: "session"}
	if err := repo.CreateSession(session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := repo.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "user", Content: "question"}); err != nil {
		t.Fatalf("CreateMessage(user) error = %v", err)
	}
	if err := repo.CreateMessage(&model.ChatMessage{SessionID: session.ID, UserID: 7, Role: "assistant", Content: "answer"}); err != nil {
		t.Fatalf("CreateMessage(assistant) error = %v", err)
	}

	messages, err := repo.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected message order: %+v", messages)
	}
}

func TestChatRepositoryFindSessionIsUserScoped(t *testing.T) {
	repo := newChatTestRepo(t)
	session := &model.ChatSession{UserID: 7, TaskID: 2, Title: "session"}
	if err := repo.CreateSession(session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	got, err := repo.FindSessionForUser(8, session.ID)
	if err != nil {
		t.Fatalf("FindSessionForUser() error = %v", err)
	}
	if got != nil {
		t.Fatalf("FindSessionForUser() = %+v, want nil for wrong user", got)
	}
}

func newChatTestRepo(t *testing.T) *ChatRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChatSession{}, &model.ChatMessage{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return NewChatRepository(db)
}
