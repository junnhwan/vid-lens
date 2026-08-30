package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func newAgentExecutionTestRepository(t *testing.T) *AgentExecutionRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite has database-wide writer locks. Keep one connection here so the
	// goroutine test exercises repository CAS deterministically; PostgreSQL's
	// row-level concurrency is covered by the integration suite.
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.AgentRun{}, &model.AgentStep{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	return NewAgentExecutionRepository(db)
}

func createAgentExecutionRun(t *testing.T, repo *AgentExecutionRepository, id string, maxSteps, maxLLM int) model.AgentRun {
	t.Helper()
	run := model.AgentRun{
		ID: id, UserID: 7, SessionID: 9, ScopeType: model.ChatScopeVideo, TaskID: 11,
		Goal: "summarize this video", Mode: "research", AgentProfile: "default",
		ProfileSnapshot: `{"llm_provider":"test","llm_model":"m"}`,
		PolicySnapshot:  `{"max_steps":4,"max_replans":1}`,
		BudgetSnapshot:  fmt.Sprintf(`{"max_steps":%d,"max_llm_calls":%d}`, maxSteps, maxLLM),
		Status:          model.AgentRunStatusRunning, MaxSteps: maxSteps, MaxToolCalls: maxSteps,
		MaxLLMCalls: maxLLM, MaxVisionCalls: 0, MaxAttemptsPerStep: 2,
		CreatedAt: time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
	}
	created, err := repo.CreateRun(context.Background(), &run)
	if err != nil || !created {
		t.Fatalf("CreateRun() = %v, %v", created, err)
	}
	return run
}

func agentClaimRequest(runID, stepID, token string, now time.Time) AgentStepClaimRequest {
	argsDigest := digestText(`{"top_k":4}`)
	return AgentStepClaimRequest{
		UserID: 7, RunID: runID, StepID: stepID, Attempt: 1, Sequence: 1,
		Kind: "tool", Action: "search_transcript", SafeReason: "retrieve current video evidence",
		InputSummary:    `{"top_k":4,"question_digest":"sha256:abc"}`,
		ArgumentsDigest: argsDigest, CallDigest: digestText(runID + ":" + stepID + ":" + argsDigest),
		ToolName: "search_transcript", ReplaySafe: true, LeaseToken: token,
		Now: now, LeaseUntil: now.Add(time.Minute),
	}
}

func TestAgentExecutionPersistsFrozenRunAndIdempotentCompletedStep(t *testing.T) {
	repo := newAgentExecutionTestRepository(t)
	run := createAgentExecutionRun(t, repo, "run-frozen", 3, 2)
	duplicate := run
	duplicate.Goal = "changed goal"
	created, err := repo.CreateRun(context.Background(), &duplicate)
	if err != nil || created {
		t.Fatalf("duplicate CreateRun() = %v, %v", created, err)
	}

	now := run.CreatedAt.Add(time.Second)
	claim, err := repo.ClaimStep(context.Background(), agentClaimRequest(run.ID, "tool-1", "worker-a", now))
	if err != nil || claim.Outcome != AgentStepClaimAcquired || claim.ToolCall == nil {
		t.Fatalf("ClaimStep() = %+v, %v", claim, err)
	}
	completed, err := repo.CompleteStep(context.Background(), AgentStepCompletion{
		UserID: 7, RunID: run.ID, StepID: "tool-1", Attempt: 1, LeaseToken: "worker-a",
		OutputRef: "citations:2", ResultCheckpoint: `{"citations":["e1","e2"]}`,
		EvidenceRefs: `["e1","e2"]`, Now: now.Add(2 * time.Second),
	})
	if err != nil || !completed {
		t.Fatalf("CompleteStep() = %v, %v", completed, err)
	}
	replay := agentClaimRequest(run.ID, "tool-1", "worker-b", now.Add(3*time.Second))
	claim, err = repo.ClaimStep(context.Background(), replay)
	if err != nil || claim.Outcome != AgentStepClaimCompleted || claim.Step.ResultCheckpoint == "" {
		t.Fatalf("replay ClaimStep() = %+v, %v", claim, err)
	}

	records, err := repo.GetExecution(context.Background(), 7, run.ID)
	if err != nil || records == nil || len(records.Steps) != 1 || len(records.ToolCalls) != 1 {
		t.Fatalf("GetExecution() = %+v, %v", records, err)
	}
	if records.Run.Goal != "summarize this video" || records.Run.StepsUsed != 1 || records.Run.ToolCallsUsed != 1 {
		t.Fatalf("frozen/counters = %+v", records.Run)
	}
	if records.ToolCalls[0].InputSummary != replay.InputSummary || records.ToolCalls[0].ResultDigest == "" {
		t.Fatalf("tool call = %+v", records.ToolCalls[0])
	}
}

