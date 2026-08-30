package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
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
	RunID     string
}

// AskResearch runs the opt-in goal-driven path. The existing Ask method stays
// as the deterministic template baseline for comparison and fallback.
func (s *VideoAgentService) AskResearch(ctx context.Context, req VideoResearchRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (result *VideoAgentResult, err error) {
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
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = uuid.NewString()
	}
	policy := req.Policy
	var frozenPolicy frozenAgentPolicy
	var budget frozenAgentBudget
	journal := s.executionJournal
	if existing, lookupErr := journal.GetRun(ctx, req.UserID, runID); lookupErr != nil {
		return nil, lookupErr
	} else if existing != nil {
		// A run is an immutable execution contract. Do not validate or use a
		// newly supplied policy before the historical snapshots are loaded.
		if err := json.Unmarshal([]byte(existing.PolicySnapshot), &frozenPolicy); err != nil {
			return nil, fmt.Errorf("decode frozen agent policy: %w", err)
		}
		if err := json.Unmarshal([]byte(existing.BudgetSnapshot), &budget); err != nil {
			return nil, fmt.Errorf("decode frozen agent budget: %w", err)
		}
		policy = VideoResearchPolicy{MaxSteps: frozenPolicy.MaxSteps, MaxReplans: frozenPolicy.MaxReplans}
	} else {
		if policy == (VideoResearchPolicy{}) {
			policy = DefaultVideoResearchPolicy()
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		frozenPolicy, budget = researchAgentPolicy(req.TopK, policy)
	}
	run, err := s.ensureAgentRun(ctx, runID, req.UserID, session, req.Goal, string(VideoAgentResearchTemplate), "default", profile, frozenPolicy, budget)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(run.PolicySnapshot), &frozenPolicy); err != nil {
		return nil, fmt.Errorf("decode frozen agent policy: %w", err)
	}
	if err := json.Unmarshal([]byte(run.BudgetSnapshot), &budget); err != nil {
		return nil, fmt.Errorf("decode frozen agent budget: %w", err)
	}
	req.TopK = frozenPolicy.TopK
	policy = VideoResearchPolicy{MaxSteps: frozenPolicy.MaxSteps, MaxReplans: frozenPolicy.MaxReplans}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("frozen research policy is invalid: %w", err)
	}
	if run.Status != model.AgentRunStatusRunning && run.Status != model.AgentRunStatusPending {
		stored, storedErr := loadAgentRunResult(ctx, s, req.UserID, req.SessionID, runID)
		if storedErr != nil {
			return nil, storedErr
		}
		if stored != nil {
			return stored, nil
		}
		return nil, fmt.Errorf("agent run is terminal: %s", run.Status)
	}
	defer func() {
		if err == nil || errors.Is(err, errAgentExecutionBusy) {
			return
		}
		status, reason := model.AgentRunStatusFailed, "execution_failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status, reason = model.AgentRunStatusCancelled, "request_cancelled"
		}
		s.markAgentRunTerminal(ctx, req.UserID, runID, status, reason, err)
	}()

	recentLimit := s.chatSvc.cfg.RecentTurns * 2
	recent, err := s.chatSvc.loadRecentMessages(ctx, req.UserID, req.SessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	memorySnapshot := s.loadAgentMemorySnapshot(ctx, req.UserID, session.TaskID, runID, req.Goal)
	embedding, chat = s.chatSvc.observedAIClients(req.UserID, req.SessionID, session.TaskID, embedding, chat, profile)
	tools := NewVideoAgentTools(s.chatSvc.repos, s.chatSvc.newRetrievalPipeline(req.TopK, chat, profile), chat)
	tools.SetMemorySnapshot(memorySnapshot)
	runner, err := NewVideoResearchRunner(tools.Registry(), NewLLMVideoResearchPlanner(chat), DefaultVideoResearchObserver{}, policy)
	if err != nil {
		return nil, err
	}
	if err := runner.SetDurableExecution(journal, req.UserID, runID); err != nil {
		return nil, err
	}
	runResult, err := runner.Run(ctx, req.Goal, VideoAgentToolRuntime{
		UserID:         req.UserID,
		TaskID:         session.TaskID,
		Recent:         recent,
		TopK:           req.TopK,
		EmbeddingModel: profile.EmbeddingModel,
		Embedding:      embedding,
		MemorySnapshot: memorySnapshot,
	})
	trace := videoResearchTrace(runResult)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, trace)
	}
	if runResult.State.StopReason == "budget_exhausted" {
		s.markAgentRunTerminal(ctx, req.UserID, runID, model.AgentRunStatusBudgetExhausted, "budget_exhausted", nil)
	}
	if strings.TrimSpace(runResult.State.Answer) == "" {
		return nil, newVideoAgentExecutionError(errors.New("研究任务未生成最终回答"), trace)
	}

	result = &VideoAgentResult{
		Answer:    runResult.State.Answer,
		Template:  string(VideoAgentResearchTemplate),
		Citations: append([]Citation(nil), runResult.State.Citations...),
		Trace:     trace,
		Model:     profile.LLMModel,
		RunID:     runID,
		Mode:      string(VideoAgentResearchTemplate),
		Memory:    memorySnapshot.Identity(),
	}
	if err := s.saveAgentRunExchange(ctx, req.UserID, req.SessionID, req.Goal, result, recentLimit); err != nil {
		return nil, err
	}
	rawAnswer, answerEvidence := researchAnswerLedgerInput(req.Goal, runResult)
	s.persistEvidenceLedger(ctx, EvidenceLedgerRecordRequest{
		UserID: req.UserID, SessionID: req.SessionID, MessageID: result.MessageID, TaskID: session.TaskID,
		RunID: runID, RawAnswer: rawAnswer, Evidence: answerEvidence, Retrieved: buildCitations(req.Goal, runResult.State.Evidence),
	})
	s.markAgentRunTerminal(ctx, req.UserID, runID, model.AgentRunStatusCompleted, firstNonEmpty(runResult.State.StopReason, "goal_satisfied"), nil)
	return result, nil
}

