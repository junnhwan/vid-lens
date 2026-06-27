# 分布式锁 — 面试题

> 基于 `internal/pkg/lock/redis_lock.go` 真实源码，共 10 道题。
> 每题含：考察点、代码片段（带行号）、参考答案、追问链。

---

## Q1: RedisLock 为什么用 UUID 而不是时间戳做 owner？

**考察点**: 分布式锁互斥性、并发竞态

**关键代码** `internal/pkg/lock/redis_lock.go:42-46`

```go
// 行 42: TryLock 入口
func (l *RedisLock) TryLock(ctx context.Context, waitTimeout time.Duration) (bool, error) {
    // 行 44: 注释说明——高并发下同纳秒会产生相同 owner
    lockValue := uuid.New().String()  // 行 45
    l.value = lockValue               // 行 46
```

**参考答案**

UUID v4 的碰撞概率约 2^122 分之一，几乎为零。而 `time.Now().UnixNano()` 在高并发下（同一纳秒多个 goroutine 同时调用）会产生相同值。如果两个实例拿到相同 owner，Lua 脚本 `GET == ARGV[1]` 校验就会通过，导致 B 能释放 A 的锁，破坏互斥性。

测试 `TestRedisLockMutualExclusion`（`redis_lock_test.go:23`）正是对此的回归保护。

**追问链**

1. UUID v4 和 v1 有什么区别？v1 基于时间+MAC 地址，有信息泄露风险；v4 纯随机，更适合做 owner。
2. 如果 UUID 碰撞了怎么办？实际上 2^122 碰撞概率可忽略；若极端担忧，可加机器 ID 前缀（类似 Snowflake）。
3. 除了 UUID 还有什么方案？Redis 6.2+ 的 `SET` 命令支持 `GET` 参数可取旧值；也可用 `hostname+pid+timestamp` 组合。

---

## Q2: SetNX 和 SET NX EX 有什么区别？VidLens 用的哪种？

**考察点**: Redis 原子操作、竞态窗口

**关键代码** `internal/pkg/lock/redis_lock.go:51-52`

```go
    // 行 51: SetNX 是单命令原子操作，同时携带 TTL
    // 行 52: 等价于 SET key value NX EX 30
    ok, err := l.client.SetNX(ctx, l.key, lockValue, l.ttl).Result()
```

**参考答案**

go-redis 的 `SetNX(key, value, ttl)` 底层发送的是 `SET key value NX EX 30`，一条命令完成"不存在则设置 + 设置过期时间"，是原子操作。

如果先 `SETNX` 再 `EXPIRE`（两条命令），中间宕机会导致锁永不过期（死锁）。VidLens 用单命令方案避免了这个窗口。

**追问链**

1. `SETNX` 能设置 TTL 吗？不能，`SETNX` 只做存在性检查。需要 `SET key value NX EX ttl`。
2. Redis 2.6.12 之前没有 `SET NX EX`，怎么保证原子性？用 Lua 脚本：先 `EXISTS`，不存在则 `SET` + `EXPIRE`。
3. go-redis 的 `SetNX` 和原生 `SETNX` 命令什么关系？go-redis 的 `SetNX` 映射到 `SET ... NX`，不是老的 `SETNX` 命令。

---

## Q3: WatchDog 的续期间隔为什么是 ttl/3 而不是 ttl/2？

**考察点**: 续期策略、时钟漂移容错

**关键代码** `internal/pkg/lock/redis_lock.go:79-80`

```go
// 行 79: watchdog 函数签名
func (l *RedisLock) watchdog(ctx context.Context) {
    ticker := time.NewTicker(l.ttl / 3) // 行 80: 30s / 3 = 10s
```

**参考答案**

ttl/3（10s 续期一次，TTL 30s）提供了 **2 次容错机会**：如果某次续期因网络抖动失败，还有 10s 的窗口可以重试。如果用 ttl/2（15s 续期，TTL 30s），只有 1 次容错，网络稍有延迟锁就可能过期。

Redisson 也是用 ttl/3 策略，这是工业界验证过的经验值。

**追问链**

1. 续期失败会怎样？看 `renew` 函数（行 101-119）：记录日志，WatchDog 不退出，下次 tick 继续重试。
2. 如果续期连续失败 3 次（30s），锁会过期吗？会，锁过期后其他节点可抢到。但业务层有状态机兜底。
3. 为什么不把续期间隔设得更短（如 1s）？会增加 Redis 压力，且无必要——10s 间隔已足够容错。

---

## Q4: renew 的 Lua 脚本做了什么？为什么必须用 Lua？

**考察点**: Redis Lua 脚本原子性、CAS 语义

**关键代码** `internal/pkg/lock/redis_lock.go:102-107`

```go
// 行 102-107: renew Lua 脚本
    script := `
    if redis.call("get", KEYS[1]) == ARGV[1] then
        return redis.call("expire", KEYS[1], ARGV[2])
    else
        return 0
    end`
```

**参考答案**

