package main

import (
	"context"
	"encoding/json"
	"time"

	"vid-lens/internal/eval"
	"vid-lens/internal/model"
	"vid-lens/internal/service"
)

// conversationProductExecutor uses the application's execution object unchanged;
// fixtures may replace providers, never the runner, budget, memory or persistence.
func conversationProductExecutor(execution *service.ConversationExecution, readRun func(context.Context, int64, string) (*model.AgentRun, error)) eval.ProductExecutor {
	return func(ctx context.Context, turn eval.ProductTurn) (eval.ProductObservation, error) {
		start := time.Now()
		ob := eval.ProductObservation{Usage: eval.ProductUsage{Source: "unknown"}}
		req := service.ConversationRequest{Kind: service.ConversationKind(turn.Kind), UserID: turn.UserID, SessionID: turn.SessionID, Question: turn.Question, TopK: turn.TopK, RunID: turn.RunID}
		var result service.ConversationResult
		var err error
		if turn.Stream {
			result, err = execution.Stream(ctx, req, func(event service.ConversationStreamEvent) error {
				elapsed := float64(time.Since(start).Microseconds()) / 1000
				if event.Type == "progress" && ob.FirstProgressMS == nil {
					ob.FirstProgressMS = &elapsed
				}
				if (event.Type == "answer" || event.Type == "delta") && ob.FirstAnswerMS == nil {
					ob.FirstAnswerMS = &elapsed
				}
				return nil
			})
		} else {
			result, err = execution.Execute(ctx, req)
		}
		if a := result.Agent; a != nil {
			ob.RunID, ob.MessageID, ob.Answer, ob.Model, ob.Degraded = a.RunID, a.MessageID, a.Answer, a.Model, a.Degraded
			ob.Citations, _ = json.Marshal(a.Citations)
		}
		if a := result.Chat; a != nil {
			ob.MessageID, ob.Answer, ob.Model, ob.Degraded = a.MessageID, a.Answer, a.Model, a.Degraded
			ob.Citations, _ = json.Marshal(a.Citations)
		}
		if err == nil && ob.MessageID > 0 {
			ob.Status = "completed"
		}
		if ob.RunID != "" {
			ob.Status = "unknown"
			if readRun != nil {
				run, readErr := readRun(ctx, turn.UserID, ob.RunID)
				if readErr == nil && run != nil {
					ob.Status, ob.StopReason = run.Status, run.StopReason
					if run.TokenUsageSource != "" && run.TokenUsageSource != "unknown" {
						ob.Usage = eval.ProductUsage{Source: run.TokenUsageSource, PromptTokens: &run.PromptTokensUsed, CompletionTokens: &run.CompletionTokensUsed}
					}
				}
			}
		}
		return ob, err
	}
}
