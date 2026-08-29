package mq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// idempotencyChecker is the consumer-side message-level dedup gate. A repeat
// delivery of the same MessageId is suppressed (returns alreadyHandled=true)
// so the business logic runs exactly once. This is the MQ-layer idempotency;
// it is distinct from the task-state-machine CAS, which suppresses repeat
// processing of the same task across different messages (e.g. retries with a
// new MessageId). The two layers compose: message-level dedup blocks
// redeliveries of the identical message; the task state machine blocks
// cross-message duplicate execution.
type idempotencyChecker interface {
	// Acquire returns alreadyHandled=true when the messageID has been recorded
	// by a prior delivery that is still within its retention window. On first
	// sight it records the key and returns alreadyHandled=false so the caller
	// proceeds; the key is NOT deleted on business success — it expires via TTL,
	// so a crash between Acquire and Ack still suppresses a redelivery.
	Acquire(ctx context.Context, queue, messageID string) (alreadyHandled bool, err error)
	// Release deletes the dedup key. It MUST be called when the handler fails
	// before the message is Nacked for redelivery — otherwise the redelivery
	// (same MessageId) would be suppressed by the still-present key, breaking
	// at-least-once for transient handler failures. On business success the key
	// is intentionally NOT released; it expires via TTL.
	Release(ctx context.Context, queue, messageID string) error
}

// redisIdempotencyChecker implements idempotencyChecker with Redis SETNX.
// Key: mq:dedup:<queue>:<messageID>. TTL is the processing lease plus a guard so
// a redelivery that arrives just after the lease expires is still suppressed
// while the task is finalised; the key then expires to allow later genuine
// retries (which carry a different MessageId anyway).
type redisIdempotencyChecker struct {
	rdb redis.Cmdable
	ttl time.Duration
}

func newRedisIdempotencyChecker(rdb redis.Cmdable, retention time.Duration) *redisIdempotencyChecker {
	if retention <= 0 {
		retention = 40 * time.Minute // 30m default processing lease + 10m guard
	}
	return &redisIdempotencyChecker{rdb: rdb, ttl: retention}
}

func (c *redisIdempotencyChecker) Acquire(ctx context.Context, queue, messageID string) (bool, error) {
	if c == nil || c.rdb == nil || messageID == "" {
		return false, nil
	}
	key := fmt.Sprintf("mq:dedup:%s:%s", queue, messageID)
	ok, err := c.rdb.SetNX(ctx, key, "1", c.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("idempotency setnx %s: %w", key, err)
	}
	return !ok, nil
}

func (c *redisIdempotencyChecker) Release(ctx context.Context, queue, messageID string) error {
	if c == nil || c.rdb == nil || messageID == "" {
		return nil
	}
	key := fmt.Sprintf("mq:dedup:%s:%s", queue, messageID)
	if err := c.rdb.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("idempotency release %s: %w", key, err)
	}
	return nil
}

// noOpIdempotencyChecker disables message-level dedup (used when Redis is not
// configured, e.g. in unit tests that exercise the task-state-machine path only).
type noOpIdempotencyChecker struct{}

func (noOpIdempotencyChecker) Acquire(context.Context, string, string) (bool, error) {
	return false, nil
}

func (noOpIdempotencyChecker) Release(context.Context, string, string) error { return nil }

// errIdempotencyUnavailable is returned by acquireOrSkip when the checker errors;
// the caller Nacks for redelivery rather than risk a duplicate execution.
var errIdempotencyUnavailable = errors.New("idempotency checker unavailable")
