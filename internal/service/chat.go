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
	CandidateK  int
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
	ChunkID                int64    `json:"chunk_id"`
	ChunkIndex             int      `json:"chunk_index"`
	Score                  float32  `json:"score"`
	Content                string   `json:"content"`
	Source                 string   `json:"source,omitempty"`
	VectorRank             int      `json:"vector_rank,omitempty"`
	KeywordRank            int      `json:"keyword_rank,omitempty"`
	RRFScore               float64  `json:"rrf_score,omitempty"`
	ExpandedFromChunkIndex int      `json:"expanded_from_chunk_index,omitempty"`
	ExpandedWindowStart    int      `json:"expanded_window_start,omitempty"`
	ExpandedWindowEnd      int      `json:"expanded_window_end,omitempty"`
	WindowTruncated        bool     `json:"window_truncated,omitempty"`
	RerankScore            float64  `json:"rerank_score,omitempty"`
	FinalRank              int      `json:"final_rank,omitempty"`
	MatchedQuery           string   `json:"matched_query,omitempty"`
	CrossQueryRank         int      `json:"cross_query_rank,omitempty"`
	Fallbacks              []string `json:"fallbacks,omitempty"`
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
	recorder  ai.CallRecorder
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

type preparedRAGChat struct {
	Session     *model.ChatSession
	Question    string
	TopK        int
	RecentLimit int
	Citations   []RetrievedChunk
	Messages    []ai.ChatMessage
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

func (s *ChatService) SetAIRecorder(recorder ai.CallRecorder) {
	s.recorder = recorder
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
	embedding, chat = s.observedAIClients(userID, sessionID, 0, embedding, chat, profile)
	prepared, err := s.prepareRAGChat(ctx, userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}

	answer, err := chat.Chat(ctx, prepared.Messages)
	if err != nil {
		return nil, err
	}

	return s.saveChatExchange(ctx, userID, sessionID, prepared.Question, answer, prepared.Citations, prepared.RecentLimit, profile.LLMModel)
}

func (s *ChatService) prepareRAGChat(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*preparedRAGChat, error) {
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
	if s.retriever == nil {
		return nil, fmt.Errorf("当前视频尚未构建 RAG 索引")
	}
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK > 10 {
		topK = 10
	}

	recentLimit := s.cfg.RecentTurns * 2
	recent, err := s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	retrieval, err := s.newRetrievalPipeline(topK, chat).Retrieve(ctx, RetrievalPipelineRequest{
		UserID:         userID,
		TaskID:         session.TaskID,
		Question:       question,
		Recent:         recent,
		TopK:           topK,
		EmbeddingModel: profile.EmbeddingModel,
		Embedding:      embedding,
	})
	if err != nil {
		return nil, err
	}
	citations := retrieval.Citations
	if len(citations) == 0 {
		return nil, fmt.Errorf("未检索到足够相关的视频片段")
	}
	messages := buildRAGMessages(citations, recent, question)
	return &preparedRAGChat{
		Session:     session,
		Question:    question,
		TopK:        topK,
		RecentLimit: recentLimit,
		Citations:   citations,
		Messages:    messages,
	}, nil
}

func (s *ChatService) newRetrievalPipeline(topK int, chat ai.ChatClient) *RetrievalPipeline {
	return &RetrievalPipeline{
		repos:     s.repos,
		retriever: s.retriever,
		rewriter:  NewLLMQueryRewriter(chat),
		expander: &ContextExpander{
			repos:               s.repos,
			Radius:              1,
			MaxCharsPerCitation: 4000,
		},
		reranker:   DeterministicReranker{},
		CandidateK: s.candidateK(topK),
		MinScore:   s.cfg.MinScore,
	}
}

func (s *ChatService) saveChatExchange(ctx context.Context, userID, sessionID int64, question, answer string, citations []RetrievedChunk, recentLimit int, modelName string) (*AskResult, error) {
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
		ModelName:         modelName,
	}
	if err := s.repos.Chat.CreateMessage(assistantMessage); err != nil {
		return nil, err
	}
	_ = s.refreshRecentMemory(ctx, userID, sessionID, recentLimit)

	return &AskResult{
		MessageID: assistantMessage.ID,
		Answer:    answer,
		Citations: citations,
		Model:     modelName,
	}, nil
}

