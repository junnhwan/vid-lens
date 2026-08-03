# Spec 02: 投递一致性与任务状态机

> 推进顺序第二份。决策母本见 `docs/specs/00-refactor-decisions.md`；领域语言见 `CONTEXT.md`。
> 与 spec 01 的关系：独立，无依赖，可并行。

## Problem Statement

VidLens 的视频处理链路（ASR、摘要、RAG 索引）是又慢又贵又容易挂的外部 AI 调用，不能丢在 HTTP 请求里同步跑。现有代码已经把首次投递做成 **dispatch lease** 模式：业务事务里把任务状态置为 Queued 并写一段 `lease_kind=dispatch` 的租约（`lease_expires_at`），事务提交后再 publish 到 MQ，publish 失败则 `RestoreRetryDispatch` 补偿，进程在 commit 与 publish 之间崩溃由 RetryScheduler 发现过期租约补投。

这套**已经填了"DB 已提交但消息未投递"的首次投递窗口**——不是空地。但从简历与面试角度有两个真问题：

1. **MQ 选型讲不出痛点**。`internal/mq/producer.go` 第 96-105 行的选型理由是"Kafka 是 Go 生态最主流、社区活跃、天然持久化、拉取削峰、分区水平扩展"——这恰好是校招面试最容易被反问塌的写法：vid-lens 的吞吐是用户级并发（个位到几十 QPS），不是日志管道（万级 TPS），Kafka 的大杀器（partition 并行、ISR、高吞吐日志聚合）一个用不上。选 Kafka 是"堆名气"，不是"对痛点"。面试官一句"你一个 topic 一个 consumer、吞吐又不高，为什么用 Kafka 不用 RabbitMQ"就答含糊。
2. **dispatch lease 没有作为 outbox 等价物被命名和讲清**。机制做对了，但叙事散在 `task_dispatch.go` + `task_dispatch_initial.go` + `retry.go` 三处，没有统一的"投递一致性"叙事，简历写不出稀缺点。

## Solution

两件事，都复用现有 dispatch lease + RetryScheduler seam，不重建：

1. **把 MQ 从 Kafka 换成 RabbitMQ**。选型理由改成痛点驱动：vid-lens 用 MQ 的真实痛点是"耗时任务出 HTTP 请求 + 失败可恢复（AI 服务挂任务不丢）+ 削峰（ASR 配额有限需排队）"，**没有一个需要 Kafka 的大数据量/日志聚合/高吞吐特性**。RabbitMQ 的 ack 重投 / 死信队列 / 优先级 / 路由天然咬合"任务可靠投递"痛点。配置：classic persistent queue + publisher confirm + manual ack + 现有 dispatch lease（lease 在 MQ 投递语义层之外，填 publisher confirm 回调前的窗口）。
2. **把 dispatch lease + RetryScheduler 补投 正式命名为"transactional outbox 等价的投递一致性机制"，并补全故障矩阵测试**。dispatch lease 行就是 outbox 行的等价物（业务事务内同表写、提交后投递、过期由 poller 补投）。只是现有实现是"同表 lease 列"而非"独立 outbox 表"——这是实现选择，不影响叙事。spec 决定**保留同表 lease 而非新建 outbox 表**，因为同表 lease 已与 task 状态机原子耦合，新表是多余；但叙事统一用"投递一致性"讲。
3. **片级 ASR 失败复用折进这条 bullet**。长音频按 300s 分片转写、每片独立持久化状态、失败仅重跑缺失片——这是 job 级投递一致性在片粒度的对称应用，不单立 bullet（原②隐性故障叙事已弃，真实根因是 ASR 模型单次≤10MB 显性约束）。

## User Stories

