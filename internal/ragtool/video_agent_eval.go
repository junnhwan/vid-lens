package ragtool

import (
	"strings"
	"time"
	"unicode"

	"vid-lens/internal/service"
)

type VideoAgentAnswerEvalCaseResult struct {
	Case            RAGEvalCase
	Answer          string
	Citations       []service.RetrievedChunk
	Trace           []service.VideoAgentStep
	Duration        time.Duration
	FallbackOrError bool
	Error           string
}

type VideoAgentAnswerEvalCaseReport struct {
	Category                  string   `json:"category,omitempty"`
	Question                  string   `json:"question"`
	AnswerPointCoverage       float64  `json:"answer_point_coverage"`
	CoveredAnswerPoints       int      `json:"covered_answer_points"`
	TotalAnswerPoints         int      `json:"total_answer_points"`
	CitationHit               bool     `json:"citation_hit"`
	CitationKeywords          []string `json:"citation_keywords,omitempty"`
	NoAnswer                  bool     `json:"no_answer"`
	ToolSteps                 int      `json:"tool_steps"`
	FallbackOrError           bool     `json:"fallback_or_error"`
	Error                     string   `json:"error,omitempty"`
	LatencyMs                 float64  `json:"latency_ms"`
	SkippedAnswerPointScoring bool     `json:"skipped_answer_point_scoring,omitempty"`
}

type VideoAgentAnswerEvalReport struct {
	TotalCases                 int                              `json:"total_cases"`
	AnswerPointEvaluableCases  int                              `json:"answer_point_evaluable_cases"`
	CitationEvaluableCases     int                              `json:"citation_evaluable_cases"`
	AnswerPointCoverage        float64                          `json:"answer_point_coverage"`
	CitationHitRate            float64                          `json:"citation_hit_rate"`
	NoAnswerRate               float64                          `json:"no_answer_rate"`
	AvgToolSteps               float64                          `json:"avg_tool_steps"`
	FallbackErrorRate          float64                          `json:"fallback_error_rate"`
	AvgLatencyMs               float64                          `json:"avg_latency_ms"`
	Cases                      []VideoAgentAnswerEvalCaseReport `json:"cases"`
	TotalExpectedAnswerPoints  int                              `json:"total_expected_answer_points"`
	CoveredExpectedAnswerPoint int                              `json:"covered_expected_answer_points"`
	CitationHitCases           int                              `json:"citation_hit_cases"`
	NoAnswerCases              int                              `json:"no_answer_cases"`
	FallbackErrorCases         int                              `json:"fallback_error_cases"`
}

