package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/observability"
	"vid-lens/internal/pkg/quota"
	"vid-lens/internal/repository"
)

func TestAIUsageGovernorReservesSettlesAndReleasesWithNullUnknownCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AIUsageLedger{}, &model.QuotaCompensation{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositories(db)
	s := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { rc.Close() })
	cache := quota.NewRedisUsageCache(rc, 48*time.Hour)
	now := time.Date(2026, 7, 13, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	g := NewAIUsageGovernor(repos, cache, time.FixedZone("CST", 8*3600))
	g.now = func() time.Time { return now }
	g.newKey = func() string { return "call-success" }
	ctx := observability.WithCorrelation(context.Background(), observability.Correlation{UserID: 7, TaskID: 9, JobID: 11})
	reservation, err := g.Reserve(ctx, ai.Call{Operation: "chat", Provider: "mock", Model: "m", InputChars: 40})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Settle(ctx, reservation, ai.CallResult{}); err != nil {
		t.Fatal(err)
	}
	ledger, err := repos.UsageLedger.GetByIdempotencyKey("call-success")
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Status != model.UsageLedgerSettled || ledger.UsageSource != model.UsageSourceEstimated || ledger.TotalTokens != nil || ledger.EstimatedCost != nil || ledger.UsageDate != "2026-07-13" {
		t.Fatalf("ledger=%+v", ledger)
	}
	g.newKey = func() string { return "call-failed" }
	reservation, err = g.Reserve(ctx, ai.Call{Operation: "embedding", Provider: "mock", Model: "e", InputChars: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Release(ctx, reservation, ai.CallResult{Err: context.DeadlineExceeded}); err != nil {
		t.Fatal(err)
	}
	ledger, err = repos.UsageLedger.GetByIdempotencyKey("call-failed")
	if err != nil || ledger.Status != model.UsageLedgerReleased {
		t.Fatalf("ledger=%+v err=%v", ledger, err)
	}
}
