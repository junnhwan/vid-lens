package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type invalidRecoveryTool struct{ calls int }

func (t *invalidRecoveryTool) Definition() VideoAgentToolDefinition {
	return VideoAgentToolDefinition{Name: "inspect"}
}
func (t *invalidRecoveryTool) Execute(context.Context, VideoAgentToolRequest) (VideoAgentToolResult, error) {
	t.calls++
	return VideoAgentToolResult{Step: VideoAgentStep{Tool: "inspect"}}, &InvalidToolArguments{Cause: errors.New("query required")}
}

func TestArgumentCorrectionReplaysSubsequentPlannerCheckpoint(t *testing.T) {
	t.Run("one_correction_replays", func(t *testing.T) { testArgumentCorrectionJournalReplay(t, false) })
	t.Run("two_corrections_stop", func(t *testing.T) { testArgumentCorrectionJournalReplay(t, true) })
}

func testArgumentCorrectionJournalReplay(t *testing.T, exhausted bool) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentRun{}, &model.AgentStep{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAgentExecutionRepository(db)
	run := &model.AgentRun{ID: "argument-recovery", UserID: 7, SessionID: 9, ScopeType: model.ChatScopeVideo, TaskID: 11, Goal: "recover goal", Mode: "research", AgentProfile: "default", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`, Status: model.AgentRunStatusRunning, MaxSteps: 7, MaxToolCalls: 3, MaxLLMCalls: 4, MaxAttemptsPerStep: 2, CreatedAt: time.Now()}
	if _, err := repo.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	registry := NewVideoAgentToolRegistry()
	tool := &invalidRecoveryTool{}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	planner := &scriptedVideoAgentLoopPlanner{decisions: []VideoAgentLoopDecision{{Tool: "inspect", Reason: "inspect evidence", Arguments: json.RawMessage(`{}`)}, {Done: true, StopReason: "recovered"}}}
	if exhausted {
		planner.decisions[1] = VideoAgentLoopDecision{Tool: "inspect", Reason: "correct arguments", Arguments: json.RawMessage(`{"query":"retry"}`)}
	}
	runner, err := NewVideoAgentLoopRunner(registry, planner, &recordingVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 3, MaxReplans: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SetDurableExecution(NewAgentExecutionJournal(repo), 7, run.ID); err != nil {
		t.Fatal(err)
	}
	runtime := VideoAgentToolRuntime{UserID: 7, TaskID: 11}
	first, err := runner.Run(context.Background(), run.Goal, runtime)
	if exhausted {
		if err == nil || first.State.ArgumentCorrections != 2 {
			t.Fatalf("expected correction limit: state=%+v err=%v", first, err)
		}
		// Both invalid checkpoints exist, including the one immediately before the live loop stops.
		_, replayErr := runner.Run(context.Background(), run.Goal, runtime)
		if replayErr == nil || replayErr.Error() != "工具参数纠正次数已用尽" {
			t.Fatalf("replay should preserve correction limit: %v", replayErr)
		}
		if tool.calls != 2 || planner.calls != 2 {
			t.Fatalf("replayed external calls: tool=%d planner=%d", tool.calls, planner.calls)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if first.State.ArgumentCorrections != 1 || first.State.Steps[0].Status != VideoAgentLoopStepFailed {
		t.Fatalf("first state: %+v", first.State)
	}
	// This traverses persisted plan-1, invalid tool-1 and plan-2 through the real journal.
	replay, err := runner.Run(context.Background(), run.Goal, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 || planner.calls != 2 {
		t.Fatalf("replayed external calls: tool=%d planner=%d", tool.calls, planner.calls)
	}
	if replay.State.Steps[0].Status != first.State.Steps[0].Status || replay.State.Steps[0].Error != first.State.Steps[0].Error || replay.State.ArgumentCorrections != 1 {
		t.Fatalf("replay state: %+v", replay.State)
	}
}
