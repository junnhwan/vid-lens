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

const VideoAgentLoopTemplate VideoAgentTemplate = "agent"

type VideoAgentLoopRequest struct {
	UserID     int64
	SessionID  int64
	Goal       string
	TopK       int
	Policy     VideoAgentLoopPolicy
	RunID      string
	Observer   VideoAgentStepObserver
	EmitAnswer func(string) error
}

// RunAgent is the single owner-scoped Planner/Tool/Observe execution path.
func (s *VideoAgentService) RunAgent(ctx context.Context, req VideoAgentLoopRequest, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (result *VideoAgentResult, err error) {
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Goal == "" {
		return nil, fmt.Errorf("问题不能为空")
	}
	if s == nil || s.chatSvc == nil || s.executionJournal == nil {
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
		return nil, errors.New("知识库会话暂不支持 Agent")
	}
	memoryPolicy := s.chatSvc.effectiveMemoryPolicyForRequest(ctx, session)
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
		if existing.SessionID != req.SessionID || existing.TaskID != session.TaskID || existing.Goal != req.Goal {
			return nil, errors.New("agent run scope or goal mismatch")
		}
		// A run is an immutable execution contract. Do not validate or use a
		// newly supplied policy before the historical snapshots are loaded.
		if err := json.Unmarshal([]byte(existing.PolicySnapshot), &frozenPolicy); err != nil {
			return nil, fmt.Errorf("decode frozen agent policy: %w", err)
		}
		if err := json.Unmarshal([]byte(existing.BudgetSnapshot), &budget); err != nil {
			return nil, fmt.Errorf("decode frozen agent budget: %w", err)
		}
		if existing.Mode != AgentStreamMode || frozenPolicy.EngineVersion != 2 {
			if existing.Status == model.AgentRunStatusCompleted {
				return loadAgentRunResult(ctx, s, req.UserID, req.SessionID, runID)
			}
			return nil, errors.New("旧执行模式不能继续运行，请重新提问")
		}
		policy = VideoAgentLoopPolicy{MaxSteps: frozenPolicy.MaxSteps, MaxReplans: frozenPolicy.MaxReplans}
	} else {
		if policy == (VideoAgentLoopPolicy{}) {
			policy = DefaultVideoAgentLoopPolicy()
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		frozenPolicy, budget = loopAgentPolicyWithVisual(req.TopK, policy, s.visualInvestigator != nil)
	}
	run, err := s.ensureAgentRun(ctx, runID, req.UserID, session, req.Goal, string(VideoAgentLoopTemplate), "default", profile, frozenPolicy, budget)
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
	policy = VideoAgentLoopPolicy{MaxSteps: frozenPolicy.MaxSteps, MaxReplans: frozenPolicy.MaxReplans}
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

	if budget.MaxDurationMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(budget.MaxDurationMs)*time.Millisecond)
		defer cancel()
	}
	recentLimit := s.chatSvc.cfg.RecentTurns * 2
	recent, err := s.chatSvc.loadRecentMessages(ctx, req.UserID, req.SessionID, recentLimit)
	if err != nil {
		return nil, err
	}
	memorySnapshot := s.loadAgentMemorySnapshot(ctx, req.UserID, session.TaskID, runID, req.Goal, memoryPolicy)
	embedding, chat = s.chatSvc.observedAIClients(req.UserID, req.SessionID, session.TaskID, embedding, chat, profile)
	pipeline := s.chatSvc.newRetrievalPipeline(req.TopK, chat, profile)
	// The Planner supplies the search query; do not hide another LLM call inside a tool.
	pipeline.rewriter = NoopQueryRewriter{}
	tools := NewVideoAgentTools(s.chatSvc.repos, pipeline, chat)
	tools.SetVisualInvestigator(s.visualInvestigator)
	tools.SetMemorySnapshot(memorySnapshot)
	tools.SetStepObserver(req.Observer)
	tools.emitAnswer = req.EmitAnswer
	var progress []ConversationProgress
	parentProgress := progressContext(ctx)
	progressSink := parentProgress.emit
	parentProgress.emit = func(p ConversationProgress) error {
		p.RunID = runID
		if p.Status != "running" {
			progress = append(progress, p)
		}
		if progressSink != nil {
			return progressSink(p)
		}
		return nil
	}
	ctx = context.WithValue(ctx, conversationProgressKey{}, parentProgress)
	runner, err := NewVideoAgentLoopRunner(tools.Registry(), NewLLMVideoAgentLoopPlanner(chat), DefaultVideoAgentLoopObserver{}, policy)
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
	trace := videoAgentLoopTrace(runResult)
	if err != nil {
		return nil, newVideoAgentExecutionError(err, trace)
	}
	// Recovered plans are not re-emitted as fresh work, but their public
	// summaries must remain available in the final history snapshot.
	for _, step := range runResult.State.Steps {
		id := fmt.Sprintf("plan-%d", step.Number)
		found := false
		for _, p := range progress {
			if p.ID == id {
				found = true
				break
			}
		}
		if !found {
			progress = append(progress, ConversationProgress{ID: id, PlanID: id, RunID: runID, Kind: "plan", Label: "规划下一步", Status: "done", Detail: publicDecisionSummary(step.Action, 0)})
		}
	}
	degraded := runResult.State.StopReason == "budget_exhausted"
	if degraded && strings.TrimSpace(runResult.State.Answer) == "" {
		fallback := agentBudgetAnswer(req.Goal, runResult.State.Evidence)
		runResult.State.Answer, runResult.State.Citations = fallback.Answer, fallback.Citations
	}
	if strings.TrimSpace(runResult.State.Answer) == "" {
		return nil, newVideoAgentExecutionError(errors.New("Agent 未生成最终回答"), trace)
	}

	result = &VideoAgentResult{
		Progress:     progress,
		Degraded:     degraded,
		Answer:       runResult.State.Answer,
		Template:     string(VideoAgentLoopTemplate),
		Citations:    append([]Citation(nil), runResult.State.Citations...),
		Trace:        trace,
		Model:        profile.LLMModel,
		RunID:        runID,
		Mode:         string(VideoAgentLoopTemplate),
		Memory:       memorySnapshot.Identity(),
		MemoryPolicy: memoryPolicy,
	}
	if err := emitProgress(ctx, ConversationProgress{ID: "save", Kind: "save", Label: "保存回答与引用", Status: "running"}); err != nil {
		return nil, err
	}
	if err := s.saveAgentRunExchange(ctx, req.UserID, req.SessionID, req.Goal, result, recentLimit); err != nil {
		return nil, err
	}
	if err := emitProgress(ctx, ConversationProgress{ID: "save", Kind: "save", Label: "保存回答与引用", Status: "done"}); err != nil {
		return nil, err
	}
	status := model.AgentRunStatusCompleted
	if degraded {
		status = model.AgentRunStatusBudgetExhausted
	}
	s.markAgentRunTerminal(ctx, req.UserID, runID, status, firstNonEmpty(runResult.State.StopReason, "goal_satisfied"), nil)
	return result, nil
}

