package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/pkg/response"
)

// RateLimiter 基于 Redis + Lua 的令牌桶限流器
type RateLimiter struct {
	client   redis.Cmdable
	capacity int // 桶容量
	rate     int // 每秒生成令牌数
}

// NewRateLimiter 创建限流器
func NewRateLimiter(client redis.Cmdable, capacity, rate int) *RateLimiter {
	return &RateLimiter{
		client:   client,
		capacity: capacity,
		rate:     rate,
	}
}

// 令牌桶 Lua 脚本
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
func (r *RateLimiter) Allow(ctx context.Context, key string) bool {
	now := time.Now().UnixMilli()
	result, err := tokenBucketScript.Run(ctx, r.client,
		[]string{fmt.Sprintf("rate_limiter:%s", key)},
		r.rate, r.capacity, now,
	).Int()
	if err != nil {
		return true // 限流器异常时放行
	}
	return result == 1
}

// RateLimit Gin 中间件
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.FullPath()
		if key == "" {
			key = c.Request.URL.Path
		}

		if !limiter.Allow(c.Request.Context(), key) {
			response.TooManyRequests(c, "当前请求过多，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
