package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

type scriptedAgentExecutionStore struct {
	claims      []repository.AgentStepClaim
	claimInputs []repository.AgentStepClaimRequest
	completed   []repository.AgentStepCompletion
	failed      []repository.AgentStepFailure
}

func (s *scriptedAgentExecutionStore) CreateRun(context.Context, *model.AgentRun) (bool, error) {
	return false, nil
}
func (s *scriptedAgentExecutionStore) GetRun(context.Context, int64, string) (*model.AgentRun, error) {
	return nil, nil
}
func (s *scriptedAgentExecutionStore) GetExecution(context.Context, int64, string) (*repository.AgentExecutionRecords, error) {
	return nil, nil
}
func (s *scriptedAgentExecutionStore) MarkFinalEvidenceRefs(context.Context, int64, string, []string) error {
	return nil
}
func (s *scriptedAgentExecutionStore) ClaimStep(_ context.Context, req repository.AgentStepClaimRequest) (repository.AgentStepClaim, error) {
	s.claimInputs = append(s.claimInputs, req)
	if len(s.claims) == 0 {
		return repository.AgentStepClaim{}, errors.New("unexpected claim")
	}
	claim := s.claims[0]
	s.claims = s.claims[1:]
	if claim.Outcome == repository.AgentStepClaimAcquired {
		claim.Step = model.AgentStep{StepID: req.StepID, Attempt: req.Attempt, LeaseToken: req.LeaseToken, ReplaySafe: req.ReplaySafe}
	}
	if claim.Outcome == repository.AgentStepClaimCompleted && claim.ToolCall != nil {
		claim.ToolCall.CallDigest = req.CallDigest
		claim.ToolCall.ArgumentsDigest = req.ArgumentsDigest
		claim.ToolCall.ToolName = req.ToolName
		claim.ToolCall.CallKind = req.CallKind
	}
	return claim, nil
}
func (s *scriptedAgentExecutionStore) CompleteStep(_ context.Context, req repository.AgentStepCompletion) (bool, error) {
	s.completed = append(s.completed, req)
	return true, nil
}
func (s *scriptedAgentExecutionStore) FailStep(_ context.Context, req repository.AgentStepFailure) (bool, error) {
	s.failed = append(s.failed, req)
	return true, nil
}
func (s *scriptedAgentExecutionStore) MarkRunTerminal(context.Context, repository.AgentRunTerminalUpdate) (bool, error) {
	return true, nil
}

func TestAgentExecutionJournalReplaysMatchingCheckpointWithoutInvokingAction(t *testing.T) {
	store := &scriptedAgentExecutionStore{claims: []repository.AgentStepClaim{{
		Outcome:  repository.AgentStepClaimCompleted,
		Step:     model.AgentStep{StepID: "tool-1", Attempt: 1, Status: model.AgentStepStatusCompleted, ResultCheckpoint: `{"answer":"saved"}`},
		ToolCall: &model.AgentToolCall{},
	}}}
	journal := NewAgentExecutionJournal(store)
	invoked := false

	result, err := journal.Execute(context.Background(), AgentJournalStep{
		UserID: 7, RunID: "run-1", StepID: "tool-1", Sequence: 1,
		Kind: "tool", Action: "search_transcript", ToolName: "search_transcript",
		ArgumentsDigest: "args", ReplaySafe: true,
	}, func() (AgentJournalResult, error) {
		invoked = true
		return AgentJournalResult{}, nil
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if invoked || !result.Replayed || string(result.Checkpoint) != `{"answer":"saved"}` {
		t.Fatalf("matching replay = %+v, invoked=%v", result, invoked)
	}
	if len(store.completed) != 0 || len(store.failed) != 0 {
		t.Fatalf("replay wrote terminal state: completed=%d failed=%d", len(store.completed), len(store.failed))
	}
}

func TestAgentExecutionJournalOwnsLeaseCompletionAndReplaySafeRetry(t *testing.T) {
	store := &scriptedAgentExecutionStore{claims: []repository.AgentStepClaim{
		{Outcome: repository.AgentStepClaimTerminal, Step: model.AgentStep{StepID: "funnel-context", Attempt: 1, Status: model.AgentStepStatusFailed, ReplaySafe: true}},
		{Outcome: repository.AgentStepClaimAcquired},
	}}
	journal := NewAgentExecutionJournal(store)
	journal.now = func() time.Time { return time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC) }
	journal.newLeaseToken = func() string { return "lease-2" }

	result, err := journal.Execute(context.Background(), AgentJournalStep{
		UserID: 7, RunID: "run-2", StepID: "funnel-context", Sequence: 1,
		Kind: "retrieve", Action: "browse_context", ToolName: "browse_context",
		ArgumentsDigest: "args", ReplaySafe: true, RetryReplaySafe: true,
	}, func() (AgentJournalResult, error) {
		return AgentJournalResult{Checkpoint: map[string]any{"ok": true}, OutputRef: "summary:true"}, nil
	})

	if err != nil || result.Replayed || result.BudgetExhausted {
		t.Fatalf("Execute() = %+v, %v", result, err)
	}
	if len(store.claimInputs) != 2 || store.claimInputs[1].Attempt != 2 || store.claimInputs[1].LeaseToken != "lease-2" {
		t.Fatalf("retry claims = %+v", store.claimInputs)
	}
	if len(store.completed) != 1 || store.completed[0].Attempt != 2 || store.completed[0].LeaseToken != "lease-2" || store.completed[0].ResultCheckpoint != `{"ok":true}` {
		t.Fatalf("completion = %+v", store.completed)
	}
}
