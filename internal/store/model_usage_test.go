package store

import (
	"testing"

	"vidlens/internal/domain"
)

func token(value int64) *int64 { return &value }

func TestAttachModelUsageSplitsSendsAndTotalsCalls(t *testing.T) {
	steps := []domain.Step{
		{Kind: "model_response", ModelCall: 1},
		{Kind: "model_submission", ModelCall: 1, Submission: 1},
		{Kind: "model_submission", ModelCall: 1, Submission: 2},
		{Kind: "model_response", ModelCall: 2},
		{Kind: "model_submission", ModelCall: 2, Submission: 1},
	}
	attachModelUsage(steps, []modelUsageRow{
		{OperationKey: "model:0", Submission: 1, Input: token(1200), Output: token(300)},
		{OperationKey: "model:0", Submission: 2, Input: token(800), Output: token(200)},
		{OperationKey: "model:1", Submission: 1, Input: token(500), Output: token(90)},
	})
	if steps[0].Usage == nil || steps[0].Usage.Input != 2000 || steps[0].Usage.Output != 500 {
		t.Fatalf("call total = %+v, want 2000/500", steps[0].Usage)
	}
	if steps[1].Usage == nil || steps[1].Usage.Input != 1200 || steps[1].Usage.Output != 300 {
		t.Fatalf("first send = %+v, want 1200/300", steps[1].Usage)
	}
	if steps[2].Usage == nil || steps[2].Usage.Input != 800 || steps[2].Usage.Output != 200 {
		t.Fatalf("second send = %+v, want 800/200", steps[2].Usage)
	}
	if steps[3].Usage == nil || steps[3].Usage.Input != 500 || steps[3].Usage.Output != 90 {
		t.Fatalf("second call = %+v, want 500/90", steps[3].Usage)
	}
	if steps[4].Usage == nil || steps[4].Usage.Input != 500 {
		t.Fatalf("second call send = %+v, want 500 input", steps[4].Usage)
	}
}

func TestAttachModelUsageHidesPartialTotals(t *testing.T) {
	steps := []domain.Step{
		{Kind: "model_response", ModelCall: 3},
		{Kind: "model_submission", ModelCall: 3, Submission: 1},
		{Kind: "model_submission", ModelCall: 3, Submission: 2},
	}
	attachModelUsage(steps, []modelUsageRow{
		{OperationKey: "model:2", Submission: 1, Input: token(100), Output: token(20)},
		{OperationKey: "model:2", Submission: 2, Input: token(50)},
	})
	if steps[0].Usage != nil {
		t.Fatalf("partial call total leaked: %+v", steps[0].Usage)
	}
	if steps[1].Usage == nil || steps[1].Usage.Input != 100 {
		t.Fatalf("complete send = %+v, want 100 input", steps[1].Usage)
	}
	if steps[2].Usage != nil {
		t.Fatalf("incomplete send leaked: %+v", steps[2].Usage)
	}
}

func TestAttachModelUsageSkipsUnsettledSteps(t *testing.T) {
	steps := []domain.Step{
		{Kind: "model_response", ModelCall: 1, Status: "running"},
		{Kind: "model_submission", ModelCall: 1, Submission: 1, Status: "running"},
	}
	attachModelUsage(steps, []modelUsageRow{
		{OperationKey: "model:0", Submission: 1, Input: token(100), Output: token(20)},
	})
	if steps[0].Usage != nil || steps[1].Usage != nil {
		t.Fatal("usage attached to an unsettled step")
	}
}

func TestAttachModelUsageIgnoresLegacyOrForeignSteps(t *testing.T) {
	steps := []domain.Step{
		{Kind: "model_response", ModelCall: 1},
		{Kind: "model_submission", ModelCall: 1, Submission: 1},
		{Kind: "tool_execution"},
	}
	attachModelUsage(steps, nil)
	for i, step := range steps {
		if step.Usage != nil {
			t.Fatalf("step %d gained usage without rows: %+v", i, step.Usage)
		}
	}
	attachModelUsage(steps, []modelUsageRow{
		{OperationKey: "candidate:1", Submission: 1, Input: token(9), Output: token(1)},
	})
	if steps[0].Usage != nil || steps[1].Usage != nil {
		t.Fatal("non-model operation leaked into step usage")
	}
}
