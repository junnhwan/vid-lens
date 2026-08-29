package eval

import (
	"fmt"
	"sort"
)

// AnalyzePairedRunArtifacts joins baseline and candidate by case ID before
// running source-group bootstrap. Failed cases are represented by their zeroed
// CaseMetric and therefore remain in the paired denominator.
func AnalyzePairedRunArtifacts(registry ExperimentRegistry, experimentID, candidateID string, baseline, candidate RunArtifact) (ExperimentAnalysis, error) {
	experiment, err := registry.Experiment(experimentID)
	if err != nil {
		return ExperimentAnalysis{}, err
	}
	if err := validatePairedMetadata(experiment, baseline.Metadata, candidate.Metadata); err != nil {
		return ExperimentAnalysis{}, err
	}
	baselineCases, err := indexCaseArtifacts(baseline.Cases)
	if err != nil {
		return ExperimentAnalysis{}, fmt.Errorf("baseline: %w", err)
	}
	candidateCases, err := indexCaseArtifacts(candidate.Cases)
	if err != nil {
		return ExperimentAnalysis{}, fmt.Errorf("candidate: %w", err)
	}
	if len(baselineCases) != len(candidateCases) {
		return ExperimentAnalysis{}, fmt.Errorf("baseline/candidate case set mismatch: %d vs %d", len(baselineCases), len(candidateCases))
	}
	caseIDs := make([]string, 0, len(baselineCases))
	for caseID := range baselineCases {
		if _, ok := candidateCases[caseID]; !ok {
			return ExperimentAnalysis{}, fmt.Errorf("baseline/candidate case set mismatch: candidate missing %q", caseID)
		}
		caseIDs = append(caseIDs, caseID)
	}
	sort.Strings(caseIDs)
	observations := make([]PairedObservation, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		baseCase := baselineCases[caseID].Metric
		candidateCase := candidateCases[caseID].Metric
		if baseCase.VideoID != candidateCase.VideoID || baseCase.SourceGroup != candidateCase.SourceGroup {
			return ExperimentAnalysis{}, fmt.Errorf("case %q source identity drift between baseline and candidate", caseID)
		}
		baseValue, err := pairedCaseMetricValue(baseCase, experiment.PrimaryMetric)
		if err != nil {
			return ExperimentAnalysis{}, err
		}
		candidateValue, err := pairedCaseMetricValue(candidateCase, experiment.PrimaryMetric)
		if err != nil {
			return ExperimentAnalysis{}, err
		}
		observations = append(observations, PairedObservation{
			CaseID: caseID, SourceGroup: baseCase.SourceGroup, VideoID: baseCase.VideoID,
			Baseline: baseValue, Candidate: candidateValue,
		})
	}
	guardrailEffects := make(map[string]float64, len(experiment.Guardrails))
	for _, guardrail := range experiment.Guardrails {
		baseValue, err := reportMetricValue(baseline.Summary.Overall, guardrail.Metric)
		if err != nil {
			return ExperimentAnalysis{}, err
		}
		candidateValue, err := reportMetricValue(candidate.Summary.Overall, guardrail.Metric)
		if err != nil {
			return ExperimentAnalysis{}, err
		}
		guardrailEffects[guardrail.Metric] = candidateValue - baseValue
	}
	return AnalyzeExperiment(registry, experimentID, candidateID, observations, guardrailEffects)
}

func validatePairedMetadata(experiment PreregisteredExperiment, baseline, candidate RunMetadata) error {
	if baseline.DatasetVersion != "" && baseline.DatasetVersion != experiment.DatasetVersion {
		return fmt.Errorf("baseline dataset version %q does not match experiment %q", baseline.DatasetVersion, experiment.DatasetVersion)
	}
	if candidate.DatasetVersion != "" && candidate.DatasetVersion != experiment.DatasetVersion {
		return fmt.Errorf("candidate dataset version %q does not match experiment %q", candidate.DatasetVersion, experiment.DatasetVersion)
	}
	if baseline.VariantID != "" && baseline.VariantID != experiment.BaselineVariant {
		return fmt.Errorf("baseline artifact variant %q does not match frozen baseline %q", baseline.VariantID, experiment.BaselineVariant)
	}
	for name, values := range map[string][2]string{
		"dataset":         {baseline.DatasetSHA256, candidate.DatasetSHA256},
		"source manifest": {baseline.SourceManifestSHA256, candidate.SourceManifestSHA256},
		"corpus":          {baseline.CorpusSHA256, candidate.CorpusSHA256},
		"chunk manifest":  {baseline.ChunkManifestSHA256, candidate.ChunkManifestSHA256},
		"vector artifact": {baseline.VectorArtifactSHA256, candidate.VectorArtifactSHA256},
	} {
		if values[0] != values[1] {
			return fmt.Errorf("baseline/candidate %s hash mismatch", name)
		}
	}
	return nil
}

func indexCaseArtifacts(cases []CaseArtifact) (map[string]CaseArtifact, error) {
	indexed := make(map[string]CaseArtifact, len(cases))
	for _, item := range cases {
		if item.CaseID == "" {
			return nil, fmt.Errorf("case set contains empty case_id")
		}
		if _, exists := indexed[item.CaseID]; exists {
			return nil, fmt.Errorf("case set contains duplicate case_id %q", item.CaseID)
		}
		indexed[item.CaseID] = item
	}
	return indexed, nil
}

func pairedCaseMetricValue(metric CaseMetric, name string) (float64, error) {
	switch name {
	case "recall_at_k":
		return metric.RecallAtK, nil
	case "mrr":
		return metric.ReciprocalRank, nil
	case "ndcg_at_k":
		return metric.NDCGAtK, nil
	case "context_precision_at_k":
		return metric.ContextPrecisionAtK, nil
	case "complete_evidence_recall":
		return metric.CompleteEvidenceRecall, nil
	case "answerability_f1":
		return 0, fmt.Errorf("answerability_f1 is not case-decomposable and cannot be the paired bootstrap primary metric")
	default:
		return 0, fmt.Errorf("unsupported paired metric %q", name)
	}
}

func reportMetricValue(metric MetricResult, name string) (float64, error) {
	switch name {
	case "recall_at_k":
		return metric.RecallAtK, nil
	case "mrr":
		return metric.MRR, nil
	case "ndcg_at_k":
		return metric.NDCGAtK, nil
	case "context_precision_at_k":
		return metric.ContextPrecisionAtK, nil
	case "complete_evidence_recall":
		return metric.CompleteEvidenceRecall, nil
	case "answerability_f1":
		return metric.AnswerabilityF1, nil
	default:
		return 0, fmt.Errorf("unsupported report metric %q", name)
	}
}
