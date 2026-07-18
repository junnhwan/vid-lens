# Redis 锁与限流拷打

> 目标：把 Redis 讲成 VidLens 里的并发和成本控制工具。不要只背 SETNX 或令牌桶公式。

### 1. 为什么 VidLens 仍需要分布式锁？

- 题目：锁的业务必要性。
- 简答：Redis lock 主要用于跨实例保护昂贵、可重复触发的业务临界区，例如普通上传按完整 MD5 创建资产以及分析 Consumer 的并发处理。新的 durable upload session complete 不再依赖旧 merge lock，而使用 PostgreSQL token + expiry lease。
- 深答：

  <details>
  <summary>展开深答</summary>

  本地 mutex 只能保护单进程，多实例下无法阻止两个请求同时做昂贵对象操作或两个 Consumer 同时处理同一视频。VidLens 的 Redis lock 使用随机 owner、TTL、WatchDog 和 owner-aware Lua unlock，降低重复计算和外部调用。

  但锁不是正确性的唯一来源：`video_assets` 的唯一约束、task processing lease 和幂等状态转换才是物理兜底。上传 session 的 owner、manifest、chunk ledger 和 completion ownership 已归 PostgreSQL，不应为了“统一用 Redis”重新引入全局 merge lock。
  </details>

- 延伸追问：
  - 只靠 PostgreSQL unique index 行不行？
  - 本地 mutex 为什么不够？
  - Redis 锁过期但业务仍在执行怎么办？
- 项目证据：
  - `internal/pkg/lock/redis_lock.go`
  - `internal/mq/consumer_analyze.go`
  - `internal/service/media_file_upload.go`
  - `internal/repository/task_lease*.go`
- 当前边界：锁减少竞争和昂贵重复工作，不提供跨 Redis/PostgreSQL/MinIO 的强一致事务。

### 2. SETNX + 固定 TTL 有什么问题？

- 题目：分布式锁基础追问。
- 面试官想听什么：长任务锁过期导致并发进入。
- 简答：固定 TTL 的问题是任务没结束锁先过期，另一个 worker 进来并发执行。视频合并、下载、AI 处理都可能比预估更慢，所以 VidLens 的锁在获取成功后启动 WatchDog 续期，并且释放时用 Lua 校验 owner。
- 深答：

  <details>
  <summary>展开深答</summary>

  `SET key value NX EX ttl` 只能保证加锁瞬间原子，但不能保证业务执行时间一定小于 ttl。比如合并大文件时，如果 MinIO 操作慢，锁过期后另一个请求拿到锁，就会出现两个请求同时认为自己是持有者。

  当前 `RedisLock` 的设计是：加锁时 value 是 UUID owner，成功后启动 WatchDog，定期续期；续期和释放都通过 Lua 校验当前 value 是否还是这个 owner。这样至少可以避免“我的锁过期后被别人拿走，但我结束时把别人的锁删掉”的误删问题。
  </details>

- 延伸追问：
  - WatchDog 如果续期失败怎么办？
  - Redis 宕机会不会锁丢失？
  - 为什么不直接用 Redisson？
- 项目证据：
  - `internal/pkg/lock/redis_lock.go:15` 注释说明不用固定 TTL，使用 WatchDog。
  - `internal/pkg/lock/redis_lock.go:51` 加锁使用 SetNX 和 TTL。
  - `internal/pkg/lock/redis_lock.go:57` 抢锁成功后启动 WatchDog。
  - `internal/pkg/lock/redis_lock.go:122` Unlock 通过 Lua 校验持有者。
- 当前边界：这是自定义 Redis lock，不是 Redisson；也不是强一致分布式锁协议。

### 3. owner 为什么要用 UUID？安全释放怎么做？

- 题目：锁误删问题。
- 面试官想听什么：释放锁必须确认身份。
- 简答：owner 用 UUID 是为了标识“这次加锁的持有者”。释放时不能直接 DEL key，而要用 Lua 比较 Redis 里的 value 是否等于当前 owner，相等才删除；续期也一样要校验 owner。
- 深答：

  <details>
  <summary>展开深答</summary>

  分布式锁里最危险的错误之一是误删别人的锁。假设 A 拿到锁后因为 GC、网络或业务慢导致锁过期，B 随后拿到同一把锁。如果 A 结束时直接 `DEL key`，就会把 B 的锁删掉，导致 C 又能进来，锁就失效了。

  VidLens 的 `RedisLock` 每次实例都有随机 owner。`renew` 和 `Unlock` 都用 Lua 脚本先判断 `GET key == owner`，只有持有者才续期或删除。这个语义接近 Redisson 的 `isHeldByCurrentThread`，但实现是项目自定义的。
  </details>

