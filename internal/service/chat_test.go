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
	messages  []ai.ChatMessage
	chatCalls int
}

func (c *recordingChatClient) Chat(_ context.Context, messages []ai.ChatMessage) (string, error) {
	c.chatCalls++
	c.messages = append([]ai.ChatMessage(nil), messages...)
	return "这是基于视频片段的回答", nil
}

type streamingRecordingChatClient struct {
	recordingChatClient
	streamed    []string
	streamCalls int
}

func (c *streamingRecordingChatClient) StreamChat(_ context.Context, messages []ai.ChatMessage, emit func(delta string) error) error {
	c.streamCalls++
	c.messages = append([]ai.ChatMessage(nil), messages...)
	for _, delta := range c.streamed {
		if err := emit(delta); err != nil {
			return err
		}
	}
	return nil
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

func TestChatServiceAskRecordsEmbeddingAndLLMCalls(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "acacacacacacacacacacacacacacacac", Filename: "video.mp4", FileURL: "videos/audit-chat.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	svc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "AI 调用审计测试片段"},
	}}, ChatConfig{TopK: 5, MinScore: 0.3})
	svc.SetAIRecorder(NewAIObserver(repos))

	_, err := svc.Ask(context.Background(), 7, session.ID, "审计会记录哪些字段？", 0, &fakeEmbeddingClient{dim: 3}, &recordingChatClient{}, ai.Profile{
		EmbeddingProvider: "openai_compatible",
		EmbeddingModel:    "text-embedding-3-small",
		LLMProvider:       "openai_compatible",
		LLMModel:          "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}

	logs, err := repos.AICallLog.ListByUserID(7, 10)
	if err != nil {
		t.Fatalf("list ai call logs: %v", err)
	}
	kinds := make(map[string]bool)
	for _, log := range logs {
		kinds[log.Kind] = true
		if log.UserID != 7 || log.TaskID != task.ID || log.SessionID != session.ID {
			t.Fatalf("log scope = %+v", log)
		}
		if log.InputChars <= 0 {
			t.Fatalf("log should record input char count: %+v", log)
		}
	}
	if !kinds[model.AICallKindEmbedding] || !kinds[model.AICallKindLLM] {
		t.Fatalf("logs = %+v, want embedding and llm calls", logs)
	}

	usage, err := repos.AICallLog.FindDailyUsage(7, logs[0].CreatedAt.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("find daily usage: %v", err)
	}
	if usage == nil || usage.EmbeddingRequests != 1 || usage.LLMRequests != 1 {
		t.Fatalf("usage = %+v, want embedding=1 llm=1", usage)
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

func TestChatServiceAskMergesKeywordChunksWithVectorResults(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "fefefefefefefefefefefefefefefefe", Filename: "video.mp4", FileURL: "videos/hybrid.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := repos.VideoChunk.ReplaceTaskChunks(task.ID, "text-embedding-3-small", []model.VideoChunk{
		{UserID: 7, TaskID: task.ID, ChunkIndex: 0, Content: "关键词命中的分布式锁片段", ContentHash: "hash0", EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536, VectorID: "kw-vector"},
	}); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	chatClient := &recordingChatClient{}
	retriever := &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 99, ChunkIndex: 9, Score: 0.82, Content: "纯向量召回片段"},
	}}
	svc := NewChatService(repos, retriever, ChatConfig{TopK: 2, CandidateK: 6, MinScore: 0.3})

	result, err := svc.Ask(context.Background(), 7, session.ID, "分布式锁", 2, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if len(result.Citations) != 2 {
		t.Fatalf("citations = %+v, want vector and keyword chunks", result.Citations)
	}

	joinedPrompt := ""
	for _, msg := range chatClient.messages {
		joinedPrompt += msg.Content + "\n"
	}
	if !strings.Contains(joinedPrompt, "关键词命中的分布式锁片段") {
		t.Fatalf("prompt did not include keyword chunk: %s", joinedPrompt)
	}
	if retriever.lastReq.TopK != 6 {
		t.Fatalf("vector candidate TopK = %d, want 6", retriever.lastReq.TopK)
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

func TestChatServiceAskStreamUsesProviderStreamingAndStoresAccumulatedAnswer(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	task := &model.VideoTask{UserID: 7, FileMD5: "bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc", Filename: "video.mp4", FileURL: "videos/stream-real.mp4"}
	if err := repos.Task.Create(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	session := &model.ChatSession{UserID: 7, TaskID: task.ID, Title: "session"}
	if err := repos.Chat.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	chatClient := &streamingRecordingChatClient{streamed: []string{"第一段", "第二段"}}
	svc := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{
		{ChunkID: 1, ChunkIndex: 2, Score: 0.82, Content: "真正 token streaming 片段"},
	}}, ChatConfig{TopK: 5, MinScore: 0.3})

	var events []ChatStreamEvent
	result, err := svc.AskStream(context.Background(), 7, session.ID, "如何真正流式？", 0, &fakeEmbeddingClient{dim: 3}, chatClient, ai.Profile{
		EmbeddingModel: "text-embedding-3-small",
		LLMModel:       "chat-model",
	}, func(event ChatStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("AskStream() error = %v", err)
	}
	if chatClient.chatCalls != 0 {
		t.Fatalf("Chat() calls = %d, want provider streaming path", chatClient.chatCalls)
	}
	if chatClient.streamCalls != 1 {
		t.Fatalf("StreamChat() calls = %d, want 1", chatClient.streamCalls)
	}
	if result.Answer != "第一段第二段" {
		t.Fatalf("answer = %q, want accumulated streaming answer", result.Answer)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want citations, two answer deltas and done", events)
	}
	if events[0].Type != "citations" || events[1].Type != "answer" || events[2].Type != "answer" || events[3].Type != "done" {
		t.Fatalf("event order = %#v", events)
	}
	if events[1].Data != "第一段" || events[2].Data != "第二段" {
		t.Fatalf("answer events = %#v", events)
	}

	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("stored messages = %d, want 2", len(messages))
	}
	if messages[1].Content != "第一段第二段" {
		t.Fatalf("stored assistant content = %q", messages[1].Content)
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
