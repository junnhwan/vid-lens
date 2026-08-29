package quota

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/model"
)

func TestRedisUsageCacheAppliesCompensationExactlyOnceAndKeepsFrozenDate(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })
	cache := NewRedisUsageCache(client, 48*time.Hour)
	e := model.QuotaCompensation{EventKey: "event-1", UserID: 7, UsageDate: "2026-07-13", Kind: model.AICallKindLLM, Unit: model.UsageUnitToken, DeltaUnits: 20}
	if err := cache.Apply(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := cache.Apply(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got, err := client.HGet(context.Background(), "quota:daily:7:2026-07-13", "llm:token").Float64()
	if err != nil || got != 20 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if client.Exists(context.Background(), "quota:daily:7:2026-07-14").Val() != 0 {
		t.Fatal("event crossed date boundary")
	}
}
