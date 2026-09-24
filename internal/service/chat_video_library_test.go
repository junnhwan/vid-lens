package service

import (
	"testing"

	"vid-lens/internal/model"
)

func TestVideoLibrarySessionStaysSeparateAndRetrievesOnlyOwnedIndexedVideos(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	owned := &model.VideoTask{UserID: 7, FileMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Filename: "owned.mp4", FileURL: "owned.mp4"}
	other := &model.VideoTask{UserID: 8, FileMD5: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "other.mp4", FileURL: "other.mp4"}
	for _, task := range []*model.VideoTask{owned, other} {
		if err := repos.Task.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []*model.VideoTask{owned, other} {
		if err := repos.RAGIndex.Upsert(&model.VideoRAGIndex{UserID: task.UserID, TaskID: task.ID, FileMD5: task.FileMD5, EmbeddingModel: "embed", EmbeddingDim: 3, Status: model.RAGIndexStatusIndexed}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewChatService(repos, nil, ChatConfig{})
	library, err := svc.CreateScopedSession(7, CreateChatSessionRequest{ScopeType: model.ChatScopeVideoLibrary})
	if err != nil {
		t.Fatal(err)
	}
	if library.TaskID != 0 || library.KnowledgeBaseID != 0 {
		t.Fatalf("scope mixed: %+v", library)
	}
	if _, err := svc.CreateScopedSession(7, CreateChatSessionRequest{ScopeType: model.ChatScopeVideoLibrary, TaskID: owned.ID}); err == nil {
		t.Fatal("mixed scope was accepted")
	}
	ids, err := svc.sessionRetrievalTaskIDs(7, library, "embed")
	if err != nil || len(ids) != 1 || ids[0] != owned.ID {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	filtered, err := svc.ListSessionsWithFilter(7, ListChatSessionsFilter{ScopeType: model.ChatScopeVideoLibrary})
	if err != nil || len(filtered) != 1 || filtered[0].ID != library.ID {
		t.Fatalf("sessions = %+v, %v", filtered, err)
	}
	makeExchange := func() (*model.ChatMessage, *model.ChatMessage) {
		return &model.ChatMessage{UserID: 7, SessionID: library.ID, Role: "user", Content: "问题"}, &model.ChatMessage{UserID: 7, SessionID: library.ID, Role: "assistant", Content: "回答"}
	}
	question, answer := makeExchange()
	if err := repos.Chat.CreateExchange(7, question, answer, []int64{other.ID}); err == nil {
		t.Fatal("cross-owner source was accepted")
	}
	question, answer = makeExchange()
	if err := repos.Chat.CreateExchange(7, question, answer, []int64{owned.ID}); err != nil {
		t.Fatalf("owned source rejected: %v", err)
	}
}
