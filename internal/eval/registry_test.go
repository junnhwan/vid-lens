package eval

import (
	"strings"
	"testing"
)

func TestRegistryRequiresPreregisteredPrimaryMetricEffectGuardrailsAndCandidates(t *testing.T) {
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
	}}}
	err := registry.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	for _, want := range []string{"primary_metric", "minimum_effect", "guardrail", "candidate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Validate() error = %q, want %s", err, want)
		}
	}
}

func TestRegistryLoadsFrozenCandidateConfiguration(t *testing.T) {
	raw := []byte(`
registry_version: "1"
experiments:
  - experiment_id: exp-1
    dataset_version: rag-v1
    status: preregistered
    baseline_variant: vector-only
    primary_metric: ndcg_at_k
    direction: higher
    minimum_effect: 0.02
    bootstrap:
      iterations: 5000
      confidence_level: 0.95
      seed: 7
    guardrails:
      - metric: answerability_f1
        direction: higher
        max_regression: 0.01
    candidates:
      - variant_id: hybrid-rrf
        commit: abc123
        config_sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`)
	registry, err := LoadExperimentRegistry(raw)
	if err != nil {
		t.Fatalf("LoadExperimentRegistry() error = %v", err)
	}
	experiment, err := registry.Experiment("exp-1")
	if err != nil {
		t.Fatal(err)
	}
	if experiment.PrimaryMetric != "ndcg_at_k" || experiment.Candidates[0].VariantID != "hybrid-rrf" || experiment.Bootstrap.Seed != 7 {
		t.Fatalf("experiment = %+v", experiment)
	}
}