// ResumeAgent reconstructs a running research loop exclusively from the
// authoritative Run/Step/ToolCall records. retrieval_snapshot is never read.
func (s *VideoAgentService) ResumeAgent(ctx context.Context, userID int64, runID string, embedding ai.EmbeddingClient, chat ai.ChatClient, profile ai.Profile) (*VideoAgentResult, error) {
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
	if run.Mode != string(VideoAgentLoopTemplate) || run.ScopeType != model.ChatScopeVideo {
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
	return s.RunAgent(ctx, VideoAgentLoopRequest{
		UserID: userID, SessionID: run.SessionID, Goal: run.Goal, TopK: policy.TopK,
		Policy: VideoAgentLoopPolicy{MaxSteps: policy.MaxSteps, MaxReplans: policy.MaxReplans}, RunID: run.ID,
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
		if decodeErr != nil || snapshot.RunID != runID {
			continue
		}
		return &VideoAgentResult{Degraded: snapshot.Degraded, Answer: message.Content, Template: snapshot.Template, Citations: append([]Citation(nil), snapshot.Citations...), Trace: append([]VideoAgentStep(nil), snapshot.Trace...), Model: message.ModelName, MessageID: message.ID, RunID: snapshot.RunID, Mode: snapshot.Mode, Memory: snapshot.Memory, MemoryPolicy: snapshot.MemoryPolicy}, nil
	}
	return nil, nil
}

func videoAgentLoopTrace(result *VideoAgentLoopResult) []VideoAgentStep {
	if result == nil {
		return nil
	}
	trace := make([]VideoAgentStep, 0, len(result.State.Steps))
	for _, step := range result.State.Steps {
		trace = append(trace, step.Trace)
	}
	return trace
}

// Budget exhaustion must not discard evidence already obtained. No additional model call.
func agentBudgetAnswer(goal string, evidence []RetrievedChunk) finalizedAnswer {
	if len(evidence) > 3 {
		evidence = evidence[:3]
	}
	citations := buildCitations(goal, evidence)
	text := "本轮分析预算已用尽，未完成进一步核对。"
	if len(citations) == 0 {
		return finalizedAnswer{Answer: text + "当前没有足够证据确认视频中的答案。", Citations: []Citation{}}
	}
	text += "以下是已取得的视频证据摘录，不代表完整结论：\n"
	for i, c := range citations {
		text += fmt.Sprintf("\n%d. %s [C%d]\n", i+1, c.Content, i+1)
	}
	return finalizeAnswerCitations(text, citations)
}
