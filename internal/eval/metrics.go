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
	// RetrieveLatencyMS is the wall-clock cost of one case's retrieval. Executor
	// failures record 0 (by convention — the executor never completed) and stay in
	// the P95 sample so latency cannot look healthier as failures rise. This is the
	// only sanctioned executor-side schema extension; the strict dataset schema's
	// `case` is untouched.
	RetrieveLatencyMS int64 `json:"retrieve_latency_ms"`
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
	// P95RetrieveLatencyMS is the nearest-rank 95th percentile of RetrieveLatencyMS
	// over every case in this bucket. Failed cases contribute 0 and remain in the
	// sample (the failure-as-zero convention) so a run cannot hide latency behind
	// dropped failures.
	P95RetrieveLatencyMS int64 `json:"p95_retrieve_latency_ms"`
	// SuccessP95RetrieveLatencyMS is the same percentile restricted to cases with
	// RetrieveLatencyMS > 0 (successful retrievals). It is a companion health
	// number, not the headline; the headline P95 includes failures as zero.
	SuccessP95RetrieveLatencyMS int64 `json:"success_p95_retrieve_latency_ms"`
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
	RetrieveLatencyMS      int64   `json:"retrieve_latency_ms"`
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
		RetrieveLatencyMS:   result.RetrieveLatencyMS,
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
	latencies := make([]int64, 0, len(cases))
	successLatencies := make([]int64, 0, len(cases))
	for _, c := range cases {
		if c.Failed {
			result.FailedCases++
		}
		latencies = append(latencies, c.RetrieveLatencyMS)
		if c.RetrieveLatencyMS > 0 {
			successLatencies = append(successLatencies, c.RetrieveLatencyMS)
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
	result.P95RetrieveLatencyMS = p95NearestRank(latencies)
	result.SuccessP95RetrieveLatencyMS = p95NearestRank(successLatencies)
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

// p95NearestRank returns the nearest-rank 95th percentile of the sample: the
// value at index ceil(0.95*n)-1 in ascending order. An empty sample yields 0.
// Failed cases must remain in the caller's sample as zero so that the headline
// P95 cannot improve by dropping failures; the success-only subset is the
// caller's responsibility (pass only positive latencies).
func p95NearestRank(sample []int64) int64 {
	if len(sample) == 0 {
		return 0
	}
	sorted := append([]int64(nil), sample...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
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
