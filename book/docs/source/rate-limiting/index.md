# 源码走读 - Rate Limiting 中间件

## 基于 VidLens 项目的限流实现分析

---

## 文件结构

| 文件 | 用途 | 行数 |
|------|------|------|
| `internal/middleware/ratelimit.go` | 限流核心实现 | 133 |
| `internal/middleware/ratelimit_test.go` | 单元测试 | - |

---

## 核心结构体

### RateLimiter (行 19-30)

```go
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
```

**设计分析**:
- `redis.Cmdable`: 接口类型，支持 Redis 客户端和集群客户端
- `capacity`: 桶容量，决定突发流量上限
- `rate`: 令牌生成速率，决定平均 QPS
- `overrides`: 路由级别的配置覆盖，支持精细化限流
- `mu`: 读写锁，保证 `overrides` 并发安全

### routeLimit (行 24-27)

```go
type routeLimit struct {
    capacity int
    rate     int
}
```

**设计分析**:
- 独立结构体，便于扩展（如添加 `burst` 字段）
- 按路由粒度配置，不同接口可设置不同限流策略

---

## 关键函数

### 1. 初始化函数 (行 32-40)

```go
func NewRateLimiter(client redis.Cmdable, capacity, rate int) *RateLimiter {
    return &RateLimiter{
        client:    client,
        capacity:  capacity,
        rate:      rate,
        overrides: make(map[string]routeLimit),
    }
}
```

**设计决策**:
- 使用构造函数模式，确保 map 已初始化
- 接收 `redis.Cmdable` 接口，便于测试时注入 miniredis

### 2. SetRouteLimit (行 43-47)

```go
func (r *RateLimiter) SetRouteLimit(path string, capacity, rate int) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.overrides[path] = routeLimit{capacity: capacity, rate: rate}
}
```

**设计决策**:
- 运行时动态配置，无需重启服务
- 使用 `sync.RWMutex` 保证并发安全
- 支持热更新，适合配置中心场景

### 3. configFor (行 50-57)

```go
func (r *RateLimiter) configFor(path string) (int, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cfg, ok := r.overrides[path]; ok {
		return cfg.capacity, cfg.rate
	}
	return r.capacity, r.rate
}
```

**设计决策**:
- 路由级配置优先，未配置时使用全局默认值
- 简洁的降级逻辑

### 4. Allow 方法 (行 95-106)

```go
func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) bool {
    now := time.Now().UnixMilli()
    result, err := tokenBucketScript.Run(ctx, r.client,
        []string{fmt.Sprintf("rate_limiter:%s", key)},
        rate, capacity, now,
    ).Int()
    if err != nil {
        log.Printf("[ratelimit] Redis 异常，fail-open 放行 key=%s err=%v", key, err)
        return true  // Fail-Open
    }
    return result == 1
}
```

**设计决策**:
- **Fail-Open 策略**: Redis 异常时放行请求，保证服务可用性
- **日志记录**: 记录错误信息，便于监控告警
- **返回值**: `true` 表示放行，`false` 表示限流

### 5. RateLimit 中间件 (行 108-132)

```go
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
            response.TooManyRequests(c, "当前请求过多，请稍后再试")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**设计决策**:
- **Key 设计**: 路由路径 + 用户标识，实现细粒度限流
- **中间件模式**: 与业务逻辑解耦，可复用
- **错误响应**: 返回 429 状态码，符合 HTTP 规范

---

## Lua 脚本解析

### 令牌桶脚本 (行 60-90)

```lua
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
```

#### 参数说明

| 参数 | 类型 | 说明 |
|------|------|------|
| KEYS[1] | string | Redis key，格式为 `路由:用户标识` |
| ARGV[1] | int | 令牌生成速率 (tokens/sec) |
| ARGV[2] | int | 桶容量 |
| ARGV[3] | int | 当前时间戳 (毫秒) |

#### 执行流程

```
1. 获取当前桶状态 (tokens, last_time)
2. 如果是新桶，初始化为满桶状态
3. 计算时间差 elapsed (毫秒)
4. 计算新生成的令牌数 new_tokens = elapsed / 1000 * rate
5. 更新令牌数，不超过容量
6. 更新最后更新时间
7. 如果令牌 >= 1，消耗一个令牌，返回 1 (放行)
8. 否则返回 0 (限流)
```

#### 关键算法

```lua
-- 时间差计算
local elapsed = now - last_time

-- 令牌生成计算
local new_tokens = elapsed / 1000 * rate
-- elapsed / 1000: 毫秒转秒
-- * rate: 乘以速率，得到这段时间生成的令牌数

