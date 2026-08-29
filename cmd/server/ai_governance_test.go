package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
)

func TestNewAIGovernanceRuntimeWiresDurableComponentsAndConfiguredPolicies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:server-governance?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AIRetryBudget{}, &model.AIRetryAttempt{}, &model.AIUsageLedger{}, &model.QuotaCompensation{}); err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	repos := repository.NewRepositories(db)

	runtime, err := newAIGovernanceRuntime(config.AIGovernanceConfig{
		RedisDefaultPolicy: config.RedisPolicyFailOpen,
		RedisAIPolicy:      config.RedisPolicyFailClosed,
	}, redisClient, repos)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Admission == nil || runtime.Admission.Attempts != repos.RetryBudget || runtime.Admission.Usage == nil {
		t.Fatalf("admission not fully wired: %+v", runtime.Admission)
	}
	if runtime.Reconciler == nil {
		t.Fatal("quota reconciler not wired")
	}
	if runtime.Policies.Policy("health") != quota.FailOpen || runtime.Policies.Policy("asr") != quota.FailClosed || runtime.Policies.Policy("embedding") != quota.FailClosed {
		t.Fatalf("policies=%+v", runtime.Policies)
	}

	ctx := ai.WithGovernanceContext(context.Background(), ai.GovernanceContext{Subject: "user-7"})
	reservation, err := runtime.Admission.Usage.Reserve(ctx, ai.Call{Operation: "chat", Provider: "mock", Model: "m", InputChars: 20})
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Key != "" {
		t.Fatal("a request without business correlation must not create a user ledger row")
	}
}

func TestQuotaPolicyFromConfigRejectsUnknownValue(t *testing.T) {
	if _, err := quotaPolicyFromConfig("unknown"); err == nil {
		t.Fatal("expected invalid failure policy")
	}
}

func TestStartQuotaReconcilerStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := startQuotaReconciler(ctx, nil, time.Millisecond)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciler worker did not stop")
	}
}
