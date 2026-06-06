package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type ChatConfig struct {
	TopK        int
	MinScore    float32
	RecentTurns int
}

type RetrievalRequest struct {
	UserID         int64
	TaskID         int64
	EmbeddingModel string
	TopK           int
	MinScore       float32
}

type RetrievedChunk struct {
	ChunkID    int64   `json:"chunk_id"`
	ChunkIndex int     `json:"chunk_index"`
	Score      float32 `json:"score"`
	Content    string  `json:"content"`
}

type RAGRetriever interface {
	Search(ctx context.Context, query []float32, req RetrievalRequest) ([]RetrievedChunk, error)
}

type ChatMemoryStore interface {
	GetRecentMessages(ctx context.Context, sessionID int64, limit int) ([]model.ChatMessage, error)
	SaveRecentMessages(ctx context.Context, sessionID int64, messages []model.ChatMessage, limit int) error
}

type ChatService struct {
	repos     *repository.Repositories
	retriever RAGRetriever
	memory    ChatMemoryStore
	cfg       ChatConfig
}

type AskResult struct {
	MessageID int64            `json:"message_id"`
	Answer    string           `json:"answer"`
	Citations []RetrievedChunk `json:"citations"`
	Model     string           `json:"model"`
}

type ChatStreamEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

func NewChatService(repos *repository.Repositories, retriever RAGRetriever, cfg ChatConfig) *ChatService {
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.RecentTurns <= 0 {
		cfg.RecentTurns = 8
	}
	return &ChatService{repos: repos, retriever: retriever, cfg: cfg}
}

func (s *ChatService) SetMemoryStore(memory ChatMemoryStore) {
	s.memory = memory
}

func (s *ChatService) CreateSession(userID, taskID int64, title string) (*model.ChatSession, error) {
	task, err := s.repos.Task.FindByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("无权访问此任务")
	}
	if strings.TrimSpace(title) == "" {
		title = task.Filename
	}
	session := &model.ChatSession{UserID: userID, TaskID: taskID, Title: strings.TrimSpace(title)}
	if err := s.repos.Chat.CreateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *ChatService) ListSessions(userID, taskID int64) ([]model.ChatSession, error) {
	return s.repos.Chat.ListSessions(userID, taskID)
}

func (s *ChatService) ListMessages(userID, sessionID int64) ([]model.ChatMessage, error) {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}
	return s.repos.Chat.ListMessages(userID, sessionID)
}

func (s *ChatService) Ask(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*AskResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if len([]rune(question)) > 1000 {
		return nil, fmt.Errorf("问题过长")
	}

	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("无权访问此会话")
	}

	queryVector, err := embedding.Embed(ctx, question)
	if err != nil {
		return nil, err
	}
	if s.retriever == nil {
		return nil, fmt.Errorf("当前视频尚未构建 RAG 索引")
	}
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK > 10 {
		topK = 10
	}
	citations, err := s.retriever.Search(ctx, queryVector, RetrievalRequest{
		UserID:         userID,
		TaskID:         session.TaskID,
		EmbeddingModel: profile.EmbeddingModel,
		TopK:           topK,
		MinScore:       s.cfg.MinScore,
	})
	if err != nil {
		return nil, err
	}
	if len(citations) == 0 {
		return nil, fmt.Errorf("未检索到足够相关的视频片段")
	}

	recentLimit := s.cfg.RecentTurns * 2
	recent, err := s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	messages := buildRAGMessages(citations, recent, question)
	answer, err := chat.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	if err := s.repos.Chat.CreateMessage(&model.ChatMessage{
		SessionID: sessionID,
		UserID:    userID,
		Role:      "user",
		Content:   question,
	}); err != nil {
		return nil, err
	}

	snapshot, err := json.Marshal(citations)
	if err != nil {
		return nil, err
	}
	snapshotText := string(snapshot)
	assistantMessage := &model.ChatMessage{
		SessionID:         sessionID,
		UserID:            userID,
		Role:              "assistant",
		Content:           answer,
		RetrievalSnapshot: &snapshotText,
		ModelName:         profile.LLMModel,
	}
	if err := s.repos.Chat.CreateMessage(assistantMessage); err != nil {
		return nil, err
	}
	_ = s.refreshRecentMemory(ctx, userID, sessionID, recentLimit)

	return &AskResult{
		MessageID: assistantMessage.ID,
		Answer:    answer,
		Citations: citations,
		Model:     profile.LLMModel,
	}, nil
}

func (s *ChatService) AskStream(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(ChatStreamEvent) error) (*AskResult, error) {
	if emit == nil {
		return nil, fmt.Errorf("stream emit 不能为空")
	}
	result, err := s.Ask(ctx, userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "citations", Data: result.Citations}); err != nil {
		return nil, err
	}
	for _, chunk := range splitAnswerForStream(result.Answer, 80) {
		if err := emit(ChatStreamEvent{Type: "answer", Data: chunk}); err != nil {
			return nil, err
		}
	}
	if err := emit(ChatStreamEvent{Type: "done", Data: map[string]interface{}{
		"message_id": result.MessageID,
		"model":      result.Model,
	}}); err != nil {
		return nil, err
	}
	return result, nil
}

func splitAnswerForStream(answer string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 80
	}
	runes := []rune(answer)
	if len(runes) == 0 {
		return []string{""}
	}
	parts := make([]string, 0, (len(runes)/maxRunes)+1)
	for len(runes) > 0 {
		n := maxRunes
		if len(runes) < n {
			n = len(runes)
		}
		parts = append(parts, string(runes[:n]))
		runes = runes[n:]
	}
	return parts
}

func (s *ChatService) loadRecentMessages(ctx context.Context, userID, sessionID int64, limit int) ([]model.ChatMessage, error) {
	if s.memory != nil {
		cached, err := s.memory.GetRecentMessages(ctx, sessionID, limit)
		if err != nil {
			return nil, err
		}
		if len(cached) > 0 {
			return cached, nil
		}
	}

	recent, err := s.repos.Chat.ListRecentMessages(userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	if s.memory != nil && len(recent) > 0 {
		_ = s.memory.SaveRecentMessages(ctx, sessionID, recent, limit)
	}
	return recent, nil
}

func (s *ChatService) refreshRecentMemory(ctx context.Context, userID, sessionID int64, limit int) error {
	if s.memory == nil {
		return nil
	}
	recent, err := s.repos.Chat.ListRecentMessages(userID, sessionID, limit)
	if err != nil {
		return err
	}
	return s.memory.SaveRecentMessages(ctx, sessionID, recent, limit)
}

func buildRAGMessages(citations []RetrievedChunk, recent []model.ChatMessage, question string) []ai.ChatMessage {
	contextLines := make([]string, 0, len(citations))
	for _, chunk := range citations {
		contextLines = append(contextLines, fmt.Sprintf("[Chunk %d score=%.3f]\n%s", chunk.ChunkIndex, chunk.Score, chunk.Content))
	}

	messages := []ai.ChatMessage{
		{
			Role:    "system",
			Content: "你是 VidLens 的视频内容问答助手。你只能基于给定的视频片段和必要的会话上下文回答。如果检索片段中没有答案，直接说明当前视频片段中没有找到相关信息，不要编造。回答应尽量引用具体片段。",
		},
		{
			Role:    "system",
			Content: "检索到的视频片段：\n" + strings.Join(contextLines, "\n\n"),
		},
	}
	for _, msg := range recent {
		if msg.Role == "user" || msg.Role == "assistant" {
			messages = append(messages, ai.ChatMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	messages = append(messages, ai.ChatMessage{Role: "user", Content: question})
	return messages
}
