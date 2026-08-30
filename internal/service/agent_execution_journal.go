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
	"vid-lens/internal/repository"
)

var (
	errAgentExecutionBusy            = errors.New("agent execution step is owned by another worker")
	errAgentExecutionBudgetExhausted = errors.New("agent execution budget exhausted")
)

// AgentExecutionStore is the persistence seam used by AgentExecutionJournal.
// PostgreSQL and the deterministic test adapter are the two concrete adapters.
type AgentExecutionStore interface {
	CreateRun(context.Context, *model.AgentRun) (bool, error)
	GetRun(context.Context, int64, string) (*model.AgentRun, error)
	GetExecution(context.Context, int64, string) (*repository.AgentExecutionRecords, error)
	MarkFinalEvidenceRefs(context.Context, int64, string, []string) error
	ClaimStep(context.Context, repository.AgentStepClaimRequest) (repository.AgentStepClaim, error)
	CompleteStep(context.Context, repository.AgentStepCompletion) (bool, error)
	FailStep(context.Context, repository.AgentStepFailure) (bool, error)
	MarkRunTerminal(context.Context, repository.AgentRunTerminalUpdate) (bool, error)
}

var _ AgentExecutionStore = (*repository.AgentExecutionRepository)(nil)

// AgentExecutionJournal centralizes durable run and step lifecycle semantics.
type AgentExecutionJournal struct {
	store         AgentExecutionStore
	now           func() time.Time
	newLeaseToken func() string
}

func NewAgentExecutionJournal(store AgentExecutionStore) *AgentExecutionJournal {
	return &AgentExecutionJournal{
		store:         store,
		now:           func() time.Time { return time.Now().UTC() },
		newLeaseToken: uuid.NewString,
	}
}

type AgentJournalRunRequest struct {
	RunID        string
	UserID       int64
	Session      *model.ChatSession
	Goal         string
	Mode         string
	AgentProfile string
	Profile      ai.Profile
	Policy       frozenAgentPolicy
	Budget       frozenAgentBudget
}

func (j *AgentExecutionJournal) EnsureRun(ctx context.Context, req AgentJournalRunRequest) (*model.AgentRun, error) {
	if j == nil || j.store == nil {
		return nil, errors.New("agent execution journal unavailable")
	}
	if req.Session == nil || req.UserID <= 0 || strings.TrimSpace(req.RunID) == "" {
		return nil, errors.New("agent run parameters are invalid")
	}
	profileJSON, err := json.Marshal(safeAgentProfile(req.Profile))
	if err != nil {
		return nil, err
	}
	policyJSON, err := json.Marshal(req.Policy)
	if err != nil {
		return nil, err
	}
	budgetJSON, err := json.Marshal(req.Budget)
	if err != nil {
		return nil, err
	}
	agentProfile := firstNonEmpty(strings.TrimSpace(req.AgentProfile), "default")
	run := &model.AgentRun{
		ID: req.RunID, UserID: req.UserID, SessionID: req.Session.ID,
		ScopeType: firstNonEmpty(req.Session.ScopeType, model.ChatScopeVideo), TaskID: req.Session.TaskID, KnowledgeBaseID: req.Session.KnowledgeBaseID,
		Goal: strings.TrimSpace(req.Goal), Mode: req.Mode, AgentProfile: agentProfile,
		ProfileSnapshot: string(profileJSON), PolicySnapshot: string(policyJSON), BudgetSnapshot: string(budgetJSON),
		Status: model.AgentRunStatusRunning, MaxSteps: req.Budget.MaxSteps, MaxToolCalls: req.Budget.MaxToolCalls,
		MaxLLMCalls: req.Budget.MaxLLMCalls, MaxVisionCalls: req.Budget.MaxVisionCalls, MaxAttemptsPerStep: req.Budget.MaxAttemptsPerStep,
		MaxRetrievalCalls: req.Budget.MaxRetrievalCalls, MaxVisualCalls: req.Budget.MaxVisualCalls, MaxFrames: req.Budget.MaxFrames,
		MaxPromptTokens: req.Budget.MaxPromptTokens, MaxCompletionTokens: req.Budget.MaxCompletionTokens, MaxCostMicros: req.Budget.MaxCostMicros,
		MaxDurationMs: req.Budget.MaxDurationMs, MaxContextChars: req.Budget.MaxContextChars, CreatedAt: j.now(),
	}
	created, err := j.store.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}
	if created {
		return run, nil
	}
	stored, err := j.store.GetRun(ctx, req.UserID, req.RunID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, errors.New("agent run is unavailable for this owner")
	}
	if stored.SessionID != run.SessionID || stored.ScopeType != run.ScopeType || stored.TaskID != run.TaskID || stored.KnowledgeBaseID != run.KnowledgeBaseID || stored.Goal != run.Goal || stored.Mode != run.Mode || stored.AgentProfile != run.AgentProfile || stored.ProfileSnapshot != run.ProfileSnapshot {
		return nil, errors.New("agent run frozen identity or profile does not match resume request")
	}
	return stored, nil
}

