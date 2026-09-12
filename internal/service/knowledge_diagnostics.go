package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"vid-lens/internal/model"
)

type KnowledgeRetrievalRequest struct {
	Question string `json:"question"`
	Mode     string `json:"mode"`
	TopK     int    `json:"top_k"`
}

type KnowledgeRetrievalResult struct {
	TaskIDs   []int64        `json:"task_ids"`
	Mode      string         `json:"mode"`
	Trace     RetrievalTrace `json:"trace"`
	Citations []Citation     `json:"citations"`
}

func (e *ConversationExecution) TestKnowledgeRetrieval(ctx context.Context, userID, kbID int64, req KnowledgeRetrievalRequest) (*KnowledgeRetrievalResult, error) {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" || len([]rune(req.Question)) > 1000 || kbID <= 0 || req.TopK < 0 || req.TopK > 10 {
		return nil, errors.New("问题、知识库或返回数量无效")
	}
	if req.Mode == "" {
		req.Mode = "hybrid"
	}
	if req.Mode != "hybrid" && req.Mode != "vector" && req.Mode != "keyword" {
		return nil, errors.New("检索模式无效")
	}
	s, ok := e.chat.(*ChatService)
	if !ok {
		return nil, errors.New("检索服务不可用")
	}
	// Authorize the resource before creating provider clients.
	kb, err := s.repos.KnowledgeBase.FindByIDForUser(userID, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, errors.New("知识库不存在或无权限")
	}
	embedding, chat, profile, err := e.prepareClients(userID)
	if err != nil {
		return nil, err
	}
	session := &model.ChatSession{UserID: userID, ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: kbID}
	ids, err := s.sessionRetrievalTaskIDs(userID, session, profile.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	if req.TopK == 0 {
		req.TopK = 5
	}
	embedding, chat = s.observedAIClients(userID, 0, 0, embedding, chat, profile)
	p := s.newRetrievalPipeline(req.TopK, chat, profile)
	cfg := DefaultRAGRetrievalConfig()
	if p.Config != nil {
		cfg = *p.Config
	}
	cfg.TopK = req.TopK
	cfg.EnableVector, cfg.EnableBM25 = req.Mode != "keyword", req.Mode != "vector"
	cfg.QueryMode, cfg.RewriteQueries = QueryModeOriginal, 1
	p.Config, p.rewriter = &cfg, NoopQueryRewriter{}
	result, err := p.Retrieve(ctx, RetrievalPipelineRequest{UserID: userID, TaskIDs: ids, Question: req.Question, TopK: req.TopK, EmbeddingModel: profile.EmbeddingModel, Embedding: embedding, Debug: true})
	if err != nil {
		return nil, err
	}
	current, err := s.sessionRetrievalTaskIDs(userID, session, profile.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	if !sameTaskIDs(ids, current) {
		return nil, errKnowledgeMembershipChanged
	}
	return &KnowledgeRetrievalResult{TaskIDs: ids, Mode: req.Mode, Trace: result.Trace, Citations: buildCitations(req.Question, result.Citations)}, nil
}

type AgentRunDetail struct {
	ID               string                 `json:"id"`
	Status           string                 `json:"status"`
	StopReason       string                 `json:"stop_reason"`
	CreatedAt        time.Time              `json:"created_at"`
	FinishedAt       *time.Time             `json:"finished_at,omitempty"`
	DurationMS       int64                  `json:"duration_ms"`
	ToolsUsed        int                    `json:"tools_used"`
	ToolsLimit       int                    `json:"tools_limit"`
	ModelCalls       int                    `json:"model_calls"`
	RetrievalCalls   int                    `json:"retrieval_calls"`
	PromptTokens     int64                  `json:"prompt_tokens"`
	CompletionTokens int64                  `json:"completion_tokens"`
	TokenSource      string                 `json:"token_source"`
	Steps            []ConversationProgress `json:"steps"`
}

func (s *ChatService) GetAgentRunDetail(ctx context.Context, userID, sessionID int64, runID string) (*AgentRunDetail, error) {
	session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("会话不存在或无权限")
	}
	records, err := s.repos.AgentExecution.GetExecution(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if records == nil || records.Run.SessionID != sessionID {
		return nil, errors.New("运行不存在或无权限")
	}
	run := records.Run
	d := &AgentRunDetail{ID: run.ID, Status: run.Status, StopReason: run.StopReason, CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt, DurationMS: run.DurationMsUsed, ToolsUsed: run.ToolCallsUsed, ToolsLimit: run.MaxToolCalls, ModelCalls: run.LLMCallsUsed, RetrievalCalls: run.RetrievalCallsUsed, PromptTokens: run.PromptTokensUsed, CompletionTokens: run.CompletionTokensUsed, TokenSource: run.TokenUsageSource, Steps: []ConversationProgress{}}
	for _, step := range records.Steps {
		status := "running"
		if step.Status == model.AgentStepStatusCompleted {
			status = "done"
		} else if step.Status != model.AgentStepStatusRunning {
			status = "error"
		}
		label := agentSnapshotStepLabel(VideoAgentStep{Tool: step.Action})
		if step.Kind == "plan" {
			label = "规划下一步"
		}
		d.Steps = append(d.Steps, ConversationProgress{ID: step.ID, RunID: run.ID, Kind: step.Kind, Label: label, Status: status, Tool: step.Action, DurationMs: step.DurationMs})
	}
	return d, nil
}
