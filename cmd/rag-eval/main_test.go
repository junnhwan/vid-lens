package main

import (
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
	if len(got.ExpectedChunkKeywords) != 2 || got.ExpectedChunkKeywords[0] != "Avatar" || got.ExpectedChunkKeywords[1] != "four nations" {
		t.Fatalf("ExpectedChunkKeywords = %#v", got.ExpectedChunkKeywords)
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
		{mode: "Vector only", report: service.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 1}},
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
