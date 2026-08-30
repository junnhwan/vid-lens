package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

const evidenceFunnelPendingAnswer = "证据校验尚未完成，当前回答不可用。"

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
	run, err := s.ensureAgentRun(ctx, runID, req.UserID, session, req.Goal, string(VideoAgentEvidenceFunnelTemplate), "bounded-evidence-funnel", profile, frozenPolicy, budget)
	if err != nil {
		return nil, err
	}
	if run.Status == model.AgentRunStatusCompleted {
		return completedEvidenceFunnelResult(s, req.UserID, req.SessionID, runID)
	}
	policy, err = evidenceFunnelPolicyFromRun(run)
	if err != nil {
		return nil, err
	}
	validationCompleted := false
	defer func() {
		var replayableFailure *evidenceFunnelReplayableFailure
		if err == nil || validationCompleted || errors.Is(err, errAgentExecutionBusy) || errors.As(err, &replayableFailure) {
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
	createdPending, pendingUserMessageID := false, int64(0)
	if existingMessage != nil {
		result.MessageID = existingMessage.ID
	} else {
		createdPending, pendingUserMessageID, err = s.saveEvidenceFunnelPendingExchange(req.UserID, req.SessionID, req.Goal, result)
		if err != nil {
			return nil, err
		}
	}
	ledgerRequest := EvidenceLedgerRecordRequest{
		UserID: req.UserID, SessionID: req.SessionID, MessageID: result.MessageID, TaskID: session.TaskID, RunID: runID,
		RawAnswer: funnel.RawAnswer, Evidence: funnel.Citations, Retrieved: buildCitations(req.Goal, funnel.Evidence),
	}
	if err := runner.ValidateAndRecord(ctx, ledgerRequest, sortedFinalEvidenceRefs(funnel.Citations)); err != nil {
		return nil, fmt.Errorf("evidence funnel validation failed: %w", err)
	}
	validationCompleted = true
	result.Trace = evidenceFunnelTrace(ctx, s, req.UserID, runID)
	if snapshot, snapshotErr := MarshalAgentSnapshot(result); snapshotErr != nil {
		return nil, snapshotErr
	} else if updated, updateErr := s.publishEvidenceFunnelResult(req.UserID, req.SessionID, result.MessageID, result.Answer, string(snapshot), result.Model); updateErr != nil || !updated {
		if updateErr == nil {
			updateErr = errors.New("assistant message disappeared before validated funnel answer publish")
		}
		return nil, updateErr
	}
	_ = s.chatSvc.refreshRecentMemory(ctx, req.UserID, req.SessionID, recentLimit)
	if createdPending && pendingUserMessageID > 0 && s.chatSvc.memoryCapture != nil {
		_ = s.chatSvc.memoryCapture.EnqueueExtraction(MemoryExtractionRequest{
			UserID: req.UserID, UserText: req.Goal, SourceRef: fmt.Sprintf("chat_message:%d", pendingUserMessageID),
		})
	}
	s.markAgentRunTerminal(ctx, req.UserID, runID, model.AgentRunStatusCompleted, "evidence_validated", nil)
	return result, nil
}

func evidenceFunnelPolicyFromRun(run *model.AgentRun) (EvidenceFunnelPolicy, error) {
	if run == nil {
		return EvidenceFunnelPolicy{}, errors.New("agent run unavailable")
	}
	var frozen frozenAgentPolicy
	if err := json.Unmarshal([]byte(run.PolicySnapshot), &frozen); err != nil {
		return EvidenceFunnelPolicy{}, fmt.Errorf("decode frozen evidence funnel policy: %w", err)
	}
	if len(frozen.AllowedTools) != len(evidenceFunnelActionOrder) {
		return EvidenceFunnelPolicy{}, errors.New("frozen evidence funnel action set is unsupported")
	}
	for index, action := range evidenceFunnelActionOrder {
		if frozen.AllowedTools[index] != action {
			return EvidenceFunnelPolicy{}, errors.New("frozen evidence funnel action order is unsupported")
		}
	}
	policy := EvidenceFunnelPolicy{
		TopK: frozen.TopK, MaxWindowSelections: frozen.MaxWindowSelections, WindowRadius: frozen.WindowRadius,
		MaxVisualCandidates: frozen.MaxVisualCandidates, MaxVisualSelections: frozen.MaxVisualSelections,
		MaxFinalEvidenceItems: frozen.MaxFinalEvidenceItems,
	}
	if policy.TopK <= 0 || policy.MaxWindowSelections <= 0 || policy.WindowRadius < 0 || policy.MaxVisualCandidates <= 0 || policy.MaxVisualSelections <= 0 || policy.MaxFinalEvidenceItems <= 0 {
		return EvidenceFunnelPolicy{}, errors.New("frozen evidence funnel policy is invalid")
	}
	return policy, nil
}

func (s *VideoAgentService) publishEvidenceFunnelResult(userID, sessionID, messageID int64, content, snapshot, modelName string) (bool, error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.Chat == nil {
		return false, errors.New("chat repository unavailable")
	}
	if s.evidenceFunnelResultPublisher != nil {
		return s.evidenceFunnelResultPublisher(userID, sessionID, messageID, content, snapshot, modelName)
	}
	return s.chatSvc.repos.Chat.UpdateAssistantResult(userID, sessionID, messageID, content, snapshot, modelName)
}

func (s *VideoAgentService) saveEvidenceFunnelPendingExchange(userID, sessionID int64, question string, result *VideoAgentResult) (bool, int64, error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.Chat == nil || result == nil {
		return false, 0, errors.New("chat repository unavailable")
	}
	pending := &VideoAgentResult{
		Answer: evidenceFunnelPendingAnswer, Template: result.Template, Trace: result.Trace,
		Model: result.Model, RunID: result.RunID, Mode: result.Mode, Citations: []Citation{},
	}
	snapshot, err := MarshalAgentSnapshot(pending)
	if err != nil {
		return false, 0, err
	}
	snapshotText := string(snapshot)
	userMessage := &model.ChatMessage{SessionID: sessionID, UserID: userID, Role: "user", Content: question}
	assistantMessage := &model.ChatMessage{
		SessionID: sessionID, UserID: userID, Role: "assistant", Content: evidenceFunnelPendingAnswer,
		RetrievalSnapshot: &snapshotText, ModelName: result.Model,
	}
	created, userMessageID, assistantMessageID, err := s.chatSvc.repos.Chat.CreateAgentRunExchange(userID, result.RunID, userMessage, assistantMessage, nil)
	if err != nil {
		return false, 0, err
	}
	result.MessageID = assistantMessageID
	if created {
		if session, findErr := s.chatSvc.repos.Chat.FindSessionForUser(userID, sessionID); findErr == nil && session != nil {
			s.chatSvc.maybeAutoTitleSession(session, question)
		}
	}
	return created, userMessageID, nil
}

func completedEvidenceFunnelResult(service *VideoAgentService, userID, sessionID int64, runID string) (*VideoAgentResult, error) {
	message, err := findAgentRunMessage(service, userID, sessionID, runID)
	if err != nil {
		return nil, err
	}
	if message == nil || message.RetrievalSnapshot == nil || message.Content == evidenceFunnelPendingAnswer {
		return nil, errors.New("completed evidence funnel answer is unavailable")
	}
	snapshot, err := DecodeAgentSnapshot(*message.RetrievalSnapshot)
	if err != nil {
		return nil, err
	}
	return &VideoAgentResult{
		Answer: message.Content, Template: snapshot.Template, Citations: snapshot.Citations, Trace: snapshot.Trace,
		Model: message.ModelName, MessageID: message.ID, RunID: snapshot.RunID, Mode: snapshot.Mode, Memory: snapshot.Memory,
	}, nil
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
