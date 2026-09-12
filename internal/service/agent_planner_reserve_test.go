package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

type reserveTestPlanner struct {
	reason string
	calls  int
	cancel context.CancelFunc
}

func (p *reserveTestPlanner) NextDecision(ctx context.Context, s VideoAgentLoopState, d []VideoAgentToolDefinition) (VideoAgentLoopDecision, error) {
	v, _, e := p.NextDecisionWithUsage(ctx, s, d)
	return v, e
}
func (p *reserveTestPlanner) NextDecisionWithUsage(ctx context.Context, _ VideoAgentLoopState, _ []VideoAgentToolDefinition) (VideoAgentLoopDecision, VideoAgentLoopPlannerCallUsage, error) {
	p.calls++
	u := VideoAgentLoopPlannerCallUsage{PromptTokens: 123, CompletionTokens: 45, UsageSource: model.AgentCallUsageActual}
	if p.cancel != nil {
		p.cancel()
	}
	if p.reason == "deadline" || p.reason == "cancel" {
		<-ctx.Done()
		return VideoAgentLoopDecision{}, u, ctx.Err()
	}
	return VideoAgentLoopDecision{}, u, &ai.ChatFinishError{Reason: p.reason, PartialContent: `{"tool":"unsafe_partial`}
}

func TestPlannerLimitsRouteToDurableFinalWithoutReplayingModel(t *testing.T) {
	for _, reason := range []string{"deadline", "length", "content_filter", "cancel"} {
		t.Run(reason, func(t *testing.T) {
			repos, task, session := newVideoAgentTestSession(t)
			run := &model.AgentRun{ID: reason, UserID: 7, SessionID: session.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID, Goal: "compare both sources", Mode: "research", AgentProfile: "default", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{"schema_version":1,"reserve_duration_ms":1900,"reserve_output_tokens":100}`, Status: model.AgentRunStatusRunning, MaxSteps: 7, MaxToolCalls: 3, MaxLLMCalls: 4, MaxAttemptsPerStep: 2, MaxDurationMs: 2000, MaxPromptTokens: 100000, MaxCompletionTokens: 100000, CreatedAt: time.Now()}
			if _, err := repos.AgentExecution.CreateRun(context.Background(), run); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			planner := &reserveTestPlanner{reason: reason}
			if reason == "cancel" {
				planner.cancel = cancel
			}
			registry := NewVideoAgentTools(nil, nil, nil).Registry()
			runner, err := NewVideoAgentLoopRunner(registry, planner, DefaultVideoAgentLoopObserver{}, VideoAgentLoopPolicy{MaxSteps: 3, MaxReplans: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := runner.SetDurableExecution(NewAgentExecutionJournal(repos.AgentExecution), 7, run.ID); err != nil {
				t.Fatal(err)
			}
			state, _ := NewVideoAgentLoopState(run.Goal, VideoAgentLoopPolicy{MaxSteps: 3, MaxReplans: 1})
			state.Evidence = []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "source", Content: "verified source"}}
			runtime := VideoAgentToolRuntime{UserID: 7, TaskID: task.ID}
			decision, exhausted, err := runner.nextResearchDecisionCheckpoint(ctx, state, runtime)
			if reason == "content_filter" || reason == "cancel" {
				if err == nil {
					t.Fatal("unrelated failure converted to final")
				}
				return
			}
			if err != nil || exhausted || decision.Tool != VideoAgentToolBuildCitedAnswer || decision.BudgetNotice == nil {
				t.Fatalf("decision=%+v exhausted=%v err=%v", decision, exhausted, err)
			}
			if ctx.Err() != nil {
				t.Fatal("final writer parent context was cancelled")
			}
			records, err := repos.AgentExecution.GetExecution(ctx, 7, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if records.Run.PromptTokensUsed != 123 || records.Run.CompletionTokensUsed != 45 || records.Run.LLMCallsUsed != 1 || records.Run.Status != model.AgentRunStatusRunning {
				t.Fatalf("usage/terminal lost: %+v", records.Run)
			}
			replay, _, err := runner.nextResearchDecisionCheckpoint(ctx, state, runtime)
			if err != nil || replay.BudgetNotice == nil || planner.calls != 1 {
				t.Fatalf("replay=%+v err=%v calls=%d", replay, err, planner.calls)
			}
			if errors.Is(context.Cause(ctx), errPlannerFinalReserve) {
				t.Fatal("reserve propagated to run")
			}
		})
	}
}
