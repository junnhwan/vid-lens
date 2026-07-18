# 专题 6：任务失败治理、lease 与退避重试

## 1. 推荐简历表述

为异步视频任务建立可恢复/不可恢复错误分类、阶梯退避、provider `Retry-After`、processing/dispatch lease 和 dead 终态，使失败能被查询、重试和人工解释，而不是只依赖 Kafka 重放。

## 2. 面试口语答案

> 外部 ASR、LLM、MinIO 和网络错误不能统一“重试三次”。超时、连接中断、429 和部分 5xx 往往可恢复；缺少用户 AI 配置、权限错误、文件不存在、Embedding 维度不匹配等错误，重复请求不会变好。VidLens 优先使用结构化 `ProviderError.Retryable` 和 `RetryAfter`，其他旧错误才用保守的文本规则兜底。
>
> 每次失败会同时更新 `video_tasks` 的兼容状态和对应 `task_jobs` 的阶段状态，记录错误码、错误摘要、重试次数和 `next_retry_at`。可恢复错误按配置的退避序列延迟；provider 明确给出的 `Retry-After` 更长时尊重它。超过最大次数进入 dead，不可恢复错误直接 failed。
>
> RetryScheduler 不是查到就直接发消息。它先用版本号、token 和过期时间获取 dispatch lease，避免多实例重复补投；Kafka enqueue 失败后再用同一个 token/CAS 恢复失败状态和新的扫描时间。consumer 接手时使用 processing lease，过期 lease 可以被后续执行者接管，旧 owner 不能完成新 lease。

## 3. 状态与恢复流程

```text
consumer failure
  -> classify
  -> non-retryable: failed
  -> retryable and budget remains:
       failed + retry_count + next_retry_at
  -> budget exhausted: dead

scheduler
  -> scan due/expired
  -> claim dispatch lease
  -> enqueue Kafka
     -> success: consumer processing lease 接管
     -> failure: restore failed + backoff
```

`video_tasks` 是对外兼容聚合状态；`task_jobs` 区分四类动作。两套记录在 repository transaction/条件更新中共同推进，不能各写一半后当作成功。

## 4. 高频追问

### 为什么不直接使用 Kafka 自动重试？

> 长耗时业务需要把错误原因、下一次时间和次数展示给用户，也需要区分确定性失败。占住 consumer 循环重试会阻塞分区，单靠 offset 也无法表达业务终态。

### 为什么需要 lease，而不是 status=running？

> `running` 不能判断执行者是否已经宕机。lease 同时提供 owner token 和 expiry；过期后允许接管，完成时再校验 token/version，避免旧执行者覆盖新结果。

### scheduler 投递成功后立刻清空 lease 吗？

> dispatch token 会随消息传递，consumer 用它完成所有权 handoff。这样可以区分合法补投消息和过期执行者，而不是在 Kafka ack 后制造无 owner 空窗。

### 重试次数是否等于 provider HTTP 次数？

> 不一定。业务 task retry、AI client 内部短重试和 usage/retry budget 是不同层次，面试时不能把它们混成一个数字。

## 5. 代码证据

- `internal/mq/retry.go`：分类、退避、RetryScheduler。
- `internal/repository/task_lease_processing.go`：processing claim。
- `internal/repository/task_lease_dispatch.go`：dispatch claim/restore。
- `internal/repository/task_lease_terminal.go`：成功与失败终态。
- `internal/model/task.go`、`internal/model/task_job.go`：字段和状态。
- `internal/mq/reliability_review_test.go`、`internal/mq/consumer_test.go`：lease 与失败恢复测试。

## 6. 当前限制

- 文本关键字分类是兼容兜底，新增 provider 应优先返回结构化错误。
- 首次业务请求的 DB 写入与第一次 enqueue 仍待 durable dispatch/outbox 修复；本专题已完成的是 consumer/retry 路径。
- dead 目前是可查询终态，不代表已经有完善的运营后台或人工工单系统。
