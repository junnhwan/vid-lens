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
