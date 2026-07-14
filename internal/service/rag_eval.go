package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type QueryMode string

const (
	QueryModeOriginal   QueryMode = "original"
	QueryModePreprocess QueryMode = "preprocess"
	QueryModeRewrite    QueryMode = "rewrite"

	ChunkerStrategySemanticBoundary = "semantic_boundary"
	RerankerModeNone                = "none"
	RerankerModeDeterministic       = "deterministic"
)

// RAGRetrievalConfig is a fully explicit, hashable experiment configuration.
// Dev runs should persist one file per variant instead of relying on hidden
// defaults. Production callers may use DefaultRAGRetrievalConfig.
type RAGRetrievalConfig struct {
	Name            string    `json:"name" yaml:"name"`
	EnableVector    bool      `json:"enable_vector" yaml:"enable_vector"`
	EnableBM25      bool      `json:"enable_bm25" yaml:"enable_bm25"`
	QueryMode       QueryMode `json:"query_mode" yaml:"query_mode"`
	RewriteQueries  int       `json:"rewrite_queries" yaml:"rewrite_queries"`
	TopK            int       `json:"top_k" yaml:"top_k"`
	CandidateK      int       `json:"candidate_k" yaml:"candidate_k"`
	RRFK            float64   `json:"rrf_k" yaml:"rrf_k"`
	NeighborRadius  int       `json:"neighbor_radius" yaml:"neighbor_radius"`
	MaxContextChars int       `json:"max_context_chars" yaml:"max_context_chars"`
	MinVectorScore  float32   `json:"min_vector_score" yaml:"min_vector_score"`

	// Index-time and post-retrieval variables are part of the frozen experiment
	// config even though chunking is not executed in the query process.
	ChunkerStrategy string `json:"chunker_strategy" yaml:"chunker_strategy"`
	ChunkerVersion  string `json:"chunker_version" yaml:"chunker_version"`
	ChunkSize       int    `json:"chunk_size" yaml:"chunk_size"`
	ChunkOverlap    int    `json:"chunk_overlap" yaml:"chunk_overlap"`
	RerankerMode    string `json:"reranker_mode" yaml:"reranker_mode"`
	RerankerVersion string `json:"reranker_version" yaml:"reranker_version"`
}

func DefaultRAGRetrievalConfig() RAGRetrievalConfig {
	return RAGRetrievalConfig{
		Name: "production-hybrid", EnableVector: true, EnableBM25: true,
		QueryMode: QueryModeRewrite, RewriteQueries: 3,
		TopK: 5, CandidateK: 20, RRFK: defaultRRFK,
		NeighborRadius: 1, MaxContextChars: 4000,
		ChunkerStrategy: ChunkerStrategySemanticBoundary, ChunkerVersion: "semantic-v1",
		ChunkSize: 800, ChunkOverlap: 100,
		RerankerMode: RerankerModeDeterministic, RerankerVersion: "deterministic-v1",
	}
}

