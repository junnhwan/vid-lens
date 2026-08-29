package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const VideoAgentResearchTemplate VideoAgentTemplate = "research"

type VideoResearchRequest struct {
	UserID    int64
	SessionID int64
	Goal      string
	TopK      int
	Policy    VideoResearchPolicy
}

// AskResearch runs the opt-in goal-driven path. The existing Ask method stays
// as the deterministic template baseline for comparison and fallback.
func (s *VideoAgentService) AskResearch(ctx context.Context, req VideoResearchRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*VideoAgentResult, error) {
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Goal == "" {
		return nil, fmt.Errorf("研究目标不能为空")
	}
	if s == nil || s.chatSvc == nil {
		return nil, errors.New("agent chat service 不能为空")
	}
	if s.chatSvc.retriever == nil {
		return nil, errors.New("当前视频尚未构建 RAG 索引")
	}
	session, err := s.chatSvc.repos.Chat.FindSessionForUser(req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("无权访问此会话")
	}
	if session.ScopeType == model.ChatScopeKnowledgeBase {
		return nil, errors.New("知识库会话暂不支持 Video Research Agent")
	}
	if req.TopK <= 0 {
		req.TopK = s.chatSvc.cfg.TopK
	}
	if req.TopK > 10 {
		req.TopK = 10
	}
	policy := req.Policy
	if policy == (VideoResearchPolicy{}) {
		policy = DefaultVideoResearchPolicy()
	}

	recentLimit := s.chatSvc.cfg.RecentTurns * 2
	recent, err := s.chatSvc.loadRecentMessages(ctx, req.UserID, req.SessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	embedding, chat = s.chatSvc.observedAIClients(req.UserID, req.SessionID, session.TaskID, embedding, chat, profile)
	tools := NewVideoAgentTools(s.chatSvc.repos, s.chatSvc.newRetrievalPipeline(req.TopK, chat, profile), chat)
	runner, err := NewVideoResearchRunner(tools.Registry(), NewLLMVideoResearchPlanner(chat), DefaultVideoResearchObserver{}, policy)
	if err != nil {
		return nil, err
	}
	runResult, err := runner.Run(ctx, req.Goal, VideoAgentToolRuntime{
		UserID:         req.UserID,
		TaskID:         session.TaskID,
		Recent:         recent,
		TopK:           req.TopK,
		EmbeddingModel: profile.EmbeddingModel,
		Embedding:      embedding,
	})
	trace := videoResearchTrace(runResult)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, trace)
	}
	if strings.TrimSpace(runResult.State.Answer) == "" {
		return nil, newVideoAgentExecutionError(errors.New("研究任务未生成最终回答"), trace)
	}

	result := &VideoAgentResult{
		Answer:    runResult.State.Answer,
		Template:  string(VideoAgentResearchTemplate),
		Citations: append([]Citation(nil), runResult.State.Citations...),
		Trace:     trace,
		Model:     profile.LLMModel,
	}
	if err := s.saveAgentExchange(ctx, req.UserID, req.SessionID, req.Goal, result, recentLimit); err != nil {
		return nil, err
	}
	return result, nil
}

func videoResearchTrace(result *VideoResearchResult) []VideoAgentStep {
	if result == nil {
		return nil
	}
	trace := make([]VideoAgentStep, 0, len(result.State.Steps))
	for _, step := range result.State.Steps {
		trace = append(trace, step.Trace)
	}
	return trace
}
