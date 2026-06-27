# 限流面试题 - Rate Limiting

## 基于 VidLens 项目的限流中间件实现

---

## 题目 1: 令牌桶算法原理

**问题**: 请解释令牌桶算法的工作原理，以及它在限流中的作用。

**代码参考** (ratelimit.go, 行 60-90):
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

**追问链**:
1. 为什么使用 `math.min(capacity, tokens + new_tokens)` 而不是直接累加？
2. `elapsed / 1000 * rate` 这个计算公式是如何实现速率控制的？
3. 为什么需要存储 `last_time` 而不仅仅是 `tokens`？

**参考答案**:
令牌桶算法维护一个令牌桶，桶以固定速率（rate）生成令牌，桶有最大容量（capacity）。每次请求消耗一个令牌，桶满时新生成的令牌被丢弃。

1. 防止令牌无限累积，确保不超过桶容量，避免突发流量过大
2. `elapsed / 1000` 将毫秒转为秒，乘以 `rate` 得到这段时间内生成的令牌数
3. `last_time` 用于计算时间差，实现动态令牌生成，避免每次请求都需要重新计算

---

## 题目 2: Redis Lua 脚本原子性

**问题**: 为什么使用 Lua 脚本而不是多条 Redis 命令？有什么好处？

**代码参考** (ratelimit.go, 行 60-90):
```lua
redis.call("HMGET", key, "tokens", "last_time")
-- ... 计算逻辑 ...
redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
redis.call("EXPIRE", key, 60)
```

**追问链**:
1. 如果使用多条 Redis 命令（GET + SET），会有什么问题？
2. Lua 脚本在 Redis 中是如何执行的？
3. 除了原子性，Lua 脚本还有什么优势？

**参考答案**:
1. 并发场景下会出现竞态条件：两个请求同时读取相同的 tokens 值，都减 1 后写入，导致实际只减了 1 次
2. Redis 单线程执行 Lua 脚本，脚本执行期间不会有其他命令插入，保证原子性
3. 减少网络往返次数（RTT），所有操作在一个脚本中完成，提升性能

---

## 题目 3: Fail-Open 策略

**问题**: 为什么在 Redis 异常时选择放行请求（Fail-Open）而不是拒绝请求（Fail-Closed）？

**代码参考** (ratelimit.go, 行 95-106):
```go
func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) bool {
    result, err := tokenBucketScript.Run(ctx, r.client, []string{key}, rate, capacity, time.Now().UnixMilli()).Int()
    if err != nil {
        log.Printf("[ratelimit] Redis 异常，fail-open 放行 key=%s err=%v", key, err)
        return true  // Fail-Open
    }
    return result == 1
}
```

**追问链**:
1. Fail-Open 和 Fail-Closed 各自适用什么场景？
2. 如果 Redis 完全不可用，Fail-Open 会导致什么后果？
3. 如何在 Fail-Open 的同时监控限流系统的健康状态？

**参考答案**:
1. Fail-Open 适合用户体验优先的场景（如 API 网关）；Fail-Closed 适合安全敏感的场景（如支付系统）
2. 无限流保护，可能导致后端服务过载，但至少用户能正常使用服务
3. 记录错误日志，设置告警阈值，当错误率超过阈值时触发告警，同时考虑降级方案（如本地限流）

---

## 题目 4: 限流 Key 设计

**问题**: 为什么限流 Key 要包含路由路径和用户标识？

**代码参考** (ratelimit.go, 行 110-130):
```go
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
    return func(c *gin.Context) {
        // key = 路由路径 + ":" + 用户ID/IP
        key := c.FullPath() + ":" + identity
        cap, rate := limiter.getRouteLimit(c.FullPath())
        if !limiter.Allow(c.Request.Context(), key, cap, rate) {
            response.TooManyRequests(c, "请求过于频繁，请稍后再试")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**追问链**:
1. 如果只用用户标识不用路由路径，会有什么问题？
2. 如果只用路由路径不用用户标识，会有什么问题？
3. 如何选择合适的用户标识（IP、UserID、API Key）？

**参考答案**:
1. 用户在一个接口的请求会影响其他接口的限流配额，不合理
2. 恶意用户可以通过调用不同接口绕过限流
3. 优先使用 UserID（已认证用户），其次 API Key，最后 IP（容易被绕过，但无需认证）

---

## 题目 5: 按路由配置限流

**问题**: 如何实现不同接口使用不同的限流配置？

**代码参考** (ratelimit.go, 行 19-29, 43-45):
```go
type RateLimiter struct {
    client      redis.Cmdable
    capacity    int                    // 全局默认桶容量
    rate        int                    // 全局默认速率 (tokens/sec)
    routeLimits map[string]routeLimit  // 按路由覆盖
}
type routeLimit struct {
    capacity int
    rate     int
}