// ResumeResearch reconstructs a running research loop exclusively from the
// authoritative Run/Step/ToolCall records. retrieval_snapshot is never read.
func (s *VideoAgentService) ResumeResearch(ctx context.Context, userID int64, runID string, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*VideoAgentResult, error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.AgentExecution == nil {
		return nil, errors.New("agent execution repository unavailable")
	}
	journal := s.executionJournal
	run, err := journal.GetRun(ctx, userID, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, errors.New("agent run not found")
	}
	if run.Mode != string(VideoAgentResearchTemplate) || run.ScopeType != model.ChatScopeVideo {
		return nil, errors.New("agent run is not a resumable single-video research run")
	}
	var policy frozenAgentPolicy
	if err := json.Unmarshal([]byte(run.PolicySnapshot), &policy); err != nil {
		return nil, fmt.Errorf("decode frozen agent policy: %w", err)
	}
	if run.Status != model.AgentRunStatusRunning && run.Status != model.AgentRunStatusPending {
		stored, storedErr := loadAgentRunResult(ctx, s, userID, run.SessionID, run.ID)
		if storedErr != nil {
			return nil, storedErr
		}
		if stored != nil {
			return stored, nil
		}
		return nil, fmt.Errorf("agent run is terminal: %s", run.Status)
	}
	return s.AskResearch(ctx, VideoResearchRequest{
		UserID: userID, SessionID: run.SessionID, Goal: run.Goal, TopK: policy.TopK,
		Policy: VideoResearchPolicy{MaxSteps: policy.MaxSteps, MaxReplans: policy.MaxReplans}, RunID: run.ID,
	}, embedding, chat, profile)
}

func loadAgentRunResult(ctx context.Context, s *VideoAgentService, userID, sessionID int64, runID string) (*VideoAgentResult, error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.Chat == nil {
		return nil, errors.New("chat repository unavailable")
	}
	messages, err := s.chatSvc.repos.Chat.ListMessages(userID, sessionID)
	if err != nil {
		return nil, err
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "assistant" || message.RetrievalSnapshot == nil {
			continue
		}
		snapshot, decodeErr := DecodeAgentSnapshot(*message.RetrievalSnapshot)
		if decodeErr != nil || snapshot.RunID != runID || snapshot.Mode != string(VideoAgentResearchTemplate) {
			continue
		}
		return &VideoAgentResult{Answer: message.Content, Template: snapshot.Template, Citations: append([]Citation(nil), snapshot.Citations...), Trace: append([]VideoAgentStep(nil), snapshot.Trace...), Model: message.ModelName, MessageID: message.ID, RunID: snapshot.RunID, Mode: snapshot.Mode, Memory: snapshot.Memory}, nil
	}
	return nil, nil
}

func researchAnswerLedgerInput(goal string, result *VideoResearchResult) (string, []Citation) {
	if result == nil {
		return "", nil
	}
	for i := len(result.State.Observations) - 1; i >= 0; i-- {
		observation := result.State.Observations[i]
		if observation.Tool != VideoAgentToolBuildCitedAnswer {
			continue
		}
		var answer BuildCitedAnswerResult
		if err := json.Unmarshal(observation.Output, &answer); err == nil && strings.TrimSpace(answer.Answer) != "" {
			return answer.Answer, buildCitations(goal, answer.Citations)
		}
	}
	return result.State.Answer, append([]Citation(nil), result.State.Citations...)
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