func TestAnalyzeExperimentReportsClusterCI_PerVideoAndFailure(t *testing.T) {
	minimum := 0.1
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
		BaselineVariant: "vector-only", PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher,
		MinimumEffect: &minimum,
		Bootstrap:     BootstrapConfig{Iterations: 2_000, ConfidenceLevel: 0.95, Seed: 7},
		Guardrails:    []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 0.01}},
		Candidates:    []CandidateVariant{{VariantID: "hybrid", Commit: "abc123", ConfigSHA256: strings.Repeat("a", 64)}},
	}}}
	observations := []PairedObservation{
		{CaseID: "a", SourceGroup: "group-a", VideoID: "video-a", Baseline: 0.5, Candidate: 0.7},
		{CaseID: "b", SourceGroup: "group-b", VideoID: "video-b", Baseline: 0.7, Candidate: 0.5},
	}

	analysis, err := AnalyzeExperiment(registry, "exp-1", "hybrid", observations, map[string]float64{"answerability_f1": -0.02})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != ExperimentStatusFailed || len(analysis.FailureReasons) < 2 {
		t.Fatalf("analysis = %+v, want failed primary+guardrail result", analysis)
	}
	if len(analysis.PerVideoEffects) != 2 || analysis.Bootstrap.Lower >= 0 || analysis.Bootstrap.Upper <= 0 {
		t.Fatalf("analysis effects/CI = %+v", analysis)
	}

	artifact := RunArtifact{Metadata: validRunMetadata(), Analysis: &analysis, Summary: MetricReport{ByVideo: map[string]MetricResult{}}}
	report := RenderMarkdownReport(artifact)
	for _, want := range []string{"FAILED", "95% cluster CI", "video-a", "guardrail"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRegistryBindsRunToFrozenDatasetCommitAndConfig(t *testing.T) {
	minimum := 0.01
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
		BaselineVariant: "vector-only", PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher,
		MinimumEffect: &minimum,
		Bootstrap:     BootstrapConfig{Iterations: 1000, ConfidenceLevel: 0.95, Seed: 7},
		Guardrails:    []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 0.01}},
		Candidates:    []CandidateVariant{{VariantID: "hybrid", Commit: "abc123", ConfigSHA256: strings.Repeat("a", 64)}},
	}}}

	if _, _, err := registry.BindRun("exp-1", "hybrid", "rag-v1", "abc123", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("BindRun() valid error = %v", err)
	}
	for _, tt := range []struct {
		name, datasetVersion, commit, configHash, want string
	}{
		{name: "dataset", datasetVersion: "rag-v2", commit: "abc123", configHash: strings.Repeat("a", 64), want: "dataset version"},
		{name: "commit", datasetVersion: "rag-v1", commit: "def456", configHash: strings.Repeat("a", 64), want: "commit"},
		{name: "config", datasetVersion: "rag-v1", commit: "abc123", configHash: strings.Repeat("b", 64), want: "config"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := registry.BindRun("exp-1", "hybrid", tt.datasetVersion, tt.commit, tt.configHash)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BindRun() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPrimaryEffectGateRequiresEntireCIToCrossMinimumEffect(t *testing.T) {
	for _, tt := range []struct {
		name      string
		direction MetricDirection
		minimum   float64
		bootstrap BootstrapResult
		want      bool
	}{
		{name: "higher observed clears but lower bound does not", direction: DirectionHigher, minimum: 0.1, bootstrap: BootstrapResult{ObservedEffect: 0.11, Lower: 0.01, Upper: 0.2}, want: false},
		{name: "higher entire interval clears", direction: DirectionHigher, minimum: 0.1, bootstrap: BootstrapResult{ObservedEffect: 0.15, Lower: 0.1, Upper: 0.2}, want: true},
		{name: "lower observed clears but upper bound does not", direction: DirectionLower, minimum: 0.1, bootstrap: BootstrapResult{ObservedEffect: -0.11, Lower: -0.2, Upper: -0.01}, want: false},
		{name: "lower entire interval clears", direction: DirectionLower, minimum: 0.1, bootstrap: BootstrapResult{ObservedEffect: -0.15, Lower: -0.2, Upper: -0.1}, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := primaryEffectPass(tt.direction, tt.minimum, tt.bootstrap); got != tt.want {
				t.Fatalf("primaryEffectPass() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRegistryBindsBaselineCandidateAndEvidenceHashes(t *testing.T) {
	minimum := 0.01
	frozen := FrozenEvidenceReference{
		CorpusSHA256:         strings.Repeat("b", 64),
		ChunkManifestSHA256:  strings.Repeat("c", 64),
		VectorArtifactSHA256: strings.Repeat("d", 64),
	}
	registry := ExperimentRegistry{RegistryVersion: "1", Experiments: []PreregisteredExperiment{{
		ExperimentID: "exp-1", DatasetVersion: "rag-v1", Status: ExperimentStatusPreregistered,
		BaselineVariant: "vector-only", BaselineConfigSHA256: strings.Repeat("e", 64), FrozenEvidence: frozen,
		PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher, MinimumEffect: &minimum,
		Bootstrap:  BootstrapConfig{Iterations: 1000, ConfidenceLevel: 0.95, Seed: 7},
		Guardrails: []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 0.01}},
		Candidates: []CandidateVariant{{VariantID: "hybrid", Commit: "abc123", ConfigSHA256: strings.Repeat("a", 64)}},
	}}}

	if _, _, err := registry.BindStrictRun("exp-1", "hybrid", "rag-v1", "abc123", strings.Repeat("e", 64), strings.Repeat("a", 64), frozen); err != nil {
		t.Fatalf("BindStrictRun() valid error = %v", err)
	}
	bad := frozen
	bad.VectorArtifactSHA256 = strings.Repeat("f", 64)
	if _, _, err := registry.BindStrictRun("exp-1", "hybrid", "rag-v1", "abc123", strings.Repeat("e", 64), strings.Repeat("a", 64), bad); err == nil || !strings.Contains(err.Error(), "vector artifact") {
		t.Fatalf("BindStrictRun() evidence error = %v", err)
	}
	if _, _, err := registry.BindStrictRun("exp-1", "hybrid", "rag-v1", "abc123", strings.Repeat("f", 64), strings.Repeat("a", 64), frozen); err == nil || !strings.Contains(err.Error(), "baseline config") {
		t.Fatalf("BindStrictRun() baseline error = %v", err)
	}
}