func (r *RateLimiter) SetRouteLimit(path string, capacity, rate int) {
    r.routeLimits[path] = routeLimit{capacity: capacity, rate: rate}
}
```

**追问链**:
1. 为什么使用 map 而不是配置文件？
2. 如何在运行时动态修改限流配置？
3. 如果路由非常多，map 会有什么性能问题？

**参考答案**:
1. map 支持运行时动态配置，适合需要热更新的场景；配置文件需要重启服务
2. 通过 API 接口调用 SetRouteLimit，或监听配置中心变更事件
3. map 查找是 O(1)，没有性能问题；但如果需要持久化，需要考虑序列化开销

---

## 题目 6: Redis 数据结构选择

**问题**: 为什么使用 Hash（HMGET/HMSET）而不是 String 存储令牌桶状态？

**代码参考** (ratelimit.go, 行 62-63):
```lua
local bucket = redis.call("HMGET", key, "tokens", "last_time")
local tokens = tonumber(bucket[1])
local last_time = tonumber(bucket[2])
```

**追问链**:
1. 使用 String 存储需要怎么做？
2. Hash 和 String 在内存占用上有什么区别？
3. 如果需要扩展更多字段（如请求次数），Hash 有什么优势？

**参考答案**:
1. 需要用两个 key（如 `key:tokens` 和 `key:last_time`），或用 JSON 序列化
2. Hash 更紧凑，多个字段共享一个 key 的元数据；String 每个 key 都有独立的元数据
3. Hash 可以直接新增字段，无需修改数据结构；String 需要重新设计序列化格式

---

## 题目 7: 时间精度与同步

**问题**: 为什么使用 `time.Now().UnixMilli()` 而不是秒级时间戳？

**代码参考** (ratelimit.go, 行 97):
```go
result, err := tokenBucketScript.Run(ctx, r.client, []string{key}, rate, capacity, time.Now().UnixMilli()).Int()
```

**追问链**:
1. 如果使用秒级时间戳，会有什么问题？
2. 多台服务器时间不同步会影响限流吗？
3. 如何处理 Redis 服务器和应用服务器的时间差异？

**参考答案**:
1. 精度不够，1 秒内的多次请求无法精确计算令牌生成
2. 会影响，时间差会导致令牌计算偏差，可能导致限流不准
3. 使用 Redis 服务器时间（`TIME` 命令），或使用相对时间差而非绝对时间

---

## 题目 8: 过期时间设置

**问题**: 为什么设置 60 秒的过期时间？这个值应该如何选择？

**代码参考** (ratelimit.go, 行 75, 80):
```lua
redis.call("EXPIRE", key, 60)
```

**追问链**:
1. 如果不设置过期时间会怎样？
2. 过期时间设置太短会有什么问题？
3. 如何根据业务场景选择合适的过期时间？

**参考答案**:
1. Redis 内存会持续增长，大量无用的限流 key 占用内存
2. 用户请求间隔超过过期时间时，令牌桶状态丢失，相当于重新开始限流
3. 根据用户请求频率设置，一般设置为最大请求间隔的 2-3 倍

---

## 题目 9: 中间件设计模式

**问题**: 为什么使用中间件模式实现限流？有什么好处？

**代码参考** (ratelimit.go, 行 110-130):
```go
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
    return func(c *gin.Context) {
        key := c.FullPath() + ":" + identity
        cap, rate := limiter.getRouteLimit(c.FullPath())
        if !limiter.Allow(c.Request.Context(), key, cap, rate) {
            response.TooManyRequests(c, "请求过于频繁，请稍后再试")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**追问链**:
1. 如果不用中间件，限流逻辑应该放在哪里？
2. 中间件的执行顺序有什么影响？
3. 如何在中间件中获取用户身份信息？

**参考答案**:
1. 放在每个 Handler 函数中，代码重复，难以维护
2. 限流中间件应该在认证中间件之后，这样可以获取用户身份
3. 从 gin.Context 中获取，如 `c.Get("userID")` 或 `c.ClientIP()`

---

## 题目 10: 测试策略

**问题**: 如何测试限流中间件？使用 miniredis 有什么好处？

**代码参考** (ratelimit_test.go):
```go
// 使用 miniredis 进行测试
mr := miniredis.RunT(t)
// 测试用例
// 1. 基本限流测试
// 2. 按路由覆盖测试
// 3. Fail-Open 测试
```

**追问链**:
1. 为什么不直接连接真实的 Redis 进行测试？
2. 如何测试 Fail-Open 场景？
3. 如何测试并发场景下的限流？

**参考答案**:
1. 真实 Redis 需要额外依赖，测试环境不稳定，miniredis 是内存 Redis，速度快且可控
2. 模拟 Redis 错误（如关闭 miniredis），验证 Allow 方法返回 true
3. 使用 goroutine 并发调用 Allow，验证最终结果符合预期

---

## 总结

这些面试题覆盖了限流系统的核心知识点：

1. **算法原理**: 令牌桶算法的工作机制
2. **技术选型**: Redis Lua 脚本的优势
3. **容错策略**: Fail-Open vs Fail-Closed
4. **Key 设计**: 限流粒度的选择
5. **配置管理**: 动态限流配置
6. **数据结构**: Redis Hash 的优势
7. **时间处理**: 精度与同步问题
8. **内存管理**: 过期时间设置
9. **架构模式**: 中间件设计
10. **测试策略**: miniredis 的使用

掌握这些知识点，能够应对大多数限流相关的面试问题。
