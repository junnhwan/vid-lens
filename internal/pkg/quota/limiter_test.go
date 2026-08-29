package quota

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func testLimiter(t *testing.T) (*Limiter, *miniredis.Miniredis) {
	s := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { c.Close() })
	return NewLimiter(c, FailClosed), s
}
func TestMultiBucketAllOrNothingAndScope(t *testing.T) {
	l, _ := testLimiter(t)
	ctx := context.Background()
	bs := []Bucket{{Scope: "user", Key: "u1", Capacity: 2, Rate: 0, Cost: 1}, {Scope: "provider", Key: "p1", Capacity: 1, Rate: 0, Cost: 1}}
	d, e := l.Acquire(ctx, bs)
	if e != nil || !d.Allowed {
		t.Fatalf("first=%+v %v", d, e)
	}
	d, e = l.Acquire(ctx, bs)
	if e != nil || d.Allowed || d.Scope != "provider" || d.RetryAfter <= 0 {
		t.Fatalf("second=%+v %v", d, e)
	}
	// provider denial must not consume the remaining user token
	d, e = l.Acquire(ctx, []Bucket{{Scope: "user", Key: "u1", Capacity: 2, Rate: 0, Cost: 1}})
	if e != nil || !d.Allowed {
		t.Fatalf("not atomic: %+v %v", d, e)
	}
}
func TestMultiBucketUsesRedisTimeAndReturnsRemaining(t *testing.T) {
	l, _ := testLimiter(t)
	d, e := l.Acquire(context.Background(), []Bucket{{Scope: "model", Key: "m", Capacity: 1, Rate: 1, Cost: 1}})
	if e != nil || !d.Allowed || d.Remaining != 0 {
		t.Fatalf("d=%+v e=%v", d, e)
	}
	time.Sleep(1100 * time.Millisecond)
	d, e = l.Acquire(context.Background(), []Bucket{{Scope: "model", Key: "m", Capacity: 1, Rate: 1, Cost: 1}})
	if e != nil || !d.Allowed {
		t.Fatalf("refill=%+v %v", d, e)
	}
}
func TestFailurePolicy(t *testing.T) {
	s := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: s.Addr()})
	s.Close()
	for _, x := range []struct {
		p       FailurePolicy
		allowed bool
	}{{FailOpen, true}, {FailClosed, false}} {
		d, e := NewLimiter(c, x.p).Acquire(context.Background(), []Bucket{{Scope: "x", Key: "x", Capacity: 1, Rate: 1, Cost: 1}})
		if e == nil || d.Allowed != x.allowed {
			t.Fatalf("policy %v d=%+v err=%v", x.p, d, e)
		}
	}
}

func TestMultiBucketConcurrentCapacityBound(t *testing.T) {
	l, _ := testLimiter(t)
	const workers = 40
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		go func() {
			d, e := l.Acquire(context.Background(), []Bucket{{Scope: "provider", Key: "shared", Capacity: 7, Rate: 0, Cost: 1}, {Scope: "model", Key: "shared", Capacity: 7, Rate: 0, Cost: 1}})
			results <- e == nil && d.Allowed
		}()
	}
	allowed := 0
	for i := 0; i < workers; i++ {
		if <-results {
			allowed++
		}
	}
	if allowed != 7 {
		t.Fatalf("allowed=%d want=7", allowed)
	}
}