- 延伸追问：
  - owner 存 goroutine id 可以吗？
  - Lua 为什么比 GET 后 DEL 更好？
  - 如果 Unlock 返回失败怎么办？
- 项目证据：
  - `internal/pkg/lock/redis_lock.go:16` 注释说明 owner 用 UUID。
  - `internal/pkg/lock/redis_lock.go:31` `NewRedisLock` 创建锁实例。
  - `internal/pkg/lock/redis_lock.go:97` renew 用 Lua 保证原子和 owner 校验。
  - `internal/pkg/lock/redis_lock.go:121` Unlock 安全释放。
- 当前边界：当前锁不可重入，也不建议跨 goroutine 共享。

### 4. WatchDog 续期失败怎么办？

- 题目：锁实现细节。
- 面试官想听什么：续期失败不是静默成功。
- 简答：当前实现会记录续期失败日志，但不会立刻退出 WatchDog，瞬时故障下次仍可续上。如果 Redis 长时间不可用，锁会自然过期，业务需要依靠 DB 约束和幂等逻辑兜底。
- 深答：

  <details>
  <summary>展开深答</summary>

  WatchDog 不是魔法，它只是降低业务执行超过 TTL 时锁过期的概率。如果 Redis 网络短暂抖动，立刻放弃锁反而可能造成业务中断。当前实现的策略是续期失败时打日志，WatchDog 继续跑，下一轮如果 Redis 恢复还能续上。

  但如果 Redis 长时间不可用，锁最终会过期。这个时候不能只依赖锁保证正确性，所以 merge 逻辑还会查 asset，数据库层也需要唯一约束或创建失败回滚对象。面试里我会说锁是并发控制手段，不是唯一一致性保障。
  </details>

- 延伸追问：
  - 续期间隔怎么定？
  - WatchDog 会不会泄漏 goroutine？
  - 业务执行完但 Unlock 失败怎么办？
- 项目证据：
  - `internal/pkg/lock/redis_lock.go:78` 注释说明持有者宕机时锁 TTL 后过期。
  - `internal/pkg/lock/redis_lock.go:86` WatchDog 定期续期。
  - `internal/pkg/lock/redis_lock.go:99` 续期失败会记录日志但不退出。
  - `internal/service/media.go:662` asset 创建失败时删除合并产物。
- 当前边界：没有实现 Redlock 多节点一致性；当前 Redis lock 适合本项目单 Redis 部署。

### 5. 为什么需要 Redis Lua 令牌桶？

- 题目：限流第一问。
- 面试官想听什么：高成本接口和并发原子性。
- 简答：AI 相关接口成本高，不能让单用户或单路由无限调用。令牌桶用 Redis Hash 存 tokens 和 last_time，Lua 在 Redis 内完成读、计算、扣减、写回，避免并发请求把令牌扣成负数。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的上传、转写、摘要、RAG 问答都可能带来外部 AI 成本或资源消耗。限流不是完整计费系统，但可以保护系统和用户 key，避免误操作或脚本调用把资源打满。

  令牌桶适合这里，因为它限制平均速率，同时允许一定突发。实现上用 Redis Hash 保存 `tokens` 和 `last_time`。如果用普通 Go 代码先 HMGET、计算、再 HMSET，多个并发请求会读到同样的旧 token，导致超发。Lua 脚本在 Redis 单线程中原子执行，所以读改写是一体的。
  </details>

- 延伸追问：
  - 固定窗口有什么问题？
  - 漏桶为什么不一定适合？
  - 限流 key 怎么设计？
- 项目证据：
  - `internal/middleware/ratelimit.go:15` 注释说明 Redis + Lua token bucket。
  - `internal/middleware/ratelimit.go:66` Lua 读取 `tokens` 和 `last_time`。
  - `internal/middleware/ratelimit.go:80` token 足够时扣减。
  - `internal/middleware/ratelimit.go:110` Gin RateLimit middleware。
