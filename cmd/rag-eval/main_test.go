package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vid-lens/internal/service"
)

func TestLoadCasesReadsTaskIDAndExpectedKeywords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.yaml")
	data := []byte(`
- task_id: 5
  task_hint: "sample"
  category: "keyword_exact"
  question: "Which show mentions Avatar?"
  expected_chunk_keywords:
    - "Avatar"
    - "four nations"
  expected_answer_points:
    - "Avatar is mentioned."
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cases, err := loadCases(path)
	if err != nil {
		t.Fatalf("loadCases() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	got := cases[0]
	if got.TaskID != 5 {
		t.Fatalf("TaskID = %d, want 5", got.TaskID)
	}
	if got.Question != "Which show mentions Avatar?" {
		t.Fatalf("Question = %q", got.Question)
	}
	if got.Category != "keyword_exact" {
		t.Fatalf("Category = %q, want keyword_exact", got.Category)
	}
	if len(got.ExpectedChunkKeywords) != 2 || got.ExpectedChunkKeywords[0] != "Avatar" || got.ExpectedChunkKeywords[1] != "four nations" {
		t.Fatalf("ExpectedChunkKeywords = %#v", got.ExpectedChunkKeywords)
	}
}

func TestCachedEvalRewriterReturnsCachedResultAndError(t *testing.T) {
	want := service.RewriteResult{
		Original: "question",
		Queries:  []string{"query one", "query two"},
		UsedLLM:  true,
	}
	wantErr := errors.New("observability error")
	rewriter := cachedEvalRewriter{result: want, err: wantErr}

	got, err := rewriter.Rewrite(context.Background(), service.RewriteInput{Question: "ignored"})

	if err != wantErr {
		t.Fatalf("err = %v, want cached error", err)
	}
	if strings.Join(got.Queries, "|") != "query one|query two" || !got.UsedLLM {
		t.Fatalf("result = %+v, want cached rewrite", got)
	}
}

func TestLoadCasesRejectsMissingRequiredFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.yaml")
	data := []byte(`
- task_hint: "sample"
  question: ""
  expected_chunk_keywords: []
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := loadCases(path); err == nil {
		t.Fatal("loadCases() error = nil, want validation error")
	}
}

func TestRenderMarkdownDoesNotClaimRecallImprovedWhenOnlyMRRImproves(t *testing.T) {
	markdown := renderMarkdown(evalOptions{environment: "test", commit: "abc123"}, []int64{5}, 19, "text-embedding-3-small", 5, 30, []modeResult{
		{mode: "Vector only", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.939}},
		{mode: "Vector + BM25 + RRF", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.974}},
	})

	if strings.Contains(markdown, "Recall@5 从 100.0% 提升至 100.0%") {
		t.Fatalf("renderMarkdown() claimed equal Recall@5 improved:\n%s", markdown)
	}
	if !strings.Contains(markdown, "Recall@5 均为 100.0%") {
		t.Fatalf("renderMarkdown() missing equal Recall@5 wording:\n%s", markdown)
	}
}

func TestRenderMarkdownIncludesRAG2ModesAndMetrics(t *testing.T) {
	results := []modeResult{
		{mode: "Vector only", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 1, Categories: map[string]service.RAGEvalCategoryReport{"keyword_exact": {TotalCases: 1, EvaluableCases: 1, HitCases: 1, RecallAtK: 1.0, MRR: 1.0}}}},
		{mode: "Vector + BM25 + RRF", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 2}},
		{mode: "Rewrite + MultiQuery + RRF", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 3, RewriteFallbackCount: 2, RewriteFallbackRate: 0.5}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Rerank", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 4, AvgExpandedContextChars: 128, RerankChangedRankCount: 1}},
	}

	markdown := renderMarkdown(evalOptions{environment: "test", commit: "abc123"}, []int64{5}, 4, "text-embedding-3-small", 5, 30, results)

	for _, want := range []string{
		"Vector only",
		"Vector + BM25 + RRF",
		"Rewrite + MultiQuery + RRF",
		"Rewrite + MultiQuery + RRF + Window + Rerank",
		"Rewrite Fallback Rate",
		"Avg Expanded Context",
		"Rerank Changed Rank Count",
		"Citation Context Hit Rate",
		"Expanded Context Hit Rate",
		"Per-Category Metrics",
		"keyword_exact",
		"设计并实现 VidLens 视频 RAG 检索评测框架",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("renderMarkdown() missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "提升") {
		t.Fatalf("renderMarkdown() should not claim improvement when metrics are equal:\n%s", markdown)
	}
}

