package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

// All dimensions share a Redis hash slot and are checked BEFORE any deduction.
// Redis server time makes refill independent of individual API hosts' clocks.
var takeTokens = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000)
local tokens, stamps = {}, {}
local retry = 0
for i,key in ipairs(KEYS) do
  local capacity = tonumber(ARGV[(i-1)*2+1])
  local period = tonumber(ARGV[(i-1)*2+2])
  local state = redis.call('HMGET',key,'tokens','updated')
  local last = tonumber(state[2]) or now
  stamps[i] = math.max(now,last)
  tokens[i] = math.min(capacity,(tonumber(state[1]) or capacity) + math.max(0,now-last)/period)
  if tokens[i] < 1 then retry = math.max(retry,math.ceil((1-tokens[i])*period + math.max(0,last-now))) end
end
if retry > 0 then return retry end
for i,key in ipairs(KEYS) do
  redis.call('HSET',key,'tokens',tokens[i]-1,'updated',stamps[i])
  redis.call('PEXPIRE',key,math.ceil(tonumber(ARGV[(i-1)*2+1])*tonumber(ARGV[(i-1)*2+2])))
end
return 0
`)

type Limiter struct {
	Redis  *redis.Client
	Limits config.SubmissionLimits
	// Tests use their own prefix and remove only keys from that namespace.
	Prefix string
}

func New(options *redis.Options, limits config.SubmissionLimits) *Limiter {
	var client *redis.Client
	if options != nil {
		bounded := *options
		bounded.Dialer = nil // Rebuild the default dialer using the shorter budget.
		bounded.DialTimeout, bounded.ReadTimeout, bounded.WriteTimeout = 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
		bounded.PoolTimeout = 500 * time.Millisecond
		bounded.ContextTimeoutEnabled = true
		// An unknown EVAL outcome may already have spent tokens. Never replay it
		// automatically; reject this attempt and let HTTP idempotency resolve any
		// committed business request on the next client retry.
		bounded.MaxRetries = -1
		client = redis.NewClient(&bounded)
	}
	return &Limiter{Redis: client, Limits: limits, Prefix: "vidlens:submission:{rate}:"}
}

func (l *Limiter) Check(ctx context.Context, kind, session, ip string) error {
	if l == nil || l.Redis == nil || !l.Limits.Valid() || session == "" || ip == "" {
		return domain.Err(503, "SUBMISSION_GUARD_UNAVAILABLE", "提交保护暂不可用，请稍后再试。")
	}
	p := l.Limits.Generation
	if kind == "question" {
		p = l.Limits.Question
	} else if kind != "generation" {
		return domain.Err(503, "SUBMISSION_GUARD_UNAVAILABLE", "提交保护暂不可用。")
	}
	keys := []string{}
	for i, value := range []string{session, ip, "all"} {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", kind, i, value)))
		keys = append(keys, fmt.Sprintf("%s%x", l.Prefix, h[:16]))
	}
	args := []any{}
	for _, b := range []config.TokenBucket{p.Session, p.IP, p.Global} {
		args = append(args, b.Burst, float64(b.Every)/float64(time.Millisecond))
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	wait, err := takeTokens.Run(ctx, l.Redis, keys, args...).Int64()
	if err != nil {
		return domain.Err(503, "SUBMISSION_GUARD_UNAVAILABLE", "提交保护暂不可用，请稍后再试。")
	}
	if wait > 0 {
		seconds := max(int64(1), (wait+999)/1000)
		return &domain.Problem{Status: 429, Code: "RATE_LIMITED", Detail: fmt.Sprintf("提交过于频繁，请在 %d 秒后重试。", seconds), Meta: map[string]any{"retry_after_seconds": seconds}}
	}
	return nil
}
