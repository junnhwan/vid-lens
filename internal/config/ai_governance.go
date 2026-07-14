package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	RedisPolicyFailOpen          = "fail_open"
	RedisPolicyFailClosed        = "fail_closed"
	RedisPolicyConservativeLocal = "conservative_local"
)

// AIGovernanceConfig is intentionally environment-driven so deployment policy
// can change without storing operational governance settings or secrets in config.yaml.
type AIGovernanceConfig struct {
	RedisDefaultPolicy string `yaml:"-"`
	RedisAIPolicy      string `yaml:"-"`
}

func defaultAIGovernanceConfig() AIGovernanceConfig {
	return AIGovernanceConfig{
		RedisDefaultPolicy: RedisPolicyFailOpen,
		RedisAIPolicy:      RedisPolicyConservativeLocal,
	}
}

func applyAIGovernanceEnv(cfg *AIGovernanceConfig) error {
	if cfg == nil {
		return fmt.Errorf("AI governance config is nil")
	}
	if value, ok := os.LookupEnv("VIDLENS_QUOTA_REDIS_DEFAULT_POLICY"); ok {
		cfg.RedisDefaultPolicy = strings.ToLower(strings.TrimSpace(value))
	}
	if value, ok := os.LookupEnv("VIDLENS_QUOTA_REDIS_AI_POLICY"); ok {
		cfg.RedisAIPolicy = strings.ToLower(strings.TrimSpace(value))
	}
	return cfg.Validate()
}

func (c AIGovernanceConfig) Validate() error {
	if !validRedisFailurePolicy(c.RedisDefaultPolicy) {
		return fmt.Errorf("invalid Redis default failure policy %q", c.RedisDefaultPolicy)
	}
	if !validRedisFailurePolicy(c.RedisAIPolicy) {
		return fmt.Errorf("invalid Redis AI failure policy %q", c.RedisAIPolicy)
	}
	return nil
}

func validRedisFailurePolicy(value string) bool {
	switch value {
	case RedisPolicyFailOpen, RedisPolicyFailClosed, RedisPolicyConservativeLocal:
		return true
	default:
		return false
	}
}
