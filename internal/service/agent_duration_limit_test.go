package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

func TestJournalDistinguishesRunDurationFromCallerCancellation(t *testing.T) {
	for _, budgetExpiry := range []bool{true, false} {
		name := "caller_cancelled"
		if budgetExpiry {
			name = "run_duration"
		}
		t.Run(name, func(t *testing.T) {
			repos, task, session := newVideoAgentTestSession(t)
			run := &model.AgentRun{ID: name, UserID: 7, SessionID: session.ID, ScopeType: model.ChatScopeVideo, TaskID: task.ID, Goal: "deadline", Mode: "research", AgentProfile: "default", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, BudgetSnapshot: `{}`, Status: model.AgentRunStatusRunning, MaxSteps: 3, MaxToolCalls: 2, MaxLLMCalls: 2, MaxAttemptsPerStep: 2, CreatedAt: time.Now()}
			if _, err := repos.AgentExecution.CreateRun(context.Background(), run); err != nil {
				t.Fatal(err)
			}
			base, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			ctx := base
			cancelBudget := func() {}
			if budgetExpiry {
				ctx, cancelBudget = context.WithDeadlineCause(base, time.Now().Add(50*time.Millisecond), errAgentRunDurationLimit)
			}
			defer cancelBudget()
			journal := NewAgentExecutionJournal(repos.AgentExecution)
			_, err := journal.Execute(ctx, AgentJournalStep{UserID: 7, RunID: name, StepID: "tool-1", Sequence: 1, Kind: "tool", Action: "answer", ToolName: "answer", ArgumentsDigest: digestAgentValue("{}"), LLMCall: true, EstimatedPromptTokens: 10}, func() (AgentJournalResult, error) {
				if !budgetExpiry {
					cancelCaller()
				}
				<-ctx.Done()
				return AgentJournalResult{Usage: VideoAgentLoopPlannerCallUsage{PromptTokens: 123, CompletionTokens: 45, UsageSource: model.AgentCallUsageActual}}, ctx.Err()
			})
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected interruption: %v", err)
			}
			records, err := repos.AgentExecution.GetExecution(context.Background(), 7, name)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantReason := model.AgentRunStatusCancelled, "request_cancelled"
			if budgetExpiry {
				wantStatus, wantReason = model.AgentRunStatusBudgetExhausted, "duration_limit"
			}
			if records.Run.Status != wantStatus || records.Run.StopReason != wantReason {
				t.Fatalf("wrong terminal: %+v", records.Run)
			}
			if records.Run.PromptTokensUsed != 123 || records.Run.CompletionTokensUsed != 45 || len(records.ToolCalls) != 1 || records.ToolCalls[0].UsageSource != model.AgentCallUsageActual {
				t.Fatalf("lost actual usage: %+v", records)
			}
		})
	}
}

type interruptedDurationChat struct{ cancel context.CancelFunc }

func (c interruptedDurationChat) Chat(ctx context.Context, _ []ai.ChatMessage) (string, error) {
	if c.cancel != nil {
		c.cancel()
	}
	<-ctx.Done()
	return "partial answer", ctx.Err()
}

func TestRunAgentPersistsOwnDurationAndCallerCancellationSeparately(t *testing.T) {
	for _, budgetExpiry := range []bool{true, false} {
		name := "caller_cancelled"
		if budgetExpiry {
			name = "run_duration"
		}
		t.Run(name, func(t *testing.T) {
			repos, _, session := newVideoAgentTestSession(t)
			chat := NewChatService(repos, &fakeRetriever{}, ChatConfig{TopK: 1})
			agent := NewVideoAgentService(chat)
			cfg := config.DefaultAgentBudgetConfig()
			effective, err := cfg.Resolve(nil)
			if err != nil {
				t.Fatal(err)
			}
			// Keep the fixture fast without changing server configuration or budget validation.
			effective.Values.MaxDurationSeconds = 1
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := context.WithValue(base, resolvedBudgetContextKey{}, &ResolvedConversationProfile{EffectiveAgentBudget: effective})
			client := interruptedDurationChat{}
			if !budgetExpiry {
				client.cancel = cancel
			}
			_, err = agent.RunAgent(ctx, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "deadline fixture", RunID: name}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"})
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected context interruption: %v", err)
			}
			run, err := repos.AgentExecution.GetRun(context.Background(), 7, name)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantReason := model.AgentRunStatusCancelled, "request_cancelled"
			if budgetExpiry {
				wantStatus, wantReason = model.AgentRunStatusBudgetExhausted, "duration_limit"
			}
			if run.Status != wantStatus || run.StopReason != wantReason {
				t.Fatalf("wrong terminal: %+v", run)
			}
			messages, err := repos.Chat.ListMessages(7, session.ID)
			if err != nil || len(messages) != 0 {
				t.Fatalf("interrupted answer persisted: %+v %v", messages, err)
			}
		})
	}
}
