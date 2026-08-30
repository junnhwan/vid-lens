package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func TestVideoResearchRunnerRecoversCompletedCheckpointsWithoutRepeatingTool(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentRun{}, &model.AgentStep{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAgentExecutionRepository(db)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	run := &model.AgentRun{
		ID: "recover-run", UserID: 7, SessionID: 9, ScopeType: model.ChatScopeVideo, TaskID: 11,
		Goal: "recover goal", Mode: "research", AgentProfile: "default",
		ProfileSnapshot: `{}`, PolicySnapshot: `{"max_steps":3,"max_replans":1}`, BudgetSnapshot: `{"max_steps":7}`,
		Status: model.AgentRunStatusRunning, MaxSteps: 7, MaxToolCalls: 3, MaxLLMCalls: 4, MaxVisionCalls: 0, MaxAttemptsPerStep: 2, CreatedAt: now,
	}
	if created, err := repo.CreateRun(context.Background(), run); err != nil || !created {
		t.Fatalf("CreateRun() = %v, %v", created, err)
	}
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{definition: VideoAgentToolDefinition{Name: "inspect"}, output: json.RawMessage(`{"value":"must-not-run"}`)}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}

	planDecision := durableResearchDecision{Tool: "inspect", Arguments: json.RawMessage(`{"query":"owner"}`)}
	planCheckpoint, _ := json.Marshal(planDecision)
	initialState, err := NewVideoResearchState(run.Goal, VideoResearchPolicy{MaxSteps: 3, MaxReplans: 1})
	if err != nil {
		t.Fatal(err)
	}
	plannerSummary, plannerDigest := safePlannerInputSummary(initialState, registry.Definitions())
	claim, err := repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
		UserID: 7, RunID: run.ID, StepID: "plan-1", Attempt: 1, Sequence: 1, Kind: "plan", Action: "select_next_action",
		SafeReason: "select next action", InputSummary: plannerSummary, ArgumentsDigest: plannerDigest,
		CallDigest: digestAgentValue(run.ID + ":plan-1:1:" + videoResearchPlannerCall + ":" + plannerDigest),
		ToolName:   videoResearchPlannerCall, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true, ReplaySafe: false, LLMCall: true,
		LeaseToken: "planner-1", Now: now.Add(time.Second), LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || claim.Outcome != repository.AgentStepClaimAcquired {
		t.Fatalf("plan claim = %+v, %v", claim, err)
	}
	if changed, err := repo.CompleteStep(context.Background(), repository.AgentStepCompletion{UserID: 7, RunID: run.ID, StepID: "plan-1", Attempt: 1, LeaseToken: "planner-1", OutputRef: "inspect", ResultCheckpoint: string(planCheckpoint), Now: now.Add(2 * time.Second)}); err != nil || !changed {
		t.Fatalf("complete plan = %v, %v", changed, err)
	}

	toolResult := VideoAgentToolResult{Output: json.RawMessage(`{"value":"persisted"}`), Step: VideoAgentStep{Name: "inspect", Tool: "inspect", OutputRef: "persisted-result"}}
	observation := VideoResearchObservation{Tool: "inspect", Output: toolResult.Output, Step: toolResult.Step}
	toolCheckpoint, _ := json.Marshal(durableResearchToolCheckpoint{Result: toolResult, Observation: observation})
	argsDigest := digestAgentValue(string(planDecision.Arguments))
	claim, err = repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
		UserID: 7, RunID: run.ID, StepID: "tool-1", Attempt: 1, Sequence: 2, Kind: "tool", Action: "inspect",
		SafeReason: "inspect", InputSummary: `{"arguments_digest":"sha256"}`, ArgumentsDigest: argsDigest,
		CallDigest: digestAgentValue(run.ID + ":tool-1:" + argsDigest), ToolName: "inspect", ReplaySafe: true,
		LeaseToken: "tool-1", Now: now.Add(3 * time.Second), LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || claim.Outcome != repository.AgentStepClaimAcquired {
		t.Fatalf("tool claim = %+v, %v", claim, err)
	}
	if changed, err := repo.CompleteStep(context.Background(), repository.AgentStepCompletion{UserID: 7, RunID: run.ID, StepID: "tool-1", Attempt: 1, LeaseToken: "tool-1", OutputRef: "persisted-result", ResultCheckpoint: string(toolCheckpoint), Now: now.Add(4 * time.Second)}); err != nil || !changed {
		t.Fatalf("complete tool = %v, %v", changed, err)
	}

	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{{Done: true, StopReason: "recovered"}}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 3, MaxReplans: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SetDurableExecution(repo, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), run.Goal, VideoAgentToolRuntime{UserID: 7, TaskID: 11})
	if err != nil {
		t.Fatalf("recovered Run() error = %v", err)
	}
	if result.State.Status != VideoResearchStatusCompleted || result.State.CurrentStep != 1 || tool.calls != 0 || planner.calls != 1 {
		t.Fatalf("recovered state=%+v tool_calls=%d planner_calls=%d", result.State, tool.calls, planner.calls)
	}
	if len(result.State.Observations) != 1 || string(result.State.Observations[0].Output) != `{"value":"persisted"}` {
		t.Fatalf("recovered observations = %+v", result.State.Observations)
	}
	records, err := repo.GetExecution(context.Background(), 7, run.ID)
	if err != nil || records == nil || len(records.ToolCalls) != 3 {
		t.Fatalf("recovered execution = %+v, %v", records, err)
	}
	plannerCalls := 0
	for _, call := range records.ToolCalls {
		if call.CallKind == model.AgentCallKindPlannerLLM {
			plannerCalls++
		}
	}
	if plannerCalls != 2 || records.Run.ToolCallsUsed != 1 {
		t.Fatalf("recovered planner/tool counters = %+v", records)
	}
}