Lua 脚本实现了一个 **原子 CAS 续期**：先 `GET` 检查 owner 是否一致，一致才 `EXPIRE`。如果用两条命令（`GET` + `EXPIRE`），中间锁可能已过期被别人抢走，此时 `EXPIRE` 会续上别人的锁——破坏互斥性。

Redis 执行 Lua 脚本是单线程串行的，保证 `GET` 和 `EXPIRE` 之间不会有其他命令插入。

**追问链**

1. Lua 脚本在 Redis 中是阻塞的吗？是的，复杂脚本会阻塞其他命令。但这个脚本只有 2 条命令，耗时极短。
2. `redis.call` 和 `pcall` 区别？`call` 出错会中断脚本返回错误；`pcall` 捕获错误继续执行。这里用 `call` 即可。
3. KEYS 和 ARGV 的区别？`KEYS` 是键名（用于集群路由），`ARGV` 是参数。Lua 中通过下标 1 开始访问。

---

## Q5: Unlock 为什么要先 close(stopChan)？有什么边界情况？

**考察点**: goroutine 生命周期管理、double-close 防护

**关键代码** `internal/pkg/lock/redis_lock.go:124-129`

```go
// 行 124: Unlock 入口
func (l *RedisLock) Unlock(ctx context.Context) error {
    // 行 126: nil 检查防止 double-close panic
    if l.stopChan != nil {       // 行 126
        close(l.stopChan)        // 行 127: 通知 watchdog 退出
        l.stopChan = nil         // 行 128: 置 nil 标记已关闭
    }
```

**参考答案**

`close(l.stopChan)` 向 watchdog goroutine 发送退出信号。如果不关闭，watchdog 会持续运行并续期——锁永远不过期（泄漏）。

`l.stopChan = nil` 是 double-close 防护：调用两次 `Unlock()` 时，第二次 `l.stopChan` 已经是 nil，跳过 `close`，避免 panic。测试 `TestRedisLockDoubleUnlockNoPanic`（`redis_lock_test.go:82`）验证了这一点。

**追问链**

1. 如果 Unlock 前进程崩溃了怎么办？watchdog 随进程退出，锁在 TTL（30s）后自动过期，不会死锁。
2. 为什么不用 `sync.Once` 防止 double-close？`sync.Once` 也能实现，但 `nil` 检查更简洁且无额外依赖。
3. `close(channel)` 后已有的 goroutine 收到什么？已 select 该 channel 的 goroutine 会立即收到零值，退出循环。

---

## Q6: TryLock 的等待重试用了什么策略？有什么问题？

**考察点**: 自旋等待、公平性、Redis 压力

**关键代码** `internal/pkg/lock/redis_lock.go:49-73`

```go
    deadline := time.Now().Add(waitTimeout)  // 行 49
    for {                                     // 行 50
        ok, err := l.client.SetNX(ctx, l.key, lockValue, l.ttl).Result() // 行 52
        // ... 成功/失败处理 ...
        // 行 67-72: 短暂等待后重试
        select {
        case <-ctx.Done():
            return false, ctx.Err()            // 行 70
        case <-time.After(100 * time.Millisecond):  // 行 71: 固定 100ms 间隔
        }
    }
```

**参考答案**

用固定 100ms 间隔轮询（自旋等待）。问题：

1. **惊群效应**：锁释放瞬间，所有等待者同时发起 `SetNX`，只有 1 个成功，其余白耗 Redis 连接。
2. **不公平**：不保证 FIFO，后来的请求可能先抢到锁。
3. **Redis 压力**：高并发下每秒产生大量无效 `SetNX` 请求。

**追问链**

1. 怎么改进？可用 Redis 的 **Pub/Sub** 或 **BLPOP** 通知等待者，减少无效轮询。
2. Redisson 用什么方案？Redisson 用 Lua 脚本订阅锁释放事件（Pub/Sub），锁释放时只唤醒一个等待者。
3. 为什么不实现指数退避？指数退避会增加等待延迟，对低并发场景不友好。100ms 固定间隔是简单实用的折中。

---

## Q7: Unlock 的 Lua 脚本返回 0 代表什么？VidLens 怎么处理？

**考察点**: 锁释放的幂等性、错误处理

**关键代码** `internal/pkg/lock/redis_lock.go:131-139`

```go
    // 行 132-137: Unlock Lua 脚本
    script := `
    if redis.call("get", KEYS[1]) == ARGV[1] then
        return redis.call("del", KEYS[1])
    else
        return 0
    end`
    // 行 138: 注意 VidLens 忽略了返回值
    _, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
    return err
```

**参考答案**

返回 0 表示 owner 不匹配（锁已不归当前持有者），`DEL` 未执行。VidLens 只检查了 `err`（Redis 通信错误），**没有检查返回值 0**。

这是一个设计取舍：如果锁已过期被别人抢走，再 `Unlock` 返回 0 是正常情况（不是错误），调用方无法区分"成功释放"和"锁已不归我"。

**追问链**

1. 这会导致什么问题？如果业务需要确认"锁确实在自己释放前一直持有"，当前实现无法感知。
2. 怎么改进？让 `Unlock` 返回 `(bool, error)`，返回 false 表示锁已丢失。
3. 测试中怎么验证？`TestRedisLockUnlockOnlyReleasesOwner`（行 44）用伪造的 intruder 调 Unlock，验证锁未被删除。