-- 令牌数限制
tokens = math.min(capacity, tokens + new_tokens)
-- 防止令牌数超过桶容量
```

#### Redis 操作

| 操作 | 命令 | 说明 |
|------|------|------|
| 读取状态 | HMGET key tokens last_time | 原子性获取多个字段 |
| 写入状态 | HMSET key tokens T last_time L | 原子性更新多个字段 |
| 设置过期 | EXPIRE key 60 | 60 秒后自动删除 |

---

## 设计决策分析

### 1. 为什么使用 Lua 脚本？

**问题**: 多条 Redis 命令的原子性问题

**错误示例**:
```go
// 非原子操作，并发场景下有问题
tokens, _ := client.HGet(ctx, key, "tokens").Int()
lastTime, _ := client.HGet(ctx, key, "last_time").Int64()
// 计算新令牌数...
client.HSet(ctx, key, "tokens", newTokens, "last_time", now)
```

**问题**:
- 两个请求同时读取相同的 tokens 值
- 都减 1 后写入，实际只减了 1 次
- 导致限流失效

**Lua 脚本优势**:
- 原子性: 脚本执行期间不会有其他命令插入
- 性能: 减少网络往返次数
- 简洁: 所有逻辑在一个脚本中

### 2. 为什么使用 Hash 而不是 String？

**String 方案**:
```go
// 需要两个 key
client.Set(ctx, key + ":tokens", tokens, 0)
client.Set(ctx, key + ":last_time", lastTime, 0)
// 或者序列化为 JSON
client.Set(ctx, key, `{"tokens":10,"last_time":1234567890}`, 0)
```

**Hash 优势**:
- 内存效率: 多个字段共享一个 key 的元数据
- 原子操作: HMGET/HMSET 保证原子性
- 可扩展: 新增字段无需修改数据结构

### 3. 为什么使用 Fail-Open？

**场景分析**:
- 限流是保护机制，不是核心功能
- Redis 故障时，放行请求比拒绝请求更合理
- 保证用户体验，避免因限流系统故障导致服务不可用

**监控策略**:
```go
if err != nil {
    log.Printf("[ratelimit] Redis 异常，fail-open 放行 key=%s err=%v", key, err)
    // 告警: 当错误率超过阈值时触发
    // 降级: 切换到本地限流（如 golang.org/x/time/rate）
    return true
}
```

### 4. 为什么设置 60 秒过期时间？

**考虑因素**:
- 内存占用: 无用 key 需要及时清理
- 用户体验: 过期时间太短会导致限流状态丢失
- 业务场景: 根据用户请求频率设置

**计算方法**:
```
过期时间 = 最大请求间隔 * 2 ~ 3 倍
示例: 如果用户最大请求间隔为 30 秒，设置 60 秒过期
```

### 5. 为什么使用毫秒级时间戳？

**精度分析**:
- 秒级时间戳: 1 秒内的多次请求无法精确计算
- 毫秒级时间戳: 更精确的令牌生成计算

**示例**:
```
rate = 10 tokens/sec
秒级: 1 秒内只能计算一次令牌生成
毫秒级: 每 100ms 可以计算一次令牌生成
```

---

## 测试策略

### 使用 miniredis

```go
func TestRateLimiter(t *testing.T) {
    mr := miniredis.RunT(t)
    client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
    limiter := NewRateLimiter(client, 10, 5)
    
    // 测试基本限流
    // 测试按路由覆盖
    // 测试 Fail-Open
}
```

**miniredis 优势**:
- 内存 Redis，速度快
- 无需外部依赖
- 可控性强，便于模拟错误场景

### 测试用例

1. **基本限流测试**: 验证令牌消耗和恢复
2. **按路由覆盖测试**: 验证不同路由使用不同配置
3. **Fail-Open 测试**: 模拟 Redis 错误，验证放行逻辑

---

## 性能考虑

### Redis 操作开销

| 操作 | 耗时 | 说明 |
|------|------|------|
| Lua 脚本执行 | ~0.1ms | 单线程执行，无竞争 |
| 网络往返 | ~1ms | 取决于网络延迟 |
| 序列化/反序列化 | ~0.01ms | 几乎可忽略 |

### 优化建议

1. **Pipeline**: 批量执行多个限流检查
2. **本地缓存**: 热点 key 的限流结果缓存
3. **异步日志**: 错误日志异步写入，避免阻塞请求

---

## 扩展点

### 1. 支持滑动窗口

```go
// 滑动窗口算法，更平滑的限流
type SlidingWindowLimiter struct {
    windowSize time.Duration
    maxRequests int
}
```

### 2. 支持多维度限流

```go
// 按用户 + IP + 路由 多维度限流
key := fmt.Sprintf("%s:%s:%s", userID, clientIP, path)
```

### 3. 支持动态权重

```go
// 不同用户不同权重
weight := getUserWeight(userID)
tokens -= weight
```

---

## 总结

VidLens 的限流中间件实现简洁而完整，具有以下特点:

1. **算法选择**: 令牌桶算法，支持突发流量
2. **存储方案**: Redis + Lua 脚本，保证原子性
3. **容错策略**: Fail-Open，保证服务可用性
4. **配置灵活**: 支持全局默认和路由级覆盖
5. **测试友好**: 使用 miniredis，便于单元测试

这是一个生产级的限流实现，适合大多数 API 网关场景。
