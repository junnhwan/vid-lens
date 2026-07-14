package eval

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ExperimentStatus string
type MetricDirection string

const (
	ExperimentStatusPreregistered ExperimentStatus = "preregistered"
	ExperimentStatusPassed        ExperimentStatus = "passed"
	ExperimentStatusFailed        ExperimentStatus = "failed"
	DirectionHigher               MetricDirection  = "higher"
	DirectionLower                MetricDirection  = "lower"
)

type ExperimentRegistry struct {
	RegistryVersion string                    `json:"registry_version" yaml:"registry_version"`
	Experiments     []PreregisteredExperiment `json:"experiments" yaml:"experiments"`
}

type PreregisteredExperiment struct {
	ExperimentID         string                  `json:"experiment_id" yaml:"experiment_id"`
	DatasetVersion       string                  `json:"dataset_version" yaml:"dataset_version"`
	Status               ExperimentStatus        `json:"status" yaml:"status"`
	BaselineVariant      string                  `json:"baseline_variant" yaml:"baseline_variant"`
	BaselineConfigSHA256 string                  `json:"baseline_config_sha256,omitempty" yaml:"baseline_config_sha256,omitempty"`
	FrozenEvidence       FrozenEvidenceReference `json:"frozen_evidence,omitempty" yaml:"frozen_evidence,omitempty"`
	PrimaryMetric        string                  `json:"primary_metric" yaml:"primary_metric"`
	Direction            MetricDirection         `json:"direction" yaml:"direction"`
	MinimumEffect        *float64                `json:"minimum_effect" yaml:"minimum_effect"`
	Bootstrap            BootstrapConfig         `json:"bootstrap" yaml:"bootstrap"`
	Guardrails           []Guardrail             `json:"guardrails" yaml:"guardrails"`
	Candidates           []CandidateVariant      `json:"candidates" yaml:"candidates"`
}

type FrozenEvidenceReference struct {
	CorpusSHA256         string `json:"corpus_sha256" yaml:"corpus_sha256"`
	ChunkManifestSHA256  string `json:"chunk_manifest_sha256" yaml:"chunk_manifest_sha256"`
	VectorArtifactSHA256 string `json:"vector_artifact_sha256" yaml:"vector_artifact_sha256"`
}

func (r FrozenEvidenceReference) Validate() error {
	for name, value := range map[string]string{
		"corpus":          r.CorpusSHA256,
		"chunk manifest":  r.ChunkManifestSHA256,
		"vector artifact": r.VectorArtifactSHA256,
	} {
		if !isSHA256(value) {
			return fmt.Errorf("frozen %s sha256 must be a 64-character digest", name)
		}
	}
	return nil
}

type Guardrail struct {
	Metric        string          `json:"metric" yaml:"metric"`
	Direction     MetricDirection `json:"direction" yaml:"direction"`
	MaxRegression float64         `json:"max_regression" yaml:"max_regression"`
}

type CandidateVariant struct {
	VariantID    string `json:"variant_id" yaml:"variant_id"`
	Commit       string `json:"commit" yaml:"commit"`
	ConfigSHA256 string `json:"config_sha256" yaml:"config_sha256"`
}

type ExperimentAnalysis struct {
	ExperimentID          string             `json:"experiment_id"`
	CandidateID           string             `json:"candidate_id"`
	PrimaryMetric         string             `json:"primary_metric"`
	Status                ExperimentStatus   `json:"status"`
	MinimumEffect         float64            `json:"minimum_effect"`
	Bootstrap             BootstrapResult    `json:"bootstrap"`
	GuardrailEffects      map[string]float64 `json:"guardrail_effects"`
	PerVideoEffects       map[string]float64 `json:"per_video_effects"`
	PerSourceGroupEffects map[string]float64 `json:"per_source_group_effects"`
	FailureReasons        []string           `json:"failure_reasons,omitempty"`
}

func LoadExperimentRegistry(raw []byte) (ExperimentRegistry, error) {
	var registry ExperimentRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return ExperimentRegistry{}, fmt.Errorf("parse experiment registry: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return ExperimentRegistry{}, err
	}
	return registry, nil
}

