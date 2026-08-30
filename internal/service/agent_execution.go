package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const agentStepLeaseDuration = 2 * time.Minute

type frozenAgentProfile struct {
	LLMProvider             string `json:"llm_provider"`
	LLMModel                string `json:"llm_model"`
	LLMEndpointDigest       string `json:"llm_endpoint_digest"`
	EmbeddingProvider       string `json:"embedding_provider"`
	EmbeddingModel          string `json:"embedding_model"`
	EmbeddingDim            int    `json:"embedding_dim"`
	EmbeddingEndpointDigest string `json:"embedding_endpoint_digest"`
	VisionProvider          string `json:"vision_provider,omitempty"`
	VisionModel             string `json:"vision_model,omitempty"`
	VisionEndpointDigest    string `json:"vision_endpoint_digest,omitempty"`
}

type frozenAgentPolicy struct {
	TopK                  int      `json:"top_k"`
	MaxSteps              int      `json:"max_steps"`
	MaxReplans            int      `json:"max_replans"`
	AllowedTools          []string `json:"allowed_tools"`
	MaxWindowSelections   int      `json:"max_window_selections,omitempty"`
	WindowRadius          int      `json:"window_radius,omitempty"`
	MaxVisualCandidates   int      `json:"max_visual_candidates,omitempty"`
	MaxVisualSelections   int      `json:"max_visual_selections,omitempty"`
	MaxFinalEvidenceItems int      `json:"max_final_evidence_items,omitempty"`
}

type frozenAgentBudget struct {
	MaxSteps           int `json:"max_steps"`
	MaxToolCalls       int `json:"max_tool_calls"`
	MaxLLMCalls        int `json:"max_llm_calls"`
	MaxVisionCalls     int `json:"max_vision_calls"`
	MaxAttemptsPerStep int `json:"max_attempts_per_step"`
}

func (s *VideoAgentService) ensureAgentRun(ctx context.Context, runID string, userID int64, session *model.ChatSession, goal, mode, agentProfile string, profile ai.Profile, policy frozenAgentPolicy, budget frozenAgentBudget) (*model.AgentRun, error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.AgentExecution == nil {
		return nil, errors.New("agent execution repository unavailable")
	}
	profileJSON, err := json.Marshal(safeAgentProfile(profile))
	if err != nil {
		return nil, err
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	budgetJSON, err := json.Marshal(budget)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(agentProfile) == "" {
		agentProfile = "default"
	}
	run := &model.AgentRun{
		ID: runID, UserID: userID, SessionID: session.ID, ScopeType: firstNonEmpty(session.ScopeType, model.ChatScopeVideo),
		TaskID: session.TaskID, KnowledgeBaseID: session.KnowledgeBaseID, Goal: strings.TrimSpace(goal), Mode: mode,
		AgentProfile: agentProfile, ProfileSnapshot: string(profileJSON), PolicySnapshot: string(policyJSON), BudgetSnapshot: string(budgetJSON),
		Status: model.AgentRunStatusRunning, MaxSteps: budget.MaxSteps, MaxToolCalls: budget.MaxToolCalls,
		MaxLLMCalls: budget.MaxLLMCalls, MaxVisionCalls: budget.MaxVisionCalls, MaxAttemptsPerStep: budget.MaxAttemptsPerStep,
		CreatedAt: time.Now().UTC(),
	}
	created, err := s.chatSvc.repos.AgentExecution.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}
	if created {
		return run, nil
	}
	stored, err := s.chatSvc.repos.AgentExecution.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, errors.New("agent run is unavailable for this owner")
	}
	if stored.SessionID != session.ID || stored.ScopeType != run.ScopeType || stored.TaskID != run.TaskID || stored.KnowledgeBaseID != run.KnowledgeBaseID || stored.Goal != run.Goal || stored.Mode != run.Mode || stored.AgentProfile != run.AgentProfile || stored.ProfileSnapshot != run.ProfileSnapshot {
		return nil, errors.New("agent run frozen identity or profile does not match resume request")
	}
	return stored, nil
}

func safeAgentProfile(profile ai.Profile) frozenAgentProfile {
	return frozenAgentProfile{
		LLMProvider: profile.LLMProvider, LLMModel: profile.LLMModel, LLMEndpointDigest: digestAgentValue(strings.TrimSpace(profile.LLMBaseURL)),
		EmbeddingProvider: profile.EmbeddingProvider, EmbeddingModel: profile.EmbeddingModel, EmbeddingDim: profile.EmbeddingDim,
		EmbeddingEndpointDigest: digestAgentValue(strings.TrimSpace(profile.EmbeddingEndpoint)),
		VisionProvider:          profile.VisionProvider, VisionModel: profile.VisionModel, VisionEndpointDigest: digestAgentValue(strings.TrimSpace(profile.VisionBaseURL)),
	}
}

