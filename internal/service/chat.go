package service

import (
	"context"
	"errors"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

// ChatService 的公共配置、依赖和问答流程共享数据结构。
// 会话、检索准备、回答、流式适配和最近消息分别位于 chat_*.go。
type ChatMode string

const (
	ChatModeVideoAssistant ChatMode = "video_assistant"
	ChatModeStrictRAG      ChatMode = "strict_rag"
)

const maxVideoContextRunes = 8000

var (
	errRAGIndexUnavailable = errors.New("当前视频尚未构建 RAG 索引")
	errNoRetrievedContext  = errors.New("未检索到足够相关的视频片段")
)

type ChatConfig struct {
	TopK                 int
	CandidateK           int
	MinScore             float32
	RecentTurns          int
	Retrieval            *RAGRetrievalConfig
	ModelRerankerFactory func(ai.Profile) Reranker
}

type RetrievalRequest struct {
	UserID         int64
	TaskID         int64
	EmbeddingModel string
	TopK           int
	MinScore       float32
}

type RetrievedChunk struct {
	EvidenceID             string   `json:"evidence_id"`
	ChunkID                int64    `json:"chunk_id"`
	ChunkIndex             int      `json:"chunk_index"`
	Score                  float32  `json:"score"`
	Content                string   `json:"content"`
	AnchorContent          string   `json:"anchor_content,omitempty"`
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

// Citation is the public, persisted evidence view. It intentionally excludes
// expanded LLM context and anchor internals so API/SSE/snapshots cannot expose
// the large retrieval window by accident.
type Citation struct {
	CitationID  string  `json:"citation_id"`
	EvidenceID  string  `json:"evidence_id"`
	ChunkID     int64   `json:"chunk_id"`
	ChunkIndex  int     `json:"chunk_index"`
	Score       float32 `json:"score"`
	Content     string  `json:"content"`
	Source      string  `json:"source,omitempty"`
	VectorRank  int     `json:"vector_rank,omitempty"`
	KeywordRank int     `json:"keyword_rank,omitempty"`
	RRFScore    float64 `json:"rrf_score,omitempty"`
	RerankScore float64 `json:"rerank_score,omitempty"`
	FinalRank   int     `json:"final_rank,omitempty"`
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
	MessageID int64      `json:"message_id"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
	Model     string     `json:"model"`
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
	Contexts    []RetrievedChunk
	Citations   []Citation
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