func TestVideoResearchRunnerContinuesAfterCompletedStepsAndRetryAttempt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentRun{}, &model.AgentStep{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAgentExecutionRepository(db)
	now := time.Date(2026, 8, 30, 12, 30, 0, 0, time.UTC)
	run := &model.AgentRun{
		ID: "recover-progress-run", UserID: 7, SessionID: 9, ScopeType: model.ChatScopeVideo, TaskID: 11,
		Goal: "continue research", Mode: "research", AgentProfile: "default", ProfileSnapshot: `{}`,
		PolicySnapshot: `{"max_steps":4,"max_replans":1}`, BudgetSnapshot: `{"max_steps":9}`,
		Status: model.AgentRunStatusRunning, MaxSteps: 9, MaxToolCalls: 4, MaxLLMCalls: 5, MaxVisionCalls: 0, MaxAttemptsPerStep: 2, CreatedAt: now,
	}
	if created, err := repo.CreateRun(context.Background(), run); err != nil || !created {
		t.Fatalf("CreateRun() = %v, %v", created, err)
	}
	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{definition: VideoAgentToolDefinition{Name: VideoAgentToolSearchTranscript}, output: json.RawMessage(`{"value":"must-not-run"}`)}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	policy := VideoResearchPolicy{MaxSteps: 4, MaxReplans: 1}
	recoveredState, err := NewVideoResearchState(run.Goal, policy)
	if err != nil {
		t.Fatal(err)
	}

	persistPlan := func(number int, state VideoResearchState, decision durableResearchDecision) {
		t.Helper()
		stepID := fmt.Sprintf("plan-%d", number)
		summary, inputDigest := safePlannerInputSummary(state, registry.Definitions())
		claim, claimErr := repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
			UserID: 7, RunID: run.ID, StepID: stepID, Attempt: 1, Sequence: number*2 - 1,
			Kind: "plan", Action: "select_next_action", SafeReason: "select next action", InputSummary: summary,
			ArgumentsDigest: inputDigest, CallDigest: digestAgentValue(run.ID + ":" + stepID + ":1:" + videoResearchPlannerCall + ":" + inputDigest),
			ToolName: videoResearchPlannerCall, CallKind: model.AgentCallKindPlannerLLM, InternalCall: true, ReplaySafe: false, LLMCall: true,
			LeaseToken: stepID, Now: now.Add(time.Duration(number*10) * time.Second), LeaseUntil: now.Add(time.Hour),
		})
		if claimErr != nil || claim.Outcome != repository.AgentStepClaimAcquired {
			t.Fatalf("%s claim = %+v, %v", stepID, claim, claimErr)
		}
		checkpoint, _ := json.Marshal(decision)
		if changed, completeErr := repo.CompleteStep(context.Background(), repository.AgentStepCompletion{
			UserID: 7, RunID: run.ID, StepID: stepID, Attempt: 1, LeaseToken: stepID,
			OutputRef: decision.Tool, ResultCheckpoint: string(checkpoint), Now: now.Add(time.Duration(number*10+1) * time.Second),
		}); completeErr != nil || !changed {
			t.Fatalf("complete %s = %v, %v", stepID, changed, completeErr)
		}
	}
	persistTool := func(number, attempt int, decision durableResearchDecision, result VideoAgentToolResult, observation VideoResearchObservation, fail bool) {
		t.Helper()
		stepID := fmt.Sprintf("tool-%d", number)
		argsDigest := digestAgentValue(string(decision.Arguments))
		token := fmt.Sprintf("%s-attempt-%d", stepID, attempt)
		claim, claimErr := repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
			UserID: 7, RunID: run.ID, StepID: stepID, Attempt: attempt, Sequence: number * 2,
			Kind: "retrieve", Action: decision.Tool, SafeReason: "retrieve evidence", InputSummary: safeResearchArgumentsSummary(decision.Tool, decision.Arguments),
			ArgumentsDigest: argsDigest, CallDigest: digestAgentValue(fmt.Sprintf("%s:%s:%d:%s:%s", run.ID, stepID, attempt, decision.Tool, argsDigest)),
			ToolName: decision.Tool, ReplaySafe: true, LeaseToken: token,
			Now: now.Add(time.Duration(number*10+attempt+1) * time.Second), LeaseUntil: now.Add(time.Hour),
		})
		if claimErr != nil || claim.Outcome != repository.AgentStepClaimAcquired {
			t.Fatalf("%s attempt %d claim = %+v, %v", stepID, attempt, claim, claimErr)
		}
		if fail {
			if changed, failErr := repo.FailStep(context.Background(), repository.AgentStepFailure{
				UserID: 7, RunID: run.ID, StepID: stepID, Attempt: attempt, LeaseToken: token,
				ErrorCode: "temporary_read_failure", ErrorMessage: "temporary read failure", Now: now.Add(time.Duration(number*10+attempt+2) * time.Second),
			}); failErr != nil || !changed {
				t.Fatalf("fail %s attempt %d = %v, %v", stepID, attempt, changed, failErr)
			}
			return
		}
		checkpoint, _ := json.Marshal(durableResearchToolCheckpoint{Result: result, Observation: observation})
		if changed, completeErr := repo.CompleteStep(context.Background(), repository.AgentStepCompletion{
			UserID: 7, RunID: run.ID, StepID: stepID, Attempt: attempt, LeaseToken: token,
			OutputRef: result.Step.OutputRef, ResultCheckpoint: string(checkpoint), Now: now.Add(time.Duration(number*10+attempt+2) * time.Second),
		}); completeErr != nil || !changed {
			t.Fatalf("complete %s attempt %d = %v, %v", stepID, attempt, changed, completeErr)
		}
	}
	applyExpected := func(number int, decision durableResearchDecision, result VideoAgentToolResult, observation VideoResearchObservation) {
		action := decision.toDecision()
		recoveredState.CurrentStep++
		recoveredState.Steps = append(recoveredState.Steps, VideoResearchStep{Number: number, Action: action, Status: VideoResearchStepCompleted, Trace: result.Step, Observation: &observation})
		recoveredState.Observations = append(recoveredState.Observations, observation)
		recoveredState.Evidence = mergeVideoResearchEvidence(recoveredState.Evidence, observation.NewEvidence)
		recoveredState.PendingQuestions = append([]string(nil), observation.UnresolvedQuestions...)
	}

	firstDecision := durableResearchDecision{Tool: VideoAgentToolSearchTranscript, Arguments: json.RawMessage(`{"question":"first","top_k":1}`)}
	firstResult := VideoAgentToolResult{Output: json.RawMessage(`{"value":"first"}`), Step: VideoAgentStep{Name: "first", Tool: VideoAgentToolSearchTranscript, OutputRef: "first-result"}}
	firstObservation := VideoResearchObservation{Tool: VideoAgentToolSearchTranscript, Output: firstResult.Output, Step: firstResult.Step}
	persistPlan(1, recoveredState, firstDecision)
	persistTool(1, 1, firstDecision, VideoAgentToolResult{}, VideoResearchObservation{}, true)
	persistTool(1, 2, firstDecision, firstResult, firstObservation, false)
	applyExpected(1, firstDecision, firstResult, firstObservation)

	secondDecision := durableResearchDecision{Tool: VideoAgentToolSearchTranscript, Arguments: json.RawMessage(`{"question":"second","top_k":1}`)}
	secondResult := VideoAgentToolResult{Output: json.RawMessage(`{"value":"second"}`), Step: VideoAgentStep{Name: "second", Tool: VideoAgentToolSearchTranscript, OutputRef: "second-result"}}
	secondObservation := VideoResearchObservation{Tool: VideoAgentToolSearchTranscript, Output: secondResult.Output, Step: secondResult.Step}
	persistPlan(2, recoveredState, secondDecision)
	persistTool(2, 1, secondDecision, secondResult, secondObservation, false)

	planner := &scriptedVideoResearchPlanner{decisions: []VideoResearchDecision{{Done: true, StopReason: "continued"}}}
	runner, err := NewVideoResearchRunner(registry, planner, &recordingVideoResearchObserver{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SetDurableExecution(repo, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), run.Goal, VideoAgentToolRuntime{UserID: 7, TaskID: 11})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State.Status != VideoResearchStatusCompleted || result.State.CurrentStep != 2 || len(result.State.Steps) != 2 || planner.calls != 1 || tool.calls != 0 {
		t.Fatalf("continued state=%+v planner_calls=%d tool_calls=%d", result.State, planner.calls, tool.calls)
	}
	if result.State.Steps[0].Trace.OutputRef != "first-result" || result.State.Steps[1].Trace.OutputRef != "second-result" {
		t.Fatalf("recovered traces = %+v", result.State.Steps)
	}
}