func defaultTemplateAgentPolicy(topK int) (frozenAgentPolicy, frozenAgentBudget) {
	allowed := defaultAgentToolNames()
	return frozenAgentPolicy{TopK: topK, MaxSteps: 16, AllowedTools: allowed}, frozenAgentBudget{
		MaxSteps: 16, MaxToolCalls: 16, MaxLLMCalls: 2, MaxVisionCalls: 0, MaxAttemptsPerStep: 1,
	}
}

func researchAgentPolicy(topK int, policy VideoResearchPolicy) (frozenAgentPolicy, frozenAgentBudget) {
	allowed := defaultAgentToolNames()
	return frozenAgentPolicy{TopK: topK, MaxSteps: policy.MaxSteps, MaxReplans: policy.MaxReplans, AllowedTools: allowed}, frozenAgentBudget{
		// Each research iteration has one planner checkpoint plus one tool step.
		MaxSteps: policy.MaxSteps*2 + 1, MaxToolCalls: policy.MaxSteps,
		MaxLLMCalls: policy.MaxSteps*2 + 1, MaxVisionCalls: 0, MaxAttemptsPerStep: 1,
	}
}

func defaultAgentToolNames() []string {
	names := []string{VideoAgentToolSearchTranscript, VideoAgentToolGetTranscriptWindow, VideoAgentToolSummarizeSegments, VideoAgentToolCompareSegments, VideoAgentToolBuildCitedAnswer}
	sort.Strings(names)
	return names
}

func (s *VideoAgentService) markAgentRunTerminal(ctx context.Context, userID int64, runID, status, reason string, err error) {
	if s == nil || s.chatSvc == nil || s.chatSvc.repos == nil || s.chatSvc.repos.AgentExecution == nil {
		return
	}
	errorCode, errorMessage := "", ""
	if err != nil {
		errorCode, errorMessage = reason, safeAgentError(err)
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	_, _ = s.chatSvc.repos.AgentExecution.MarkRunTerminal(ctx, repository.AgentRunTerminalUpdate{
		UserID: userID, RunID: runID, Status: status, StopReason: reason, ErrorCode: errorCode, ErrorMessage: errorMessage, Now: time.Now().UTC(),
	})
}

func safeAgentError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	runes := []rune(message)
	if len(runes) > 1000 {
		message = string(runes[:1000])
	}
	return message
}

type durableAgentStepObserver struct {
	repo     *repository.AgentExecutionRepository
	userID   int64
	runID    string
	nextStep int
	active   *durableObservedStep
	delegate VideoAgentStepObserver
	now      func() time.Time
}

type durableObservedStep struct {
	stepID  string
	attempt int
	token   string
	step    VideoAgentStep
}

func newDurableAgentStepObserver(repo *repository.AgentExecutionRepository, userID int64, runID string, delegate VideoAgentStepObserver) *durableAgentStepObserver {
	return &durableAgentStepObserver{repo: repo, userID: userID, runID: runID, delegate: delegate, now: func() time.Time { return time.Now().UTC() }}
}

func (o *durableAgentStepObserver) StepStart(step VideoAgentStep) error {
	if o == nil || o.repo == nil {
		return errors.New("durable agent observer unavailable")
	}
	if o.active != nil {
		return errors.New("durable agent observer already has a running step")
	}
	o.nextStep++
	stepID := fmt.Sprintf("s%d", o.nextStep)
	now := o.now()
	summary, argsDigest := safeToolInputSummary(step.Tool, step.Input)
	callDigest := digestAgentValue(o.runID + ":" + stepID + ":1:" + step.Tool + ":" + argsDigest)
	claim, err := o.repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
		UserID: o.userID, RunID: o.runID, StepID: stepID, Attempt: 1, Sequence: o.nextStep,
		Kind: videoAgentStepKind(step.Tool), Action: step.Tool, SafeReason: safeToolReason(step.Tool), InputSummary: summary,
		ArgumentsDigest: argsDigest, CallDigest: callDigest, ToolName: step.Tool,
		ReplaySafe: replaySafeAgentAction(step.Tool), LLMCall: llmAgentAction(step.Tool), VisionCall: visionAgentAction(step.Tool),
		LeaseToken: uuid.NewString(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
	})
	if err != nil {
		return err
	}
	if claim.Outcome != repository.AgentStepClaimAcquired {
		return fmt.Errorf("agent step %s claim outcome: %s", stepID, claim.Outcome)
	}
	o.active = &durableObservedStep{stepID: stepID, attempt: 1, token: claim.Step.LeaseToken, step: step}
	if o.delegate != nil {
		if err := o.delegate.StepStart(step); err != nil {
			_, _ = o.repo.FailStep(context.Background(), repository.AgentStepFailure{
				UserID: o.userID, RunID: o.runID, StepID: stepID, Attempt: 1, LeaseToken: claim.Step.LeaseToken,
				ErrorCode: "stream_observer_failure", ErrorMessage: safeAgentError(err), Now: o.now(),
			})
			o.active = nil
			return err
		}
	}
	return nil
}

