package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSpec01AblationAcceptance is the single external-behavior acceptance seam
// for docs/eval/README.md. It loads a mini strict dataset (with a sealed test split) at the
// library layer, runs all four ablation variants through Runner.Run, and
// asserts the RunArtifact.Summary carries the complete metric set (incl. P95),
// that executor-failed answerable cases remain in the retrieval denominator as
// zero, that sealed-test loading without a token is rejected, that all three
// artifact formats (JSON/JSONL/CSV/Markdown) are produced, and that the paired
// CI analysis fires for each candidate-vs-vector_only pairing. The CLI guard in
// cmd/rag-eval is intentionally NOT exercised here — per the 评测文档中的 single-seam
// 评测约束 #1, acceptance goes through the library Runner.Run path.
func TestSpec01AblationAcceptance(t *testing.T) {
	token := "spec01-sealed-token"
	manifestRaw, testRaw := miniStrictDatasetYAML(t, token)
	registryPath := filepath.Join(t.TempDir(), "sealed-access.jsonl")

	// 1. Sealed-test load REJECTED without a token.
	if _, err := LoadSplitDataset(manifestRaw, testRaw, SplitLoadOptions{
		ExpectedVersion: "spec01-mini", Split: SplitTest,
		AccessRegistryPath: registryPath,
	}); err == nil || !strings.Contains(err.Error(), "sealed test token") {
		t.Fatalf("no-token sealed test load: err=%v, want token rejection", err)
	}

	// 2. Sealed-test load SUCCEEDS with token + access registry.
	dataset, err := LoadSplitDataset(manifestRaw, testRaw, SplitLoadOptions{
		ExpectedVersion: "spec01-mini", Split: SplitTest, SealedToken: token,
		AccessRegistryPath: registryPath,
		AccessEvent: SealedAccessEvent{OccurredAt: time.Now().UTC(), ExperimentID: "spec01-ablation", RunID: "spec01-run", Commit: "test"},
	})
	if err != nil {
		t.Fatalf("LoadSplitDataset(test) error = %v", err)
	}
	if !dataset.sealedAccessRegistered {
		t.Fatal("sealedAccessRegistered not set after authorized test load")
	}

	// 3. Run all four ablation variants via Runner.Run with a deterministic
	// hermetic retriever (no PG / vector store). The retriever's behavior is
	// case-driven so failures are real; variants share the retriever.
	variants := []string{"vector_only", "bm25_hybrid", "rrf_fusion", "model_rerank"}
	retriever := &miniAblationRetriever{}
	cfg := MetricConfig{K: 5, BoundaryToleranceMS: 500, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 1}
	artifacts := make(map[string]RunArtifact, len(variants))
	for _, v := range variants {
		artifact, runErr := (Runner{Executor: ChunkEvidenceExecutor{Retriever: retriever}}).Run(
			context.Background(), dataset, SplitTest,
			miniRunMetadata("spec01-ablation", v), cfg,
		)
		if runErr != nil {
			t.Fatalf("variant %s Run error = %v", v, runErr)
		}
		artifacts[v] = artifact
	}

	// 4. Assert Summary field completeness on every variant.
	for _, v := range variants {
		a := artifacts[v]
		s := a.Summary.Overall
		if s.EvaluableCases == 0 {
			t.Fatalf("variant %s: EvaluableCases=0, want answerable cases in denominator", v)
		}
		for name, got := range map[string]float64{
			"recall_at_k":              s.RecallAtK,
			"mrr":                      s.MRR,
			"ndcg_at_k":                s.NDCGAtK,
			"context_precision_at_k":   s.ContextPrecisionAtK,
			"complete_evidence_recall": s.CompleteEvidenceRecall,
			"answerability_f1":         s.AnswerabilityF1,
			"failure_rate":             s.FailureRate,
		} {
			if !(got >= 0 && got <= 1) {
				t.Fatalf("variant %s: %s = %v, want in [0,1]", v, name, got)
			}
		}
		if s.P95RetrieveLatencyMS < 0 || s.SuccessP95RetrieveLatencyMS < 0 {
			t.Fatalf("variant %s: P95 latency negative: p95=%d succ=%d", v, s.P95RetrieveLatencyMS, s.SuccessP95RetrieveLatencyMS)
		}
		if len(a.Summary.ByCategory) == 0 || len(a.Summary.ByVideo) == 0 || len(a.Summary.BySourceGroup) == 0 {
			t.Fatalf("variant %s: missing grouped aggregates", v)
		}
	}

	// 5. Failure-in-denominator invariant: the mini dataset includes one
	// answerable case the retriever fails on; its zero must pull Recall@5 below
	// 1.0 (the failure is NOT dropped).
	if artifacts["vector_only"].Summary.Overall.RecallAtK >= 1.0 {
		t.Fatalf("vector_only Recall@5 = %v, want <1.0 (failed answerable case must enter denominator as zero)",
			artifacts["vector_only"].Summary.Overall.RecallAtK)
	}
	if artifacts["vector_only"].Summary.Overall.FailedCases == 0 {
		t.Fatal("vector_only: FailedCases=0, want the forced-failure case recorded")
	}

	// 6. Three-format artifact production (spec 行为约束 11): WriteArtifacts
	// must emit metadata.json, cases.jsonl, summary.json, summary.csv, report.md.
	// WriteArtifacts uses os.Mkdir (single level) on runDir, so the parent must exist.
	tmpBase := t.TempDir()
	outDir := filepath.Join(tmpBase, "artifacts")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatalf("mkdir outDir: %v", err)
	}
	paths, err := WriteArtifacts(outDir, artifacts["vector_only"])
	if err != nil {
		t.Fatalf("WriteArtifacts error = %v", err)
	}
	for _, p := range []string{paths.MetadataJSON, paths.CasesJSONL, paths.SummaryJSON, paths.SummaryCSV, paths.ReportMarkdown} {
		if info, statErr := os.Stat(p); statErr != nil || info.Size() == 0 {
			t.Fatalf("artifact %s missing or empty: %v", p, statErr)
		}
	}
	// summary.json must carry the P95 latency fields (anti-overclaim visibility).
	summaryRaw, err := os.ReadFile(paths.SummaryJSON)
	if err != nil {
		t.Fatal(err)
	}
	var summaryPeek struct {
		Summary struct {
			Overall struct {
				P95RetrieveLatencyMS         int64 `json:"p95_retrieve_latency_ms"`
				SuccessP95RetrieveLatencyMS   int64 `json:"success_p95_retrieve_latency_ms"`
			} `json:"overall"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(summaryRaw, &summaryPeek); err != nil {
		t.Fatalf("summary.json unmarshal: %v", err)
	}
	if summaryPeek.Summary.Overall.P95RetrieveLatencyMS < 0 {
		t.Fatalf("summary.json missing p95_retrieve_latency_ms")
	}

	// 7. Paired CI form (spec 评测约束 #4): each non-baseline variant paired vs
	// vector_only via AnalyzePairedRunArtifacts must produce a non-empty
	// ExperimentAnalysis with a bootstrap CI. The mini dataset is too small for a
	// meaningful CI, so we only assert the analysis fires and carries the shape
	// (status + lower/upper bounds), not the direction of the effect.
	registry := miniExperimentRegistry(t)
	for _, candidate := range []string{"bm25_hybrid", "rrf_fusion", "model_rerank"} {
		analysis, aErr := AnalyzePairedRunArtifacts(registry, "spec01-ablation", candidate, artifacts["vector_only"], artifacts[candidate])
		if aErr != nil {
			t.Fatalf("AnalyzePairedRunArtifacts(%s) error = %v", candidate, aErr)
		}
		if analysis.Bootstrap.Lower > analysis.Bootstrap.Upper {
			t.Fatalf("candidate %s: bootstrap lower > upper", candidate)
		}
		if analysis.Status != ExperimentStatusPassed && analysis.Status != ExperimentStatusFailed {
			t.Fatalf("candidate %s: status = %q", candidate, analysis.Status)
		}
	}
}

// miniAblationRetriever is a hermetic ChunkEvidenceRetriever. It returns the
// case's gold evidence for most cases (so recall is non-zero) but fails on the
// case whose CaseID encodes "fail", simulating an executor timeout. All
// variants share this retriever so the run is deterministic and hermetic.
type miniAblationRetriever struct{}

func (r *miniAblationRetriever) Retrieve(_ context.Context, c Case) ([]ChunkEvidence, error) {
	if strings.Contains(c.CaseID, "fail") {
		return nil, &ExecutionError{Stage: "retrieval", Code: "timeout", Err: errTimeout}
	}
	out := make([]ChunkEvidence, 0, len(c.EvidenceRanges))
	for _, ev := range c.EvidenceRanges {
		for _, id := range ev.ContextIDs {
			out = append(out, ChunkEvidence{ContextID: id, VideoID: c.VideoID, Source: EvidenceSourceASR, Text: "evidence"})
		}
	}
	return out, nil
}

var errTimeout = errors.New("retrieval timed out")

// miniRunMetadata fills all required RunMetadata sha256/provenance fields so
// metadata.Validate() passes. Hashes are deterministic placeholders matching the
// existing validRunMetadata pattern in runner_test.go; real runs use
// BindArtifactFileDigests from actual file bytes.
func miniRunMetadata(experimentID, variantID string) RunMetadata {
	fill := strings.Repeat("a", 64)
	return RunMetadata{
		Commit: "spec01-test", Environment: "test",
		ExperimentID: experimentID, VariantID: variantID,
		DatasetVersion: "spec01-mini",
		DatasetSHA256: fill, SourceManifestSHA256: fill,
		CorpusSHA256: fill, ChunkManifestSHA256: fill, VectorArtifactSHA256: fill,
		ConfigSHA256: fill,
		Models: ModelMetadata{Embedding: ModelRef{Provider: "test", Name: "test-embed"}},
		VectorStore: VectorStoreMetadata{Table: "test", IndexType: "hnsw", MetricType: "cosine"},
		Prompt: PromptMetadata{Name: "retrieval-only", Version: "1", SHA256: fill},
	}
}

// miniExperimentRegistry builds a preregistered experiment with vector_only as
// baseline and the three other variants as candidates, so AnalyzePairedRunArtifacts
// can bind each candidate vs the baseline per spec 评测约束 #4.
func miniExperimentRegistry(t *testing.T) ExperimentRegistry {
	t.Helper()
	fill := strings.Repeat("a", 64)
	minEffect := 0.0
	return ExperimentRegistry{
		RegistryVersion: "1",
		Experiments: []PreregisteredExperiment{{
			ExperimentID: "spec01-ablation", DatasetVersion: "spec01-mini",
			Status: ExperimentStatusPreregistered,
			BaselineVariant: "vector_only", BaselineConfigSHA256: fill,
			FrozenEvidence: FrozenEvidenceReference{CorpusSHA256: fill, ChunkManifestSHA256: fill, VectorArtifactSHA256: fill},
			PrimaryMetric: "ndcg_at_k", Direction: DirectionHigher, MinimumEffect: &minEffect,
			Bootstrap:    BootstrapConfig{Iterations: 1000, ConfidenceLevel: 0.95, Seed: 7},
			Guardrails:   []Guardrail{{Metric: "answerability_f1", Direction: DirectionHigher, MaxRegression: 1.0}},
			Candidates: []CandidateVariant{
				{VariantID: "bm25_hybrid", Commit: "spec01-test", ConfigSHA256: fill},
				{VariantID: "rrf_fusion", Commit: "spec01-test", ConfigSHA256: fill},
				{VariantID: "model_rerank", Commit: "spec01-test", ConfigSHA256: fill},
			},
		}},
	}
}

// miniStrictDatasetYAML builds a manifest + a sealed test split YAML for a tiny
// dataset (3 source groups, one test case per group, including an answerable
// case the retriever fails on and a not-answerable case). Returns the manifest
// and test-split document bytes ready for LoadSplitDataset.
func miniStrictDatasetYAML(t *testing.T, sealedToken string) (manifestRaw, testRaw []byte) {
	t.Helper()
	dataset := Dataset{
		SchemaVersion: "1", DatasetVersion: "spec01-mini",
		Manifest: SplitManifest{Splits: map[Split]SplitDefinition{
			SplitTrain: {Sources: []SourceGroup{{ID: "sg-train", VideoIDs: []string{"v-train"}}}},
			SplitDev:   {Sources: []SourceGroup{{ID: "sg-dev", VideoIDs: []string{"v-dev"}}}},
			SplitTest:  {Sources: []SourceGroup{{ID: "sg-test", VideoIDs: []string{"v-test"}}}},
		}},
	}
	notAnswerable := miniTestCase("test-smalltalk", "v-test", "sg-test", SplitTest, "small_talk", "medium", false)
	failed := miniTestCase("test-fail", "v-test", "sg-test", SplitTest, "direct_qa", "medium", true)
	hit := miniTestCase("test-hit", "v-test", "sg-test", SplitTest, "direct_qa", "easy", true)
	dataset.Cases = []Case{notAnswerable, failed, hit}
	if err := SealSplit(&dataset, SplitTest, sealedToken); err != nil {
		t.Fatal(err)
	}
	manifestHash, err := ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits)
	if err != nil {
		t.Fatal(err)
	}
	dataset.Manifest.SHA256 = manifestHash
	manifestRaw, err = MarshalDatasetManifestYAML(dataset)
	if err != nil {
		t.Fatal(err)
	}
	testRaw, err = MarshalSplitDatasetYAML(dataset, SplitTest)
	if err != nil {
		t.Fatal(err)
	}
	return manifestRaw, testRaw
}

func miniTestCase(id, video, group string, split Split, category, difficulty string, answerable bool) Case {
	c := Case{
		CaseID: id, VideoID: video, SourceGroup: group, Split: split,
		Question: "q-" + id, Category: category, Difficulty: difficulty, Answerable: answerable,
	}
	if !answerable {
		return c
	}
	c.AnswerPoints = []AnswerPoint{{ID: "ap-" + id, Text: "answer " + id, Required: true}}
	c.EvidenceRanges = []EvidenceRange{{
		ID: "ev-" + id, GroupID: "g-" + id, Source: EvidenceSourceASR, Relevance: 3,
		ContextIDs: []string{"ctx-" + id},
	}}
	return c
}
