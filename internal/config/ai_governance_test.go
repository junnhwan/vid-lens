package config

import (
	"os"
	"testing"
)

func TestAIGovernanceEnvironmentPoliciesAndValidation(t *testing.T) {
	t.Setenv("VIDLENS_QUOTA_REDIS_DEFAULT_POLICY", "fail_open")
	t.Setenv("VIDLENS_QUOTA_REDIS_AI_POLICY", "conservative_local")
	cfg := defaultAIGovernanceConfig()
	if err := applyAIGovernanceEnv(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.RedisDefaultPolicy != "fail_open" || cfg.RedisAIPolicy != "conservative_local" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	os.Setenv("VIDLENS_QUOTA_REDIS_AI_POLICY", "silently_ignore")
	t.Cleanup(func() { os.Unsetenv("VIDLENS_QUOTA_REDIS_AI_POLICY") })
	cfg = defaultAIGovernanceConfig()
	if err := applyAIGovernanceEnv(&cfg); err == nil {
		t.Fatal("expected invalid policy")
	}
}
