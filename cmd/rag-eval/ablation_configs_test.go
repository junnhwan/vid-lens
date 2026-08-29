package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"vid-lens/internal/service"
)

// TestAblationConfigsValidateAndFormProgressiveChain loads the four spec-01
// ablation variant configs from docs/eval/ablation-configs/, asserts each
// passes RAGRetrievalConfig.Validate + ValidateStrictExperiment (frozen provenance),
// and asserts the four variants form the progressive chain the spec describes:
// vector_only (baseline, no BM25/no rerank) -> bm25_hybrid (+BM25) ->
// rrf_fusion (+rewrite/fusion) -> model_rerank (+rerank, deterministic proxy).
func TestAblationConfigsValidateAndFormProgressiveChain(t *testing.T) {
	root := "../../docs/eval/ablation-configs"
	variants := []struct {
		file          string
		name          string
		enableBM25    bool
		queryMode     service.QueryMode
		rerankerMode  string
	}{
		{"vector_only.yaml", "vector_only", false, service.QueryModeOriginal, service.RerankerModeNone},
		{"bm25_hybrid.yaml", "bm25_hybrid", true, service.QueryModeOriginal, service.RerankerModeNone},
		{"rrf_fusion.yaml", "rrf_fusion", true, service.QueryModeRewrite, service.RerankerModeNone},
		{"model_rerank.yaml", "model_rerank", true, service.QueryModeRewrite, service.RerankerModeDeterministic},
	}
	loaded := make(map[string]service.RAGRetrievalConfig, len(variants))
	for _, v := range variants {
		path := filepath.Join(root, v.file)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", v.file, err)
		}
		var cfg service.RAGRetrievalConfig
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("parse %s: %v", v.file, err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s.Validate: %v", v.file, err)
		}
		if err := cfg.ValidateStrictExperiment(); err != nil {
			t.Fatalf("%s.ValidateStrictExperiment: %v", v.file, err)
		}
		if cfg.Name != v.name {
			t.Fatalf("%s: name=%q want %q", v.file, cfg.Name, v.name)
		}
		if cfg.EnableBM25 != v.enableBM25 {
			t.Fatalf("%s: enable_bm25=%v want %v", v.file, cfg.EnableBM25, v.enableBM25)
		}
		if cfg.QueryMode != v.queryMode {
			t.Fatalf("%s: query_mode=%q want %q", v.file, cfg.QueryMode, v.queryMode)
		}
		if cfg.RerankerMode != v.rerankerMode {
			t.Fatalf("%s: reranker_mode=%q want %q", v.file, cfg.RerankerMode, v.rerankerMode)
		}
		loaded[v.name] = cfg
	}

	// Progressive-chain invariant: frozen retrieval-agnostic params (k, chunker,
	// chunk size/overlap) MUST be identical across all variants — see docs/eval/README.md
	// 实验配置 "每档冻结 k/boundary_tolerance_ms/.../min_evidence_coverage".
	base := loaded["vector_only"]
	for name, cfg := range loaded {
		if cfg.TopK != base.TopK || cfg.ChunkerStrategy != base.ChunkerStrategy ||
			cfg.ChunkerVersion != base.ChunkerVersion || cfg.ChunkSize != base.ChunkSize ||
			cfg.ChunkOverlap != base.ChunkOverlap {
			t.Fatalf("variant %q drifts on frozen k/chunker params: %+v vs baseline %+v", name, cfg, base)
		}
	}

	// Honest-label requirement (docs/eval/README.md 的 model_rerank 代理标注约束): the
	// model_rerank config file MUST carry the non-overclaiming proxy label so a
	// reader cannot mistake the deterministic proxy for a real model-rerank lift.
	modelRerankRaw, err := os.ReadFile(filepath.Join(root, "model_rerank.yaml"))
	if err != nil {
		t.Fatalf("read model_rerank.yaml: %v", err)
	}
	for _, want := range []string{"deterministic", "代理", "ModelRerankerFactory"} {
		if !strings.Contains(string(modelRerankRaw), want) {
			t.Fatalf("model_rerank.yaml missing honest-label token %q (docs/eval/README.md model_rerank 代理标注约束)", want)
		}
	}
}