func (c RAGRetrievalConfig) Validate() error {
	var problems []string
	if strings.TrimSpace(c.Name) == "" {
		problems = append(problems, "name is required")
	}
	if !c.EnableVector && !c.EnableBM25 {
		problems = append(problems, "at least one retriever must be enabled")
	}
	switch c.QueryMode {
	case QueryModeOriginal, QueryModePreprocess:
		if c.RewriteQueries > 1 {
			problems = append(problems, "rewrite_queries must be 0 or 1 unless query_mode is rewrite")
		}
	case QueryModeRewrite:
		if c.RewriteQueries < 2 || c.RewriteQueries > 5 {
			problems = append(problems, "rewrite_queries must be between 2 and 5 in rewrite mode")
		}
	default:
		problems = append(problems, fmt.Sprintf("unsupported query_mode %q", c.QueryMode))
	}
	if c.TopK <= 0 || c.TopK > 50 {
		problems = append(problems, "top_k must be in [1,50]")
	}
	if c.CandidateK < c.TopK || c.CandidateK > 100 {
		problems = append(problems, "candidate_k must be between top_k and 100")
	}
	if c.EnableVector && c.MinVectorScore < 0 {
		problems = append(problems, "min_vector_score must not be negative")
	}
	if c.EnableVector && c.EnableBM25 && c.RRFK <= 0 {
		problems = append(problems, "rrf_k must be positive for hybrid retrieval")
	}
	if c.NeighborRadius < 0 || c.NeighborRadius > 5 {
		problems = append(problems, "neighbor_radius must be in [0,5]")
	}
	if c.NeighborRadius > 0 && c.MaxContextChars <= 0 {
		problems = append(problems, "max_context_chars must be positive when neighbor expansion is enabled")
	}
	if c.ChunkSize < 0 || (c.ChunkSize > 0 && c.ChunkOverlap >= c.ChunkSize) || c.ChunkOverlap < 0 {
		problems = append(problems, "chunk size/overlap must satisfy size > overlap >= 0")
	}
	if c.RerankerMode != "" && c.RerankerMode != RerankerModeNone && c.RerankerMode != RerankerModeDeterministic {
		problems = append(problems, fmt.Sprintf("unsupported reranker_mode %q", c.RerankerMode))
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid retrieval config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (c RAGRetrievalConfig) ValidateStrictExperiment() error {
	if err := c.Validate(); err != nil {
		return err
	}
	var missing []string
	for field, value := range map[string]string{
		"chunker_strategy": c.ChunkerStrategy,
		"chunker_version":  c.ChunkerVersion,
		"reranker_mode":    c.RerankerMode,
		"reranker_version": c.RerankerVersion,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	if c.ChunkSize <= 0 || c.ChunkOverlap < 0 || c.ChunkOverlap >= c.ChunkSize {
		missing = append(missing, "valid chunk_size/chunk_overlap")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("strict experiment config must freeze %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateSingleVariableAblation enforces the dev discipline that a candidate
// changes one retrieval factor only. Name is metadata and is ignored.
func ValidateSingleVariableAblation(base, candidate RAGRetrievalConfig) (string, error) {
	if err := base.ValidateStrictExperiment(); err != nil {
		return "", fmt.Errorf("baseline: %w", err)
	}
	if err := candidate.ValidateStrictExperiment(); err != nil {
		return "", fmt.Errorf("candidate: %w", err)
	}
	type factor struct {
		name    string
		changed bool
	}
	factors := []factor{
		{"enable_vector", base.EnableVector != candidate.EnableVector},
		{"enable_bm25", base.EnableBM25 != candidate.EnableBM25},
		{"query", base.QueryMode != candidate.QueryMode || base.RewriteQueries != candidate.RewriteQueries},
		{"top_k", base.TopK != candidate.TopK},
		{"candidate_k", base.CandidateK != candidate.CandidateK},
		{"rrf_k", base.RRFK != candidate.RRFK},
		{"neighbor_radius", base.NeighborRadius != candidate.NeighborRadius},
		{"max_context_chars", base.MaxContextChars != candidate.MaxContextChars},
		{"min_vector_score", base.MinVectorScore != candidate.MinVectorScore},
		{"chunker_strategy", base.ChunkerStrategy != candidate.ChunkerStrategy},
		{"chunker_version", base.ChunkerVersion != candidate.ChunkerVersion},
		{"chunk_size", base.ChunkSize != candidate.ChunkSize},
		{"chunk_overlap", base.ChunkOverlap != candidate.ChunkOverlap},
		{"reranker_mode", base.RerankerMode != candidate.RerankerMode},
		{"reranker_version", base.RerankerVersion != candidate.RerankerVersion},
	}
	var changed []string
	for _, f := range factors {
		if f.changed {
			changed = append(changed, f.name)
		}
	}
	if len(changed) != 1 {
		return "", fmt.Errorf("candidate must change exactly one factor; changed=%v", changed)
	}
	return changed[0], nil
}

func NormalizeRetrievalQuery(query string) string {
	query = strings.TrimSpace(query)
	var b strings.Builder
	space := false
	for _, r := range query {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		switch r {
		case '？':
			r = '?'
		case '，':
			r = ','
		case '：':
			r = ':'
		case '；':
			r = ';'
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

type PreprocessQueryRewriter struct{}

func (PreprocessQueryRewriter) Rewrite(_ context.Context, input RewriteInput) (RewriteResult, error) {
	query := NormalizeRetrievalQuery(input.Question)
	if query == "" {
		return RewriteResult{}, fmt.Errorf("问题不能为空")
	}
	return RewriteResult{Original: strings.TrimSpace(input.Question), Queries: []string{query}}, nil
}

type RAGEvalCase struct {
	Category              string   `json:"category,omitempty" yaml:"category,omitempty"`
	TaskHint              string   `json:"task_hint,omitempty" yaml:"task_hint,omitempty"`
	Question              string   `json:"question" yaml:"question"`
	ExpectedChunkKeywords []string `json:"expected_chunk_keywords" yaml:"expected_chunk_keywords"`
	ExpectedAnswerPoints  []string `json:"expected_answer_points,omitempty" yaml:"expected_answer_points,omitempty"`
}

type RAGEvalCaseResult struct {
	Case                 RAGEvalCase
	Citations            []RetrievedChunk
	Duration             time.Duration
	RewriteFallback      bool
	ExpandedContextChars int
	RerankChangedRank    bool
}

type RAGEvalRetriever func(ctx context.Context, evalCase RAGEvalCase, topK int) ([]RetrievedChunk, error)

type RAGEvalCaseReport struct {
	Category           string   `json:"category,omitempty"`
	Question           string   `json:"question"`
	Hit                bool     `json:"hit"`
	Skipped            bool     `json:"skipped"`
	FirstHitRank       int      `json:"first_hit_rank"`
	Keywords           []string `json:"keywords"`
	LatencyMs          float64  `json:"latency_ms"`
	ResultCount        int      `json:"result_count"`
	CitationContextHit bool     `json:"citation_context_hit"`
	ExpandedContextHit bool     `json:"expanded_context_hit"`
}

type RAGEvalReport struct {
	TotalCases     int                              `json:"total_cases"`
	EvaluableCases int                              `json:"evaluable_cases"`
	HitCases       int                              `json:"hit_cases"`
	RecallAtK      float64                          `json:"recall_at_k"`
	MRR            float64                          `json:"mrr"`
	NoResultRate   float64                          `json:"no_result_rate"`
	AvgLatencyMs   float64                          `json:"avg_latency_ms"`
	SourceCounts   map[string]int                   `json:"source_counts"`
	Cases          []RAGEvalCaseReport              `json:"cases"`
	Categories     map[string]RAGEvalCategoryReport `json:"categories,omitempty"`

	RewriteFallbackCount    int     `json:"rewrite_fallback_count"`
	RewriteFallbackRate     float64 `json:"rewrite_fallback_rate"`
	AvgExpandedContextChars float64 `json:"avg_expanded_context_chars"`
	RerankChangedRankCount  int     `json:"rerank_changed_rank_count"`
	CitationContextHitCases int     `json:"citation_context_hit_cases"`
	CitationContextHitRate  float64 `json:"citation_context_hit_rate"`
	ExpandedContextHitCases int     `json:"expanded_context_hit_cases"`
	ExpandedContextHitRate  float64 `json:"expanded_context_hit_rate"`
}

type RAGEvalCategoryReport struct {
	TotalCases              int     `json:"total_cases"`
	EvaluableCases          int     `json:"evaluable_cases"`
	HitCases                int     `json:"hit_cases"`
	RecallAtK               float64 `json:"recall_at_k"`
	MRR                     float64 `json:"mrr"`
	NoResultRate            float64 `json:"no_result_rate"`
	AvgLatencyMs            float64 `json:"avg_latency_ms"`
	RewriteFallbackCount    int     `json:"rewrite_fallback_count"`
	RewriteFallbackRate     float64 `json:"rewrite_fallback_rate"`
	AvgExpandedContextChars float64 `json:"avg_expanded_context_chars"`
	RerankChangedRankCount  int     `json:"rerank_changed_rank_count"`
	CitationContextHitCases int     `json:"citation_context_hit_cases"`
	CitationContextHitRate  float64 `json:"citation_context_hit_rate"`
	ExpandedContextHitCases int     `json:"expanded_context_hit_cases"`
	ExpandedContextHitRate  float64 `json:"expanded_context_hit_rate"`
}

type ragEvalCategoryAccumulator struct {
	report            RAGEvalCategoryReport
	reciprocalRankSum float64
	noResultCases     int
	latencySum        time.Duration
	expandedChars     int
}

func EvaluateRAGRetrieval(results []RAGEvalCaseResult, topK int) RAGEvalReport {
	if topK <= 0 {
		topK = 5
	}
	report := RAGEvalReport{
		TotalCases:   len(results),
		SourceCounts: make(map[string]int),
		Cases:        make([]RAGEvalCaseReport, 0, len(results)),
		Categories:   make(map[string]RAGEvalCategoryReport),
	}
	if len(results) == 0 {
		return report
	}

	var reciprocalRankSum float64
	var noResultCases int
	var latencySum time.Duration
	var expandedContextChars int
	categoryAccs := make(map[string]*ragEvalCategoryAccumulator)

	for _, result := range results {
		category := normalizeEvalCategory(result.Case.Category)
		categoryAcc := categoryAccs[category]
		if categoryAcc == nil {
			categoryAcc = &ragEvalCategoryAccumulator{}
			categoryAccs[category] = categoryAcc
		}
		categoryAcc.report.TotalCases++

		citations := result.Citations
		if len(citations) == 0 {
			noResultCases++
			categoryAcc.noResultCases++
		}
		latencySum += result.Duration
		categoryAcc.latencySum += result.Duration
		if result.RewriteFallback {
			report.RewriteFallbackCount++
			categoryAcc.report.RewriteFallbackCount++
		}
		expandedContextChars += result.ExpandedContextChars
		categoryAcc.expandedChars += result.ExpandedContextChars
		if result.RerankChangedRank {
			report.RerankChangedRankCount++
			categoryAcc.report.RerankChangedRankCount++
		}
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
			Category:    category,
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
		categoryAcc.report.EvaluableCases++
		limit := topK
		if len(citations) < limit {
			limit = len(citations)
		}
		for i := 0; i < limit; i++ {
			anchorHit := chunkMatchesExpectedKeywords(anchorContentForEval(citations[i]), caseReport.Keywords)
			contextHit := chunkMatchesExpectedKeywords(citations[i].Content, caseReport.Keywords)
			if contextHit {
				caseReport.CitationContextHit = true
			}
			if contextHit && !anchorHit && citations[i].AnchorContent != "" {
				caseReport.ExpandedContextHit = true
			}
			if anchorHit {
				caseReport.Hit = true
				caseReport.FirstHitRank = i + 1
				break
			}
		}
		if caseReport.Hit {
			report.HitCases++
			reciprocalRankSum += 1.0 / float64(caseReport.FirstHitRank)
			categoryAcc.report.HitCases++
			categoryAcc.reciprocalRankSum += 1.0 / float64(caseReport.FirstHitRank)
		}
		if caseReport.CitationContextHit {
			report.CitationContextHitCases++
			categoryAcc.report.CitationContextHitCases++
		}
		if caseReport.ExpandedContextHit {
			report.ExpandedContextHitCases++
			categoryAcc.report.ExpandedContextHitCases++
		}
		report.Cases = append(report.Cases, caseReport)
	}

	if report.EvaluableCases > 0 {
		report.RecallAtK = float64(report.HitCases) / float64(report.EvaluableCases)
		report.MRR = reciprocalRankSum / float64(report.EvaluableCases)
		report.CitationContextHitRate = float64(report.CitationContextHitCases) / float64(report.EvaluableCases)
		report.ExpandedContextHitRate = float64(report.ExpandedContextHitCases) / float64(report.EvaluableCases)
	}
	report.NoResultRate = float64(noResultCases) / float64(len(results))
	report.AvgLatencyMs = durationMillis(latencySum) / float64(len(results))
	report.RewriteFallbackRate = float64(report.RewriteFallbackCount) / float64(len(results))
	report.AvgExpandedContextChars = float64(expandedContextChars) / float64(len(results))
	for category, acc := range categoryAccs {
		categoryReport := acc.report
		if categoryReport.EvaluableCases > 0 {
			categoryReport.RecallAtK = float64(categoryReport.HitCases) / float64(categoryReport.EvaluableCases)
			categoryReport.MRR = acc.reciprocalRankSum / float64(categoryReport.EvaluableCases)
			categoryReport.CitationContextHitRate = float64(categoryReport.CitationContextHitCases) / float64(categoryReport.EvaluableCases)
			categoryReport.ExpandedContextHitRate = float64(categoryReport.ExpandedContextHitCases) / float64(categoryReport.EvaluableCases)
		}
		if categoryReport.TotalCases > 0 {
			categoryReport.NoResultRate = float64(acc.noResultCases) / float64(categoryReport.TotalCases)
			categoryReport.AvgLatencyMs = durationMillis(acc.latencySum) / float64(categoryReport.TotalCases)
			categoryReport.RewriteFallbackRate = float64(categoryReport.RewriteFallbackCount) / float64(categoryReport.TotalCases)
			categoryReport.AvgExpandedContextChars = float64(acc.expandedChars) / float64(categoryReport.TotalCases)
		}
		report.Categories[category] = categoryReport
	}
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

func normalizeEvalCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "uncategorized"
	}
	return category
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

func anchorContentForEval(chunk RetrievedChunk) string {
	if strings.TrimSpace(chunk.AnchorContent) != "" {
		return chunk.AnchorContent
	}
	return chunk.Content
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