func (s *ChatService) candidateK(topK int) int {
	candidateK := s.cfg.CandidateK
	if candidateK <= 0 {
		return topK
	}
	if candidateK < topK {
		return topK
	}
	if candidateK > 50 {
		return 50
	}
	return candidateK
}

func (s *ChatService) mergeKeywordChunks(taskID, userID int64, embeddingModel, question string, vectorChunks []RetrievedChunk, candidateK, topK int) ([]RetrievedChunk, error) {
	terms := ExtractQueryTerms(question)
	keywordResults, err := s.repos.VideoChunk.SearchByBM25(userID, taskID, embeddingModel, terms, candidateK)
	if err != nil {
		return nil, err
	}
	keywordChunks := make([]RetrievedChunk, 0, len(keywordResults))
	for _, result := range keywordResults {
		keywordChunks = append(keywordChunks, RetrievedChunk{
			ChunkID:     result.Chunk.ID,
			ChunkIndex:  result.Chunk.ChunkIndex,
			Score:       float32(result.Score),
			Content:     result.Chunk.Content,
			Source:      RetrievalSourceKeyword,
			KeywordRank: result.Rank,
		})
	}
	return FuseRetrievedChunks(vectorChunks, keywordChunks, topK, defaultRRFK), nil
}

func retrievalChunkKey(chunk RetrievedChunk) string {
	if chunk.ChunkID > 0 {
		return fmt.Sprintf("id:%d", chunk.ChunkID)
	}
	return fmt.Sprintf("idx:%d:%s", chunk.ChunkIndex, chunk.Content)
}

func (s *ChatService) AskStream(ctx context.Context, userID, sessionID int64, question string, topK int, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile, emit func(ChatStreamEvent) error) (*AskResult, error) {
	if emit == nil {
		return nil, fmt.Errorf("stream emit 不能为空")
	}
	embedding, chat = s.observedAIClients(userID, sessionID, 0, embedding, chat, profile)
	prepared, err := s.prepareRAGChat(ctx, userID, sessionID, question, topK, embedding, chat, profile)
	if err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "citations", Data: prepared.Citations}); err != nil {
		return nil, err
	}

	var answer string
	if streaming, ok := chat.(ai.StreamingChatClient); ok {
		err = streaming.StreamChat(ctx, prepared.Messages, func(delta string) error {
			answer += delta
			return emit(ChatStreamEvent{Type: "answer", Data: delta})
		})
		if err != nil {
			return nil, err
		}
	} else {
		answer, err = chat.Chat(ctx, prepared.Messages)
		if err != nil {
			return nil, err
		}
		for _, chunk := range splitAnswerForStream(answer, 80) {
			if err := emit(ChatStreamEvent{Type: "answer", Data: chunk}); err != nil {
				return nil, err
			}
		}
	}

	result, err := s.saveChatExchange(ctx, userID, sessionID, prepared.Question, answer, prepared.Citations, prepared.RecentLimit, profile.LLMModel)
	if err != nil {
		return nil, err
	}
	if err := emit(ChatStreamEvent{Type: "done", Data: map[string]interface{}{
		"message_id": result.MessageID,
		"model":      result.Model,
	}}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ChatService) observedAIClients(userID, sessionID, taskID int64, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (ai.EmbeddingClient, ai.ChatClient) {
	if s.recorder == nil {
		return embedding, chat
	}
	if taskID <= 0 && sessionID > 0 {
		session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
		if err == nil && session != nil {
			taskID = session.TaskID
		}
	}
	embedding = ai.NewObservedEmbeddingClient(embedding, s.recorder, ai.CallContext{
		UserID:    userID,
		TaskID:    taskID,
		SessionID: sessionID,
		Provider:  profile.EmbeddingProvider,
		Model:     profile.EmbeddingModel,
	})
	chat = ai.NewObservedChatClient(chat, s.recorder, ai.CallContext{
		UserID:    userID,
		TaskID:    taskID,
		SessionID: sessionID,
		Provider:  profile.LLMProvider,
		Model:     profile.LLMModel,
	})
	return embedding, chat
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
		contextLines = append(contextLines, fmt.Sprintf("%s\n%s", describeRetrievedChunk(chunk), chunk.Content))
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