1. 作为项目维护者，我想要 MQ 选型理由是痛点驱动而非名气驱动，以便面试"为什么用 RabbitMQ 不用 Kafka"能硬答。
2. 作为项目维护者，我想要 RabbitMQ 配 classic persistent queue + publisher confirm + manual ack，以便消息从 queue 到 consumer 是 at-least-once、进程崩在 consumer ack 前不丢。
3. 作为项目维护者，我想要 dispatch lease 继续填"DB 提交后 publisher confirm 回调前进程崩"的窗口，以便 RabbitMQ 原生 publisher confirm 填不了的盲区被覆盖。
4. 作为项目维护者，我想要 RetryScheduler 发现过期 dispatch lease 自动补投，以便进程在 commit 与 publish 之间崩溃后任务不永久丢失。
5. 作为项目维护者，我想要 publish 失败走 RestoreRetryDispatch 补偿并把任务恢复到可重投状态，以便首次投递的同步失败路径闭环。
6. 作为项目维护者，我想要故障矩阵测试覆盖 DB 回滚 / publish 失败 / 进程崩溃（lease 过期）/ 重复消息消费四档，以便简历"故障矩阵下任务不丢失、重复消费幂等无副作用"有可跑证据。
7. 作为项目维护者，我想要消费侧幂等键保证重复投递无副作用，以便 at-least-once 语义下重复消费不产生重复 ASR/索引烧 token。
8. 作为项目维护者，我想要长音频按 300s 分片转写并独立持久化每片状态，以便 ASR 模型单次≤10MB 限制下能处理长视频、失败仅重跑缺失片。
9. 作为项目维护者，我想要片级失败复用与 job 级投递一致性共享同一叙事（"投递-处理-片级"三层失败复用），以便面试能讲清"我在 job 和片两个粒度都做了失败复用"。
10. 作为项目维护者，我想要 dispatch lease 机制被统一命名为"投递一致性 lease"而非散在三处文件，以便叙事清晰、简历可写。
11. 作为项目维护者，我想要 MQ 切换不破坏现有 processing lease（运行期租约）与 RetryScheduler，以便只改投递层、处理层不动。
12. 作为项目维护者，我想要 RabbitMQ 死信队列接住毒消息（重试 MaxRetries 仍失败的 job），以便毒消息不阻塞队列且可由管理接口重投。
13. 作为项目维护者，我想要 trace_id 跨 HTTP → RabbitMQ → consumer 传播，以便跨进程追踪（现有 `internal/mq/trace.go` 已有，切换 MQ 时保留契约）。
14. 作为项目维护者，我想要 claim_token + retry_budget_id 在 RabbitMQ payload 里继续传递，以便 consumer 能原子地把 dispatch lease handoff 到 processing lease（现有契约不因换 MQ 改变）。

## Implementation Decisions

### MQ 切换：Kafka → RabbitMQ

- 替换 `internal/mq/producer.go` 的 `segmentio/kafka-go` 为 RabbitMQ 客户端（`rabbitmq/amqp091-go`）。Producer 的四个 Enqueue 方法（Analyze/Transcribe/Download/RAGIndex）签名不变，只换底层 transport。
- 替换 `internal/mq/consumer*.go` 的 `kafka.Reader` 为 RabbitMQ consumer。`FetchMessage`/`CommitMessages` 契约改为 `Consume`/`Ack`/`Nack`，但上层 `kafkaMessageHandler` 抽象保留（重命名为 `messageHandler`）。
- 配置：queue durable=true；publisher confirm 模式；consumer manual ack；消息 `delivery_mode=2` 持久化。不上 quorum queue（基于 Raft，单人项目面试问 Raft 答不实易翻车，作加分项提一句）。
- partition key 语义改为 RabbitMQ routing key + 按 `md5`/`taskID` 做 consistent-hash exchange 或直接单 queue（vid-lens 不需要 partition 并行，单 queue + prefetch 限流即可）。**这一条是简化点**：Kafka 的 partition 在 vid-lens 是过度设计，RabbitMQ 单 queue + prefetch 更诚实。
- 死信队列：MaxRetries 仍失败的 job 投递到 `video-<jobtype>-dlq`，可由管理接口重投。
- `CreateTopics`/`PingBroker` 改为 RabbitMQ 的 queue declare + connection ping。

### 选型理由叙事（写进代码注释 + 简历）

- 删掉 `producer.go` 现有第 96-105 行的"Kafka 最主流/社区活跃/天然持久化/拉取削峰/分区水平扩展"弱理由。
- 替换为痛点驱动理由：vid-lens 用 MQ 的痛点是任务可靠投递 + 失败可恢复 + 削峰，不是高吞吐日志；RabbitMQ 的 ack 重投 / 死信 / 优先级 / 路由咬合任务队列场景；Kafka 的大数据量/日志聚合/分区并行特性在用户级并发吞吐下用不上，选它是堆名气。

### dispatch lease 命名与叙事统一

