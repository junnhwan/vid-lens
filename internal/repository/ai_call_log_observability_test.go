package repository

import (
	"testing"

	"vid-lens/internal/model"
)

func TestAICallLogNullableUsageAndCorrelationQueries(t *testing.T) {
	repo := newAICallLogTestRepo(t)
	log := &model.AICallLog{
		UserID: 7, TaskID: 42, JobID: 8, TraceID: "trace-1", JobType: "analyze", Stage: "summarizing", Attempt: 2,
		Kind: model.AICallKindLLM, Provider: "mimo", ModelName: "mimo-v2.5", Status: model.AICallStatusSuccess,
		Currency: "CNY", PriceVersion: "2026-07", ProviderRequestID: "request-1",
	}
	if err := repo.Create(log); err != nil {
		t.Fatal(err)
	}
	byTrace, err := repo.ListByTraceID("trace-1", 10)
	if err != nil || len(byTrace) != 1 {
		t.Fatalf("ListByTraceID logs=%+v err=%v", byTrace, err)
	}
	byTask, err := repo.ListByTaskID(42, 10)
	if err != nil || len(byTask) != 1 {
		t.Fatalf("ListByTaskID logs=%+v err=%v", byTask, err)
	}
	got := byTrace[0]
	if got.PromptTokens != nil || got.CompletionTokens != nil || got.TotalTokens != nil || got.EstimatedCost != nil {
		t.Fatalf("unknown usage persisted as value: %+v", got)
	}
	if got.JobID != 8 || got.Stage != "summarizing" || got.Attempt != 2 || got.ProviderRequestID != "request-1" || got.PriceVersion != "2026-07" {
		t.Fatalf("audit fields=%+v", got)
	}
}