- 当前边界：这是请求限流，不是套餐额度、余额扣费或 token 级 billing。

### 6. 令牌桶和固定窗口、漏桶怎么比较？

- 题目：算法对比题。
- 面试官想听什么：根据业务选择，而不是背定义。
- 简答：固定窗口实现简单但边界会突刺；漏桶输出平滑但可能让空闲资源也排队；令牌桶能限制平均速率，同时允许用户短时间正常连点几次。VidLens 更需要保护高成本接口，同时不让体验太僵硬，所以用令牌桶。
- 深答：

  <details>
  <summary>展开深答</summary>

  固定窗口的问题是边界效应，比如 10:00:59 和 10:01:00 各打满一次，瞬时请求量会翻倍。漏桶更像匀速排队，适合需要稳定出口流量的场景，但用户刚打开页面连续触发几个合理请求时，它可能也强制等待。

  VidLens 的场景是保护资源和成本，而不是绝对平滑输出。比如用户可能连续查看任务、发起一次问答、刷新状态，这种短突发可以接受；但持续高频调用 ASR 或 chat 不应该放行。所以令牌桶更合适。
  </details>

- 延伸追问：
  - route override 有什么用？
  - 怎么按用户限流？
  - IP 限流和用户限流怎么组合？
- 项目证据：
  - `internal/middleware/ratelimit.go:42` 支持按路由设置专属桶。
  - `internal/middleware/ratelimit_test.go:55` 测试 route override。
  - `internal/middleware/ratelimit_test.go:69` 测试更严格路由配额。
  - `internal/middleware/ratelimit.go:125` 请求不允许时返回 429。
- 当前边界：当前没有动态用户套餐限额，后续可结合 `user_usage_daily` 做额度控制。

### 7. Redis 限流故障时 fail-open 还是 fail-closed？

- 题目：可用性和保护性的取舍。
- 面试官想听什么：知道限流不是关键业务存储。
- 简答：当前选择 fail-open。Redis 异常时放行请求，因为限流是保护手段，不应该让 Redis 短故障导致所有 API 不可用。但这也意味着故障期间保护能力下降，生产环境可以对特别高成本接口改成更保守策略。
- 深答：

  <details>
  <summary>展开深答</summary>

  fail-open 和 fail-closed 没有绝对答案。VidLens 当前是简历项目和公开演示，如果 Redis 短暂异常就让所有接口返回 429/500，用户体验会很差，而且上传/查看任务这类操作不一定应该被限流组件阻断。

  所以代码在 Lua 执行失败时记录日志并放行。面试里我会补边界：如果未来做真实付费或严格成本控制，ASR、Embedding、LLM 这类高成本动作可以 fail-closed 或降级为本地小窗口限流；普通查询接口仍可 fail-open。
  </details>

- 延伸追问：
  - Redis 挂了会不会被刷爆？
  - 哪些接口应该 fail-closed？
  - 有没有本地 fallback？
- 项目证据：
  - `internal/middleware/ratelimit.go:94` 注释说明 Redis 异常 fail-open。
  - `internal/middleware/ratelimit.go:102` Redis 异常记录日志并放行。
  - `internal/middleware/ratelimit_test.go:92` 测试 Redis 异常放行。
- 当前边界：fail-open 是当前取舍，不是所有生产场景最优策略。

### 8. Redis 在分片上传里负责什么？

- 简答：当前不负责。durable upload session 的 manifest、进度、chunk hash、完成 lease 和 task identity 都在 PostgreSQL；MinIO 只保存字节。Redis 停止不能让已经接受的 chunk 从业务台账消失。
- 为什么改：旧 Redis 片号协议没有 user ownership、不可变 manifest、同片冲突和稳定完成回执，TTL 也会让业务事实消失。
- Redis 当前职责：分布式锁、Lua 令牌桶和最近聊天记忆。
- 项目证据：
  - `internal/service/upload_session*.go`
  - `internal/repository/upload_session.go`
  - `internal/model/upload_session*.go`
  - [上传专项](/interview/resume-grill/resume-core/chunk-upload/)
- 当前边界：abandoned session 和孤儿对象回收仍需后台任务，但不能用本地内存或 Redis fallback 替代 PostgreSQL 事实。