func (j *AgentExecutionJournal) GetRun(ctx context.Context, userID int64, runID string) (*model.AgentRun, error) {
	if j == nil || j.store == nil {
		return nil, errors.New("agent execution journal unavailable")
	}
	return j.store.GetRun(ctx, userID, strings.TrimSpace(runID))
}

func (j *AgentExecutionJournal) Recover(ctx context.Context, userID int64, runID string) (*repository.AgentExecutionRecords, error) {
	if j == nil || j.store == nil {
		return nil, errors.New("agent execution journal unavailable")
	}
	return j.store.GetExecution(ctx, userID, strings.TrimSpace(runID))
}

func (j *AgentExecutionJournal) MarkFinalEvidenceRefs(ctx context.Context, userID int64, runID string, refs []string) error {
	if j == nil || j.store == nil {
		return errors.New("agent execution journal unavailable")
	}
	return j.store.MarkFinalEvidenceRefs(ctx, userID, strings.TrimSpace(runID), refs)
}

func (j *AgentExecutionJournal) MarkTerminal(ctx context.Context, userID int64, runID, status, reason string, cause error) {
	if j == nil || j.store == nil {
		return
	}
	errorCode, errorMessage := "", ""
	if cause != nil {
		errorCode, errorMessage = reason, safeAgentError(cause)
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	_, _ = j.store.MarkRunTerminal(ctx, repository.AgentRunTerminalUpdate{
		UserID: userID, RunID: strings.TrimSpace(runID), Status: status, StopReason: reason,
		ErrorCode: errorCode, ErrorMessage: errorMessage, Now: j.now(),
	})
}

type agentJournalLease struct {
	stepID  string
	attempt int
	token   string
}

func (j *AgentExecutionJournal) beginObservedStep(ctx context.Context, spec AgentJournalStep) (agentJournalLease, error) {
	now := j.now()
	callDigest := digestAgentValue(fmt.Sprintf("%s:%s:1:%s:%s", spec.RunID, spec.StepID, spec.Action, spec.ArgumentsDigest))
	claim, err := j.store.ClaimStep(ctx, repository.AgentStepClaimRequest{
		UserID: spec.UserID, RunID: spec.RunID, StepID: spec.StepID, Attempt: 1, Sequence: spec.Sequence,
		Kind: spec.Kind, Action: spec.Action, SafeReason: spec.SafeReason, InputSummary: spec.InputSummary,
		ArgumentsDigest: spec.ArgumentsDigest, CallDigest: callDigest, ToolName: firstNonEmpty(spec.ToolName, spec.Action),
		CallKind: firstNonEmpty(spec.CallKind, model.AgentCallKindTool), ReplaySafe: spec.ReplaySafe,
		LLMCall: spec.LLMCall, VisionCall: spec.VisionCall, RetrievalCall: spec.RetrievalCall,
		LeaseToken: j.newLeaseToken(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
	})
	if err != nil {
		return agentJournalLease{}, err
	}
	if claim.Outcome != repository.AgentStepClaimAcquired {
		return agentJournalLease{}, fmt.Errorf("agent step %s claim outcome: %s", spec.StepID, claim.Outcome)
	}
	return agentJournalLease{stepID: spec.StepID, attempt: claim.Step.Attempt, token: claim.Step.LeaseToken}, nil
}

func (j *AgentExecutionJournal) completeObservedStep(ctx context.Context, userID int64, runID string, lease agentJournalLease, outputRef string, checkpoint any, evidenceRefs string) error {
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	changed, err := j.store.CompleteStep(ctx, repository.AgentStepCompletion{
		UserID: userID, RunID: runID, StepID: lease.stepID, Attempt: lease.attempt, LeaseToken: lease.token,
		OutputRef: outputRef, ResultCheckpoint: string(encoded), EvidenceRefs: evidenceRefs, Now: j.now(),
	})
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("agent step completion CAS failed")
	}
	return nil
}

func (j *AgentExecutionJournal) failObservedStep(ctx context.Context, userID int64, runID string, lease agentJournalLease, code string, cause error) error {
	changed, err := j.store.FailStep(ctx, repository.AgentStepFailure{
		UserID: userID, RunID: runID, StepID: lease.stepID, Attempt: lease.attempt, LeaseToken: lease.token,
		ErrorCode: code, ErrorMessage: safeAgentError(cause), Cancelled: errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded), Now: j.now(),
	})
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("agent step failure CAS failed")
	}
	return nil
}

