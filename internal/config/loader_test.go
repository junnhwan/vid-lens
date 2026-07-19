package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLoaderTestConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "top level",
			raw:  "servre:\n  port: 8080\n",
			want: "servre",
		},
		{
			name: "nested",
			raw:  "rag:\n  candidate_k: 30\n  candiate_k: 40\n",
			want: "candiate_k",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeLoaderTestConfig(t, tt.raw))
			if err == nil {
				t.Fatalf("Load() error = nil, want unknown field %q to be rejected", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %q, want field name %q", err, tt.want)
			}
		})
	}
}

func TestLoadReportsDeprecatedFieldMigration(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "milvus collection", path: "collection", want: "milvus.collection"},
		{name: "rerank endpoint", path: "rerank_endpoint", want: "--rerank-endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "rag:\n  " + tt.path + ": legacy-value\n"
			_, err := Load(writeLoaderTestConfig(t, raw))
			if err == nil {
				t.Fatalf("Load() error = nil, want rag.%s deprecation error", tt.path)
			}
			if !strings.Contains(err.Error(), "rag."+tt.path) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %q, want deprecated path and migration hint %q", err, tt.want)
			}
		})
	}
}

func TestLoadDeprecatedFieldDetectionIgnoresCommentsAndValues(t *testing.T) {
	path := writeLoaderTestConfig(t, `
# rag.collection is documented as deprecated here, not configured.
tools:
  proxy_url: "http://example.test/rag.collection"
`)

	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v, want comments and scalar values ignored", err)
	}
}

func TestLoadParsesMilvusCollection(t *testing.T) {
	path := writeLoaderTestConfig(t, `
rag:
  store: pgvector
milvus:
  address: 127.0.0.1:19530
  collection: vidlens_video_chunks
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Milvus.Collection != "vidlens_video_chunks" {
		t.Fatalf("milvus.collection = %q, want vidlens_video_chunks", cfg.Milvus.Collection)
	}
}

func TestLoadRejectsMultipleYAMLDocuments(t *testing.T) {
	path := writeLoaderTestConfig(t, `
server:
  port: 8080
---
server:
  port: 9090
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want multiple YAML documents rejected")
	}
	if !strings.Contains(err.Error(), "多个 YAML 文档") {
		t.Fatalf("Load() error = %q, want multiple-document diagnostic", err)
	}
}

func TestLoadAcceptsEmptyConfigurationAndAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeLoaderTestConfig(t, ""))
	if err != nil {
		t.Fatalf("Load() error = %v, want value validation left to command-specific validators", err)
	}
	if cfg.Kafka.DownloadTopic != DefaultKafkaDownloadTopic || cfg.Kafka.RAGIndexTopic != DefaultKafkaRAGIndexTopic {
		t.Fatalf("Kafka defaults = %q/%q", cfg.Kafka.DownloadTopic, cfg.Kafka.RAGIndexTopic)
	}
}

func TestLoadParsesCleanupConfiguration(t *testing.T) {
	path := writeLoaderTestConfig(t, `
cleanup:
  scan_interval_seconds: 30
  batch_size: 20
  lease_seconds: 120
  retry_backoff_seconds: 60
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Cleanup.ScanIntervalSeconds != 30 || cfg.Cleanup.BatchSize != 20 || cfg.Cleanup.LeaseSeconds != 120 || cfg.Cleanup.RetryBackoffSeconds != 60 {
		t.Fatalf("cleanup config = %+v", cfg.Cleanup)
	}
}

func TestLoadParsesRerankModel(t *testing.T) {
	path := writeLoaderTestConfig(t, `
rag:
  rerank_model: Qwen/Qwen3-Reranker-4B
  rewrite_queries: 3
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RAG.RerankModel != "Qwen/Qwen3-Reranker-4B" {
		t.Fatalf("rag.rerank_model = %q", cfg.RAG.RerankModel)
	}
	if cfg.RAG.RewriteQueries != 3 {
		t.Fatalf("rag.rewrite_queries = %d", cfg.RAG.RewriteQueries)
	}
}
