package service

import (
	"context"
	"encoding/json"
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

	planDecision := durableResearchDecision{Tool: "inspect", Arguments: json.RawMessage(`{"query":"owner"}`)}
	planCheckpoint, _ := json.Marshal(planDecision)
	claim, err := repo.ClaimStep(context.Background(), repository.AgentStepClaimRequest{
		UserID: 7, RunID: run.ID, StepID: "plan-1", Attempt: 1, Sequence: 1, Kind: "plan", Action: "select_next_action",
		SafeReason: "select next action", InputSummary: `{}`, ReplaySafe: false, LLMCall: true,
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

	registry := NewVideoAgentToolRegistry()
	tool := &scriptedVideoResearchTool{definition: VideoAgentToolDefinition{Name: "inspect"}, output: json.RawMessage(`{"value":"must-not-run"}`)}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
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
}