func EvaluateVideoAgentAnswers(results []VideoAgentAnswerEvalCaseResult) VideoAgentAnswerEvalReport {
	report := VideoAgentAnswerEvalReport{
		TotalCases: len(results),
		Cases:      make([]VideoAgentAnswerEvalCaseReport, 0, len(results)),
	}
	if len(results) == 0 {
		return report
	}

	var totalToolSteps int
	var totalLatency time.Duration
	for _, result := range results {
		caseReport := VideoAgentAnswerEvalCaseReport{
			Category:         normalizeEvalCategory(result.Case.Category),
			Question:         result.Case.Question,
			CitationKeywords: normalizedEvalKeywords(result.Case.ExpectedChunkKeywords),
			ToolSteps:        len(result.Trace),
			FallbackOrError:  result.FallbackOrError,
			Error:            result.Error,
			LatencyMs:        durationMillis(result.Duration),
		}
		totalToolSteps += caseReport.ToolSteps
		totalLatency += result.Duration

		if len(result.Case.ExpectedAnswerPoints) == 0 {
			caseReport.SkippedAnswerPointScoring = true
		} else {
			report.AnswerPointEvaluableCases++
			for _, point := range result.Case.ExpectedAnswerPoints {
				report.TotalExpectedAnswerPoints++
				caseReport.TotalAnswerPoints++
				if answerCoversExpectedPoint(result.Answer, point) {
					report.CoveredExpectedAnswerPoint++
					caseReport.CoveredAnswerPoints++
				}
			}
			if caseReport.TotalAnswerPoints > 0 {
				caseReport.AnswerPointCoverage = float64(caseReport.CoveredAnswerPoints) / float64(caseReport.TotalAnswerPoints)
			}
		}

		if len(caseReport.CitationKeywords) > 0 {
			report.CitationEvaluableCases++
			caseReport.CitationHit = citationsContainExpectedKeywords(result.Citations, caseReport.CitationKeywords)
			if caseReport.CitationHit {
				report.CitationHitCases++
			}
		}
		caseReport.NoAnswer = isNoAnswer(result.Answer)
		if caseReport.NoAnswer {
			report.NoAnswerCases++
		}
		if result.FallbackOrError || strings.TrimSpace(result.Error) != "" {
			report.FallbackErrorCases++
		}
		report.Cases = append(report.Cases, caseReport)
	}

	if report.TotalExpectedAnswerPoints > 0 {
		report.AnswerPointCoverage = float64(report.CoveredExpectedAnswerPoint) / float64(report.TotalExpectedAnswerPoints)
	}
	if report.CitationEvaluableCases > 0 {
		report.CitationHitRate = float64(report.CitationHitCases) / float64(report.CitationEvaluableCases)
	}
	report.NoAnswerRate = float64(report.NoAnswerCases) / float64(len(results))
	report.AvgToolSteps = float64(totalToolSteps) / float64(len(results))
	report.FallbackErrorRate = float64(report.FallbackErrorCases) / float64(len(results))
	report.AvgLatencyMs = durationMillis(totalLatency) / float64(len(results))
	return report
}

func citationsContainExpectedKeywords(citations []service.RetrievedChunk, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	var b strings.Builder
	for _, citation := range citations {
		if strings.TrimSpace(citation.AnchorContent) != "" {
			b.WriteString(citation.AnchorContent)
			b.WriteByte('\n')
		}
		b.WriteString(citation.Content)
		b.WriteByte('\n')
	}
	return chunkMatchesExpectedKeywords(b.String(), keywords)
}

func answerCoversExpectedPoint(answer, point string) bool {
	normalizedAnswer := normalizeAnswerEvalText(answer)
	normalizedPoint := normalizeAnswerEvalText(point)
	if normalizedAnswer == "" || normalizedPoint == "" {
		return false
	}
	if strings.Contains(normalizedAnswer, normalizedPoint) {
		return true
	}
	terms := meaningfulAnswerPointTerms(point)
	if len(terms) == 0 {
		return false
	}
	matched := 0
	for _, term := range terms {
		if strings.Contains(normalizedAnswer, term) {
			matched++
		}
	}
	if len(terms) <= 2 {
		return matched == len(terms)
	}
	return float64(matched)/float64(len(terms)) >= 0.8
}

func meaningfulAnswerPointTerms(point string) []string {
	normalized := normalizeAnswerEvalText(point)
	if normalized == "" {
		return nil
	}
	stopwords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "been": true, "but": true, "by": true, "for": true, "from": true,
		"had": true, "has": true, "have": true, "in": true, "is": true, "it": true,
		"more": true, "of": true, "on": true, "or": true, "such": true, "than": true,
		"that": true, "the": true, "they": true, "this": true, "to": true, "with": true,
	}
	terms := make([]string, 0)
	seen := make(map[string]bool)
	for _, term := range strings.Fields(normalized) {
		if stopwords[term] || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

func normalizeAnswerEvalText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := true
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func isNoAnswer(answer string) bool {
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return true
	}
	patterns := []string{
		"没有找到", "未找到", "未检索到", "没有相关", "无法回答", "不能回答", "不知道",
		"no answer", "not found", "no relevant", "cannot answer", "can't answer", "do not know", "don't know",
	}
	for _, pattern := range patterns {
		if strings.Contains(answer, pattern) {
			return true
		}
	}
	return false
}
