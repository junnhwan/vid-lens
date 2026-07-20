package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

func TestSaveChatExchangeRollsBackMessagesAndTitleWhenSourceInsertFails(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "dddddddddddddddddddddddddddddddd", Filename: "rollback.mp4", FileURL: "videos/rollback.mp4", Title: "回滚视频"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, ScopeType: model.ChatScopeVideo, Title: task.Title}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}

	svc := NewChatService(repos, nil, ChatConfig{RecentTurns: 8})
	_, err := svc.saveChatExchange(context.Background(), 7, session.ID, "首个问题应该成为标题", "回答", []Citation{{TaskID: task.ID + 999, CitationID: "C1", Content: "invalid source"}}, 16, "chat")
	if err == nil {
		t.Fatal("saveChatExchange succeeded with invalid source")
	}

	messages, listErr := repos.Chat.ListMessages(7, session.ID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(messages) != 0 {
		t.Fatalf("messages=%+v, want transaction rollback", messages)
	}
	reloaded, findErr := repos.Chat.FindSessionForUser(7, session.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if reloaded.Title != task.Title {
		t.Fatalf("title=%q, want unchanged %q", reloaded.Title, task.Title)
	}
}
