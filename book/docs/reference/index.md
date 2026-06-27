# 📋 八股速查表

## Go 并发

| 概念 | 要点 |
|------|------|
| goroutine | 轻量级协程，初始栈 2KB，可动态增长 |
| channel | 有缓冲/无缓冲，select 多路复用 |
| sync.WaitGroup | Add/Done/Wait，计数器归零时解除阻塞 |
| sync.Mutex | 互斥锁，Lock/Unlock，不可重入 |
| sync.Once | 单次执行，Do() 保证并发安全 |
| context | WithCancel/WithTimeout/WithValue，取消传播 |
| atomic | 原子操作，CAS 比 Mutex 更轻量 |
| errgroup | 并发执行 + 错误聚合 + context 取消 |
| singleflight | 同一 key 只执行一次，防缓存击穿 |

## Redis

| 命令 | 用途 | 项目使用 |
|------|------|----------|
| SETNX | 分布式锁 | `redis_lock.go` TryLock |
| HMGET/HMSET | Hash 存储 | `ratelimit.go` 令牌桶 |
| EXPIRE | TTL 设置 | 锁续期、令牌桶过期 |
| SET | 缓存 | ChatMemory |
| Lua 脚本 | 原子操作 | 锁释放、令牌桶扣减 |

## Kafka

| 概念 | 要点 | 项目使用 |
|------|------|----------|
| Producer | 同步发送，RequiredAcks=All | `producer.go` |
| Consumer Group | 组内竞争消费 | 4 个 consumer goroutine |
| Offset Commit | 手动提交，at-least-once | `consumer.go` |
| 幂等性 | 消费前检查任务状态 | `UpdateStatusIf` 乐观锁 |
| 死信队列 | 多次重试后标记 Dead | `retry.go` |

## MySQL

| 概念 | 要点 | 项目使用 |
|------|------|----------|
| 事务 | BEGIN/COMMIT/ROLLBACK | `repository.go` Transaction |
| 唯一索引 | 防重复插入 | Asset.MD5, TaskJob.(task_id, job_type) |
| 软删除 | gorm.DeletedAt | 所有主表 |
| 条件更新 | WHERE status IN (...) | `UpdateStatusIf` 乐观锁 |
| Upsert | ON CONFLICT DO UPDATE | `TaskJobRepository` |
| 索引 | 复合索引、覆盖索引 | VideoChunk(task_id, chunk_index, embedding_model) |

## 分布式系统

| 概念 | 要点 | 项目使用 |
|------|------|----------|
| 分布式锁 | Redis SetNX + Lua | `redis_lock.go` |
| 状态机 | 条件更新防并发 | `UpdateStatusIf` |
| 幂等性 | 唯一约束 + Upsert | TaskJob 投递 |
| 重试退避 | 指数退避 + 死信 | backoff [60, 300, 900]s |
| 限流 | 令牌桶 + Fail-Open | `ratelimit.go` |
| SSRF 防护 | 域名白名单 + IP 检查 | `remote_video_url.go` |

## 设计模式

| 模式 | 场景 | 项目使用 |
|------|------|----------|
| 策略 | 算法可替换 | `ai/strategy.go` |
| 工厂 | 创建复杂对象 | `ai/factory.go` |
| 装饰器 | 附加功能不改原类 | `ai/observed.go` |
| Repository | 数据访问抽象 | `repository/` |
| 状态机 | 状态流转控制 | `model/task.go` |
| 观察者 | 事件通知 | `ai_observer.go` |
| 模板方法 | 流程固定步骤可变 | Consumer 6 步处理 |

## 网络协议

| 概念 | 要点 | 项目使用 |
|------|------|----------|
| SSE | Server-Sent Events，单向流 | `ChatHandler.AskStream` |
| JWT | Bearer Token 认证 | `middleware/auth.go` |
| CORS | 跨域资源共享 | `middleware/cors.go` |
| multipart/form-data | 文件上传 | `MediaHandler.UploadChunk` |
