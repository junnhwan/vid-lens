# 分布式锁 — 源码走读

> 模块路径: `internal/pkg/lock/`
> 源文件: `redis_lock.go`、`redis_lock_test.go`

<div class="diagram-container">

![分布式锁流程](/diagrams/distributed-lock.svg)

</div>

---

## 文件清单

| 文件 | 行数 | 职责 |
|------|------|------|
| `redis_lock.go` | 141 | RedisLock 结构体 + TryLock/Unlock/renew/watchdog |
| `redis_lock_test.go` | 166 | 5 个测试用例，使用 miniredis 模拟 Redis |

---

## 结构体

### RedisLock

`internal/pkg/lock/redis_lock.go:22-28`

```go
type RedisLock struct {
    client   redis.Cmdable   // Redis 客户端接口（支持单机/哨兵/集群）
    key      string           // Redis 锁键名
    value    string           // 持有者标识（UUID），TryLock 时生成
    ttl      time.Duration    // 锁过期时间，默认 30s
    stopChan chan struct{}     // WatchDog 停止信号
}
```

**设计要点**：

- `value` 用 UUID 而非时间戳，避免高并发下同纳秒碰撞（行 44 注释）
- `stopChan` 用于 Unlock 时通知 WatchDog goroutine 退出
- `ttl` 默认 30s，适配视频处理等长耗时场景
- **不可重入、不可跨 goroutine 共享**（行 20-21 注释），每次加锁必须 `NewRedisLock` 新建实例

### 构造函数

`internal/pkg/lock/redis_lock.go:31-37`

```go
func NewRedisLock(client redis.Cmdable, key string) *RedisLock {
    return &RedisLock{
        client: client,
        key:    key,
        ttl:    30 * time.Second,
    }
}
```

只接受 `redis.Cmdable` 接口，不依赖具体实现，方便测试注入 miniredis。

---

## 核心函数

### TryLock

`internal/pkg/lock/redis_lock.go:42-74`

```go
func (l *RedisLock) TryLock(ctx context.Context, waitTimeout time.Duration) (bool, error) {
    lockValue := uuid.New().String()    // 行 45: 生成唯一 owner
    l.value = lockValue                  // 行 46: 写入实例状态
    l.stopChan = make(chan struct{})      // 行 47: 初始化停止信号

    deadline := time.Now().Add(waitTimeout)  // 行 49: 计算截止时间
    for {
        ok, err := l.client.SetNX(ctx, l.key, lockValue, l.ttl).Result() // 行 52
        if err != nil {
            return false, fmt.Errorf("获取锁失败: %w", err)  // 行 54
        }
        if ok {
            go l.watchdog(ctx)       // 行 58: 启动看门狗续期
            return true, nil         // 行 59: 抢锁成功
        }
        if time.Now().After(deadline) {
            return false, nil        // 行 64: 超时返回 false
        }
        select {                     // 行 67-72: 等待 100ms 后重试
        case <-ctx.Done():
            return false, ctx.Err()
        case <-time.After(100 * time.Millisecond):
        }
    }
}
```

**执行流程**：

```
TryLock 调用
  → 生成 UUID owner
  → 循环: SetNX 尝试
    → 成功: 启动 watchdog goroutine, return true
    → 失败: 检查超时
      → 超时: return false
      → 未超时: sleep 100ms, 重试
```

**关键决策**：

- `SetNX` 是单命令原子操作（等价 `SET key value NX EX 30`），不存在 SETNX + EXPIRE 两步窗口
- 100ms 固定重试间隔，简单但存在惊群效应（锁释放时所有等待者同时抢）
- `waitTimeout=0` 表示不等待，只试一次

### watchdog

`internal/pkg/lock/redis_lock.go:79-95`

```go
func (l *RedisLock) watchdog(ctx context.Context) {
    ticker := time.NewTicker(l.ttl / 3)  // 行 80: 10s 续期一次
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            l.renew(ctx)           // 行 87: Lua 脚本原子续期
        case <-l.stopChan:         // 行 89: 锁已释放，退出
            return
        case <-ctx.Done():         // 行 91: context 取消，退出
            return
        }
    }
}
```

**续期策略**: ttl/3（默认 10s），提供 2 次容错机会。参考 Redisson 设计。

**退出条件**:

1. `l.stopChan` 被 close — Unlock 时触发
2. `ctx.Done()` — 调用方取消 context
3. 注意：`renew` 失败不会退出 watchdog，下次 tick 继续重试

### renew

`internal/pkg/lock/redis_lock.go:101-119`

