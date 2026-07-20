# 专题 1：Kafka 异步任务与状态恢复

> 当前事实以 `internal/mq/`、`internal/repository/task_lease*.go` 和 `docs/backend-maintenance-map.md` 为准。

## 1. 推荐简历表述

使用 Kafka 将视频下载、分段 ASR、摘要和 RAG 索引移出 HTTP 长请求，并以 PostgreSQL task/job 状态、processing lease、错误分类和重试调度管理分钟级任务的重复消费与失败恢复。

## 2. 面试口语答案

> 视频处理会经历下载、FFmpeg、外部 ASR、LLM 和 Embedding，任一步都可能持续几十秒或失败。如果直接放在 HTTP 请求中，客户端超时后很难判断任务是否还在执行，进程重启也会丢掉内存任务。因此 VidLens 让 HTTP 接口只负责短事务和任务请求，真正处理由 Kafka consumer 完成。
>
> Kafka 只负责持久化消息和 consumer group 分发，用户可见的业务状态在 PostgreSQL。`video_tasks` 保留聚合状态，`task_jobs` 区分 download、transcribe、analyze、rag_index；consumer 开始前获取带 token、版本和过期时间的 processing lease，重复消息拿不到所有权就不会重复推进。失败时按是否可恢复写入错误码、重试次数和 `next_retry_at`，RetryScheduler 到期后先获取 dispatch lease，再投递 Kafka；投递失败会通过 token/CAS 恢复为可再次扫描的失败状态。
>
> 我不会把它说成 exactly-once。Kafka 仍按 at-least-once 理解，可靠性来自“消息可能重复，但状态转换有 owner 和幂等边界”。目前重试补投失败已经闭环，但首次 task/job 入库与第一次 enqueue 之间还没有 transactional outbox，这是下一阶段需要补的明确一致性缺口。

## 3. 当前主流程

```text
HTTP 请求
  -> PostgreSQL 创建/更新 task 与 task_job
  -> Producer 同步写 Kafka（RequireAll，MaxAttempts=3）
  -> Consumer group 拉取消息
  -> ClaimTaskProcessing：token + lease + version
  -> 下载 / ASR / 摘要 / RAG
  -> CompleteTaskProcessing 或 FailTaskProcessing
  -> 业务结果或失败移交可靠后再提交 offset

RetryScheduler
  -> 扫描 next_retry_at 到期任务或过期 lease
  -> ClaimRetryDispatch
  -> enqueue
  -> 成功：等待 consumer 接管
  -> 失败：RestoreRetryDispatch，退避后可再次扫描
```

Topic 按阶段拆为 `video-download`、`video-transcribe`、`video-analyze`、`video-rag-index`。拆分是为了隔离不同耗时和依赖，不代表实现了通用工作流引擎。

## 4. 高频追问

### 为什么不用 goroutine？

> goroutine 只能提供进程内异步。进程退出后任务不可恢复，多实例之间也没有共享积压和消费所有权；视频任务不能把可靠性寄托在内存队列上。

### Kafka 和 PostgreSQL 谁是真相源？

> Kafka 是待处理工作载体，PostgreSQL 是业务状态真相源。只看 offset 无法回答用户任务处于哪个阶段、为什么失败、何时重试。

### 重复消息怎么办？

> consumer 用 task/job 当前状态、processing token、lease version 和条件更新判断是否拥有执行权。Kafka key 影响分区，但不能代替数据库幂等。

### 为什么业务失败后可以提交 offset？

> 只有失败状态和重试时间已经可靠写入 PostgreSQL，或失败已进入不可恢复终态，当前消息的责任才完成。若失败状态本身无法落库，不应该把它描述成已经可靠移交。

### Kafka 写成功、数据库失败怎么办？

> consumer 收到消息后仍要按数据库状态/lease 判定；无合法任务或无法 claim 时不会盲目执行。反方向“数据库成功、首次 Kafka 写失败”目前尚未完全闭环，不能用 RetryScheduler 的补投闭环冒充首次投递 outbox。

### 为什么选 Kafka 而不是 RocketMQ？

> RocketMQ 在 Java 业务消息、延迟消息和事务消息场景很强；本项目是 Go 栈，Kafka 客户端、consumer group 和持久化日志足以支撑当前任务队列。延迟重试语义由 PostgreSQL 状态机补充。选择是生态和已有实现的折中，不是说 Kafka 全面优于 RocketMQ。

## 5. 代码证据

- `internal/mq/producer.go`：同步 producer、payload、key 和 acks。
- `internal/mq/consumer_lifecycle.go`：reader 生命周期、手动 commit、poison message 处理。
- `internal/mq/consumer_*.go`：各阶段执行逻辑。
- `internal/mq/retry.go`：错误分类、退避和 RetryScheduler。
- `internal/repository/task_lease*.go`：processing/dispatch lease 与 CAS。
- `internal/model/task.go`、`internal/model/task_job.go`：任务状态模型。

## 6. 当前限制

- 首次 task/job 写入与 Kafka enqueue 尚无 transactional outbox；相关修复完成前不能声称原子提交。
- 单机 Compose 的 Kafka replication factor 是 1，`RequireAll` 不等于生产级多副本容灾。
- 不是 exactly-once，也不是 Saga/工作流引擎。
- URL 下载不是简历核心能力，下载安全边界也未达到生产级 SSRF 隔离。