type AgentJournalStep struct {
	UserID                int64
	RunID                 string
	StepID                string
	Sequence              int
	Kind                  string
	Action                string
	DigestAction          string
	SafeReason            string
	InputSummary          string
	ArgumentsDigest       string
	ToolName              string
	CallKind              string
	InternalCall          bool
	ReplaySafe            bool
	RetryReplaySafe       bool
	LLMCall               bool
	VisionCall            bool
	RetrievalCall         bool
	VisualCall            bool
	FrameCount            int
	ContextChars          int64
	EstimatedPromptTokens int64
	FailureCode           string
}

type AgentJournalResult struct {
	Checkpoint   any
	OutputRef    string
	EvidenceRefs string
	MetricsJSON  string
	Usage        VideoResearchPlannerCallUsage
}

type AgentJournalExecution struct {
	Checkpoint      json.RawMessage
	Replayed        bool
	BudgetExhausted bool
	Step            model.AgentStep
	ToolCall        *model.AgentToolCall
}

func (j *AgentExecutionJournal) Execute(ctx context.Context, spec AgentJournalStep, invoke func() (AgentJournalResult, error)) (AgentJournalExecution, error) {
	if j == nil || j.store == nil {
		return AgentJournalExecution{}, errors.New("agent execution journal unavailable")
	}
	if invoke == nil {
		return AgentJournalExecution{}, errors.New("agent execution action unavailable")
	}
	if spec.UserID <= 0 || strings.TrimSpace(spec.RunID) == "" || strings.TrimSpace(spec.StepID) == "" || strings.TrimSpace(spec.Action) == "" {
		return AgentJournalExecution{}, errors.New("agent execution action parameters are invalid")
	}
	toolName := firstNonEmpty(spec.ToolName, spec.Action)
	callKind := firstNonEmpty(spec.CallKind, model.AgentCallKindTool)
	attempt := 1
	var claim repository.AgentStepClaim
	for {
		now := j.now()
		callDigest := digestAgentValue(fmt.Sprintf("%s:%s:%d:%s:%s", spec.RunID, spec.StepID, attempt, firstNonEmpty(spec.DigestAction, spec.Action), spec.ArgumentsDigest))
		var err error
		claim, err = j.store.ClaimStep(ctx, repository.AgentStepClaimRequest{
			UserID: spec.UserID, RunID: spec.RunID, StepID: spec.StepID, Attempt: attempt, Sequence: spec.Sequence,
			Kind: spec.Kind, Action: spec.Action, SafeReason: spec.SafeReason, InputSummary: spec.InputSummary,
			ArgumentsDigest: spec.ArgumentsDigest, CallDigest: callDigest, ToolName: toolName, CallKind: callKind,
			InternalCall: spec.InternalCall, ReplaySafe: spec.ReplaySafe, LLMCall: spec.LLMCall, VisionCall: spec.VisionCall,
			RetrievalCall: spec.RetrievalCall, VisualCall: spec.VisualCall, FrameCount: spec.FrameCount,
			ContextChars: spec.ContextChars, EstimatedPromptTokens: spec.EstimatedPromptTokens,
			LeaseToken: j.newLeaseToken(), Now: now, LeaseUntil: now.Add(agentStepLeaseDuration),
		})
		if err != nil {
			return AgentJournalExecution{}, err
		}
		switch claim.Outcome {
		case repository.AgentStepClaimCompleted:
			if claim.ToolCall == nil || claim.ToolCall.CallDigest != callDigest || claim.ToolCall.ArgumentsDigest != spec.ArgumentsDigest || claim.ToolCall.ToolName != toolName || claim.ToolCall.CallKind != callKind {
				return AgentJournalExecution{}, fmt.Errorf("persisted agent checkpoint %s does not match the current action", spec.StepID)
			}
			return AgentJournalExecution{Checkpoint: json.RawMessage(claim.Step.ResultCheckpoint), Replayed: true, Step: claim.Step, ToolCall: claim.ToolCall}, nil
		case repository.AgentStepClaimBusy:
			return AgentJournalExecution{}, errAgentExecutionBusy
		case repository.AgentStepClaimExhausted:
			return AgentJournalExecution{BudgetExhausted: true, Step: claim.Step, ToolCall: claim.ToolCall}, nil
		case repository.AgentStepClaimAmbiguous, repository.AgentStepClaimTerminal:
			if spec.RetryReplaySafe && spec.ReplaySafe && claim.Step.ReplaySafe && (claim.Step.Status == model.AgentStepStatusFailed || claim.Step.Status == model.AgentStepStatusAmbiguous) {
				attempt = claim.Step.Attempt + 1
				continue
			}
			return AgentJournalExecution{}, fmt.Errorf("agent step %s cannot be replayed safely: %s", spec.StepID, claim.Outcome)
		case repository.AgentStepClaimAcquired:
			attempt = claim.Step.Attempt
		default:
			return AgentJournalExecution{}, fmt.Errorf("agent step %s claim failed", spec.StepID)
		}
		break
	}

	result, invokeErr := invoke()
	contextChars := firstPositive(result.Usage.ContextChars, spec.ContextChars)
	metricsJSON := mergeAgentUsageMetrics(result.MetricsJSON, result.Usage, contextChars)
	if invokeErr != nil {
		cancelled := errors.Is(invokeErr, context.Canceled) || errors.Is(invokeErr, context.DeadlineExceeded)
		failureCode := firstNonEmpty(spec.FailureCode, "agent_action_failure")
		changed, failErr := j.store.FailStep(context.WithoutCancel(ctx), repository.AgentStepFailure{
			UserID: spec.UserID, RunID: spec.RunID, StepID: spec.StepID, Attempt: attempt, LeaseToken: claim.Step.LeaseToken,
			ErrorCode: failureCode, ErrorMessage: safeAgentError(invokeErr), Cancelled: cancelled,
			PromptTokens: result.Usage.PromptTokens, CompletionTokens: result.Usage.CompletionTokens, CostMicros: result.Usage.CostMicros,
			UsageSource: result.Usage.UsageSource, TokenEstimated: result.Usage.TokenEstimated, Currency: result.Usage.Currency,
			PriceVersion: result.Usage.PriceVersion, ContextChars: contextChars, ContextUsageSource: usageSourceForContext(contextChars),
			MetricsJSON: metricsJSON, Now: j.now(),
		})
		if failErr != nil {
			return AgentJournalExecution{}, failErr
		}
		if !changed {
			return AgentJournalExecution{}, errors.New("agent step failure CAS failed")
		}
		return AgentJournalExecution{}, invokeErr
	}

	checkpoint, err := json.Marshal(result.Checkpoint)
	if err != nil {
		return AgentJournalExecution{}, err
	}
	changed, err := j.store.CompleteStep(context.WithoutCancel(ctx), repository.AgentStepCompletion{
		UserID: spec.UserID, RunID: spec.RunID, StepID: spec.StepID, Attempt: attempt, LeaseToken: claim.Step.LeaseToken,
		OutputRef: result.OutputRef, ResultCheckpoint: string(checkpoint), EvidenceRefs: result.EvidenceRefs, MetricsJSON: metricsJSON,
		PromptTokens: result.Usage.PromptTokens, CompletionTokens: result.Usage.CompletionTokens, CostMicros: result.Usage.CostMicros,
		UsageSource: result.Usage.UsageSource, TokenEstimated: result.Usage.TokenEstimated, Currency: result.Usage.Currency,
		PriceVersion: result.Usage.PriceVersion, ContextChars: contextChars, ContextUsageSource: usageSourceForContext(contextChars), Now: j.now(),
	})
	if err != nil {
		return AgentJournalExecution{}, err
	}
	if !changed {
		return AgentJournalExecution{}, errors.New("agent step completion CAS failed")
	}
	return AgentJournalExecution{Checkpoint: checkpoint, Step: claim.Step, ToolCall: claim.ToolCall}, nil
}