func (o *durableAgentStepObserver) StepDone(step VideoAgentStep, output any) error {
	if o == nil || o.active == nil {
		return errors.New("durable agent step done without start")
	}
	checkpoint, err := json.Marshal(output)
	if err != nil {
		return err
	}
	refs := agentEvidenceRefs(output)
	completed, err := o.repo.CompleteStep(context.Background(), repository.AgentStepCompletion{
		UserID: o.userID, RunID: o.runID, StepID: o.active.stepID, Attempt: o.active.attempt, LeaseToken: o.active.token,
		OutputRef: agentToolOutputRef(step), ResultCheckpoint: string(checkpoint), EvidenceRefs: refs, Now: o.now(),
	})
	if err != nil {
		return err
	}
	if !completed {
		return errors.New("durable agent step completion CAS failed")
	}
	o.active = nil
	if o.delegate != nil {
		return o.delegate.StepDone(step, output)
	}
	return nil
}

func (o *durableAgentStepObserver) StepError(step VideoAgentStep, cause error) error {
	if o == nil || o.active == nil {
		return errors.New("durable agent step error without start")
	}
	failed, err := o.repo.FailStep(context.Background(), repository.AgentStepFailure{
		UserID: o.userID, RunID: o.runID, StepID: o.active.stepID, Attempt: o.active.attempt, LeaseToken: o.active.token,
		ErrorCode: "tool_failure", ErrorMessage: safeAgentError(cause), Cancelled: errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded), Now: o.now(),
	})
	if err != nil {
		return err
	}
	if !failed {
		return errors.New("durable agent step failure CAS failed")
	}
	o.active = nil
	if o.delegate != nil {
		return o.delegate.StepError(step, cause)
	}
	return nil
}

func safeToolInputSummary(tool string, input map[string]any) (string, string) {
	encoded, _ := json.Marshal(input)
	summary := map[string]any{"schema": 1}
	switch tool {
	case VideoAgentToolSearchTranscript:
		summary["question_digest"] = "sha256:" + digestAgentValue(fmt.Sprint(input["question"]))
		summary["top_k"] = input["top_k"]
	case VideoAgentToolGetTranscriptWindow:
		summary["chunk_index"], summary["radius"] = input["chunk_index"], input["radius"]
	case VideoAgentToolSummarizeSegments:
		summary["segment_count"] = input["segment_count"]
	case VideoAgentToolCompareSegments:
		summary["group_count"] = input["group_count"]
	case VideoAgentToolBuildCitedAnswer:
		summary["citation_count"] = input["citation_count"]
	default:
		summary["input_digest"] = "sha256:" + digestAgentValue(string(encoded))
	}
	safe, _ := json.Marshal(summary)
	return string(safe), digestAgentValue(string(encoded))
}

func safeToolReason(tool string) string {
	switch tool {
	case VideoAgentToolSearchTranscript:
		return "retrieve evidence from the current video"
	case VideoAgentToolGetTranscriptWindow:
		return "load context around an observed transcript chunk"
	case VideoAgentToolSummarizeSegments:
		return "summarize selected transcript segments"
	case VideoAgentToolCompareSegments:
		return "compare selected transcript segment groups"
	case VideoAgentToolBuildCitedAnswer:
		return "build the cited answer from observed evidence"
	default:
		return "execute an allow-listed Agent tool"
	}
}

func replaySafeAgentAction(tool string) bool {
	return tool == VideoAgentToolSearchTranscript || tool == VideoAgentToolGetTranscriptWindow
}

func llmAgentAction(tool string) bool {
	return tool == VideoAgentToolSummarizeSegments || tool == VideoAgentToolCompareSegments || tool == VideoAgentToolBuildCitedAnswer
}

func visionAgentAction(string) bool { return false }

func digestAgentValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func agentEvidenceRefs(output any) string {
	refs := make([]string, 0)
	switch value := output.(type) {
	case SearchTranscriptResult:
		for _, item := range value.Citations {
			if item.EvidenceID != "" {
				refs = append(refs, item.EvidenceID)
			}
		}
	case BuildCitedAnswerResult:
		for _, item := range value.Citations {
			if item.EvidenceID != "" {
				refs = append(refs, item.EvidenceID)
			}
		}
	}
	encoded, _ := json.Marshal(refs)
	return string(encoded)
}
