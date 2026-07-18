# 专题 2：Redis owner lock、内容指纹与数据库幂等

## 1. 推荐简历表述

> 对分析消费使用带 owner 校验和 WatchDog 续租的 Redis 分布式锁，并结合 PostgreSQL processing lease、资产唯一约束和业务幂等降低同一视频并发重复处理风险；上传完成并发控制由 PostgreSQL token/lease 负责。

不要再说“Redis 锁保护分片合并”。当前 durable upload session 不使用 Redis 锁，也不依赖 Redis 保存进度。

## 2. 三类机制不能混为一谈

```text
Redis owner lock
└── internal/mq/consumer_analyze.go：按视频 MD5 降低并发分析

PostgreSQL processing lease
└── task_jobs：决定某个 task/job 当前执行者的所有权

PostgreSQL upload completion lease
└── upload_sessions：决定某个 session 当前 complete 执行者的所有权
```

Redis lock 是前置互斥，不是业务事实源；两个 PostgreSQL lease 才保存可恢复的执行所有权。后续修改时不能因为它们都叫“锁”而复用同一状态机。

## 3. 当前调用链

### 普通上传与资产复用

1. `UploadFile` 流式写临时文件并在服务端计算完整 MD5 和实际大小。
2. 先按 MD5 查询 active `video_assets`；命中时为当前用户创建新的 task，复用同一对象。
3. 未命中时上传新 MinIO 对象并调用 `CreateOrRestore`。
4. PostgreSQL `video_assets.file_md5` 唯一约束处理并发 winner；loser 查询 winner 资产并 best-effort 删除自己多上传的对象。
5. 创建 task 前使用 `FOR UPDATE` 锁定 active asset，避免与 durable cleanup 的 deleting ownership 冲突。

### 分片上传完成

完整流程在 `03-chunk-upload-resume.md`。complete 通过 PostgreSQL CAS 获取 token/lease，完成事务内锁定或创建 asset、创建 task 并写 session 最终身份。这里不使用 Redis lock。

### 分析消费

`consumer_analyze.go` 先按 MD5 尝试获取 Redis lock，再获取 task/job processing lease。Redis lock 降低同一内容并发进入昂贵分析的概率，processing lease 才决定该 task/job 是否有权产生业务副作用。

## 4. Redis 锁实现

`internal/pkg/lock/redis_lock.go` 的关键点：

- `SET NX PX` 获取带 TTL 的锁；
- value 保存随机 owner，而不是固定值；
- 解锁 Lua 只有在 value 等于 owner 时才删除；
- WatchDog 续租 Lua 同样校验 owner；
- `Unlock` 或 context 结束时停止续租；
- Redis 异常和“锁忙”是不同结果，调用方不能混为一次普通未命中。

这不是 Redisson。可以说借鉴 WatchDog 的生命周期思想，但实现和测试都在 Go 项目内。

## 5. 为什么仍需要数据库兜底

Redis lock 不能提供绝对 exactly-once：进程暂停、网络分区、TTL 边界和 Redis 故障都可能使前置互斥失效。因此当前正确性还依赖：

- `video_assets.file_md5` 唯一约束；
- task/job processing token 与 lease；
- 带 owner 的阶段副作用；
- `CreateOrRestore` 的行锁与 lifecycle 检查；
- consumer 对 terminal、stale、busy 状态的显式处理。

准确表述是“降低重复处理风险”，不是“保证绝不重复”。

## 6. MD5 的边界

项目使用 MD5 作为视频内容指纹和资产复用键，不把它用于密码或签名安全。服务端普通上传与 session complete 都会对完整字节重新计算 MD5，不能只信客户端声明。

MD5 存在理论碰撞，因此它适合当前非对抗性内容复用场景，但不是恶意上传环境下的强安全身份。若业务进入不可信多租户或合规场景，应评估 SHA-256 资产键、抽样/全量二次校验以及迁移兼容方案。

## 7. 高频追问

### Redis 挂了会怎样？

分析入口无法安全获取前置锁时应失败或进入恢复流程，不能假装成功继续昂贵副作用。数据库 processing lease 和唯一约束仍是最终状态兜底。上传 session 不以 Redis 为事实源，因此已接受分片和完成状态不会因 Redis 数据丢失而消失。

### 为什么不只用 Redis lock？

因为锁结束后不保存业务终态，也不能表达 task/job 的 stale、terminal、retry 等状态。可恢复状态必须持久化到 PostgreSQL。

### 为什么不只用数据库？

对当前规模，完全可以进一步评估只保留 PostgreSQL lease。现在 Redis lock 的价值是按内容指纹在进入分析前做跨 task 的粗粒度互斥；processing lease 只隔离具体 task/job。是否保留 Redis lock 应由重复分析成本和实际竞争指标决定，而不是为了展示中间件。

### 不同用户上传同一视频是否共用任务？

不共用 task，只可能复用底层 asset/object。用户状态、AI profile、聊天和 RAG scope 仍按各自 task 与 user 隔离。

## 8. 代码证据

- `internal/pkg/lock/redis_lock.go`
- `internal/pkg/lock/redis_lock_test.go`
- `internal/mq/consumer_analyze.go`
- `internal/repository/task_lease*.go`
- `internal/service/media_file_upload.go`
- `internal/repository/asset.go`
- `internal/service/upload_session_complete.go`

## 9. 30 秒话术

> 我把并发控制分成三层：Redis owner lock 按内容指纹降低并发分析，PostgreSQL processing lease 决定具体 task/job 的执行所有权，上传 complete 则使用独立的 PostgreSQL token/lease。普通上传在服务端计算完整 MD5，`video_assets` 唯一约束决定并发创建 winner，loser 复用 winner 并清理多余对象。所以 Redis 锁只是前置优化，最终正确性依赖数据库状态、唯一约束和幂等，不能说它保证 exactly-once。

## 10. 不要这么说

- 不要说 Redis lock 保护当前分片 merge。
- 不要说 Redis 保存当前上传 session 或已上传片号。
- 不要说使用 Redisson。
- 不要说 MD5 是安全哈希或绝不会冲突。
- 不要说 Redis lock 单独保证强一致或 exactly-once。
