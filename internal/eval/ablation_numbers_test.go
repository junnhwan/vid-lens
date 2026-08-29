package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestSpec01PrintAblationNumbers is a NON-gating helper test: it runs the same
// four-variant ablation the acceptance test runs and prints the headline
// numbers (Recall@5, MRR, P95, failure rate) per variant to test output. The
// 评测文档中的 "待评测指标" section is backfilled from these printed numbers. It
// always passes — it only emits numbers; gating assertions live in
// TestSpec01AblationAcceptance.
func TestSpec01PrintAblationNumbers(t *testing.T) {
	token := "spec01-numbers"
	manifestRaw, testRaw := miniStrictDatasetYAML(t, token)
	registryPath := filepath.Join(t.TempDir(), "sealed-access.jsonl")
	dataset, err := LoadSplitDataset(manifestRaw, testRaw, SplitLoadOptions{
		ExpectedVersion: "spec01-mini", Split: SplitTest, SealedToken: token,
		AccessRegistryPath: registryPath,
		AccessEvent: SealedAccessEvent{OccurredAt: time.Now().UTC(), ExperimentID: "spec01-ablation", RunID: "spec01-num", Commit: "test"},
	})
	if err != nil {
		t.Fatalf("LoadSplitDataset: %v", err)
	}
	retriever := &miniAblationRetriever{}
	cfg := MetricConfig{K: 5, BoundaryToleranceMS: 500, MaxChunkDurationMS: 30_000, MinEvidenceCoverage: 1}
	variants := []string{"vector_only", "bm25_hybrid", "rrf_fusion", "model_rerank"}
	fmt.Println("=== spec01 ablation numbers (mini sealed dataset, hermetic retriever) ===")
	fmt.Printf("dataset_version=spec01-mini cases=%d videos=1 source_groups=1\n", len(dataset.Cases))
	for _, v := range variants {
		a, err := (Runner{Executor: ChunkEvidenceExecutor{Retriever: retriever}}).Run(
			context.Background(), dataset, SplitTest, miniRunMetadata("spec01-ablation", v), cfg)
		if err != nil {
			t.Fatalf("variant %s Run: %v", v, err)
		}
		s := a.Summary.Overall
		fmt.Printf("variant=%s recall@5=%.3f mrr=%.3f ndcg=%.3f ctxprec=%.3f p95=%dms succp95=%dms failed=%d/%d eval=%d\n",
			v, s.RecallAtK, s.MRR, s.NDCGAtK, s.ContextPrecisionAtK,
			s.P95RetrieveLatencyMS, s.SuccessP95RetrieveLatencyMS,
			s.FailedCases, s.Cases, s.EvaluableCases)
	}
}