func TestVideoResearchRunnerPersistsFailedPlannerAuditAndUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentRun{}, &model.AgentStep{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAgentExecutionRepository(db)
	now := time.Date(2026, 8, 30, 13, 0, 0, 0, time.UTC)
	run := &model.AgentRun{
		ID: "planner-failure-run", UserID: 7, SessionID: 9, ScopeType: model.ChatScopeVideo, TaskID: 11,
		Goal: "private planner goal", Mode: "research", AgentProfile: "default", ProfileSnapshot: `{}`,
		PolicySnapshot: `{"max_steps":1,"max_replans":0}`, BudgetSnapshot: `{"max_steps":3}`,
		Status: model.AgentRunStatusRunning, MaxSteps: 3, MaxToolCalls: 1, MaxLLMCalls: 2, MaxVisionCalls: 0, MaxAttemptsPerStep: 2, CreatedAt: now,
	}
	if created, err := repo.CreateRun(context.Background(), run); err != nil || !created {
		t.Fatalf("CreateRun() = %v, %v", created, err)
	}
	planner := &usageVideoResearchPlanner{
		err: errors.New("provider unavailable"),
		usage: VideoResearchPlannerCallUsage{
			PromptTokens: 120, CompletionTokens: 3, CostMicros: 77,
			UsageSource: model.AgentCallUsageActual, Currency: "USD", PriceVersion: "test-v1",
		},
	}
	runner, err := NewVideoResearchRunner(NewVideoAgentToolRegistry(), planner, &recordingVideoResearchObserver{}, VideoResearchPolicy{MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SetDurableExecution(repo, 7, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), run.Goal, VideoAgentToolRuntime{UserID: 7, TaskID: 11}); err == nil {
		t.Fatal("Run() error = nil")
	}
	records, err := repo.GetExecution(context.Background(), 7, run.ID)
	if err != nil || records == nil || len(records.ToolCalls) != 1 {
		t.Fatalf("failed execution = %+v, %v", records, err)
	}
	call := records.ToolCalls[0]
	if call.CallKind != model.AgentCallKindPlannerLLM || call.Status != model.AgentToolCallStatusFailed || call.CallDigest == "" || call.ArgumentsDigest == "" || call.PromptTokens != 120 || call.CompletionTokens != 3 || call.CostMicros != 77 || call.UsageSource != model.AgentCallUsageActual || call.Currency != "USD" || call.PriceVersion != "test-v1" {
		t.Fatalf("failed planner call = %+v", call)
	}
	if strings.Contains(call.InputSummary, run.Goal) || strings.Contains(call.ResultCheckpoint, run.Goal) {
		t.Fatalf("planner audit leaked private input: %+v", call)
	}
}

type usageVideoResearchPlanner struct {
	decision VideoResearchDecision
	usage    VideoResearchPlannerCallUsage
	err      error
	calls    int
}

func (p *usageVideoResearchPlanner) NextDecision(ctx context.Context, state VideoResearchState, tools []VideoAgentToolDefinition) (VideoResearchDecision, error) {
	decision, _, err := p.NextDecisionWithUsage(ctx, state, tools)
	return decision, err
}

func (p *usageVideoResearchPlanner) NextDecisionWithUsage(_ context.Context, _ VideoResearchState, _ []VideoAgentToolDefinition) (VideoResearchDecision, VideoResearchPlannerCallUsage, error) {
	p.calls++
	return p.decision, p.usage, p.err
}
