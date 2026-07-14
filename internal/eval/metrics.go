package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type MetricConfig struct {
	K                   int     `json:"k" yaml:"k"`
	BoundaryToleranceMS int64   `json:"boundary_tolerance_ms" yaml:"boundary_tolerance_ms"`
	MaxChunkDurationMS  int64   `json:"max_chunk_duration_ms" yaml:"max_chunk_duration_ms"`
	MinEvidenceCoverage float64 `json:"min_evidence_coverage" yaml:"min_evidence_coverage"`
}

type RetrievedContext struct {
	ContextID  string         `json:"context_id"`
	VideoID    string         `json:"video_id,omitempty"`
	StartMS    int64          `json:"start_ms"`
	EndMS      int64          `json:"end_ms"`
	Source     EvidenceSource `json:"source"`
	Text       string         `json:"text,omitempty"`
	TokenCount int            `json:"token_count,omitempty"`
}

type RunFailure struct {
	Stage   string `json:"stage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type EvaluationCaseResult struct {
	Case                Case               `json:"case"`
	Retrieved           []RetrievedContext `json:"retrieved_contexts"`
	PredictedAnswerable bool               `json:"predicted_answerable"`
	Response            string             `json:"response,omitempty"`
	Failure             *RunFailure        `json:"failure,omitempty"`
}

type MetricResult struct {
	Cases                  int     `json:"cases"`
	EvaluableCases         int     `json:"evaluable_cases"`
	FailedCases            int     `json:"failed_cases"`
	FailureRate            float64 `json:"failure_rate"`
	RecallAtK              float64 `json:"recall_at_k"`
	MRR                    float64 `json:"mrr"`
	NDCGAtK                float64 `json:"ndcg_at_k"`
	ContextPrecisionAtK    float64 `json:"context_precision_at_k"`
	CompleteEvidenceRecall float64 `json:"complete_evidence_recall"`
	AnswerabilityPrecision float64 `json:"answerability_precision"`
	AnswerabilityRecall    float64 `json:"answerability_recall"`
	AnswerabilityF1        float64 `json:"answerability_f1"`
}

type CaseMetric struct {
	CaseID                 string  `json:"case_id"`
	VideoID                string  `json:"video_id"`
	SourceGroup            string  `json:"source_group"`
	Category               string  `json:"category"`
	Answerable             bool    `json:"answerable"`
	PredictedAnswerable    bool    `json:"predicted_answerable"`
	Failed                 bool    `json:"failed"`
	RecallAtK              float64 `json:"recall_at_k"`
	ReciprocalRank         float64 `json:"reciprocal_rank"`
	NDCGAtK                float64 `json:"ndcg_at_k"`
	ContextPrecisionAtK    float64 `json:"context_precision_at_k"`
	CompleteEvidenceRecall float64 `json:"complete_evidence_recall"`
	FirstRelevantRank      int     `json:"first_relevant_rank"`
	RelevantContextCount   int     `json:"relevant_context_count"`
	RetrievedContextCount  int     `json:"retrieved_context_count"`
}

type MetricReport struct {
	Config        MetricConfig            `json:"config"`
	Overall       MetricResult            `json:"overall"`
	Cases         []CaseMetric            `json:"cases"`
	ByCategory    map[string]MetricResult `json:"by_category"`
	ByVideo       map[string]MetricResult `json:"by_video"`
	BySourceGroup map[string]MetricResult `json:"by_source_group"`
}

func EvaluateMetrics(results []EvaluationCaseResult, cfg MetricConfig) (MetricReport, error) {
	if err := cfg.Validate(); err != nil {
		return MetricReport{}, err
	}
	report := MetricReport{
		Config:        cfg,
		Cases:         make([]CaseMetric, 0, len(results)),
		ByCategory:    make(map[string]MetricResult),
		ByVideo:       make(map[string]MetricResult),
		BySourceGroup: make(map[string]MetricResult),
	}
	for _, result := range results {
		report.Cases = append(report.Cases, evaluateCaseMetrics(result, cfg))
	}
	report.Overall = aggregateCaseMetrics(report.Cases)
	report.ByCategory = aggregateBy(report.Cases, func(c CaseMetric) string { return normalizeGroupKey(c.Category) })
	report.ByVideo = aggregateBy(report.Cases, func(c CaseMetric) string { return normalizeGroupKey(c.VideoID) })
	report.BySourceGroup = aggregateBy(report.Cases, func(c CaseMetric) string { return normalizeGroupKey(c.SourceGroup) })
	return report, nil
}

func (c MetricConfig) Validate() error {
	var problems []string
	if c.K <= 0 {
		problems = append(problems, "k must be positive")
	}
	if c.BoundaryToleranceMS < 0 {
		problems = append(problems, "boundary_tolerance_ms must not be negative")
	}
	if c.MaxChunkDurationMS <= 0 {
		problems = append(problems, "max_chunk_duration_ms must be positive")
	}
	if c.MinEvidenceCoverage <= 0 || c.MinEvidenceCoverage > 1 {
		problems = append(problems, "min_evidence_coverage must be in (0,1]")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid metric config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func evaluateCaseMetrics(result EvaluationCaseResult, cfg MetricConfig) CaseMetric {
	c := CaseMetric{
		CaseID:              result.Case.CaseID,
		VideoID:             result.Case.VideoID,
		SourceGroup:         result.Case.SourceGroup,
		Category:            result.Case.Category,
		Answerable:          result.Case.Answerable,
		PredictedAnswerable: result.PredictedAnswerable,
		Failed:              result.Failure != nil,
	}
	limit := cfg.K
	if len(result.Retrieved) < limit {
		limit = len(result.Retrieved)
	}
	c.RetrievedContextCount = limit
	if !result.Case.Answerable || result.Failure != nil {
		return c
	}

	groupGrades := evidenceGroupGrades(result.Case.EvidenceRanges)
	idealGrades := make([]int, 0, len(groupGrades))
	for _, grade := range groupGrades {
		idealGrades = append(idealGrades, grade)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(idealGrades)))
	if len(idealGrades) > cfg.K {
		idealGrades = idealGrades[:cfg.K]
	}

	seenGroups := make(map[string]bool)
	gains := make([]int, limit)
	precisionSum := 0.0
	relevantSeen := 0
	for i := 0; i < limit; i++ {
		matched := matchedEvidenceGroups(result.Retrieved[i], result.Case.VideoID, result.Case.EvidenceRanges, cfg)
		bestNewGrade := 0
		for groupID, grade := range matched {
			if seenGroups[groupID] {
				continue
			}
			seenGroups[groupID] = true
			if grade > bestNewGrade {
				bestNewGrade = grade
			}
		}
		if bestNewGrade == 0 {
			continue
		}
		gains[i] = bestNewGrade
		relevantSeen++
		if c.FirstRelevantRank == 0 {
			c.FirstRelevantRank = i + 1
			c.ReciprocalRank = 1 / float64(i+1)
		}
		precisionSum += float64(relevantSeen) / float64(i+1)
	}
	c.RelevantContextCount = relevantSeen
	if relevantSeen > 0 {
		c.RecallAtK = 1
		c.ContextPrecisionAtK = precisionSum / float64(relevantSeen)
	}
	c.NDCGAtK = ndcg(gains, idealGrades)
	if len(groupGrades) > 0 && len(seenGroups) == len(groupGrades) {
		c.CompleteEvidenceRecall = 1
	}
	return c
}

func matchedEvidenceGroups(context RetrievedContext, expectedVideoID string, evidence []EvidenceRange, cfg MetricConfig) map[string]int {
	matched := make(map[string]int)
	if context.VideoID != expectedVideoID {
		return matched
	}
	for _, item := range evidence {
		if !sourceCompatible(context.Source, item.Source) {
			continue
		}
		identityMatch := context.ContextID != "" && containsContextID(item.ContextIDs, context.ContextID)
		timeMatch := false
		if !identityMatch && len(item.ContextIDs) == 0 {
			duration := context.EndMS - context.StartMS
			evidenceDuration := item.EndMS - item.StartMS
			if context.StartMS >= 0 && duration > 0 && duration <= cfg.MaxChunkDurationMS && evidenceDuration > 0 {
				expandedStart := context.StartMS - cfg.BoundaryToleranceMS
				if expandedStart < 0 {
					expandedStart = 0
				}
				expandedEnd := context.EndMS + cfg.BoundaryToleranceMS
				overlap := minInt64(expandedEnd, item.EndMS) - maxInt64(expandedStart, item.StartMS)
				timeMatch = overlap > 0 && float64(overlap)/float64(evidenceDuration) >= cfg.MinEvidenceCoverage
			}
		}
		if !identityMatch && !timeMatch {
			continue
		}
		if item.Relevance > matched[item.GroupID] {
			matched[item.GroupID] = item.Relevance
		}
	}
	return matched
}

func containsContextID(ids []string, want string) bool {
	for _, id := range ids {
		if strings.TrimSpace(id) == want {
			return true
		}
	}
	return false
}

func sourceCompatible(contextSource, evidenceSource EvidenceSource) bool {
	if contextSource == EvidenceSourceBoth || evidenceSource == EvidenceSourceBoth {
		return true
	}
	return contextSource == evidenceSource
}

func evidenceGroupGrades(evidence []EvidenceRange) map[string]int {
	groups := make(map[string]int)
	for _, item := range evidence {
		if item.Relevance > groups[item.GroupID] {
			groups[item.GroupID] = item.Relevance
		}
	}
	return groups
}

func ndcg(actualGrades, idealGrades []int) float64 {
	ideal := dcg(idealGrades)
	if ideal == 0 {
		return 0
	}
	return dcg(actualGrades) / ideal
}

func dcg(grades []int) float64 {
	total := 0.0
	for i, grade := range grades {
		if grade <= 0 {
			continue
		}
		total += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(i+2))
	}
	return total
}

func aggregateBy(cases []CaseMetric, key func(CaseMetric) string) map[string]MetricResult {
	groups := make(map[string][]CaseMetric)
	for _, c := range cases {
		groups[key(c)] = append(groups[key(c)], c)
	}
	result := make(map[string]MetricResult, len(groups))
	for group, items := range groups {
		result[group] = aggregateCaseMetrics(items)
	}
	return result
}

func aggregateCaseMetrics(cases []CaseMetric) MetricResult {
	result := MetricResult{Cases: len(cases)}
	var recall, reciprocalRank, ndcgSum, precision, complete float64
	var tp, fp, fn int
	for _, c := range cases {
		if c.Failed {
			result.FailedCases++
		}
		if c.Answerable {
			result.EvaluableCases++
			recall += c.RecallAtK
			reciprocalRank += c.ReciprocalRank
			ndcgSum += c.NDCGAtK
			precision += c.ContextPrecisionAtK
			complete += c.CompleteEvidenceRecall
		}
		switch {
		case c.Answerable && c.PredictedAnswerable:
			tp++
		case !c.Answerable && c.PredictedAnswerable:
			fp++
		case c.Answerable && !c.PredictedAnswerable:
			fn++
		}
	}
	if result.Cases > 0 {
		result.FailureRate = float64(result.FailedCases) / float64(result.Cases)
	}
	if result.EvaluableCases > 0 {
		denominator := float64(result.EvaluableCases)
		result.RecallAtK = recall / denominator
		result.MRR = reciprocalRank / denominator
		result.NDCGAtK = ndcgSum / denominator
		result.ContextPrecisionAtK = precision / denominator
		result.CompleteEvidenceRecall = complete / denominator
	}
	if tp+fp > 0 {
		result.AnswerabilityPrecision = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		result.AnswerabilityRecall = float64(tp) / float64(tp+fn)
	}
	if result.AnswerabilityPrecision+result.AnswerabilityRecall > 0 {
		result.AnswerabilityF1 = 2 * result.AnswerabilityPrecision * result.AnswerabilityRecall / (result.AnswerabilityPrecision + result.AnswerabilityRecall)
	}
	return result
}

func normalizeGroupKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "uncategorized"
	}
	return value
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
