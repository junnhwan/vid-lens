package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/observability"
)

// RateLimiter 基于 Redis + Lua 的令牌桶限流器
// 支持全局默认配额 + 按路由覆盖（SetRouteLimit），实现"按用户和接口维度"限流：
// 计数维度是 (路由, 用户/IP)，每个 (路由, 用户) 组合一个独立令牌桶，
// 且不同路由可配不同容量与速率，从而对高成本 AI 接口施加更严格的限额。
type RateLimiter struct {
	client    redis.Cmdable
	capacity  int // 全局默认桶容量
	rate      int // 全局默认每秒令牌数
	overrides map[string]routeLimit
	mu        sync.RWMutex
}

type routeLimit struct {
	capacity int
	rate     int
}

// NewRateLimiter 创建限流器，capacity/rate 为未覆盖路由的默认配额
func NewRateLimiter(client redis.Cmdable, capacity, rate int) *RateLimiter {
	return &RateLimiter{
		client:    client,
		capacity:  capacity,
		rate:      rate,
		overrides: make(map[string]routeLimit),
	}
}

// SetRouteLimit 为指定路由（c.FullPath() 形式）配置专属桶容量与速率，覆盖全局默认。
func (r *RateLimiter) SetRouteLimit(path string, capacity, rate int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[path] = routeLimit{capacity: capacity, rate: rate}
}

// configFor 返回某路由的配额，无覆盖则用全局默认
func (r *RateLimiter) configFor(path string) (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cfg, ok := r.overrides[path]; ok {
		return cfg.capacity, cfg.rate
	}
	return r.capacity, r.rate
}

// 令牌桶 Lua 脚本：capacity/rate 作为 ARGV 传入，支持按路由差异化配额
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local bucket = redis.call("HMGET", key, "tokens", "last_time")
local tokens = tonumber(bucket[1])
local last_time = tonumber(bucket[2])

if tokens == nil then
    tokens = capacity
    last_time = now
end

local elapsed = now - last_time
local new_tokens = elapsed / 1000 * rate
tokens = math.min(capacity, tokens + new_tokens)
last_time = now

if tokens >= 1 then
    tokens = tokens - 1
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 1
else
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 0
end
`)

// Allow 判断是否允许请求通过
// capacity/rate 作为参数传入，由调用方按路由取配额后注入。
// Redis 异常时 fail-open（放行）：限流是保护手段而非关键路径，不应成为单点故障。
func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) bool {
	now := time.Now().UnixMilli()
	result, err := tokenBucketScript.Run(ctx, r.client,
		[]string{fmt.Sprintf("rate_limiter:%s", key)},
		rate, capacity, now,
	).Int()
	scope := rateLimitScope(key)
	if err != nil {
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.IncRateLimit(scope, "fail_open")
		}
		observability.Log(ctx, slog.Default(), slog.LevelError, "rate limiter redis failed open",
			slog.String("scope", scope), slog.String("error", observability.SafeError(err)))
		return true
	}
	if metrics := observability.DefaultMetrics(); metrics != nil {
		if result == 1 {
			metrics.IncRateLimit(scope, "allowed")
		} else {
			metrics.IncRateLimit(scope, "rejected")
		}
	}
	return result == 1
}

func rateLimitScope(key string) string {
	lower := strings.ToLower(key)
	switch {
	case strings.Contains(lower, "/chat"), strings.Contains(lower, "/analyze"), strings.Contains(lower, "/transcribe"), strings.Contains(lower, "rag-index"):
		return "ai"
	case strings.Contains(lower, "upload"):
		return "upload"
	default:
		return "default"
	}
}

// RateLimit Gin 中间件
// key = 路由路径 + 用户ID/IP，实现 (接口, 用户) 双维度计数隔离。
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		var key string
		if userID, ok := c.Get("userID"); ok {
			key = fmt.Sprintf("%s:user:%v", path, userID)
		} else {
			key = fmt.Sprintf("%s:ip:%s", path, c.ClientIP())
		}

		capacity, rate := limiter.configFor(path)
		if !limiter.Allow(c.Request.Context(), key, capacity, rate) {
			c.Header("Retry-After", strconv.Itoa(1))
			c.JSON(http.StatusTooManyRequests, gin.H{"code": "RATE_LIMITED", "message": "当前请求过多，请稍后再试", "scope": rateLimitScope(key), "retry_after": 1})
			c.Abort()
			return
		}
		c.Next()
	}
}
