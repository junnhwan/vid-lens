package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rageval "vid-lens/internal/eval"
	"vid-lens/internal/ragtool"
	"vid-lens/internal/service"
)

func TestEvalProgressLogsStageAndCaseWhenEnabled(t *testing.T) {
	var b strings.Builder
	progress := newEvalProgress(true, &b)

	progress.stage("loaded %d cases", 50)
	progress.caseStep("embedding", 2, 50, evalCase{TaskID: 5, Question: "Which show mentions Avatar?"})

	output := b.String()
	for _, want := range []string{
		"[rag-eval]",
		"loaded 50 cases",
		"embedding case 2/50 task=5",
		"Which show mentions Avatar?",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("progress output missing %q:\n%s", want, output)
		}
	}
}

func TestEvalProgressIsSilentWhenDisabled(t *testing.T) {
	var b strings.Builder
	progress := newEvalProgress(false, &b)

	progress.stage("loaded %d cases", 50)
	progress.caseStep("embedding", 2, 50, evalCase{TaskID: 5, Question: "Which show mentions Avatar?"})

	if b.String() != "" {
		t.Fatalf("progress output = %q, want empty", b.String())
	}
}

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
		{mode: "Vector only", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.939}},
		{mode: "Vector + BM25 + RRF", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.974}},
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
		{mode: "Vector only", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 1, Categories: map[string]ragtool.RAGEvalCategoryReport{"keyword_exact": {TotalCases: 1, EvaluableCases: 1, HitCases: 1, RecallAtK: 1.0, MRR: 1.0}}}},
		{mode: "Vector + BM25 + RRF", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 2}},
		{mode: "Rewrite + MultiQuery + RRF", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 3, RewriteFallbackCount: 2, RewriteFallbackRate: 0.5}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Rerank", report: ragtool.RAGEvalReport{RecallAtK: 1.0, MRR: 0.9, AvgLatencyMs: 4, AvgExpandedContextChars: 128, RerankChangedRankCount: 1}},
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
		{mode: "Vector only", report: ragtool.RAGEvalReport{
			RecallAtK: 1.0,
			MRR:       1.0,
			Categories: map[string]ragtool.RAGEvalCategoryReport{
				"keyword_exact": {TotalCases: 2, EvaluableCases: 2, HitCases: 1, RecallAtK: 0.5, MRR: 0.5, NoResultRate: 0.5, AvgLatencyMs: 12.5},
			},
		}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: ragtool.RAGEvalReport{
			RecallAtK: 1.0,
			MRR:       1.0,
			Categories: map[string]ragtool.RAGEvalCategoryReport{
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
		{mode: "Vector only", report: ragtool.RAGEvalReport{RecallAtK: 0.96, MRR: 0.837}},
		{mode: "Vector + BM25 + RRF", report: ragtool.RAGEvalReport{RecallAtK: 0.98, MRR: 0.878}},
		{mode: "Rewrite + MultiQuery + RRF", report: ragtool.RAGEvalReport{RecallAtK: 0.96, MRR: 0.896}},
		{mode: "Rewrite + MultiQuery + RRF + Window + Model Rerank", report: ragtool.RAGEvalReport{RecallAtK: 0.96, MRR: 0.648}},
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
		{mode: "Vector only", report: ragtool.RAGEvalReport{RecallAtK: 0.96, MRR: 0.837}},
		{mode: "Vector + BM25 + RRF", report: ragtool.RAGEvalReport{RecallAtK: 0.98, MRR: 0.878}},
	}
	answerResults := []answerModeResult{
		{mode: "Ordinary RAG answer", report: ragtool.VideoAgentAnswerEvalReport{
			TotalCases:          2,
			AnswerPointCoverage: 0.50,
			CitationHitRate:     0.50,
			NoAnswerRate:        0.50,
			AvgToolSteps:        0,
			FallbackErrorRate:   0,
			AvgLatencyMs:        120,
		}},
		{mode: "Agentic answer", report: ragtool.VideoAgentAnswerEvalReport{
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

func TestParseEvalFlagsIncludesStrictArtifactAndRegistryOptions(t *testing.T) {
	opts, err := parseEvalFlags([]string{
		"--strict", "--dataset-version", "rag-v1", "--split", "dev",
		"--manifest", "manifest.yaml", "--cases", "dev.yaml",
		"--sealed-test-token", "secret", "--output-dir", "artifacts",
		"--experiment-registry", "registry.yaml", "--experiment-id", "exp-1", "--variant-id", "hybrid",
		"--corpus-hash", strings.Repeat("a", 64), "--chunk-manifest-hash", strings.Repeat("b", 64),
		"--vector-artifact-hash", strings.Repeat("c", 64), "--config-hash", strings.Repeat("d", 64),
	})
	if err != nil {
		t.Fatalf("parseEvalFlags() error = %v", err)
	}
	if !opts.strict || opts.datasetVersion != "rag-v1" || opts.split != rageval.SplitDev || opts.outputDir != "artifacts" || opts.experimentID != "exp-1" || opts.variantID != "hybrid" {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.manifestPath != "manifest.yaml" || opts.casesPath != "dev.yaml" {
		t.Fatalf("physical split paths = manifest %q cases %q", opts.manifestPath, opts.casesPath)
	}
}

func TestParseEvalFlagsIncludesExplicitLegacyModelRerankOptions(t *testing.T) {
	opts, err := parseEvalFlags([]string{
		"--rerank-endpoint", "https://api.example.com/v1/rerank",
		"--rerank-model", "Qwen/Qwen3-Reranker-4B",
	})
	if err != nil {
		t.Fatalf("parseEvalFlags() error = %v", err)
	}
	if opts.rerankEndpoint != "https://api.example.com/v1/rerank" || opts.rerankModel != "Qwen/Qwen3-Reranker-4B" {
		t.Fatalf("legacy model rerank options = endpoint %q model %q", opts.rerankEndpoint, opts.rerankModel)
	}
}

func TestLegacyModelRerankIsEnabledOnlyByExplicitModel(t *testing.T) {
	if (evalOptions{}).legacyModelRerankEnabled() {
		t.Fatal("legacy model rerank unexpectedly enabled by default")
	}
	if !(evalOptions{rerankModel: "Qwen/Qwen3-Reranker-4B"}).legacyModelRerankEnabled() {
		t.Fatal("legacy model rerank disabled despite explicit model")
	}
}

func TestParseEvalFlagsRequiresLegacyRerankModelWhenEndpointIsSet(t *testing.T) {
	_, err := parseEvalFlags([]string{"--rerank-endpoint", "https://api.example.com/v1/rerank"})
	if err == nil || !strings.Contains(err.Error(), "rerank-model") {
		t.Fatalf("parseEvalFlags() error = %v, want rerank-model requirement", err)
	}
}

func TestParseEvalFlagsRejectsLegacyModelRerankOptionsInStrictMode(t *testing.T) {
	for _, args := range [][]string{
		{"--rerank-model", "Qwen/Qwen3-Reranker-4B"},
		{"--rerank-endpoint", "https://api.example.com/v1/rerank"},
	} {
		t.Run(args[0], func(t *testing.T) {
			strictArgs := append([]string{"--strict", "--dataset-version", "rag-v1"}, args...)
			_, err := parseEvalFlags(strictArgs)
			if err == nil || !strings.Contains(err.Error(), "legacy-only") || !strings.Contains(err.Error(), "strict") {
				t.Fatalf("parseEvalFlags() error = %v, want strict-mode boundary error", err)
			}
		})
	}
}

func TestParseEvalFlagsAllowsSnapshotOnlyWithoutPreregisteredHashes(t *testing.T) {
	opts, err := parseEvalFlags([]string{
		"--strict", "--snapshot-only", "--dataset-version", "rag-v1", "--split", "dev",
		"--retrieval-config", "candidate.yaml", "--output-dir", "artifacts",
	})
	if err != nil {
		t.Fatalf("parseEvalFlags() error = %v", err)
	}
	if !opts.snapshotOnly || opts.experimentID != "" || opts.corpusSHA256 != "" {
		t.Fatalf("snapshot opts = %+v", opts)
	}
}

func TestParseEvalFlagsRequiresDatasetVersionInStrictMode(t *testing.T) {
	_, err := parseEvalFlags([]string{"--strict"})
	if err == nil || !strings.Contains(err.Error(), "dataset-version") {
		t.Fatalf("parseEvalFlags() error = %v, want explicit dataset-version error", err)
	}
}

func TestLoadSnapshotRetrievalConfigRequiresExplicitStrictProvenance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.yaml")
	cfg := service.DefaultRAGRetrievalConfig()
	cfg.ChunkerStrategy = service.ChunkerStrategyFixedWindow
	cfg.ChunkerVersion = service.FixedWindowChunkerVersion
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSnapshotRetrievalConfig(path)
	if err != nil {
		t.Fatalf("loadSnapshotRetrievalConfig() error = %v", err)
	}
	if got.ChunkerStrategy != service.ChunkerStrategyFixedWindow || got.ChunkSize != cfg.ChunkSize {
		t.Fatalf("config = %+v", got)
	}

	cfg.ChunkerVersion = ""
	raw, _ = yaml.Marshal(cfg)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSnapshotRetrievalConfig(path); err == nil || !strings.Contains(err.Error(), "chunker") {
		t.Fatalf("error = %v, want strict chunker provenance error", err)
	}
}

func TestLoadCasesKeepsLegacyBaselineCompatibility(t *testing.T) {
	cases, err := loadCases("../../docs/eval/rag-quant-cases.yaml")
	if err != nil {
		t.Fatalf("loadCases() legacy error = %v", err)
	}
	if len(cases) == 0 || cases[0].TaskID == 0 || len(cases[0].ExpectedChunkKeywords) == 0 {
		t.Fatalf("legacy cases not preserved: first=%+v count=%d", cases[0], len(cases))
	}
}

func TestRunStrictValidateOnlyChecksDatasetAndRegistryWithoutRuntimeServices(t *testing.T) {
	datasetPath, registryPath := writeStrictCLIInputs(t)
	opts := strictCLIOptions(datasetPath, registryPath)
	opts.validateOnly = true
	opts.configPath = filepath.Join(t.TempDir(), "missing-config.yaml")

	if err := run(t.Context(), opts); err != nil {
		t.Fatalf("run() strict validate-only error = %v", err)
	}
}

func TestRunStrictValidateOnlyLoadsPhysicalDevSplit(t *testing.T) {
	combinedPath, registryPath := writeStrictCLIInputs(t)
	combinedRaw, err := os.ReadFile(combinedPath)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := rageval.LoadDataset(combinedRaw, rageval.LoadOptions{Mode: rageval.LoadModeStrict, DatasetVersion: "rag-v1"})
	if err != nil {
		t.Fatal(err)
	}
	contentHash, err := rageval.ComputeSplitContentSHA256(dataset, rageval.SplitDev)
	if err != nil {
		t.Fatal(err)
	}
	definition := dataset.Manifest.Splits[rageval.SplitDev]
	definition.ContentSHA256 = contentHash
	dataset.Manifest.Splits[rageval.SplitDev] = definition
	manifestHash, err := rageval.ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits)
	if err != nil {
		t.Fatal(err)
	}
	dataset.Manifest.SHA256 = manifestHash
	manifestRaw, err := rageval.MarshalDatasetManifestYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	devRaw, err := rageval.MarshalSplitDatasetYAML(dataset, rageval.SplitDev)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	devPath := filepath.Join(dir, "dev.yaml")
	if err := os.WriteFile(manifestPath, manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devPath, devRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	opts := strictCLIOptions(devPath, registryPath)
	opts.manifestPath = manifestPath
	opts.validateOnly = true
	opts.configPath = filepath.Join(t.TempDir(), "missing-config.yaml")
	if err := run(t.Context(), opts); err != nil {
		t.Fatalf("run() physical strict dev validate-only error = %v", err)
	}
}

func TestRunStrictDevIsBlockedAfterSameDatasetTestWasAccessed(t *testing.T) {
	datasetPath, registryPath := writeStrictCLIInputs(t)
	opts := strictCLIOptions(datasetPath, registryPath)
	opts.validateOnly = true
	opts.sealedAccessRegistry = filepath.Join(t.TempDir(), "sealed-access.jsonl")
	if err := rageval.AppendSealedAccess(opts.sealedAccessRegistry, rageval.SealedAccessEvent{
		OccurredAt: time.Now(), DatasetVersion: "rag-v1", RunID: "test-run", ExperimentID: "exp-final", Commit: "abc123",
		DatasetSHA256: strings.Repeat("e", 64), TestContentSHA256: strings.Repeat("f", 64),
	}); err != nil {
		t.Fatal(err)
	}

	err := run(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "new dataset version") {
		t.Fatalf("run() error = %v, want sealed tuning guard", err)
	}
}

func TestRunStrictExecutionRequiresProductionRetrievalConfigs(t *testing.T) {
	datasetPath, registryPath := writeStrictCLIInputs(t)
	opts := strictCLIOptions(datasetPath, registryPath)

	err := run(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "--retrieval-config") {
		t.Fatalf("run() error = %v, want production retrieval config error", err)
	}
}

func strictCLIOptions(datasetPath, registryPath string) evalOptions {
	return evalOptions{
		strict: true, datasetVersion: "rag-v1", split: rageval.SplitDev,
		casesPath: datasetPath, experimentRegistry: registryPath,
		experimentID: "exp-1", variantID: "hybrid", commit: "abc123",
		configSHA256: strings.Repeat("a", 64), corpusSHA256: strings.Repeat("b", 64),
		chunkManifestSHA256: strings.Repeat("c", 64), vectorArtifactSHA256: strings.Repeat("d", 64),
		sealedAccessRegistry: filepath.Join(filepath.Dir(datasetPath), "sealed-access.jsonl"),
		outputDir:            filepath.Join(filepath.Dir(datasetPath), "artifacts"),
	}
}

func writeStrictCLIInputs(t *testing.T) (string, string) {
	t.Helper()
	dataset := rageval.Dataset{
		SchemaVersion: "1", DatasetVersion: "rag-v1",
		Manifest: rageval.SplitManifest{Splits: map[rageval.Split]rageval.SplitDefinition{
			rageval.SplitTrain: {}, rageval.SplitDev: {}, rageval.SplitTest: {},
		}},
	}
	if err := rageval.SealSplit(&dataset, rageval.SplitTest, "test-token"); err != nil {
		t.Fatal(err)
	}
	manifestHash, err := rageval.ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits)
	if err != nil {
		t.Fatal(err)
	}
	dataset.Manifest.SHA256 = manifestHash
	raw, err := rageval.MarshalDatasetYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	datasetPath := filepath.Join(dir, "dataset.yaml")
	if err := os.WriteFile(datasetPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(dir, "registry.yaml")
	registry := `registry_version: "1"
experiments:
  - experiment_id: exp-1
    dataset_version: rag-v1
    status: preregistered
    baseline_variant: vector-only
    baseline_config_sha256: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    frozen_evidence:
      corpus_sha256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      chunk_manifest_sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
      vector_artifact_sha256: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    primary_metric: ndcg_at_k
    direction: higher
    minimum_effect: 0.01
    bootstrap: {iterations: 1000, confidence_level: 0.95, seed: 7}
    guardrails:
      - {metric: answerability_f1, direction: higher, max_regression: 0.01}
    candidates:
      - variant_id: hybrid
        commit: abc123
        config_sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(registryPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	return datasetPath, registryPath
}

func TestLoadRetrievalConfigsEnforcesHashAndSingleVariable(t *testing.T) {
	base := service.DefaultRAGRetrievalConfig()
	base.Name = "base"
	base.QueryMode = service.QueryModeOriginal
	base.RewriteQueries = 1
	base.NeighborRadius = 0
	candidate := base
	candidate.Name = "candidate"
	candidate.EnableBM25 = false
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	candidatePath := filepath.Join(dir, "candidate.yaml")
	baseRaw, _ := yaml.Marshal(base)
	candidateRaw, _ := yaml.Marshal(candidate)
	if err := os.WriteFile(basePath, baseRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, candidateRaw, 0600); err != nil {
		t.Fatal(err)
	}
	baseSum := sha256.Sum256(baseRaw)
	candidateSum := sha256.Sum256(candidateRaw)
	gotBase, gotCandidate, factor, err := loadRetrievalConfigs(basePath, candidatePath, hex.EncodeToString(baseSum[:]), hex.EncodeToString(candidateSum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if factor != "enable_bm25" || gotCandidate.EnableBM25 || gotBase.EnableBM25 == gotCandidate.EnableBM25 {
		t.Fatalf("factor=%q base=%+v candidate=%+v", factor, gotBase, gotCandidate)
	}
	if _, _, _, err := loadRetrievalConfigs(basePath, candidatePath, strings.Repeat("f", 64), hex.EncodeToString(candidateSum[:])); err == nil || !strings.Contains(err.Error(), "baseline retrieval config hash mismatch") {
		t.Fatalf("baseline mismatch error = %v", err)
	}
}

func TestConfiguredRerankerAndEvidenceUseStrictConfigAndFullContext(t *testing.T) {
	none, err := configuredReranker(service.RerankerModeNone)
	if err != nil || none != nil {
		t.Fatalf("none reranker = %#v, err=%v", none, err)
	}
	deterministic, err := configuredReranker(service.RerankerModeDeterministic)
	if err != nil || deterministic == nil {
		t.Fatalf("deterministic reranker = %#v, err=%v", deterministic, err)
	}
	if _, err := configuredReranker("unknown"); err == nil {
		t.Fatal("unsupported reranker error = nil")
	}
	chunk := service.RetrievedChunk{EvidenceID: "e1", AnchorContent: "anchor", Content: "expanded full context"}
	evidence := chunkEvidenceFromCitation(chunk, "video-a")
	if evidence.Text != "expanded full context" || evidence.ContextID != "e1" {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestStrictRetrievalMetricConfigIsValid(t *testing.T) {
	cfg := strictRetrievalMetricConfig(5)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("strict retrieval metric config must be runnable: %v", err)
	}
	if cfg.K != 5 {
		t.Fatalf("K = %d, want 5", cfg.K)
	}
}
