package service

import (
	"context"
	"errors"
	"strings"

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
	TaskID         int64 // deprecated compatibility; new callers use TaskIDs
	TaskIDs        []int64
	EmbeddingModel string
	TopK           int
	MinScore       float32
}

type RetrievedChunk struct {
	TaskID                 int64    `json:"task_id"`
	VideoTitle             string   `json:"video_title,omitempty"`
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
	TaskID      int64   `json:"task_id"`
	VideoTitle  string  `json:"video_title,omitempty"`
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
	repos       *repository.Repositories
	retriever   RAGRetriever
	memory      ChatMemoryStore
	recorder    ai.CallRecorder
	cfg         ChatConfig
	intentRouter *IntentRouter // spec 05：级联 intent 分类；nil 时降级占位 classifyIntentPlaceholder
}

type AskResult struct {
	MessageID int64           `json:"message_id"`
	Answer    string          `json:"answer"`
	Citations []Citation      `json:"citations"`
	Model     string          `json:"model"`
	// Degraded 标记档2降级态（spec 06 决策记录第 10 节）：LLM 失败回退无 LLM 模式
	// （检索片段+已有摘要直拼）时为 true，对外告知用户当前降级。档1 rerank 失败回退
	// 向量基线后 LLM 仍生成完整答案，不标 degraded。
	Degraded bool `json:"degraded,omitempty"`
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
	// TaskIDs 是本次检索涉及的 task 范围（spec 07 证据约束重检索复用）。
	// strict_rag / 单视频 = [session.TaskID]；KnowledgeBase = 集合内 video_ids。
	TaskIDs []int64
	// EmbeddingModel 是本次检索用的 embedding 模型名（spec 07 重检索复用）。
	EmbeddingModel string
	// EmbeddingClient / ChatClient 供 spec 07 证据约束重检索复用（reretrieveEvidence
	// 走完整 Retrieve 链路需要 embedding 做 query 向量 + 可能的 query rewrite）。
	// 生产路径注入真实 client；测试路径用 fake re-retriever 跳过本字段。
	EmbeddingClient ai.EmbeddingClient
	ChatClient      ai.ChatClient
	// Policy 是本次问答的 ExecutionPolicy（spec 04）。spec 06 降级在其之上：
	// policy.UseLLM=false 的 intent（small_talk）不触发档2（本来就不调 LLM）。
	Policy ExecutionPolicy
}

// evidenceIDSet 返回本次检索集的 evidence id 范围（spec 07 证据约束校验用）。
// 复用 Contexts（检索召回的原始片段，含 EvidenceID）；Citations 是其公开子集，
// 证据范围以 Contexts 为准——⑨ 校验的是 LLM 引用是否在"本次检索集"内，Contexts
// 就是本次检索集的事实表示。
func (p *preparedRAGChat) evidenceIDSet() map[string]struct{} {
	set := make(map[string]struct{})
	if p == nil {
		return set
	}
	for _, c := range p.Contexts {
		if id := strings.TrimSpace(c.EvidenceID); id != "" {
			set[id] = struct{}{}
		}
	}
	return set
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

// SetIntentRouter 注入 spec 05 级联 intent 分类器（nil 时 chat_prepare 降级占位
// classifyIntentPlaceholder，保测试稳定）。生产路径由 wiring 注入；测试可不调。
func (s *ChatService) SetIntentRouter(r *IntentRouter) {
	s.intentRouter = r
}
