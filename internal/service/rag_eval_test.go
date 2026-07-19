package service

import (
	"context"
	"testing"
	"time"
)

func TestRAGEvalComputesRecallMRRAndSourceMix(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:              "为什么分布式锁释放时要校验 owner？",
				ExpectedChunkKeywords: []string{"owner", "锁"},
			},
			Citations: []RetrievedChunk{
				{ChunkID: 1, ChunkIndex: 0, Source: RetrievalSourceVector, Content: "普通背景片段"},
				{ChunkID: 2, ChunkIndex: 1, Source: RetrievalSourceHybrid, Content: "释放分布式锁前必须校验 owner，避免删掉别人的锁"},
			},
			Duration: 15 * time.Millisecond,
		},
		{
			Case: RAGEvalCase{
				Question:              "视频怎么解释 token bucket？",
				ExpectedChunkKeywords: []string{"token bucket"},
			},
			Citations: nil,
			Duration:  5 * time.Millisecond,
		},
	}, 2)

	if report.TotalCases != 2 {
		t.Fatalf("total cases = %d, want 2", report.TotalCases)
	}
	if report.HitCases != 1 {
		t.Fatalf("hit cases = %d, want 1", report.HitCases)
	}
	if report.RecallAtK != 0.5 {
		t.Fatalf("recall@k = %.3f, want 0.5", report.RecallAtK)
	}
	if report.MRR != 0.25 {
		t.Fatalf("mrr = %.3f, want 0.25", report.MRR)
	}
	if report.NoResultRate != 0.5 {
		t.Fatalf("no result rate = %.3f, want 0.5", report.NoResultRate)
	}
	if report.AvgLatencyMs != 10 {
		t.Fatalf("avg latency ms = %.3f, want 10", report.AvgLatencyMs)
	}
	if report.SourceCounts[RetrievalSourceVector] != 1 || report.SourceCounts[RetrievalSourceHybrid] != 1 {
		t.Fatalf("source counts = %#v, want vector=1 hybrid=1", report.SourceCounts)
	}
	if len(report.Cases) != 2 || !report.Cases[0].Hit || report.Cases[0].FirstHitRank != 2 {
		t.Fatalf("first case result = %+v, want hit at rank 2", report.Cases)
	}
	if report.Cases[1].Hit || report.Cases[1].FirstHitRank != 0 {
		t.Fatalf("second case result = %+v, want no hit", report.Cases[1])
	}
}

func TestRAGEvalTreatsEmptyExpectedKeywordsAsSkipped(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{Question: "没有期望关键词的样例"},
			Citations: []RetrievedChunk{
				{ChunkID: 1, Content: "任意片段", Source: RetrievalSourceKeyword},
			},
		},
	}, 3)

	if report.TotalCases != 1 {
		t.Fatalf("total cases = %d, want 1", report.TotalCases)
	}
	if report.EvaluableCases != 0 {
		t.Fatalf("evaluable cases = %d, want 0", report.EvaluableCases)
	}
	if report.RecallAtK != 0 || report.MRR != 0 {
		t.Fatalf("metrics = recall %.3f mrr %.3f, want zero when no evaluable cases", report.RecallAtK, report.MRR)
	}
	if !report.Cases[0].Skipped {
		t.Fatalf("case = %+v, want skipped", report.Cases[0])
	}
}

