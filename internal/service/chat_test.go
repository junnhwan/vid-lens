package service

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type fakeRetriever struct {
	results []RetrievedChunk
	lastReq RetrievalRequest
}

func (r *fakeRetriever) Search(ctx context.Context, query []float32, req RetrievalRequest) ([]RetrievedChunk, error) {
	r.lastReq = req
	return r.results, nil
}

type recordingChatClient struct {
	messages []ai.ChatMessage
}

func (c *recordingChatClient) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	c.messages = append([]ai.ChatMessage(nil), messages...)
	return "这是基于视频片段的回答", nil
}

type fakeChatMemoryStore struct {
	recent []model.ChatMessage
	saved  []model.ChatMessage
}

func (s *fakeChatMemoryStore) GetRecentMessages(_ context.Context, _ int64, _ int) ([]model.ChatMessage, error) {
	return s.recent, nil
}

func (s *fakeChatMemoryStore) SaveRecentMessages(_ context.Context, _ int64, messages []model.ChatMessage, _ int) error {
	s.saved = append([]model.ChatMessage(nil), messages...)
	return nil
}

func TestChatServiceAskRetrievesChunksAndStoresMessages(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "cccccccccccccccccccccccccccccccc", Filename: "video.mp4", FileURL: "videos/c.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	embedding := &fakeEmbeddingClient{dim: 3}
	chatClient := &recordingChatClient{}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "分布式锁释放时要校验 owner"},
	}}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8})

	result, err := svc.Ask(context.Background(), 7, session.ID, "为什么要校验 owner？", 0, embedding, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if result.Answer != "这是基于视频片段的回答" {
		t.Fatalf("Answer = %q", result.Answer)
	}
	if len(result.Citations) != 1 {
		t.Fatalf("citations = %+v", result.Citations)
	}

	joinedPrompt := ""
	for _, msg := range chatClient.messages {
		joinedPrompt += msg.Content + "\n"
	}
	if !strings.Contains(joinedPrompt, "分布式锁释放时要校验 owner") {
		t.Fatalf("prompt did not include retrieved chunk: %s", joinedPrompt)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("stored messages = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected stored messages: %+v", messages)
	}
	if messages[0].RetrievalSnapshot != nil {
		t.Fatalf("user retrieval snapshot = %q, want nil", *messages[0].RetrievalSnapshot)
	}
	if messages[1].RetrievalSnapshot == nil || !strings.Contains(*messages[1].RetrievalSnapshot, "分布式锁释放时要校验 owner") {
		t.Fatalf("assistant retrieval snapshot = %#v, want serialized citations", messages[1].RetrievalSnapshot)
	}
}

func TestChatServiceAskUsesRequestedTopKAndRedisRecentMemory(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Filename: "video.mp4", FileURL: "videos/e.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := repos.Chat.CreateMessage(&model.ChatMessage{
		SessionID: session.ID,
		UserID:    7,
		Role:      "user",
		Content:   "这条数据库历史不应该进入 prompt",
	}); err != nil {
		t.Fatalf("create db message: %v", err)
	}

	chatClient := &recordingChatClient{}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "缓存测试片段"},
	}}
	memory := &fakeChatMemoryStore{recent: []model.ChatMessage{
		{Role: "user", Content: "这条 Redis 最近记忆应该进入 prompt"},
	}}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 5, MinScore: 0.3, RecentTurns: 8})
	svc.SetMemoryStore(memory)

	_, err := svc.Ask(context.Background(), 7, session.ID, "使用 topK 吗？", 3, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if retriever.lastReq.TopK != 3 {
		t.Fatalf("retriever TopK = %d, want 3", retriever.lastReq.TopK)
	}
	joinedPrompt := ""
	for _, msg := range chatClient.messages {
		joinedPrompt += msg.Content + "\n"
	}
	if !strings.Contains(joinedPrompt, "这条 Redis 最近记忆应该进入 prompt") {
		t.Fatalf("prompt did not include cached memory: %s", joinedPrompt)
	}
	if strings.Contains(joinedPrompt, "这条数据库历史不应该进入 prompt") {
		t.Fatalf("prompt unexpectedly included DB fallback memory: %s", joinedPrompt)
	}
	if len(memory.saved) == 0 {
		t.Fatal("memory store was not refreshed after Ask()")
	}
}

func TestChatServiceAskRejectsNoRetrievedContext(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "dddddddddddddddddddddddddddddddd", Filename: "video.mp4", FileURL: "videos/d.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	svc := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 5, MinScore: 0.3})
	_, err := svc.Ask(context.Background(), 7, session.ID, "没有相关内容？", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err == nil {
		t.Fatal("Ask() succeeded without retrieved context")
	}
}

func TestChatServiceAskStreamEmitsCitationsAnswerChunksAndDone(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "abababababababababababababababab", Filename: "video.mp4", FileURL: "videos/stream.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	svc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "流式问答片段"},
	}}, ChatConfig{TopK: 5, MinScore: 0.3})

	var events []ChatStreamEvent
	result, err := svc.AskStream(context.Background(), 7, session.ID, "如何流式？", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	}, func(event ChatStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("AskStream() error = %v", err)
	}
	if result.Answer == "" {
		t.Fatal("expected answer")
	}
	if len(events) < 3 {
		t.Fatalf("events = %#v, want citations, answer and done", events)
	}
	if events[0].Type != "citations" {
		t.Fatalf("first event = %#v, want citations", events[0])
	}
	if events[len(events)-1].Type != "done" {
		t.Fatalf("last event = %#v, want done", events[len(events)-1])
	}
	foundAnswer := false
	for _, event := range events {
		if event.Type == "answer" {
			foundAnswer = true
		}
	}
	if !foundAnswer {
		t.Fatalf("events = %#v, want answer event", events)
	}
}

func newChatServiceTestRepositories(t *testing.T) *repository.Repositories {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return repository.NewRepositories(db)
}