func TestAgentExecutionExpiredLeaseUsesCASAndFencesConcurrentOwners(t *testing.T) {
	repo := newAgentExecutionTestRepository(t)
	run := createAgentExecutionRun(t, repo, "run-cas", 3, 1)
	now := run.CreatedAt.Add(time.Second)
	first := agentClaimRequest(run.ID, "retrieve-1", "crashed-worker", now)
	first.LeaseUntil = now.Add(time.Millisecond)
	if claim, err := repo.ClaimStep(context.Background(), first); err != nil || claim.Outcome != AgentStepClaimAcquired {
		t.Fatalf("initial claim = %+v, %v", claim, err)
	}

	start := make(chan struct{})
	results := make(chan AgentStepClaim, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := agentClaimRequest(run.ID, "retrieve-1", fmt.Sprintf("worker-%d", i), now.Add(time.Second))
			claim, err := repo.ClaimStep(context.Background(), req)
			results <- claim
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ClaimStep(): %v", err)
		}
	}
	acquired, busy := 0, 0
	for result := range results {
		switch result.Outcome {
		case AgentStepClaimAcquired:
			acquired++
		case AgentStepClaimBusy:
			busy++
		}
	}
	if acquired != 1 || busy != 1 {
		t.Fatalf("concurrent outcomes acquired=%d busy=%d", acquired, busy)
	}
}

func TestAgentExecutionDoesNotReplayExpiredLLMAndExhaustsBudget(t *testing.T) {
	repo := newAgentExecutionTestRepository(t)
	run := createAgentExecutionRun(t, repo, "run-llm", 2, 1)
	now := run.CreatedAt.Add(time.Second)
	req := agentClaimRequest(run.ID, "answer-1", "llm-worker", now)
	req.Action, req.ToolName, req.ReplaySafe, req.LLMCall = "build_cited_answer", "build_cited_answer", false, true
	req.LeaseUntil = now.Add(time.Millisecond)
	if claim, err := repo.ClaimStep(context.Background(), req); err != nil || claim.Outcome != AgentStepClaimAcquired {
		t.Fatalf("LLM claim = %+v, %v", claim, err)
	}
	req.LeaseToken, req.Now, req.LeaseUntil = "recovery-worker", now.Add(time.Second), now.Add(2*time.Second)
	claim, err := repo.ClaimStep(context.Background(), req)
	if err != nil || claim.Outcome != AgentStepClaimAmbiguous || claim.Step.ErrorCode != "interrupted_non_replayable" {
		t.Fatalf("expired LLM claim = %+v, %v", claim, err)
	}

	retry := req
	retry.Attempt, retry.LeaseToken, retry.StepID = 2, "retry-worker", "answer-1"
	retry.Now, retry.LeaseUntil = now.Add(3*time.Second), now.Add(4*time.Second)
	claim, err = repo.ClaimStep(context.Background(), retry)
	if err != nil || claim.Outcome != AgentStepClaimExhausted || claim.Run.Status != model.AgentRunStatusBudgetExhausted {
		t.Fatalf("LLM budget claim = %+v, %v", claim, err)
	}
	changed, err := repo.MarkRunTerminal(context.Background(), AgentRunTerminalUpdate{UserID: 7, RunID: run.ID, Status: model.AgentRunStatusCompleted, StopReason: "late retry", Now: now.Add(5 * time.Second)})
	if err != nil || changed {
		t.Fatalf("terminal overwrite = %v, %v", changed, err)
	}
}

func TestAgentExecutionEnforcesOwnerIsolationAndTerminalMonotonicity(t *testing.T) {
	repo := newAgentExecutionTestRepository(t)
	run := createAgentExecutionRun(t, repo, "run-owner", 2, 1)
	if got, err := repo.GetRun(context.Background(), 8, run.ID); err != nil || got != nil {
		t.Fatalf("cross-owner GetRun() = %+v, %v", got, err)
	}
	req := agentClaimRequest(run.ID, "tool-1", "attacker", run.CreatedAt.Add(time.Second))
	req.UserID = 8
	if claim, err := repo.ClaimStep(context.Background(), req); err != nil || claim.Outcome != "" {
		t.Fatalf("cross-owner ClaimStep() = %+v, %v", claim, err)
	}

	changed, err := repo.MarkRunTerminal(context.Background(), AgentRunTerminalUpdate{UserID: 7, RunID: run.ID, Status: model.AgentRunStatusCompleted, StopReason: "goal_satisfied", Now: run.CreatedAt.Add(2 * time.Second)})
	if err != nil || !changed {
		t.Fatalf("complete run = %v, %v", changed, err)
	}
	changed, err = repo.MarkRunTerminal(context.Background(), AgentRunTerminalUpdate{UserID: 7, RunID: run.ID, Status: model.AgentRunStatusFailed, StopReason: "retry_failure", Now: run.CreatedAt.Add(3 * time.Second)})
	if err != nil || changed {
		t.Fatalf("overwrite completed run = %v, %v", changed, err)
	}
	stored, err := repo.GetRun(context.Background(), 7, run.ID)
	if err != nil || stored.Status != model.AgentRunStatusCompleted || stored.StopReason != "goal_satisfied" {
		t.Fatalf("terminal run = %+v, %v", stored, err)
	}
}
