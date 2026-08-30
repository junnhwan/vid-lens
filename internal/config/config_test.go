package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExpandsEnvironmentVariablesForMimoConfig(t *testing.T) {
	t.Setenv("MIMO_API_KEY", "tp-test-key")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  port: 8080
ai:
  provider: mimo
  mimo_api_key: "${MIMO_API_KEY}"
  mimo_base_url: "https://token-plan-cn.xiaomimimo.com/v1"
  asr_model: "mimo-v2.5-asr"
  llm_model: "mimo-v2.5"
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.AI.Provider != "mimo" {
		t.Fatalf("expected provider mimo, got %q", cfg.AI.Provider)
	}
	if cfg.AI.MimoAPIKey != "tp-test-key" {
		t.Fatalf("expected expanded API key, got %q", cfg.AI.MimoAPIKey)
	}
}

func TestLoadParsesAllowedVideoHosts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tools:
  ffmpeg_path: "ffmpeg"
  ytdlp_path: "yt-dlp"
  allowed_video_hosts:
    - bilibili.com
    - youtube.com
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Tools.AllowedVideoHosts) != 2 {
		t.Fatalf("allowed_video_hosts = %#v, want 2 entries", cfg.Tools.AllowedVideoHosts)
	}
	if cfg.Tools.AllowedVideoHosts[0] != "bilibili.com" || cfg.Tools.AllowedVideoHosts[1] != "youtube.com" {
		t.Fatalf("unexpected allowed_video_hosts: %#v", cfg.Tools.AllowedVideoHosts)
	}
}

func TestLoadParsesMQRAGIndexQueue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
mq:
  brokers:
    - localhost:5672
  analyze_queue: "video-analyze"
  transcribe_queue: "video-transcribe"
  download_queue: "video-download"
  rag_index_queue: "video-rag-index"
  consumer_group: "vidlens-worker"
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MQ.RAGIndexQueue != "video-rag-index" {
		t.Fatalf("rag_index_queue = %q, want video-rag-index", cfg.MQ.RAGIndexQueue)
	}
}

func TestLoadParsesPGVectorBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
rag:
  enabled: true
  store: pgvector
  vector_table: vidlens_rag_vectors
database:
  host: 127.0.0.1
  port: 5433
  username: vidlens
  password: secret
  dbname: vidlens
  sslmode: disable
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RAG.Store != "pgvector" {
		t.Fatalf("rag.store = %q, want pgvector", cfg.RAG.Store)
	}
	if cfg.Database.Port != 5433 || cfg.Database.DBName != "vidlens" || cfg.RAG.VectorTable != "vidlens_rag_vectors" {
		t.Fatalf("unexpected database config: %+v", cfg.Database)
	}
}

func TestLoadAppliesMQQueueDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("mq:\n  brokers:\n    - localhost:5672\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MQ.DownloadQueue != DefaultMQDownloadQueue {
		t.Fatalf("download_queue = %q, want %q", cfg.MQ.DownloadQueue, DefaultMQDownloadQueue)
	}
	if cfg.MQ.RAGIndexQueue != DefaultMQRAGIndexQueue {
		t.Fatalf("rag_index_queue = %q, want %q", cfg.MQ.RAGIndexQueue, DefaultMQRAGIndexQueue)
	}
	if cfg.MQ.ASRConcurrency != DefaultASRConcurrency || cfg.MQ.ASRMaxRetries != DefaultASRMaxRetries || len(cfg.MQ.ASRRetryBackoffMS) != 2 {
		t.Fatalf("ASR processing defaults = %+v", cfg.MQ)
	}
}

func TestLoadKeepsAgentMemoryDisabledWithBoundedDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Memory.Enabled || cfg.Memory.TopK != 6 || cfg.Memory.MaxChars != 2000 || cfg.Memory.MaxTokens != 500 || cfg.Memory.QueueSize != 128 {
		t.Fatalf("memory defaults = %+v", cfg.Memory)
	}
}

func TestLoadPreservesExplicitMQQueues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("mq:\n  download_queue: custom-download\n  rag_index_queue: custom-rag\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MQ.DownloadQueue != "custom-download" || cfg.MQ.RAGIndexQueue != "custom-rag" {
		t.Fatalf("explicit queues were overwritten: %+v", cfg.MQ)
	}
}

func TestLoadAllowsDisablingInOperationASRRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("mq:\n  asr_max_retries: 0\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MQ.ASRMaxRetries != 0 || cfg.MQ.ASRConcurrency != DefaultASRConcurrency {
		t.Fatalf("ASR controls = %+v", cfg.MQ)
	}
}

func TestLoadParsesPostgresAsSingleApplicationDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  host: 127.0.0.1
  port: 5433
  username: vidlens
  password: secret
  dbname: vidlens
  sslmode: disable
rag:
  store: pgvector
  vector_table: vidlens_rag_vectors
legacy_mysql:
  host: 127.0.0.1
  port: 3307
  username: root
  password: secret
  dbname: vidlens
  charset: utf8mb4
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Port != 5433 || cfg.Database.SSLMode != "disable" {
		t.Fatalf("application database = %+v, want PostgreSQL configuration", cfg.Database)
	}
	if cfg.RAG.VectorTable != "vidlens_rag_vectors" {
		t.Fatalf("rag.vector_table = %q", cfg.RAG.VectorTable)
	}
	if cfg.LegacyMySQL.Port != 3307 || cfg.LegacyMySQL.Charset != "utf8mb4" {
		t.Fatalf("legacy MySQL = %+v", cfg.LegacyMySQL)
	}
}

func TestLoadRejectsRemovedTopLevelPostgresConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("postgres:\n  host: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want removed top-level postgres field to be rejected")
	}
}
