package quota

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"sync"
	"time"
)

type FailurePolicy int

const (
	FailOpen FailurePolicy = iota
	FailClosed
	FailConservativeLocal
)

type FailurePolicyResolver interface {
	Policy(operation string) FailurePolicy
}
type Bucket struct {
	Scope, Key           string
	Capacity, Rate, Cost float64
}
type Decision struct {
	Allowed    bool
	Scope      string
	Remaining  float64
	RetryAfter time.Duration
	Degraded   bool
}
type Limiter struct {
	client   redis.Cmdable
	failure  FailurePolicy
	resolver FailurePolicyResolver
	mu       sync.Mutex
	local    map[string]time.Time
}

func NewLimiter(c redis.Cmdable, p FailurePolicy) *Limiter {
	return &Limiter{client: c, failure: p, local: map[string]time.Time{}}
}
func NewLimiterWithPolicies(c redis.Cmdable, p FailurePolicyResolver) *Limiter {
	return &Limiter{client: c, resolver: p, failure: FailClosed, local: map[string]time.Time{}}
}
func (l *Limiter) Acquire(ctx context.Context, b []Bucket) (Decision, error) {
	return l.AcquireForOperation(ctx, "", b)
}
func (l *Limiter) AcquireForOperation(ctx context.Context, operation string, b []Bucket) (Decision, error) {
	if len(b) == 0 {
		return Decision{Allowed: true}, nil
	}
	keys := make([]string, len(b))
	args := make([]any, 0, len(b)*4)
	for i, x := range b {
		if x.Key == "" || x.Scope == "" || x.Capacity <= 0 || x.Rate < 0 || x.Cost <= 0 {
			return Decision{}, fmt.Errorf("invalid bucket %d", i)
		}
		keys[i] = "quota:" + x.Scope + ":" + x.Key
		args = append(args, x.Capacity, x.Rate, x.Cost, x.Scope)
	}
	v, e := multiBucketScript.Run(ctx, l.client, keys, args...).Slice()
	if e != nil {
		return l.onFailure(operation, keys, e)
	}
	if len(v) != 4 {
		return Decision{}, errors.New("invalid quota lua response")
	}
	allowed := toInt(v[0]) == 1
	return Decision{Allowed: allowed, Scope: fmt.Sprint(v[1]), Remaining: float64(toInt(v[2])), RetryAfter: time.Duration(toInt(v[3])) * time.Millisecond}, nil
}
func (l *Limiter) onFailure(operation string, keys []string, cause error) (Decision, error) {
	p := l.failure
	if l.resolver != nil {
		p = l.resolver.Policy(operation)
	}
	switch p {
	case FailOpen:
		return Decision{Allowed: true, Scope: "redis", Degraded: true}, cause
	case FailConservativeLocal:
		now := time.Now()
		key := operation
		if len(keys) > 0 {
			key = keys[len(keys)-1]
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		until := l.local[key]
		if now.Before(until) {
			return Decision{Allowed: false, Scope: "local_conservative", RetryAfter: time.Until(until), Degraded: true}, cause
		}
		l.local[key] = now.Add(time.Second)
		return Decision{Allowed: true, Scope: "local_conservative", Remaining: 0, Degraded: true}, cause
	default:
		return Decision{Allowed: false, Scope: "redis", Degraded: true}, cause
	}
}
func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case string:
		var n int64
		fmt.Sscan(x, &n)
		return n
	}
	return 0
}