- 现有 `enqueueInitialTask` + `PrepareInitialTaskDispatch` + `RestoreRetryDispatch` + RetryScheduler 补投 —— 这套是投递一致性机制。spec 决定**保留同表 lease 实现**（task 表的 `lease_kind=dispatch` 列 + `lease_expires_at`），不新建独立 outbox 表，因为同表 lease 已与 task 状态机原子耦合、新表是多余。
- 但叙事统一：在 `task_dispatch.go` 顶部注释把这套机制命名为"投递一致性 lease（transactional outbox 等价）"，写清它填的是"DB 提交后 publisher confirm 回调前进程崩"窗口。
- **与 RabbitMQ 原生能力的分工边界写进 spec 与代码注释**：RabbitMQ publisher confirm 保证消息从 publisher 到 queue 的投递（异步回调确认），但 confirm 回调是异步的，进程崩在 confirm 回来前消息就丢了；dispatch lease 填这个窗口。这层分工是面试最可能被追的点，必须写清。

### 故障矩阵测试

- 复用 `internal/mq/reliability_review_test.go` 的 fake producer 范式，扩为四档矩阵：
  1. DB 回滚：`PrepareInitialTaskDispatch` 失败 → task 不变、无 lease、无 publish。
  2. publish 失败：publish 返回 err → `RestoreRetryDispatch` 补偿、任务恢复可重投。
  3. 进程崩溃（lease 过期）：commit 后 publish 前进程崩 → RetryScheduler 发现过期 lease 补投。
  4. 重复消息：同一 task 被重复消费 → 消费侧幂等键保证无副作用。
- 四档都要有可跑命令与断言，这是简历"故障矩阵下不丢失、幂等无副作用"的唯二证据。

### 片级 ASR 失败复用

- 现有 FFmpeg 300s 分片 + 每片独立持久化状态 + 失败仅重跑缺失片 —— 复用，不改。
- 叙事折进本 bullet：job 级投递一致性 + 片级失败复用 = "投递-处理-片级"三层失败复用，不单立 bullet。

### GORM 混用边界（决策记录第 9 节）

- dispatch lease 的 CAS（`WHERE id = ? AND status IN ? AND lease_version = ?`）走原生 SQL 手写（现有 `task_dispatch_initial.go` 已是原生 SQL 风格，保持）。
- CRUD 路径留 GORM。不引 sqlc。

### 单一测试 seam

- 验收 seam 只有一个：`internal/mq` 的故障矩阵测试 + `internal/repository/task_lease_test.go` 的 lease CAS 测试。外部行为 = dispatch lease 在四档故障下的可观察结果。不新增多个测试入口。

## Testing Decisions

### 什么算好测试

- 只测外部行为：四档故障下任务状态、lease、publish/补偿/补投的可观察结果。
- 不测 RabbitMQ 客户端内部实现细节。
- 现有 `kafkaMessageHandler` 抽象重命名为 `messageHandler` 后，现有 `consumer_loop_test.go` 等测试范式可复用，只换 transport fake。

### 测试模块

- `internal/mq/reliability_review_test.go` 扩为四档故障矩阵。
- `internal/repository/task_lease_test.go` 保留 lease CAS 测试。
- 新增 RabbitMQ transport fake（替换现有 kafka fake），覆盖 publisher confirm 成功/失败、manual ack/nack。

### Prior art

- `internal/mq/reliability_review_test.go`、`consumer_loop_test.go`、`processing_lease_phase0_test.go` —— 现有可靠性测试范式。
- `internal/repository/task_lease_test.go`、`task_dispatch_initial.go` 的 CAS 范式。

## Out of Scope

- **不新建独立 outbox 表**。同表 dispatch lease 已是 outbox 等价物，新表是多余。
- **不上 quorum queue**。classic persistent queue 够用，quorum 的 Raft 叙事单人项目答不实易翻车。
- **不做 Kafka→RabbitMQ 的渐进迁移**。单人项目直接换，不留双 MQ 并存。
- **不重写 processing lease（运行期租约）**。只改投递层，处理层 lease 不动。
- **不单立片级 ASR bullet**。折进本 bullet 当一句。
- **不碰 RetryScheduler 的重试补投逻辑**。补投路径已闭环，只确认它在新 MQ 下仍工作。

## Further Notes

### 已知边界：消费侧消息去重与调度器重发的 40 分钟交互窗口

**现象**（线上 2026-08-03 实测）：一批任务突然全部卡在 Queued，`task_jobs` 的 `lease_expires_at` 被 RetryScheduler 每 2 分钟续一次，状态永远停在 Queued；RabbitMQ 消费端没有新的 `analyze message received` 日志，publish 计数却在增长；唯一变化是 `retry_budget.go` 的 `record not found` 日志每轮都出现。

