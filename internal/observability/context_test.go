package observability

import (
	"context"
	"testing"
)

func TestCorrelationRoundTripAndMerge(t *testing.T) {
	base := WithCorrelation(context.Background(), Correlation{
		TraceID: "trace-1", TaskID: 42, JobID: 9, UserID: 7,
		JobType: "transcribe", Stage: "transcribing", Attempt: 2,
	})
	got := CorrelationFromContext(base)
	want := (Correlation{TraceID: "trace-1", TaskID: 42, JobID: 9, UserID: 7, JobType: "transcribe", Stage: "transcribing", Attempt: 2})
	if got != want {
		t.Fatalf("correlation = %+v, want %+v", got, want)
	}

	merged := CorrelationFromContext(WithCorrelation(base, Correlation{Stage: "summarizing"}))
	if merged.TraceID != "trace-1" || merged.TaskID != 42 || merged.JobID != 9 || merged.UserID != 7 || merged.Attempt != 2 {
		t.Fatalf("zero values overwrote existing correlation: %+v", merged)
	}
	if merged.Stage != "summarizing" {
		t.Fatalf("stage = %q, want summarizing", merged.Stage)
	}
}

func TestNilContextReturnsEmptyCorrelation(t *testing.T) {
	//lint:ignore SA1012 this test documents the defensive nil-context behavior.
	if got := CorrelationFromContext(nil); got != (Correlation{}) {
		t.Fatalf("correlation = %+v, want empty", got)
	}
}
