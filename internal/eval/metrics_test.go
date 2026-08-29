package eval

import (
	"math"
	"testing"
)

func TestMetricsRecallMRRAndNDCG(t *testing.T) {
	c := metricCase()
	c.EvidenceRanges = []EvidenceRange{
		{ID: "ev-high", GroupID: "g-high", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR, Relevance: 3},
		{ID: "ev-low", GroupID: "g-low", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR, Relevance: 1},
	}
	result := EvaluationCaseResult{
		Case: c,
		Retrieved: []RetrievedContext{
			{ContextID: "low", VideoID: "video-1", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR},
			{ContextID: "high", VideoID: "video-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR},
		},
		PredictedAnswerable: true,
	}

	report, err := EvaluateMetrics([]EvaluationCaseResult{result}, MetricConfig{K: 2, BoundaryToleranceMS: 500, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Overall.RecallAtK != 1 || report.Overall.MRR != 1 {
		t.Fatalf("Recall/MRR = %.3f/%.3f, want 1/1", report.Overall.RecallAtK, report.Overall.MRR)
	}
	wantNDCG := (1 + 7/math.Log2(3)) / (7 + 1/math.Log2(3))
	if math.Abs(report.Overall.NDCGAtK-wantNDCG) > 1e-9 {
		t.Fatalf("nDCG = %.12f, want %.12f", report.Overall.NDCGAtK, wantNDCG)
	}
}

func TestMetricsContextPrecisionDeduplicatesOverlappingEvidence(t *testing.T) {
	c := metricCase()
	c.EvidenceRanges = []EvidenceRange{
		{ID: "ev-1", GroupID: "g-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR, Relevance: 3},
		{ID: "ev-2", GroupID: "g-2", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR, Relevance: 2},
	}
	result := EvaluationCaseResult{Case: c, PredictedAnswerable: true, Retrieved: []RetrievedContext{
		{ContextID: "first", VideoID: "video-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR},
		{ContextID: "noise", VideoID: "video-1", StartMS: 30_000, EndMS: 31_000, Source: EvidenceSourceASR},
		{ContextID: "duplicate", VideoID: "video-1", StartMS: 10_100, EndMS: 11_900, Source: EvidenceSourceASR},
		{ContextID: "second", VideoID: "video-1", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR},
	}}

	report, err := EvaluateMetrics([]EvaluationCaseResult{result}, MetricConfig{K: 4, BoundaryToleranceMS: 0, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	// Relevant first occurrences are at ranks 1 and 4: (1/1 + 2/4) / 2 = 0.75.
	if math.Abs(report.Overall.ContextPrecisionAtK-0.75) > 1e-9 {
		t.Fatalf("ContextPrecision@K = %.3f, want 0.75", report.Overall.ContextPrecisionAtK)
	}
}

func TestMetricsCompleteEvidenceRecallRequiresEveryGroup(t *testing.T) {
	c := metricCase()
	c.EvidenceRanges = []EvidenceRange{
		{ID: "ev-a1", GroupID: "claim-a", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR, Relevance: 3},
		{ID: "ev-a2", GroupID: "claim-a", StartMS: 13_000, EndMS: 15_000, Source: EvidenceSourceASR, Relevance: 3},
		{ID: "ev-b", GroupID: "claim-b", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR, Relevance: 2},
	}

	tests := []struct {
		name      string
		retrieved []RetrievedContext
		want      float64
	}{
		{name: "one alternative from every group", retrieved: []RetrievedContext{
			{ContextID: "a", VideoID: "video-1", StartMS: 13_000, EndMS: 15_000, Source: EvidenceSourceASR},
			{ContextID: "b", VideoID: "video-1", StartMS: 20_000, EndMS: 22_000, Source: EvidenceSourceASR},
		}, want: 1},
		{name: "missing one required group", retrieved: []RetrievedContext{
			{ContextID: "a", VideoID: "video-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR},
		}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := EvaluateMetrics([]EvaluationCaseResult{{Case: c, Retrieved: tt.retrieved, PredictedAnswerable: true}}, MetricConfig{K: 5, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
			if err != nil {
				t.Fatalf("EvaluateMetrics() error = %v", err)
			}
			if report.Overall.CompleteEvidenceRecall != tt.want {
				t.Fatalf("CompleteEvidenceRecall = %.1f, want %.1f", report.Overall.CompleteEvidenceRecall, tt.want)
			}
		})
	}
}

func TestMetricsLongChunkCannotWinByCoveringWholeVideo(t *testing.T) {
	c := metricCase()
	result := EvaluationCaseResult{
		Case:                c,
		Retrieved:           []RetrievedContext{{ContextID: "whole-video", VideoID: "video-1", StartMS: 0, EndMS: 600_000, Source: EvidenceSourceASR}},
		PredictedAnswerable: true,
	}

	report, err := EvaluateMetrics([]EvaluationCaseResult{result}, MetricConfig{K: 1, BoundaryToleranceMS: 1_000, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Overall.RecallAtK != 0 || report.Overall.MRR != 0 || report.Overall.NDCGAtK != 0 || report.Overall.CompleteEvidenceRecall != 0 {
		t.Fatalf("metrics = %+v, want no hit for overlong context", report.Overall)
	}
}

func TestMetricsBoundaryToleranceAndSourceArePreRegistered(t *testing.T) {
	c := metricCase()
	c.EvidenceRanges[0].Source = EvidenceSourceOCR
	result := EvaluationCaseResult{Case: c, PredictedAnswerable: true, Retrieved: []RetrievedContext{
		{ContextID: "wrong-source", VideoID: "video-1", StartMS: 10_500, EndMS: 12_500, Source: EvidenceSourceASR},
		{ContextID: "within-tolerance", VideoID: "video-1", StartMS: 10_500, EndMS: 12_500, Source: EvidenceSourceBoth},
	}}

	report, err := EvaluateMetrics([]EvaluationCaseResult{result}, MetricConfig{K: 2, BoundaryToleranceMS: 500, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 1})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Cases[0].FirstRelevantRank != 2 {
		t.Fatalf("FirstRelevantRank = %d, want 2 because ASR cannot satisfy OCR evidence", report.Cases[0].FirstRelevantRank)
	}
}

func TestMetricsAnswerabilityF1AndGroupedAggregates(t *testing.T) {
	answerableA := metricCase()
	answerableA.CaseID, answerableA.VideoID, answerableA.SourceGroup, answerableA.Category = "a", "video-a", "group-a", "direct_fact"
	answerableB := metricCase()
	answerableB.CaseID, answerableB.VideoID, answerableB.SourceGroup, answerableB.Category = "b", "video-a", "group-a", "direct_fact"
	unanswerable := metricCase()
	unanswerable.CaseID, unanswerable.VideoID, unanswerable.SourceGroup, unanswerable.Category = "c", "video-b", "group-b", "unanswerable"
	unanswerable.Answerable = false
	unanswerable.AnswerPoints = nil
	unanswerable.EvidenceRanges = nil

	hit := []RetrievedContext{{ContextID: "hit", VideoID: "video-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}}
	report, err := EvaluateMetrics([]EvaluationCaseResult{
		{Case: answerableA, Retrieved: hit, PredictedAnswerable: true}, // TP
		{Case: answerableB, PredictedAnswerable: false},                // FN
		{Case: unanswerable, PredictedAnswerable: true},                // FP
	}, MetricConfig{K: 1, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if math.Abs(report.Overall.AnswerabilityPrecision-0.5) > 1e-9 || math.Abs(report.Overall.AnswerabilityRecall-0.5) > 1e-9 || math.Abs(report.Overall.AnswerabilityF1-0.5) > 1e-9 {
		t.Fatalf("answerability = P %.3f R %.3f F1 %.3f, want 0.5 each", report.Overall.AnswerabilityPrecision, report.Overall.AnswerabilityRecall, report.Overall.AnswerabilityF1)
	}
	if len(report.ByCategory) != 2 || len(report.ByVideo) != 2 || len(report.BySourceGroup) != 2 {
		t.Fatalf("grouped reports: category=%d video=%d source=%d, want 2 each", len(report.ByCategory), len(report.ByVideo), len(report.BySourceGroup))
	}
	if report.ByVideo["video-a"].Cases != 2 || report.ByCategory["unanswerable"].Cases != 1 {
		t.Fatalf("grouped reports = %+v / %+v", report.ByVideo, report.ByCategory)
	}
}

func TestMetricsRejectsUnfrozenMatchingConfig(t *testing.T) {
	_, err := EvaluateMetrics(nil, MetricConfig{K: 5})
	if err == nil {
		t.Fatal("EvaluateMetrics() error = nil, want boundary/max-duration/coverage validation")
	}
}

func metricCase() Case {
	return Case{
		CaseID: "rag-001", VideoID: "video-1", SourceGroup: "group-1", Split: SplitDev,
		Question: "What happened?", Category: "direct_fact", Answerable: true,
		AnswerPoints:   []AnswerPoint{{ID: "ap-1", Text: "A fact.", Required: true}},
		EvidenceRanges: []EvidenceRange{{ID: "ev-1", GroupID: "g-1", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR, Relevance: 3}},
	}
}

func TestMetricsFailedAnswerableCasesRemainInRetrievalDenominator(t *testing.T) {
	failed := metricCase()
	failed.CaseID = "failed"
	failed.VideoID = "video-failed"
	failed.SourceGroup = "group-failed"
	hit := metricCase()
	hit.CaseID = "hit"
	hit.VideoID = "video-hit"
	hit.SourceGroup = "group-hit"

	report, err := EvaluateMetrics([]EvaluationCaseResult{
		{
			Case:                hit,
			Retrieved:           []RetrievedContext{{ContextID: "hit", VideoID: "video-hit", StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR}},
			PredictedAnswerable: true,
		},
		{
			Case:    failed,
			Failure: &RunFailure{Stage: "retrieval", Code: "timeout", Message: "timed out"},
		},
	}, MetricConfig{K: 1, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Overall.EvaluableCases != 2 {
		t.Fatalf("EvaluableCases = %d, want both answerable cases including failure", report.Overall.EvaluableCases)
	}
	for name, got := range map[string]float64{
		"recall":                   report.Overall.RecallAtK,
		"mrr":                      report.Overall.MRR,
		"ndcg":                     report.Overall.NDCGAtK,
		"context precision":        report.Overall.ContextPrecisionAtK,
		"complete evidence recall": report.Overall.CompleteEvidenceRecall,
	} {
		if math.Abs(got-0.5) > 1e-9 {
			t.Fatalf("%s = %.3f, want 0.5 because failed answerable case counts as zero", name, got)
		}
	}
	if math.Abs(report.Overall.FailureRate-0.5) > 1e-9 {
		t.Fatalf("FailureRate = %.3f, want 0.5", report.Overall.FailureRate)
	}
}

func TestMetricsRejectsEvidenceFromAnotherVideo(t *testing.T) {
	c := metricCase()
	report, err := EvaluateMetrics([]EvaluationCaseResult{{
		Case: c,
		Retrieved: []RetrievedContext{{
			ContextID: "same-time-wrong-video", VideoID: "video-2",
			StartMS: 10_000, EndMS: 12_000, Source: EvidenceSourceASR,
		}},
		PredictedAnswerable: true,
	}}, MetricConfig{K: 1, MaxChunkDurationMS: 10_000, MinEvidenceCoverage: 0.5})
	if err != nil {
		t.Fatalf("EvaluateMetrics() error = %v", err)
	}
	if report.Overall.RecallAtK != 0 || report.Overall.MRR != 0 || report.Overall.NDCGAtK != 0 || report.Overall.ContextPrecisionAtK != 0 || report.Overall.CompleteEvidenceRecall != 0 {
		t.Fatalf("metrics = %+v, want no hit for context from another video", report.Overall)
	}
}

func TestStrictMetricsMatchStableContextIdentityWithoutTimestamps(t *testing.T) {
	result := EvaluationCaseResult{
		Case:                Case{CaseID: "c1", VideoID: "v1", SourceGroup: "g1", Answerable: true, EvidenceRanges: []EvidenceRange{{ID: "e1", GroupID: "g", Source: EvidenceSourceASR, Relevance: 3, ContextIDs: []string{"task_1_hash_3"}}}},
		Retrieved:           []RetrievedContext{{ContextID: "task_1_hash_3", VideoID: "v1", Source: EvidenceSourceASR, Text: "真实片段"}},
		PredictedAnswerable: true,
	}
	report, err := EvaluateMetrics([]EvaluationCaseResult{result}, validMetricConfig())
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall.RecallAtK != 1 || report.Overall.MRR != 1 || report.Overall.NDCGAtK != 1 {
		t.Fatalf("metrics = %+v, want identity hit", report.Overall)
	}
}
