package eval

import (
	"strconv"
	"testing"
)

// TestRetrieveLatencyP95IncludesFailuresAsZero enforces the spec's anti-overclaim
// P95 rule: executor-failed answerable cases count their RetrieveLatencyMS as 0
// and stay in the P95 sample (excluding failures would make P95 look healthier
// the more the system fails — the same failure-hiding pattern the retrieval
// denominator rule forbids). A companion SuccessP95RetrieveLatencyMS reports
// the successful-retrieval subset only, so both numbers are visible.
func TestRetrieveLatencyP95IncludesFailuresAsZero(t *testing.T) {
	cfg := MetricConfig{K: 5, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5}
	// 10 cases: 8 successful with latencies 1..8 ms, 2 failed (latency 0 by convention).
	results := make([]EvaluationCaseResult, 0, 10)
	for i := 1; i <= 8; i++ {
		c := metricCase()
		c.CaseID = "ok-" + strconv.Itoa(i)
		c.VideoID = "video-ok"
		c.SourceGroup = "group-ok"
		results = append(results, EvaluationCaseResult{
			Case: c, RetrieveLatencyMS: int64(i),
			Retrieved: []RetrievedContext{{ContextID: "ev", VideoID: "video-ok",
				StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}},
			PredictedAnswerable: true,
		})
	}
	for i := 1; i <= 2; i++ {
		c := metricCase()
		c.CaseID = "fail-" + strconv.Itoa(i)
		c.VideoID = "video-fail"
		c.SourceGroup = "group-fail"
		results = append(results, EvaluationCaseResult{
			Case: c, Failure: &RunFailure{Stage: "retrieval", Code: "timeout", Message: "timed out"},
		})
	}

	report, err := EvaluateMetrics(results, cfg)
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}

	// Full-sample P95 over [0,0,1,2,3,4,5,6,7,8] using nearest-rank (ceil(0.95*n)-1).
	// n=10 -> index ceil(9.5)-1 = 9 -> 8 ms.
	if got := report.Overall.P95RetrieveLatencyMS; got != 8 {
		t.Fatalf("P95RetrieveLatencyMS = %d, want 8 (failures as 0, nearest-rank)", got)
	}
	// Success subset P95 over [1..8], n=8 -> index ceil(0.95*8)-1 = ceil(7.6)-1 = 8-1 = 7 -> 8.
	if got := report.Overall.SuccessP95RetrieveLatencyMS; got != 8 {
		t.Fatalf("SuccessP95RetrieveLatencyMS = %d, want 8", got)
	}
	if report.Overall.FailedCases != 2 {
		t.Fatalf("FailedCases = %d, want 2", report.Overall.FailedCases)
	}
}

// TestRetrieveLatencyP95AllFailedIsZero pins the degenerate case: when every
// answerable case failed, the full-sample P95 is 0 (not "undefined" or omitted)
// so a fully-failing run cannot hide behind a missing latency number.
func TestRetrieveLatencyP95AllFailedIsZero(t *testing.T) {
	cfg := MetricConfig{K: 5, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5}
	results := make([]EvaluationCaseResult, 0, 3)
	for i := 1; i <= 3; i++ {
		c := metricCase()
		c.CaseID = "fail-" + strconv.Itoa(i)
		results = append(results, EvaluationCaseResult{
			Case: c, Failure: &RunFailure{Stage: "retrieval", Code: "timeout", Message: "x"},
		})
	}
	report, err := EvaluateMetrics(results, cfg)
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Overall.P95RetrieveLatencyMS != 0 {
		t.Fatalf("P95RetrieveLatencyMS = %d, want 0 when all failed", report.Overall.P95RetrieveLatencyMS)
	}
	if report.Overall.SuccessP95RetrieveLatencyMS != 0 {
		t.Fatalf("SuccessP95RetrieveLatencyMS = %d, want 0 when no successful retrieval", report.Overall.SuccessP95RetrieveLatencyMS)
	}
}

// TestRetrieveLatencyPropagatedIntoCaseMetric guarantees the per-case latency is
// surfaced on CaseMetric so case-level JSONL traces carry it for audit.
func TestRetrieveLatencyPropagatedIntoCaseMetric(t *testing.T) {
	cfg := MetricConfig{K: 1, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5}
	c := metricCase()
	report, err := EvaluateMetrics([]EvaluationCaseResult{{
		Case: c, RetrieveLatencyMS: 17,
		Retrieved: []RetrievedContext{{ContextID: "ev", VideoID: "video-1",
			StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}},
		PredictedAnswerable: true,
	}}, cfg)
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if len(report.Cases) != 1 || report.Cases[0].RetrieveLatencyMS != 17 {
		t.Fatalf("case latency not propagated: %+v", report.Cases)
	}
}

// TestRetrieveLatencyAggregatesByGroup verifies P95 is computed within each
// ByCategory/ByVideo/BySourceGroup bucket, matching the existing aggregation shape.
func TestRetrieveLatencyAggregatesByGroup(t *testing.T) {
	cfg := MetricConfig{K: 5, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5}
	mk := func(id, group string, ms int64, fail bool) EvaluationCaseResult {
		c := metricCase()
		c.CaseID = id
		c.SourceGroup = group
		c.VideoID = group // keep video bucket == group for simplicity
		r := EvaluationCaseResult{Case: c, RetrieveLatencyMS: ms}
		if fail {
			r.Failure = &RunFailure{Stage: "retrieval", Code: "timeout", Message: "x"}
			r.RetrieveLatencyMS = 0
		} else {
			r.Retrieved = []RetrievedContext{{ContextID: "ev", VideoID: c.VideoID,
				StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}}
			r.PredictedAnswerable = true
		}
		return r
	}
	// group-a: latencies [1,2,20] -> P95 nearest-rank n=3 idx=ceil(2.85)-1=2 -> 20
	// group-b: latencies [0(fail), 5] -> full P95 over [0,5] n=2 idx=ceil(1.9)-1=1 -> 5
	report, err := EvaluateMetrics([]EvaluationCaseResult{
		mk("a1", "group-a", 1, false), mk("a2", "group-a", 2, false), mk("a3", "group-a", 20, false),
		mk("b1", "group-b", 0, true), mk("b2", "group-b", 5, false),
	}, cfg)
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if got := report.BySourceGroup["group-a"].P95RetrieveLatencyMS; got != 20 {
		t.Fatalf("group-a P95 = %d, want 20", got)
	}
	if got := report.BySourceGroup["group-b"].P95RetrieveLatencyMS; got != 5 {
		t.Fatalf("group-b P95 = %d, want 5 (failure as 0)", got)
	}
	if got := report.BySourceGroup["group-b"].SuccessP95RetrieveLatencyMS; got != 5 {
		t.Fatalf("group-b success P95 = %d, want 5", got)
	}
	if got := report.BySourceGroup["group-a"].SuccessP95RetrieveLatencyMS; got != 20 {
		t.Fatalf("group-a success P95 = %d, want 20", got)
	}
}