func TestRenderMarkdownIncludesPerCategoryMetrics(t *testing.T) {
	results := []modeResult{
		{mode: "Vector only", report: service.RAGEvalReport{
			RecallAtK: 1.0,
			MRR:       1.0,
			Categories: map[string]service.RAGEvalCategoryReport{
				"keyword_exact": {TotalCases: 2, EvaluableCases: 2, HitCases: 1, RecallAtK: 0.5, MRR: 0.5, NoResultRate: 0.5, AvgLatencyMs: 12.5},
			},
		}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: service.RAGEvalReport{
			RecallAtK: 1.0,
			MRR:       1.0,
			Categories: map[string]service.RAGEvalCategoryReport{
				"keyword_exact": {TotalCases: 2, EvaluableCases: 2, HitCases: 2, RecallAtK: 1.0, MRR: 1.0, NoResultRate: 0.0, AvgLatencyMs: 30.0, RerankChangedRankCount: 1},
			},
		}},
	}

	markdown := renderMarkdown(evalOptions{environment: "test", commit: "abc123"}, []int64{5}, 2, "text-embedding-3-small", 5, 30, results)

	for _, want := range []string{
		"### Per-Category Metrics",
		"| Mode | Category | Cases | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context |",
		"| Vector only | keyword_exact | 2 | 50.0% | 0.500 | 50.0% | 12.50 ms |",
		"| Rewrite + MultiQuery + RRF + Window + Model Rerank | keyword_exact | 2 | 100.0% | 1.000 | 0.0% | 30.00 ms |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("renderMarkdown() missing %q:\n%s", want, markdown)
		}
	}
}

func TestRenderMarkdownRecordsHybridImprovementEvenWhenModelRerankRegresses(t *testing.T) {
	results := []modeResult{
		{mode: "Vector only", report: service.RAGEvalReport{RecallAtK: 0.96, MRR: 0.837}},
		{mode: "Vector + BM25 + RRF", report: service.RAGEvalReport{RecallAtK: 0.98, MRR: 0.878}},
		{mode: "Rewrite + MultiQuery + RRF", report: service.RAGEvalReport{RecallAtK: 0.96, MRR: 0.896}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: service.RAGEvalReport{RecallAtK: 0.96, MRR: 0.648}},
	}

	markdown := renderMarkdown(evalOptions{environment: "test", commit: "abc123"}, []int64{2, 5, 6}, 50, "text-embedding-3-small", 5, 30, results)

	for _, want := range []string{
		"BM25+RRF improved Recall@5 from 96.0% to 98.0% and improved MRR from 0.837 to 0.878",
		"Model Rerank did not improve ranking in this run",
		"不要写 model rerank 提升检索排名的简历 claim",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("renderMarkdown() missing %q:\n%s", want, markdown)
		}
	}
}

func TestRenderMarkdownIncludesAgentAnswerEvaluation(t *testing.T) {
	retrievalResults := []modeResult{
		{mode: "Vector only", report: service.RAGEvalReport{RecallAtK: 0.96, MRR: 0.837}},
		{mode: "Vector + BM25 + RRF", report: service.RAGEvalReport{RecallAtK: 0.98, MRR: 0.878}},
	}
	answerResults := []answerModeResult{
		{mode: "Ordinary RAG answer", report: service.VideoAgentAnswerEvalReport{
			TotalCases:          2,
			AnswerPointCoverage: 0.50,
			CitationHitRate:     0.50,
			NoAnswerRate:        0.50,
			AvgToolSteps:        0,
			FallbackErrorRate:   0,
			AvgLatencyMs:        120,
		}},
		{mode: "Agentic answer", report: service.VideoAgentAnswerEvalReport{
			TotalCases:          2,
			AnswerPointCoverage: 0.75,
			CitationHitRate:     1.00,
			NoAnswerRate:        0,
			AvgToolSteps:        3.5,
			FallbackErrorRate:   0,
			AvgLatencyMs:        240,
		}},
	}

	markdown := renderMarkdownWithAgentAnswerEval(evalOptions{environment: "test", commit: "abc123"}, []int64{2, 5}, 2, "text-embedding-3-small", 5, 30, retrievalResults, answerResults)

	for _, want := range []string{
		"## Agent Answer Evaluation",
		"| Mode | Answer Point Coverage | Citation Hit Rate | No Answer Rate | Avg Tool Steps | Fallback/Error Rate | Avg Latency |",
		"| Ordinary RAG answer | 50.0% | 50.0% | 50.0% | 0.0 | 0.0% | 120.00 ms |",
		"| Agentic answer | 75.0% | 100.0% | 0.0% | 3.5 | 0.0% | 240.00 ms |",
		"Agentic answer improved deterministic answer-point coverage from 50.0% to 75.0%",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("renderMarkdownWithAgentAnswerEval() missing %q:\n%s", want, markdown)
		}
	}
}
