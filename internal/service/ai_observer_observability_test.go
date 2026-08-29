package service

import (
	"context"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

func TestAIObserverPersistsNullableUsageAndSanitizedCorrelation(t *testing.T) {
	repos := newAIObserverTestRepositories(t)
	observer := NewAIObserver(repos)
	ctx := observability.WithCorrelation(context.Background(), observability.Correlation{TraceID: "trace-1", TaskID: 42, JobID: 8, UserID: 7, JobType: "analyze", Stage: "summarizing", Attempt: 2})
	err := observer.RecordAICall(ctx, ai.CallRecord{
		Kind: model.AICallKindLLM, Provider: "mimo", Model: "mimo-v2.5", Status: model.AICallStatusFailed,
		ErrorCode: "rate_limited", ErrorMsg: "Authorization: Bearer secret-token api_key=sk-secret provider body",
	})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := repos.AICallLog.ListByTraceID("trace-1", 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	got := logs[0]
	if got.UserID != 7 || got.TaskID != 42 || got.JobID != 8 || got.Stage != "summarizing" || got.Attempt != 2 {
		t.Fatalf("correlation=%+v", got)
	}
	if got.PromptTokens != nil || got.CompletionTokens != nil || got.TotalTokens != nil || got.EstimatedCost != nil {
		t.Fatalf("unknown usage=%+v", got)
	}
	if got.ErrorMsg == "" || got.ErrorMsg == "Authorization: Bearer secret-token api_key=sk-secret provider body" {
		t.Fatalf("error not sanitized: %q", got.ErrorMsg)
	}
}
