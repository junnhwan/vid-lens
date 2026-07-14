package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newRateLimitTestRedis(t *testing.T) (*miniredis.Miniredis, redis.Cmdable) {
	t.Helper()
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return s, client
}

// 令牌桶容量耗尽后拒绝：rate=0 不补充令牌，纯测桶容量
func TestRateLimiterTokenBucket(t *testing.T) {
	_, client := newRateLimitTestRedis(t)
	limiter := NewRateLimiter(client, 3, 0) // 容量3，不补充
	ctx := context.Background()

	for i := range 3 {
		if !limiter.Allow(ctx, "bucket:user:1", 3, 0) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if limiter.Allow(ctx, "bucket:user:1", 3, 0) {
		t.Fatalf("第 4 次应被限流（桶空）")
	}
}

// 不同 key（不同用户）各自独立计数
func TestRateLimiterKeyIsolation(t *testing.T) {
	_, client := newRateLimitTestRedis(t)
	limiter := NewRateLimiter(client, 1, 0)
	ctx := context.Background()

	if !limiter.Allow(ctx, "route:user:1", 1, 0) {
		t.Fatalf("user:1 第1次应放行")
	}
	if limiter.Allow(ctx, "route:user:1", 1, 0) {
		t.Fatalf("user:1 第2次应被限流")
	}
	// 另一用户独立桶，不受影响
	if !limiter.Allow(ctx, "route:user:2", 1, 0) {
		t.Fatalf("user:2 应有独立配额")
	}
}

// configFor：覆盖路由用专属配额，未覆盖路由用全局默认
func TestRateLimiterRouteOverrideConfig(t *testing.T) {
	_, client := newRateLimitTestRedis(t)
	limiter := NewRateLimiter(client, 10, 5)
	limiter.SetRouteLimit("/api/v1/chat/sessions/:session_id/messages", 1, 0)

	if cap, rate := limiter.configFor("/api/v1/chat/sessions/:session_id/messages"); cap != 1 || rate != 0 {
		t.Fatalf("override 配额应为 (1,0), got (%d,%d)", cap, rate)
	}
	if cap, rate := limiter.configFor("/api/v1/media/upload"); cap != 10 || rate != 5 {
		t.Fatalf("默认配额应为 (10,5), got (%d,%d)", cap, rate)
	}
}

// 端到端：override 路由按更严格的配额限流，其他路由仍用宽松默认
func TestRateLimiterOverrideAppliesStricterQuota(t *testing.T) {
	_, client := newRateLimitTestRedis(t)
	limiter := NewRateLimiter(client, 10, 0) // 默认宽松
	limiter.SetRouteLimit("/chat", 1, 0)     // chat 严格
	ctx := context.Background()

	chatCap, chatRate := limiter.configFor("/chat")
	if !limiter.Allow(ctx, "/chat:user:1", chatCap, chatRate) {
		t.Fatalf("chat 第1次应放行")
	}
	if limiter.Allow(ctx, "/chat:user:1", chatCap, chatRate) {
		t.Fatalf("chat 第2次应被限流（override capacity=1）")
	}

	defCap, defRate := limiter.configFor("/other")
	for i := range 3 {
		if !limiter.Allow(ctx, "/other:user:1", defCap, defRate) {
			t.Fatalf("other 第%d次应放行（默认 capacity=10）", i+1)
		}
	}
}

// Redis 异常时 fail-open 放行（限流不应成为单点故障）
func TestRateLimiterFailsOpenOnRedisError(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter := NewRateLimiter(client, 1, 1)
	s.Close() // 关闭后端，使后续命令失败

	if !limiter.Allow(context.Background(), "k", 1, 1) {
		t.Fatalf("Redis 异常时应 fail-open 放行")
	}
}

// 令牌按速率补充：耗尽后等待，应能重新放行
func TestRateLimiterTokenRefill(t *testing.T) {
	_, client := newRateLimitTestRedis(t)
	limiter := NewRateLimiter(client, 1, 100) // 容量1，每秒补100个
	ctx := context.Background()

	if !limiter.Allow(ctx, "refill:user:1", 1, 100) {
		t.Fatalf("第1次应放行")
	}
	if limiter.Allow(ctx, "refill:user:1", 1, 100) {
		t.Fatalf("第2次应被限流（桶空）")
	}
	// 等令牌补充（rate=100/s，30ms 约补 3 个）
	time.Sleep(50 * time.Millisecond)
	if !limiter.Allow(ctx, "refill:user:1", 1, 100) {
		t.Fatalf("补充后应重新放行")
	}
}
func TestRateLimitReturnsStructured429AndRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, c := newRateLimitTestRedis(t)
	l := NewRateLimiter(c, 1, 0)
	r := gin.New()
	r.GET("/costly", RateLimit(l), func(x *gin.Context) { x.Status(204) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/costly", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/costly", nil))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "RATE_LIMITED") {
		t.Fatalf("code=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	_ = http.StatusOK
}
