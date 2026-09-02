package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsDotEnvNextToConfigAndExpandsDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	unsetEnvForTest(t, "TEST_DOTENV_PROVIDER")
	unsetEnvForTest(t, "TEST_DOTENV_KEY")
	unsetEnvForTest(t, "TEST_DOTENV_URL")
	unsetEnvForTest(t, "TEST_DOTENV_FALLBACK")
	unsetEnvForTest(t, "VIDLENS_QUOTA_REDIS_DEFAULT_POLICY")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
# local AI relay settings
export TEST_DOTENV_PROVIDER = openai_compatible
TEST_DOTENV_KEY='dotenv-key'
TEST_DOTENV_URL="http://relay.example/v1"
VIDLENS_QUOTA_REDIS_DEFAULT_POLICY=fail_closed
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`
ai:
  provider: "${TEST_DOTENV_PROVIDER:-openai_compatible}"
  api_key: "${TEST_DOTENV_KEY}"
  base_url: "${TEST_DOTENV_URL:-https://default.example/v1}"
tools:
  proxy_url: "${TEST_DOTENV_FALLBACK:-http://fallback.example}"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Provider != "openai_compatible" || cfg.AI.APIKey != "dotenv-key" || cfg.AI.BaseURL != "http://relay.example/v1" {
		t.Fatalf("AI config = %+v", cfg.AI)
	}
	if cfg.AIGovernance.RedisDefaultPolicy != RedisPolicyFailClosed {
		t.Fatalf("Redis default policy = %q", cfg.AIGovernance.RedisDefaultPolicy)
	}
	if cfg.Tools.ProxyURL != "http://fallback.example" {
		t.Fatalf("proxy_url = %q, want default value", cfg.Tools.ProxyURL)
	}
}

func TestLoadProcessEnvironmentOverridesDotEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	t.Setenv("TEST_DOTENV_PRECEDENCE", "process-value")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TEST_DOTENV_PRECEDENCE=dotenv-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("tools:\n  proxy_url: \"${TEST_DOTENV_PRECEDENCE}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tools.ProxyURL != "process-value" {
		t.Fatalf("proxy_url = %q, want process-value", cfg.Tools.ProxyURL)
	}
}

func TestParseDotEnvRejectsMalformedLines(t *testing.T) {
	_, err := parseDotEnv(strings.NewReader("MALFORMED\n"))
	if err == nil || !strings.Contains(err.Error(), "缺少 '='") {
		t.Fatalf("parseDotEnv() error = %v, want missing equals diagnostic", err)
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	previous, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}