func (r ExperimentRegistry) Validate() error {
	var problems []string
	if strings.TrimSpace(r.RegistryVersion) == "" {
		problems = append(problems, "missing registry_version")
	}
	ids := make(map[string]bool)
	for i, experiment := range r.Experiments {
		prefix := fmt.Sprintf("experiment %d", i+1)
		if strings.TrimSpace(experiment.ExperimentID) == "" {
			problems = append(problems, prefix+" missing experiment_id")
		}
		if ids[experiment.ExperimentID] {
			problems = append(problems, "duplicate experiment_id "+experiment.ExperimentID)
		}
		ids[experiment.ExperimentID] = true
		if strings.TrimSpace(experiment.DatasetVersion) == "" {
			problems = append(problems, prefix+" missing dataset_version")
		}
		if experiment.Status != ExperimentStatusPreregistered {
			problems = append(problems, prefix+" status must be preregistered")
		}
		if strings.TrimSpace(experiment.BaselineVariant) == "" {
			problems = append(problems, prefix+" missing baseline_variant")
		}
		if !supportedMetric(experiment.PrimaryMetric) {
			problems = append(problems, prefix+" missing or unsupported primary_metric")
		}
		if !validDirection(experiment.Direction) {
			problems = append(problems, prefix+" missing or invalid direction")
		}
		if experiment.MinimumEffect == nil || *experiment.MinimumEffect < 0 {
			problems = append(problems, prefix+" missing or invalid minimum_effect")
		}
		if err := experiment.Bootstrap.Validate(); err != nil {
			problems = append(problems, prefix+" invalid bootstrap: "+err.Error())
		}
		if len(experiment.Guardrails) == 0 {
			problems = append(problems, prefix+" missing guardrail")
		}
		for _, guardrail := range experiment.Guardrails {
			if strings.TrimSpace(guardrail.Metric) == "" || !validDirection(guardrail.Direction) || guardrail.MaxRegression < 0 {
				problems = append(problems, prefix+" invalid guardrail")
			}
		}
		if len(experiment.Candidates) == 0 {
			problems = append(problems, prefix+" missing candidate")
		}
		variants := make(map[string]bool)
		for _, candidate := range experiment.Candidates {
			if strings.TrimSpace(candidate.VariantID) == "" || strings.TrimSpace(candidate.Commit) == "" || !isSHA256(candidate.ConfigSHA256) {
				problems = append(problems, prefix+" candidate must freeze variant_id, commit, and config_sha256")
			}
			if variants[candidate.VariantID] {
				problems = append(problems, prefix+" duplicate candidate "+candidate.VariantID)
			}
			variants[candidate.VariantID] = true
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid experiment registry: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (r ExperimentRegistry) Experiment(id string) (PreregisteredExperiment, error) {
	for _, experiment := range r.Experiments {
		if experiment.ExperimentID == id {
			return experiment, nil
		}
	}
	return PreregisteredExperiment{}, fmt.Errorf("experiment %q not found", id)
}

func (r ExperimentRegistry) BindRun(experimentID, candidateID, datasetVersion, commit, configSHA256 string) (PreregisteredExperiment, CandidateVariant, error) {
	if err := r.Validate(); err != nil {
		return PreregisteredExperiment{}, CandidateVariant{}, err
	}
	experiment, err := r.Experiment(experimentID)
	if err != nil {
		return PreregisteredExperiment{}, CandidateVariant{}, err
	}
	if experiment.DatasetVersion != datasetVersion {
		return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("experiment %q freezes dataset version %q, got %q", experimentID, experiment.DatasetVersion, datasetVersion)
	}
	for _, candidate := range experiment.Candidates {
		if candidate.VariantID != candidateID {
			continue
		}
		if candidate.Commit != commit {
			return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("candidate %q freezes commit %q, got %q", candidateID, candidate.Commit, commit)
		}
		if candidate.ConfigSHA256 != configSHA256 {
			return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("candidate %q freezes config sha256 %q, got %q", candidateID, candidate.ConfigSHA256, configSHA256)
		}
		return experiment, candidate, nil
	}
	return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("candidate %q is not preregistered for experiment %q", candidateID, experimentID)
}

func AnalyzeExperiment(registry ExperimentRegistry, experimentID, candidateID string, observations []PairedObservation, guardrailEffects map[string]float64) (ExperimentAnalysis, error) {
	if err := registry.Validate(); err != nil {
		return ExperimentAnalysis{}, err
	}
	experiment, err := registry.Experiment(experimentID)
	if err != nil {
		return ExperimentAnalysis{}, err
	}
	found := false
	for _, candidate := range experiment.Candidates {
		if candidate.VariantID == candidateID {
			found = true
			break
		}
	}
	if !found {
		return ExperimentAnalysis{}, fmt.Errorf("candidate %q is not preregistered", candidateID)
	}
	bootstrap, err := PairedClusterBootstrap(observations, experiment.Bootstrap)
	if err != nil {
		return ExperimentAnalysis{}, err
	}
	analysis := ExperimentAnalysis{
		ExperimentID: experimentID, CandidateID: candidateID, PrimaryMetric: experiment.PrimaryMetric,
		Status: ExperimentStatusPassed, MinimumEffect: *experiment.MinimumEffect, Bootstrap: bootstrap,
		GuardrailEffects: guardrailEffects, PerVideoEffects: groupedEffects(observations, false),
		PerSourceGroupEffects: groupedEffects(observations, true),
	}
	primaryPass := primaryEffectPass(experiment.Direction, *experiment.MinimumEffect, bootstrap)
	if !primaryPass {
		analysis.FailureReasons = append(analysis.FailureReasons, "primary metric did not satisfy minimum effect and cluster CI gate")
	}
	for _, guardrail := range experiment.Guardrails {
		effect, ok := guardrailEffects[guardrail.Metric]
		if !ok {
			return ExperimentAnalysis{}, fmt.Errorf("missing observed guardrail %q", guardrail.Metric)
		}
		passed := (guardrail.Direction == DirectionHigher && effect >= -guardrail.MaxRegression) || (guardrail.Direction == DirectionLower && effect <= guardrail.MaxRegression)
		if !passed {
			analysis.FailureReasons = append(analysis.FailureReasons, fmt.Sprintf("guardrail %s exceeded max regression", guardrail.Metric))
		}
	}
	if len(analysis.FailureReasons) > 0 {
		analysis.Status = ExperimentStatusFailed
	}
	return analysis, nil
}

func primaryEffectPass(direction MetricDirection, minimumEffect float64, bootstrap BootstrapResult) bool {
	switch direction {
	case DirectionHigher:
		return bootstrap.Lower >= minimumEffect
	case DirectionLower:
		return bootstrap.Upper <= -minimumEffect
	default:
		return false
	}
}

func groupedEffects(observations []PairedObservation, bySource bool) map[string]float64 {
	groups := make(map[string][]float64)
	for _, observation := range observations {
		key := observation.VideoID
		if bySource {
			key = observation.SourceGroup
		}
		groups[key] = append(groups[key], observation.Candidate-observation.Baseline)
	}
	result := make(map[string]float64, len(groups))
	for key, values := range groups {
		result[key] = mean(values)
	}
	return result
}

func supportedMetric(metric string) bool {
	switch metric {
	case "recall_at_k", "mrr", "ndcg_at_k", "context_precision_at_k", "complete_evidence_recall", "answerability_f1":
		return true
	default:
		return false
	}
}
func validDirection(direction MetricDirection) bool {
	return direction == DirectionHigher || direction == DirectionLower
}

func sortedFloatKeys(values map[string]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// BindStrictRun verifies both retrieval configs and the live corpus/chunk/vector
// snapshot against the preregistration. Hand-entered hashes are not sufficient:
// callers must pass hashes computed from the live stores used by the run.
func (r ExperimentRegistry) BindStrictRun(experimentID, candidateID, datasetVersion, commit, baselineConfigSHA256, candidateConfigSHA256 string, evidence FrozenEvidenceReference) (PreregisteredExperiment, CandidateVariant, error) {
	experiment, candidate, err := r.BindRun(experimentID, candidateID, datasetVersion, commit, candidateConfigSHA256)
	if err != nil {
		return PreregisteredExperiment{}, CandidateVariant{}, err
	}
	if !isSHA256(experiment.BaselineConfigSHA256) {
		return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("experiment %q does not freeze a valid baseline config sha256", experimentID)
	}
	if !strings.EqualFold(experiment.BaselineConfigSHA256, strings.TrimSpace(baselineConfigSHA256)) {
		return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("baseline config sha256 mismatch: registry=%s live=%s", experiment.BaselineConfigSHA256, baselineConfigSHA256)
	}
	if err := experiment.FrozenEvidence.Validate(); err != nil {
		return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("experiment %q: %w", experimentID, err)
	}
	if err := evidence.Validate(); err != nil {
		return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("live evidence: %w", err)
	}
	checks := []struct{ name, frozen, live string }{
		{"corpus", experiment.FrozenEvidence.CorpusSHA256, evidence.CorpusSHA256},
		{"chunk manifest", experiment.FrozenEvidence.ChunkManifestSHA256, evidence.ChunkManifestSHA256},
		{"vector artifact", experiment.FrozenEvidence.VectorArtifactSHA256, evidence.VectorArtifactSHA256},
	}
	for _, check := range checks {
		if !strings.EqualFold(check.frozen, check.live) {
			return PreregisteredExperiment{}, CandidateVariant{}, fmt.Errorf("%s sha256 mismatch: registry=%s live=%s", check.name, check.frozen, check.live)
		}
	}
	return experiment, candidate, nil
}
