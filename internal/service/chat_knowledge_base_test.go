package service

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

func TestChatServiceCreatesVideoAndKnowledgeBaseSessionsWithScopedTitles(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "11111111111111111111111111111111", Filename: "video-a.mp4", FileURL: "videos/a.mp4", Title: "视频 A"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "后端知识库"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}

	videoSession, err := NewChatService(repos, nil, ChatConfig{}).CreateScopedSession(7, CreateChatSessionRequest{TaskID: task.ID})
	if err != nil {
		t.Fatalf("create video session: %v", err)
	}
	if videoSession.ScopeType != model.ChatScopeVideo || videoSession.TaskID != task.ID || videoSession.KnowledgeBaseID != 0 || videoSession.Title != "视频 A" {
		t.Fatalf("video session = %+v", videoSession)
	}

	kbSession, err := NewChatService(repos, nil, ChatConfig{}).CreateScopedSession(7, CreateChatSessionRequest{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID})
	if err != nil {
		t.Fatalf("create kb session: %v", err)
	}
	if kbSession.ScopeType != model.ChatScopeKnowledgeBase || kbSession.TaskID != 0 || kbSession.KnowledgeBaseID != kb.ID || kbSession.Title != kb.Name {
		t.Fatalf("kb session = %+v", kbSession)
	}
}

func TestChatServiceRejectsInvalidOrCrossOwnerSessionScope(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 8, FileMD5: "22222222222222222222222222222222", Filename: "private.mp4", FileURL: "videos/private.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 8, Name: "private"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(repos, nil, ChatConfig{})

	cases := []CreateChatSessionRequest{
		{},
		{TaskID: task.ID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID},
		{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID},
	}
	for _, req := range cases {
		if _, err := svc.CreateScopedSession(7, req); err == nil {
			t.Fatalf("CreateScopedSession(%+v) succeeded", req)
		}
	}
}

func TestChatServiceListsBothScopesAndFiltersThem(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "33333333333333333333333333333333", Filename: "a.mp4", FileURL: "videos/a.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "kb"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(repos, nil, ChatConfig{})
	if _, err := svc.CreateScopedSession(7, CreateChatSessionRequest{TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateScopedSession(7, CreateChatSessionRequest{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.ListSessionsWithFilter(7, ListChatSessionsFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %+v err=%v", all, err)
	}
	onlyKB, err := svc.ListSessionsWithFilter(7, ListChatSessionsFilter{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID})
	if err != nil || len(onlyKB) != 1 || onlyKB[0].KnowledgeBaseID != kb.ID {
		t.Fatalf("kb = %+v err=%v", onlyKB, err)
	}
	onlyVideo, err := svc.ListSessionsWithFilter(7, ListChatSessionsFilter{ScopeType: model.ChatScopeVideo, TaskID: task.ID})
	if err != nil || len(onlyVideo) != 1 || onlyVideo[0].TaskID != task.ID {
		t.Fatalf("video = %+v err=%v", onlyVideo, err)
	}
}

func TestKnowledgeBaseChatForcesStrictRAGAndRejectsUnavailableMembers(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "44444444444444444444444444444444", Filename: "a.mp4", FileURL: "videos/a.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatal(err)
	}
	kb := &model.KnowledgeBase{UserID: 7, Name: "kb"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.KnowledgeBase.AddVideoForUser(7, kb.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: kb.Name}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	if err := repos.Summary.Create(&model.AISummary{TaskID: task.ID, Content: "不能作为 KB fallback", ModelName: "m"}); err != nil {
		t.Fatal(err)
	}

	svc := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5})
	_, err := svc.AskWithMode(context.Background(), ChatModeVideoAssistant, 7, session.ID, "总结一下", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{EmbeddingModel: "embed-new", LLMModel: "chat"})
	if err == nil || !strings.Contains(err.Error(), "不可用") || !strings.Contains(err.Error(), string(rune('0'+task.ID))) {
		t.Fatalf("err = %v, want unavailable member ids", err)
	}
}

func TestVideoAgentRejectsKnowledgeBaseSession(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	kb := &model.KnowledgeBase{UserID: 7, Name: "kb"}
	if err := repos.KnowledgeBase.Create(kb); err != nil {
		t.Fatal(err)
	}
	session := &model.ChatSession{UserID: 7, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kb.ID, Title: kb.Name}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatal(err)
	}
	svc := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5}))
	_, err := svc.Ask(context.Background(), VideoAgentRequest{UserID: 7, SessionID: session.ID, Question: "总结"}, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{EmbeddingModel: "embed", LLMModel: "chat"})
	if err == nil || !strings.Contains(err.Error(), "知识库会话") {
		t.Fatalf("err = %v", err)
	}
}