func TestRAGEvalRunUsesRetrieverAndPreservesNoResultCases(t *testing.T) {
	cases := []RAGEvalCase{
		{
			Question:              "owner 校验在哪里讲到？",
			ExpectedChunkKeywords: []string{"owner"},
		},
		{
			Question:              "令牌桶在哪里讲到？",
			ExpectedChunkKeywords: []string{"令牌桶"},
		},
	}
	calls := 0
	report, err := RunRAGEval(t.Context(), cases, 3, func(_ context.Context, _ RAGEvalCase, _ int) ([]RetrievedChunk, error) {
		calls++
		if calls == 1 {
			return []RetrievedChunk{{ChunkID: 1, Content: "释放锁前校验 owner", Source: RetrievalSourceHybrid}}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RunRAGEval() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("retriever calls = %d, want 2", calls)
	}
	if report.TotalCases != 2 || report.HitCases != 1 || report.NoResultRate != 0.5 {
		t.Fatalf("report = %+v, want one hit and one no-result case", report)
	}
	if report.Cases[0].LatencyMs < 0 || report.Cases[1].LatencyMs < 0 {
		t.Fatalf("latency should be recorded: %+v", report.Cases)
	}
}

func TestRAGEvalAggregatesRewriteExpansionAndRerankMetrics(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:              "Redis 分布式锁风险？",
				ExpectedChunkKeywords: []string{"Redis"},
			},
			Citations: []RetrievedChunk{
				{ChunkID: 1, Content: "Redis 分布式锁风险", Source: RetrievalSourceHybrid},
			},
			Duration:             10 * time.Millisecond,
			RewriteFallback:      true,
			ExpandedContextChars: 120,
			RerankChangedRank:    true,
		},
		{
			Case: RAGEvalCase{
				Question:              "RAG 为什么要切片？",
				ExpectedChunkKeywords: []string{"RAG"},
			},
			Citations: []RetrievedChunk{
				{ChunkID: 2, Content: "RAG 切片", Source: RetrievalSourceVector},
			},
			Duration:             20 * time.Millisecond,
			ExpandedContextChars: 80,
		},
	}, 3)

	if report.RewriteFallbackCount != 1 || report.RewriteFallbackRate != 0.5 {
		t.Fatalf("rewrite fallback metrics = %d %.3f, want 1 and 0.5", report.RewriteFallbackCount, report.RewriteFallbackRate)
	}
	if report.AvgExpandedContextChars != 100 {
		t.Fatalf("avg expanded context chars = %.1f, want 100", report.AvgExpandedContextChars)
	}
	if report.RerankChangedRankCount != 1 {
		t.Fatalf("rerank changed count = %d, want 1", report.RerankChangedRankCount)
	}
}

func TestRAGEvalAggregatesMetricsByCategory(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{
				Category:              "keyword_exact",
				Question:              "Where is SVG mentioned?",
				ExpectedChunkKeywords: []string{"SVG"},
			},
			Citations: []RetrievedChunk{{ChunkID: 1, Content: "SVG UI", Source: RetrievalSourceKeyword}},
			Duration:  10 * time.Millisecond,
		},
		{
			Case: RAGEvalCase{
				Category:              "keyword_exact",
				Question:              "Where is PPT mentioned?",
				ExpectedChunkKeywords: []string{"PPT"},
			},
			Citations: nil,
			Duration:  20 * time.Millisecond,
		},
		{
			Case: RAGEvalCase{
				Category:              "rewrite_needed",
				Question:              "Which animation keeps the world balanced?",
				ExpectedChunkKeywords: []string{"Avatar"},
			},
			Citations: []RetrievedChunk{{ChunkID: 2, Content: "Avatar keeps peace", Source: RetrievalSourceVector}},
			Duration:  30 * time.Millisecond,
		},
	}, 5)

	if len(report.Categories) != 2 {
		t.Fatalf("categories = %#v, want 2 category reports", report.Categories)
	}
	keyword := report.Categories["keyword_exact"]
	if keyword.TotalCases != 2 || keyword.HitCases != 1 || keyword.RecallAtK != 0.5 || keyword.NoResultRate != 0.5 {
		t.Fatalf("keyword category = %+v, want 2 total, 1 hit, recall/no-result 0.5", keyword)
	}
	if keyword.MRR != 0.5 {
		t.Fatalf("keyword MRR = %.3f, want 0.5", keyword.MRR)
	}
	rewrite := report.Categories["rewrite_needed"]
	if rewrite.TotalCases != 1 || rewrite.HitCases != 1 || rewrite.RecallAtK != 1.0 || rewrite.AvgLatencyMs != 30 {
		t.Fatalf("rewrite category = %+v, want one hit with 30ms latency", rewrite)
	}
	if report.Cases[0].Category != "keyword_exact" {
		t.Fatalf("case category = %q, want keyword_exact", report.Cases[0].Category)
	}
}

func TestRAGEvalRecallAndMRRUseAnchorChunkNotExpandedWindow(t *testing.T) {
	report := EvaluateRAGRetrieval([]RAGEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:              "哪里提到 owner 校验？",
				ExpectedChunkKeywords: []string{"owner 校验"},
			},
			Citations: []RetrievedChunk{
				{
					ChunkID:       1,
					ChunkIndex:    5,
					AnchorContent: "这里只是相邻片段的锚点，没有关键词",
					Content:       "这里只是相邻片段的锚点，没有关键词\n真正命中的邻居窗口提到了 owner 校验",
				},
			},
		},
	}, 1)

	if report.RecallAtK != 0 || report.MRR != 0 || report.HitCases != 0 {
		t.Fatalf("report = %+v, want anchor-based Recall/MRR to stay zero", report)
	}
	if !report.Cases[0].CitationContextHit || !report.Cases[0].ExpandedContextHit {
		t.Fatalf("case context hit flags = %+v, want expanded context hit recorded", report.Cases[0])
	}
	if report.CitationContextHitCases != 1 || report.ExpandedContextHitCases != 1 {
		t.Fatalf("context hit counts = citation:%d expanded:%d, want 1/1", report.CitationContextHitCases, report.ExpandedContextHitCases)
	}
}