```go
func (l *RedisLock) renew(ctx context.Context) bool {
    script := `
    if redis.call("get", KEYS[1]) == ARGV[1] then
        return redis.call("expire", KEYS[1], ARGV[2])
    else
        return 0
    end`
    res, err := l.client.Eval(ctx, script, []string{l.key}, l.value, int(l.ttl.Seconds())).Result()
    if err != nil {
        log.Printf("[redis_lock] 续期失败 key=%s owner=%s err=%v", l.key, l.value, err)
        return false
    }
    n, _ := res.(int64)
    if n == 0 {
        log.Printf("[redis_lock] 续期未生效：锁已不归当前持有者 key=%s owner=%s", l.key, l.value)
    }
    return n != 0
}
```

**Lua 脚本逻辑**:

```
GET key == owner?
  → 是: EXPIRE key ttl, 返回 1
  → 否: 返回 0（锁已不归我）
```

**原子性保证**: Redis 执行 Lua 脚本是单线程串行的，GET 和 EXPIRE 之间不会有其他命令插入。

### Unlock

`internal/pkg/lock/redis_lock.go:124-140`

```go
func (l *RedisLock) Unlock(ctx context.Context) error {
    if l.stopChan != nil {       // 行 126: nil 检查防 double-close
        close(l.stopChan)        // 行 127: 通知 watchdog 退出
        l.stopChan = nil         // 行 128: 置 nil 标记已关闭
    }

    script := `
    if redis.call("get", KEYS[1]) == ARGV[1] then
        return redis.call("del", KEYS[1])
    else
        return 0
    end`
    _, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
    return err
}
```

**两步操作**:

1. 停止 WatchDog（防止继续续期）
2. Lua 脚本删除锁（校验 owner，防止误删）

**double-close 防护**: `l.stopChan = nil` 置 nil 后，再次调用 Unlock 跳过 close，避免 panic。

---

## 调用链

### 加锁流程

```
业务代码 (consumer.go / media.go)
  → NewRedisLock(client, key)
  → TryLock(ctx, waitTimeout)
    → uuid.New().String()          生成 owner
    → client.SetNX(key, owner, ttl)  Redis 原子加锁
    → go watchdog(ctx)             启动续期 goroutine
      → ticker: ttl/3 = 10s
      → renew(ctx)                 Lua 脚本续期
```

### 释放流程

```
业务代码 (defer lock.Unlock(ctx))
  → close(l.stopChan)              通知 watchdog 退出
  → Lua: GET == owner? → DEL       原子释放
```

### 续期流程 (WatchDog 内部)

```
watchdog goroutine
  → ticker.C (每 10s)
    → renew(ctx)
      → Lua: GET == owner? → EXPIRE key ttl
        → 返回 1: 续期成功
        → 返回 0: 锁已丢失, 记录日志
```

---

## 使用场景

### 1. Kafka Consumer 消费任务

`internal/mq/consumer.go` — 多个 Consumer 实例竞争消费，加锁保证同一任务不被重复处理。

### 2. MergeChunks 防并发合并

`internal/service/media.go:618-627` — 分片上传完成后合并，加锁保证只合并一次。

---

## 测试覆盖

`internal/pkg/lock/redis_lock_test.go`

| 测试函数 | 行号 | 验证点 |
|----------|------|--------|
| `TestRedisLockMutualExclusion` | 23 | A 持有锁时 B 无法获取 |
| `TestRedisLockUnlockOnlyReleasesOwner` | 44 | 错误 owner 无法释放别人的锁 |
| `TestRedisLockDoubleUnlockNoPanic` | 82 | 重复 Unlock 不会 panic |
| `TestRedisLockRenewExtendsTTL` | 99 | 续期后 TTL 重置回 30s |
| `TestRedisLockTryLockWaitsForRelease` | 133 | 等待方在锁释放后能获取到 |

测试使用 `miniredis`（纯 Go Redis 模拟器），无需启动真实 Redis，毫秒级完成。

---

## 设计决策总结

| 决策 | 选择 | 原因 |
|------|------|------|
| owner 标识 | UUID v4 | 时间戳在高并发下碰撞 |
| 加锁命令 | `SET NX EX`（单命令） | 避免 SETNX + EXPIRE 两步窗口 |
| 续期间隔 | ttl/3 = 10s | 提供 2 次容错机会（参考 Redisson） |
| 续期/释放 | Lua 脚本 | 保证 GET + 操作的原子性 |
| 重试策略 | 固定 100ms 轮询 | 简单实用，惊群可接受 |
| 可重入 | 不支持 | 业务场景不需要，降低复杂度 |
| double-close | `nil` 检查 | 比 `sync.Once` 更简洁 |
| 测试方案 | miniredis | 无需真实 Redis，毫秒级完成 |