**根因**：`consumer_lifecycle.go` 的 `dedupHandler` 用 Redis `SETNX mq:dedup:<queue>:<MessageId>` 做消费侧幂等，键 TTL 40 分钟（`idempotency.go` 默认 retention）。首次批量投递时（07:43–07:46）键被写入；随后服务器重启（08:00:58），调度器发现过期 dispatch lease 并重发**同一个 MessageId**（`analyze:<taskID>`）。消费者在 40 分钟窗口内看到键仍在，判定为重复投递，直接 Ack 为 no-op——业务逻辑从未执行，任务静默卡死。

**触发条件**：服务器重启 + 调度器在 40 分钟内重发同一任务（即 dispatch lease 恢复路径与消费侧去重窗口重叠）。

**影响**：任务不失败、不重试、不进入 DLQ，只是静默等待，直到 dedup 键 TTL 过期后调度器下一次重发才自愈。单次最多卡 40 分钟，但批量场景下表现为"整批卡死数小时"。

**处置**：无需重启服务。等键过期自动恢复；或手动 `redis-cli --scan --pattern 'mq:dedup:*' | xargs redis-cli del` 立即恢复。

**修复方向（未做，仅记录）**：dedup 键的本意是拦截"同一消息的立即重投"（Nack+requeue 场景），40 分钟 TTL 跨重启后把调度器重发也拦了。三个可选方向：
1. 调度器重发使用带 generation 的 MessageId（如 `analyze:<taskID>:<cycle>`），与首次投递区分；
2. dedup 键在任务状态机发生 dispatch 接管时释放（claim 时删键）；
3. 缩短 dedup TTL 至逼近 requeue 间隔，而非 40 分钟。

### 与 00-refactor-decisions.md 的对齐

- MQ 选型 = RabbitMQ（决策记录第 2.1 节）✅
- outbox 形态 = transactional outbox 表 + poller/dispatcher（决策记录第 9.1 节）—— 本 spec 实现为"同表 dispatch lease + RetryScheduler 补投"，是 outbox 的等价物而非字面 outbox 表，spec 第 Implementation Decisions 已写清为何不新建表。✅
- GORM 混用 = 原生 SQL 手写可靠性路径（决策记录第 9.1 节）✅
- 片级失败复用折进①当一句（决策记录第 5 节、第 9.1 节）✅

### 数字占位符（本 spec 产出的简历可用数字）

- **4** 档故障矩阵（DB 回滚 / publish 失败 / 进程崩溃 lease 过期 / 重复消息），可跑：
  - `go test ./internal/mq/ -run "TestFaultMatrix_DBRollbackLeavesTaskUnchanged|TestRetrySchedulerProducerFailureRestoresDispatchLeaseTransactionally|TestRetrySchedulerRecoversExpiredDispatchLeaseAfterCrash|TestFaultMatrix_DuplicateMessageIsIdempotent" -count=1`
  - 行数自检：`go test ./internal/mq/ -run TestFaultMatrixHasFourRows -count=1`
- 切换后 publish P95 `__`ms —— 本地 fake 测不出真实投递延迟，需真实 RabbitMQ 实例；spec 自身标注非阻塞，留 `__` 不造假数字。如 (B) 评测后续需要 P95 检索延迟，可对照在真实 broker 上补测。
- RabbitMQ 死信队列接住的毒消息数 `__` —— 长期运行后填，非本 spec 阻塞。本 spec 已声明 DLX（`video-<jobtype>-dlq`），毒消息会被路由到 DLQ，但计数需生产运行后从 RabbitMQ 管理面采集。

### 简历允许写什么 / 禁止写什么（本 spec 对应 ① bullet 的预演）

**允许写**：
- "采用 transactional outbox 等价的 dispatch lease 消除'DB 已提交但消息未投递'的首次投递窗口（业务事务内同表写 lease、独立 poller 投递 RabbitMQ+publisher confirm+manual ack），故障矩阵（DB 回滚 / publish 失败 / 进程崩溃 / 重复消息）下任务不丢失、重复消费幂等无副作用。"
- "选 RabbitMQ 而非 Kafka：痛点为任务可靠投递而非高吞吐日志，Kafka 的分区并行/日志聚合特性在用户级并发吞吐下用不上。"
- "长音频按 300s 分片持久化每片状态，任务异常仅重跑缺失片段并复用已完成结果。"

**禁止写**：
- "Kafka 是最主流的 Go MQ、社区活跃、天然持久化" —— 弱理由，已弃。
- "Kafka 分区水平扩展支持高吞吐" —— vid-lens 不需要，写了就是堆名气。
- "我写了 outbox 框架" —— 机制是 dispatch lease 已有，本 spec 是切换 MQ + 命名 + 测试，不是新写框架。
- 任何未在故障矩阵测试下产出的数字。
