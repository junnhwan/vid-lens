package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// AskEvidenceFunnel runs the explicit, single-video, bounded evidence funnel.
// It does not alter the default RAG or the existing research loop.
func (s *VideoAgentService) AskEvidenceFunnel(ctx context.Context, req EvidenceFunnelRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (result *VideoAgentResult, err error) {
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Goal == "" {
		return nil, errors.New("证据漏斗目标不能为空")
	}
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.AgentExecution == nil {
		return nil, errors.New("agent execution repository unavailable")
	}
	if s.chatSvc.retriever == nil {
		return nil, errors.New("当前视频尚未构建 RAG 索引")
	}
	session, err := s.findVideoAgentSession(req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	policy := defaultEvidenceFunnelPolicy(req.TopK)
	frozenPolicy, budget := evidenceFunnelAgentPolicy(policy)
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = uuid.NewString()
	}
	if _, err = s.ensureAgentRun(ctx, runID, req.UserID, session, req.Goal, string(VideoAgentEvidenceFunnelTemplate), "bounded-evidence-funnel", profile, frozenPolicy, budget); err != nil {
		return nil, err
	}
	defer func() {
		if err == nil || errors.Is(err, errAgentExecutionBusy) {
			return
		}
		status, reason := model.AgentRunStatusFailed, "evidence_funnel_failed"
		if strings.Contains(err.Error(), "budget exhausted") {
			status, reason = model.AgentRunStatusBudgetExhausted, "budget_exhausted"
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, reason = model.AgentRunStatusCancelled, "request_cancelled"
		}
		s.markAgentRunTerminal(ctx, req.UserID, runID, status, reason, err)
	}()

	recentLimit := s.chatSvc.cfg.RecentTurns * 2
	embedding, chat = s.chatSvc.observedAIClients(req.UserID, req.SessionID, session.TaskID, embedding, chat, profile)
	pipeline := NewRetrievalPipeline(s.chatSvc.repos, s.chatSvc.retriever, NoopQueryRewriter{}, nil, DeterministicReranker{}, s.chatSvc.candidateK(policy.TopK), s.chatSvc.cfg.MinScore)
	tools := NewVideoAgentTools(s.chatSvc.repos, pipeline, chat)
	runner := &evidenceFunnelRunner{
		repos: s.chatSvc.repos, tools: tools, planner: NewLLMEvidenceGapPlanner(chat), policy: policy,
		runtime:   VideoAgentToolRuntime{UserID: req.UserID, TaskID: session.TaskID, TopK: policy.TopK, EmbeddingModel: profile.EmbeddingModel, Embedding: embedding},
		execution: &evidenceFunnelExecution{repo: s.chatSvc.repos.AgentExecution, userID: req.UserID, runID: runID, now: func() time.Time { return time.Now().UTC() }},
	}
	funnel, err := runner.Run(ctx, req.Goal)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, evidenceFunnelTrace(ctx, s, req.UserID, runID))
	}
	result = &VideoAgentResult{
		Answer: funnel.Answer, Template: string(VideoAgentEvidenceFunnelTemplate), Citations: funnel.Citations,
		Trace: evidenceFunnelTrace(ctx, s, req.UserID, runID), Model: profile.LLMModel, RunID: runID, Mode: string(VideoAgentEvidenceFunnelTemplate),
	}
	existingMessage, err := findAgentRunMessage(s, req.UserID, req.SessionID, runID)
	if err != nil {
		return nil, err
	}
	if existingMessage != nil {
		result.MessageID = existingMessage.ID
	} else if err := s.saveAgentExchange(ctx, req.UserID, req.SessionID, req.Goal, result, recentLimit); err != nil {
		return nil, err
	}
	ledgerRequest := EvidenceLedgerRecordRequest{
		UserID: req.UserID, SessionID: req.SessionID, MessageID: result.MessageID, TaskID: session.TaskID, RunID: runID,
		RawAnswer: funnel.RawAnswer, Evidence: funnel.Citations, Retrieved: buildCitations(req.Goal, funnel.Evidence),
	}
	if err := runner.ValidateAndRecord(ctx, ledgerRequest, sortedFinalEvidenceRefs(funnel.Citations)); err != nil {
		return nil, fmt.Errorf("evidence funnel validation failed: %w", err)
	}
	result.Trace = evidenceFunnelTrace(ctx, s, req.UserID, runID)
	if snapshot, snapshotErr := MarshalAgentSnapshot(result); snapshotErr != nil {
		return nil, snapshotErr
	} else if updated, updateErr := s.chatSvc.repos.Chat.UpdateAssistantSnapshot(req.UserID, req.SessionID, result.MessageID, string(snapshot)); updateErr != nil || !updated {
		if updateErr == nil {
			updateErr = errors.New("assistant message disappeared before funnel snapshot update")
		}
		return nil, updateErr
	}
	s.markAgentRunTerminal(ctx, req.UserID, runID, model.AgentRunStatusCompleted, "evidence_validated", nil)
	return result, nil
}

func findAgentRunMessage(service *VideoAgentService, userID, sessionID int64, runID string) (*model.ChatMessage, error) {
	if service == nil || service.chatSvc == nil || service.chatSvc.repos == nil || service.chatSvc.repos.Chat == nil {
		return nil, errors.New("chat repository unavailable")
	}
	messages, err := service.chatSvc.repos.Chat.ListMessages(userID, sessionID)
	if err != nil {
		return nil, err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := &messages[index]
		if message.Role != "assistant" || message.RetrievalSnapshot == nil {
			continue
		}
		snapshot, err := DecodeAgentSnapshot(*message.RetrievalSnapshot)
		if err == nil && snapshot.RunID == runID {
			return message, nil
		}
	}
	return nil, nil
}

func evidenceFunnelTrace(ctx context.Context, service *VideoAgentService, userID int64, runID string) []VideoAgentStep {
	if service == nil || service.chatSvc == nil || service.chatSvc.repos == nil || service.chatSvc.repos.AgentExecution == nil {
		return nil
	}
	records, err := service.chatSvc.repos.AgentExecution.GetExecution(ctx, userID, runID)
	if err != nil || records == nil {
		return nil
	}
	trace := make([]VideoAgentStep, 0, len(records.ToolCalls))
	for _, call := range records.ToolCalls {
		step := VideoAgentStep{Name: call.ToolName, Tool: call.ToolName, OutputRef: call.OutputRef}
		if call.Status == model.AgentToolCallStatusFailed || call.Status == model.AgentToolCallStatusAmbiguous {
			step.Error = call.ErrorMessage
		}
		trace = append(trace, step)
	}
	return trace
}
