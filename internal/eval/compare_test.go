package eval

import (
	"strings"
	"testing"
)

func TestAnalyzePairedRunArtifactsIncludesFailedCasesAndGuardrails(t *testing.T) {
	minimum := 0.01
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
		BaselineVariant: "vector-only", PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher,
		MinimumEffect: &minimum, Bootstrap: BootstrapConfig{Iterations: 1000, ConfidenceLevel: 0.95, Seed: 7},
		Guardrails: []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 0.05}},
		Candidates: []CandidateVariant{{VariantID: "hybrid", Commit: "abc", ConfigSHA256: strings.Repeat("a", 64)}},
	}}}
	baseMeta := validRunMetadata()
	baseMeta.DatasetVersion = "rag-v1"
	baseMeta.VariantID = "vector-only"
	candidateMeta := baseMeta
	candidateMeta.VariantID = "hybrid"
	baseline := RunArtifact{Metadata: baseMeta, Summary: MetricReport{Overall: MetricResult{AnswerabilityF1: 0.8}}, Cases: []CaseArtifact{
		{CaseID: "a", Metric: CaseMetric{CaseID: "a", VideoID: "v1", SourceGroup: "g1", NDCGAtK: 0.4}},
		{CaseID: "b", Metric: CaseMetric{CaseID: "b", VideoID: "v2", SourceGroup: "g2", Failed: true}},
	}}
	candidate := RunArtifact{Metadata: candidateMeta, Summary: MetricReport{Overall: MetricResult{AnswerabilityF1: 0.81}}, Cases: []CaseArtifact{
		{CaseID: "a", Metric: CaseMetric{CaseID: "a", VideoID: "v1", SourceGroup: "g1", NDCGAtK: 0.7}},
		{CaseID: "b", Metric: CaseMetric{CaseID: "b", VideoID: "v2", SourceGroup: "g2", Failed: true}},
	}}

	analysis, err := AnalyzePairedRunArtifacts(registry, "exp-1", "hybrid", baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Bootstrap.CaseCount != 2 || analysis.Bootstrap.ClusterCount != 2 {
		t.Fatalf("bootstrap = %+v, failed case must remain paired", analysis.Bootstrap)
	}
	if got := analysis.GuardrailEffects["answerability_f1"]; got < 0.009 || got > 0.011 {
		t.Fatalf("answerability guardrail effect = %v", got)
	}
}

func TestAnalyzePairedRunArtifactsRejectsMissingOrDriftedCases(t *testing.T) {
	minimum := 0.01
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
		BaselineVariant: "base", PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher,
		MinimumEffect: &minimum, Bootstrap: BootstrapConfig{Iterations: 100, ConfidenceLevel: 0.95, Seed: 1},
		Guardrails: []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 0.1}},
		Candidates: []CandidateVariant{{VariantID: "candidate", Commit: "abc", ConfigSHA256: strings.Repeat("a", 64)}},
	}}}
	baseline := RunArtifact{Cases: []CaseArtifact{{CaseID: "a", Metric: CaseMetric{CaseID: "a", VideoID: "v", SourceGroup: "g"}}}}
	candidate := RunArtifact{Cases: nil}
	if _, err := AnalyzePairedRunArtifacts(registry, "exp-1", "candidate", baseline, candidate); err == nil || !strings.Contains(err.Error(), "case set") {
		t.Fatalf("error = %v, want case set mismatch", err)
	}
}