func TestRAGRetrievalConfigRequiresSingleVariableAblation(t *testing.T) {
	base := DefaultRAGRetrievalConfig()
	candidate := base
	candidate.RRFK = 30
	if diff, err := ValidateSingleVariableAblation(base, candidate); err != nil || diff != "rrf_k" {
		t.Fatalf("single variable diff = %q, err=%v", diff, err)
	}
	candidate.NeighborRadius = 0
	if _, err := ValidateSingleVariableAblation(base, candidate); err == nil {
		t.Fatal("two changed variables must be rejected")
	}
}

func TestRAGRetrievalConfigSupportsVectorHybridNeighborAndQueryModes(t *testing.T) {
	for _, cfg := range []RAGRetrievalConfig{
		{Name: "vector", EnableVector: true, EnableBM25: false, QueryMode: QueryModeOriginal, TopK: 5, CandidateK: 20, RRFK: 60},
		{Name: "hybrid", EnableVector: true, EnableBM25: true, QueryMode: QueryModePreprocess, TopK: 5, CandidateK: 20, RRFK: 30, NeighborRadius: 1, MaxContextChars: 4000},
		{Name: "rewrite", EnableVector: true, EnableBM25: true, QueryMode: QueryModeRewrite, RewriteQueries: 3, TopK: 5, CandidateK: 20, RRFK: 60},
	} {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("config %+v error = %v", cfg, err)
		}
	}
}

func TestNormalizeRetrievalQueryIsDeterministicAndConservative(t *testing.T) {
	got := NormalizeRetrievalQuery("  Redis\n  WatchDog 是什么？  ")
	if got != "Redis WatchDog 是什么?" {
		t.Fatalf("NormalizeRetrievalQuery() = %q", got)
	}
}

func TestRAGRetrievalConfigRejectsTwoRetrieverMechanismChanges(t *testing.T) {
	base := DefaultRAGRetrievalConfig()
	base.EnableBM25 = false
	candidate := base
	candidate.EnableVector = false
	candidate.EnableBM25 = true
	if _, err := ValidateSingleVariableAblation(base, candidate); err == nil {
		t.Fatal("vector-only to BM25-only changes two mechanisms and must be rejected")
	}
}

func TestRAGRetrievalConfigIncludesChunkerAndRerankerInAblation(t *testing.T) {
	base := DefaultRAGRetrievalConfig()
	candidate := base
	candidate.RerankerMode = RerankerModeNone
	factor, err := ValidateSingleVariableAblation(base, candidate)
	if err != nil || factor != "reranker_mode" {
		t.Fatalf("reranker factor = %q, err=%v", factor, err)
	}

	candidate = base
	candidate.ChunkerVersion = "semantic-v2"
	factor, err = ValidateSingleVariableAblation(base, candidate)
	if err != nil || factor != "chunker_version" {
		t.Fatalf("chunker factor = %q, err=%v", factor, err)
	}
}

func TestStrictExperimentConfigRequiresExplicitChunkerAndReranker(t *testing.T) {
	cfg := DefaultRAGRetrievalConfig()
	cfg.ChunkerVersion = ""
	if err := cfg.ValidateStrictExperiment(); err == nil {
		t.Fatal("strict experiment accepted hidden chunker version")
	}
	cfg = DefaultRAGRetrievalConfig()
	cfg.RerankerMode = ""
	if err := cfg.ValidateStrictExperiment(); err == nil {
		t.Fatal("strict experiment accepted hidden reranker mode")
	}
}

func TestDefaultRAGRetrievalConfigMatchesCurrentChunkerProvenance(t *testing.T) {
	cfg := DefaultRAGRetrievalConfig()
	if cfg.ChunkerStrategy != ChunkerStrategyRecursiveSentence || cfg.ChunkerVersion != RecursiveSentenceChunkerVersion {
		t.Fatalf("default chunker provenance = %q/%q, want %q/%q", cfg.ChunkerStrategy, cfg.ChunkerVersion, ChunkerStrategyRecursiveSentence, RecursiveSentenceChunkerVersion)
	}
}

func TestRAGRetrievalConfigSupportsModelReranker(t *testing.T) {
	cfg := DefaultRAGRetrievalConfig()
	cfg.EnableBM25 = false
	cfg.RerankerMode = RerankerModeModel
	cfg.RerankerVersion = "Qwen/Qwen3-Reranker-4B"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