---

## Q8: RedisLock 为什么是"不可重入"的？如果业务需要重入怎么办？

**考察点**: 可重入锁设计、goroutine 关联

**关键代码** `internal/pkg/lock/redis_lock.go:20-22`

```go
// 行 20-21: 注释明确说明
// 注意：RedisLock 是有状态的（value/stopChan 在获取锁时写入），
// 不可重入、不可跨 goroutine 共享，每次加锁必须 NewRedisLock 新建实例。
type RedisLock struct {
```

**参考答案**

可重入锁需要记录"同一个持有者加了几次锁"，释放时递减计数器，归零才真正释放。当前实现：

1. `value` 在 `TryLock` 时被覆盖（行 46），第二次调用会生成新 UUID，Lua 校验失败。
2. 没有重入计数器（`counter`）。
3. 没有记录 goroutine ID（Go 语言也没有标准 API 获取）。

实现重入锁的方案：在 Redis 中存 `value:count`，Lua 脚本做 `INCR`/`DECR`。

**追问链**

1. Go 语言怎么获取 goroutine ID？`runtime.Stack()` 可以解析，但非官方 API，不推荐。
2. VidLens 的业务场景需要重入锁吗？不需要——Kafka Consumer 处理和 MergeChunks 都是单层调用。
3. Java 的 ReentrantLock 怎么实现重入？用 `state` 计数器 + `exclusiveOwnerThread` 记录持有者。

---

## Q9: 看一下测试中 FastForward 的用法，它测了什么？

**考察点**: 时间模拟、TTL 续期验证

**关键代码** `internal/pkg/lock/redis_lock_test.go:99-130`

```go
// 行 99: TestRedisLockRenewExtendsTTL
func TestRedisLockRenewExtendsTTL(t *testing.T) {
    s, client := newTestRedis(t)              // 行 100: miniredis 实例
    // ...
    s.FastForward(20 * time.Second)           // 行 111: 模拟时间快进 20s
    if !s.Exists(key) {                       // 行 112: 验证锁仍在
        t.Fatalf("锁不应在 ttl 内过期")
    }
    if !l.renew(ctx) {                        // 行 117: 手动续期
        t.Fatalf("renew 应返回 true")
    }
    ttl := s.TTL(key)                         // 行 120: 读取剩余 TTL
    // 行 121: 验证 TTL 回到 ~30s
    if ttl < 25*time.Second || ttl > 30*time.Second {
        t.Fatalf("续期后 TTL 应≈30s，got %v", ttl)
    }
    s.FastForward(25 * time.Second)           // 行 126: 再快进 25s
    if !s.Exists(key) {                       // 行 127: 验证续期有效
        t.Fatalf("续期后锁不应在该时间点过期")
    }
}
```

**参考答案**

`miniredis.FastForward()` 可以模拟时间流逝，无需真正等待。测试逻辑：

1. 快进 20s（< TTL 30s），验证锁未过期。
2. 手动调 `renew()`，验证 TTL 重置回 30s。
3. 再快进 25s，此时距上次续期 25s < 30s，锁仍存活。

这比 `time.Sleep()` 快几个数量级，测试可在毫秒级完成。

**追问链**

1. miniredis 是什么？纯 Go 实现的 Redis 内存模拟器，适合单元测试，无需启动真实 Redis。
2. FastForward 会影响真实时间吗？不会，只影响 miniredis 内部的过期计算。
3. 生产环境怎么测续期？用集成测试 + 真实 Redis，`time.Sleep(35s)` 等待验证。或者用 Redis 的 `DEBUG SLEEP`。

---

## Q10: VidLens 在哪些场景用了分布式锁？为什么这些场景需要锁？

**考察点**: 分布式锁应用场景、业务理解

**参考答案**

根据源码走读，VidLens 在两个场景使用分布式锁：

1. **Kafka Consumer 处理任务**（`consumer.go`）：多个 Consumer 实例可能同时消费同一 Topic Partition，加锁保证同一任务不被重复处理。
2. **MergeChunks 防并发合并**（`media.go:618-627`）：分片上传完成后合并文件，多个并发请求可能同时触发合并，加锁保证只合并一次。

**为什么需要分布式锁而不是单机锁？**

VidLens 是多实例部署（K8s/Docker），`sync.Mutex` 只在单进程内有效。分布式锁依赖 Redis 这个共享存储，跨进程/跨机器生效。

**追问链**

1. 这两个场景可以用数据库乐观锁代替吗？可以，MergeChunks 可以用 `UPDATE ... WHERE status='uploading'` 条件更新。但 Kafka Consumer 用锁更直接。
2. 分布式锁和幂等性什么关系？分布式锁是实现幂等性的一种手段——锁住资源，确保同一时刻只有一个执行者。
3. 如果 Redis 集群故障，锁失效怎么办？VidLens 的业务层有状态机兜底：即使锁失效导致并发执行，状态条件更新（`WHERE status='xxx'`）也能防止数据错乱。
