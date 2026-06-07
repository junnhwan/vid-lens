package service

import (
	"context"
	"strings"
	"time"
)

type RAGEvalCase struct {
	TaskHint              string   `json:"task_hint,omitempty" yaml:"task_hint,omitempty"`
	Question              string   `json:"question" yaml:"question"`
	ExpectedChunkKeywords []string `json:"expected_chunk_keywords" yaml:"expected_chunk_keywords"`
	ExpectedAnswerPoints  []string `json:"expected_answer_points,omitempty" yaml:"expected_answer_points,omitempty"`
}

type RAGEvalCaseResult struct {
	Case      RAGEvalCase
	Citations []RetrievedChunk
	Duration  time.Duration
}

type RAGEvalRetriever func(ctx context.Context, evalCase RAGEvalCase, topK int) ([]RetrievedChunk, error)

type RAGEvalCaseReport struct {
	Question     string   `json:"question"`
	Hit          bool     `json:"hit"`
	Skipped      bool     `json:"skipped"`
	FirstHitRank int      `json:"first_hit_rank"`
	Keywords     []string `json:"keywords"`
	LatencyMs    float64  `json:"latency_ms"`
	ResultCount  int      `json:"result_count"`
}

type RAGEvalReport struct {
	TotalCases     int                 `json:"total_cases"`
	EvaluableCases int                 `json:"evaluable_cases"`
	HitCases       int                 `json:"hit_cases"`
	RecallAtK      float64             `json:"recall_at_k"`
	MRR            float64             `json:"mrr"`
	NoResultRate   float64             `json:"no_result_rate"`
	AvgLatencyMs   float64             `json:"avg_latency_ms"`
	SourceCounts   map[string]int      `json:"source_counts"`
	Cases          []RAGEvalCaseReport `json:"cases"`
}

func EvaluateRAGRetrieval(results []RAGEvalCaseResult, topK int) RAGEvalReport {
	if topK <= 0 {
		topK = 5
	}
	report := RAGEvalReport{
		TotalCases:   len(results),
		SourceCounts: make(map[string]int),
		Cases:        make([]RAGEvalCaseReport, 0, len(results)),
	}
	if len(results) == 0 {
		return report
	}

	var reciprocalRankSum float64
	var noResultCases int
	var latencySum time.Duration

	for _, result := range results {
		citations := result.Citations
		if len(citations) == 0 {
			noResultCases++
		}
		latencySum += result.Duration
		for _, citation := range citations {
			source := citation.Source
			if source == "" {
				source = sourceForRanks(citation.VectorRank, citation.KeywordRank)
			}
			if source == "" {
				source = "unknown"
			}
			report.SourceCounts[source]++
		}

		caseReport := RAGEvalCaseReport{
			Question:    result.Case.Question,
			Keywords:    normalizedEvalKeywords(result.Case.ExpectedChunkKeywords),
			LatencyMs:   durationMillis(result.Duration),
			ResultCount: len(citations),
		}
		if len(caseReport.Keywords) == 0 {
			caseReport.Skipped = true
			report.Cases = append(report.Cases, caseReport)
			continue
		}

		report.EvaluableCases++
		limit := topK
		if len(citations) < limit {
			limit = len(citations)
		}
		for i := 0; i < limit; i++ {
			if chunkMatchesExpectedKeywords(citations[i].Content, caseReport.Keywords) {
				caseReport.Hit = true
				caseReport.FirstHitRank = i + 1
				break
			}
		}
		if caseReport.Hit {
			report.HitCases++
			reciprocalRankSum += 1.0 / float64(caseReport.FirstHitRank)
		}
		report.Cases = append(report.Cases, caseReport)
	}

	if report.EvaluableCases > 0 {
		report.RecallAtK = float64(report.HitCases) / float64(report.EvaluableCases)
		report.MRR = reciprocalRankSum / float64(report.EvaluableCases)
	}
	report.NoResultRate = float64(noResultCases) / float64(len(results))
	report.AvgLatencyMs = durationMillis(latencySum) / float64(len(results))
	return report
}

func RunRAGEval(ctx context.Context, cases []RAGEvalCase, topK int, retrieve RAGEvalRetriever) (RAGEvalReport, error) {
	if retrieve == nil {
		return RAGEvalReport{}, nil
	}
	results := make([]RAGEvalCaseResult, 0, len(cases))
	for _, evalCase := range cases {
		startedAt := time.Now()
		citations, err := retrieve(ctx, evalCase, topK)
		duration := time.Since(startedAt)
		if err != nil {
			return RAGEvalReport{}, err
		}
		results = append(results, RAGEvalCaseResult{
			Case:      evalCase,
			Citations: citations,
			Duration:  duration,
		})
	}
	return EvaluateRAGRetrieval(results, topK), nil
}

func normalizedEvalKeywords(keywords []string) []string {
	normalized := make([]string, 0, len(keywords))
	seen := make(map[string]bool, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" || seen[keyword] {
			continue
		}
		seen[keyword] = true
		normalized = append(normalized, keyword)
	}
	return normalized
}

func chunkMatchesExpectedKeywords(content string, keywords []string) bool {
	content = strings.ToLower(content)
	for _, keyword := range keywords {
		if !strings.Contains(content, keyword) {
			return false
		}
	}
	return true
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
