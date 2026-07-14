package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
)

const quotaReconcileInterval = 30 * time.Second

type aiGovernanceRuntime struct {
	Admission  *ai.QuotaAdmission
	Reconciler *quota.Reconciler
	Policies   quota.OperationPolicies
}

func newAIGovernanceRuntime(cfg config.AIGovernanceConfig, redisClient redis.Cmdable, repos *repository.Repositories) (*aiGovernanceRuntime, error) {
	if redisClient == nil {
		return nil, fmt.Errorf("AI governance Redis client is nil")
	}
	if repos == nil || repos.RetryBudget == nil || repos.UsageLedger == nil || repos.QuotaCompensation == nil {
		return nil, fmt.Errorf("AI governance repositories are not initialized")
	}
	defaultPolicy, err := quotaPolicyFromConfig(cfg.RedisDefaultPolicy)
	if err != nil {
		return nil, err
	}
	aiPolicy, err := quotaPolicyFromConfig(cfg.RedisAIPolicy)
	if err != nil {
		return nil, err
	}
	policies := quota.OperationPolicies{
		Default: defaultPolicy,
		Operations: map[string]quota.FailurePolicy{
			"asr":         aiPolicy,
			"chat":        aiPolicy,
			"chat_stream": aiPolicy,
			"embedding":   aiPolicy,
			"rerank":      aiPolicy,
		},
	}
	if err := policies.Validate(); err != nil {
		return nil, fmt.Errorf("validate AI quota policies: %w", err)
	}

	usageCache := quota.NewRedisUsageCache(redisClient, 48*time.Hour)
	usageGovernor := service.NewAIUsageGovernor(repos, usageCache, time.Local)
	admission := &ai.QuotaAdmission{
		Limiter:   quota.NewLimiterWithPolicies(redisClient, policies),
		Attempts:  repos.RetryBudget,
		Usage:     usageGovernor,
		User:      ai.BucketRule{Capacity: 30, Rate: 0.5},
		Operation: ai.BucketRule{Capacity: 30, Rate: 0.5},
		Provider:  ai.BucketRule{Capacity: 20, Rate: 0.33},
		Model:     ai.BucketRule{Capacity: 10, Rate: 0.16},
	}
	reconciler := quota.NewReconciler(repos.QuotaCompensation, usageCache, quota.ReconcilerConfig{})
	return &aiGovernanceRuntime{Admission: admission, Reconciler: reconciler, Policies: policies}, nil
}

func quotaPolicyFromConfig(value string) (quota.FailurePolicy, error) {
	switch value {
	case config.RedisPolicyFailOpen:
		return quota.FailOpen, nil
	case config.RedisPolicyFailClosed:
		return quota.FailClosed, nil
	case config.RedisPolicyConservativeLocal:
		return quota.FailConservativeLocal, nil
	default:
		return quota.FailClosed, fmt.Errorf("unknown Redis quota failure policy %q", value)
	}
}

func startQuotaReconciler(ctx context.Context, reconciler *quota.Reconciler, interval time.Duration) <-chan struct{} {
	if reconciler == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return reconciler.Start(ctx, interval)
}
