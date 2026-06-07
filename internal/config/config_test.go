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

func TestLoadParsesKafkaRAGIndexTopic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
kafka:
  brokers:
    - localhost:19092
  analyze_topic: "video-analyze"
  transcribe_topic: "video-transcribe"
  download_topic: "video-download"
  rag_index_topic: "video-rag-index"
  consumer_group: "vidlens-worker"
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Kafka.RAGIndexTopic != "video-rag-index" {
		t.Fatalf("rag_index_topic = %q, want video-rag-index", cfg.Kafka.RAGIndexTopic)
	}
}
