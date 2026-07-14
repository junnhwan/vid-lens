package quota

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vid-lens/internal/model"
)

type memoryCompStore struct {
	mu     sync.Mutex
	events map[int64]*model.QuotaCompensation
	next   int64
}

func (s *memoryCompStore) ListDueCompensations(time.Time, int) ([]model.QuotaCompensation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.QuotaCompensation{}
	for _, e := range s.events {
		if e.Status == model.CompensationPending {
			out = append(out, *e)
		}
	}
	return out, nil
}
func (s *memoryCompStore) ClaimCompensation(id int64, token string, now, until time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.events[id]
	if e == nil || e.Status != model.CompensationPending {
		return false, nil
	}
	e.Status = model.CompensationProcessing
	e.LeaseToken = token
	e.LeaseExpiresAt = &until
	return true, nil
}
func (s *memoryCompStore) CompleteCompensation(id int64, token string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.events[id]
	if e.LeaseToken != token {
		return errors.New("stale")
	}
	e.Status = model.CompensationCompleted
	return nil
}
func (s *memoryCompStore) RetryCompensation(id int64, token, msg string, next time.Time, max int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.events[id]
	e.AttemptCount++
	e.Status = model.CompensationPending
	e.LastError = msg
	e.NextAttemptAt = &next
	return nil
}

type flakyUsageCache struct {
	mu      sync.Mutex
	applied map[string]float64
	fail    int
}

func (c *flakyUsageCache) Apply(_ context.Context, e model.QuotaCompensation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail > 0 {
		c.fail--
		return errors.New("redis down")
	}
	if _, ok := c.applied[e.EventKey]; !ok {
		c.applied[e.EventKey] = e.DeltaUnits
	}
	return nil
}

func TestReconcilerReplaysCompensationIdempotentlyAfterCacheFailure(t *testing.T) {
	now := time.Now()
	store := &memoryCompStore{events: map[int64]*model.QuotaCompensation{1: {ID: 1, EventKey: "evt-1", Status: model.CompensationPending, DeltaUnits: 10}}}
	cache := &flakyUsageCache{applied: map[string]float64{}, fail: 1}
	r := NewReconciler(store, cache, ReconcilerConfig{BatchSize: 10, MaxAttempts: 3, Backoff: time.Second, Lease: time.Minute, Now: func() time.Time { return now }, NewToken: func() string { return "lease" }})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.events[1].Status != model.CompensationPending || store.events[1].AttemptCount != 1 {
		t.Fatalf("after fail=%+v", store.events[1])
	}
	now = now.Add(2 * time.Second)
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.events[1].Status != model.CompensationCompleted || cache.applied["evt-1"] != 10 {
		t.Fatalf("after replay=%+v cache=%+v", store.events[1], cache.applied)
	}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(cache.applied) != 1 {
		t.Fatalf("cache=%+v", cache.applied)
	}
}

func TestReconcilerStartStopsAfterContextCancellation(t *testing.T) {
	now := time.Now()
	store := &memoryCompStore{events: map[int64]*model.QuotaCompensation{}}
	cache := &flakyUsageCache{applied: map[string]float64{}}
	reconciler := NewReconciler(store, cache, ReconcilerConfig{Now: func() time.Time { return now }})
	ctx, cancel := context.WithCancel(context.Background())
	done := reconciler.Start(ctx, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not stop after context cancellation")
	}
}
