# Kafka 异步处理 -- 面试题

> 基于 VidLens 项目 `internal/mq/` 包的真实源码，覆盖 Producer、Consumer、重试系统、TraceID 传播等核心模块。

---

## Q1: VidLens 的 Kafka Producer 为什么要用同步发送而不是异步发送？

**源码位置**: `internal/mq/producer.go:56-59`

```go
// producer.go:52-59
return &kafka.Writer{
    Addr:         kafka.TCP(brokers...),
    Topic:        topic,
    Balancer:     &kafka.LeastBytes{},
    RequiredAcks: kafka.RequireAll,    // 等所有 ISR 副本确认
    MaxAttempts:  3,                   // 发送失败最多重试 3 次
    Async:        false,               // 同步发送，确保消息投递成功
}
```

**参考答案**:

VidLens 的任务投递发生在 HTTP 请求链路中（用户上传视频后立即投递）。同步发送 (`Async: false`) 配合 `RequiredAcks: RequireAll` 能确保消息到达 Kafka 后才返回成功，避免用户看到"上传成功"但消息实际丢失的情况。虽然同步发送会增加接口 RT，但 VidLens 通过 `RequiredAcks=All` + `MaxAttempts=3` 的组合，在可靠性和性能之间取得了平衡。

**追问链**:

- Q: 同步发送不是会拖慢接口响应吗？VidLens 怎么解决的？
  - A: Producer 投递的是一个极小的 JSON 消息（taskID + MD5 + traceID），序列化后通常只有几十字节。Kafka 单条小消息的同步写入延迟在 5-20ms 级别，对用户上传接口的总 RT 影响很小。且 VidLens 采用"投递即返回"策略（`producer.go:75` 注释），接口 RT 压缩到 50ms 以内。
- Q: 如果 Kafka 完全不可用怎么办？
  - A: `WriteMessages` 会返回 error，调用方应捕获并返回 HTTP 500。但 VidLens 在 Handler 层还有兜底：即使投递失败，`VideoTask` 已经以 `StatusQueued` 写入 DB，`RetryScheduler` 会定时扫描并重新投递。
- Q: `RequiredAcks=All` 和 `RequiredAcks=1` 有什么区别？
  - A: `All` 等待所有 ISR 副本确认，消息最安全但延迟最高；`1` 只等 Leader 确认，速度快但 Leader 宕机可能丢消息。VidLens 选 `All` 因为任务消息是业务核心数据，不可丢失。

---

## Q2: VidLens 的消息 Key 路由策略是什么？为什么选 MD5 作为 Key？

**源码位置**: `internal/mq/producer.go:84-87`

```go
// producer.go:84-87
return p.analyzeWriter.WriteMessages(ctx, kafka.Message{
    Key:   []byte(md5), // Key = MD5，保证同视频进入同一分区
    Value: payload,
})
```

**参考答案**:

VidLens 使用视频文件的 MD5 作为 Kafka 消息 Key。Kafka 的 `LeastBytes` Balancer 会根据 Key 的哈希值将消息路由到固定分区。这意味着同一个视频的所有任务消息（analyze、transcribe、download）都会进入同一个分区，被同一个 Consumer 实例消费，天然保证了同一视频的处理顺序性。

**追问链**:

- Q: 为什么不直接用 taskID 作为 Key？
  - A: taskID 在投递时可能还未生成（虽然 VidLens 是先建 Task 再投递），更关键的是：同一视频可能被不同用户上传生成不同 taskID，用 MD5 能将这些任务聚合到同一分区，配合分布式锁实现"同一视频全局只处理一次"。
- Q: 如果两个用户上传了同一个视频会怎样？
  - A: 两个 task 的消息 Key 相同（MD5 相同），会进入同一分区、被同一 Consumer 消费。Consumer 在 `handleAnalyze` 中通过分布式锁（`vidlens:lock:{md5}`）保证同一时间只有一个任务在处理。第二个任务抢锁失败后直接跳过（`producer.go:411`），实现了内容级去重。
- Q: 分区数设为 4 是怎么考虑的？
  - A: `CreateTopics` 中 `NumPartitions: 4`（`producer.go:173`），配合单机部署 `ReplicationFactor: 1`。4 个分区支持未来扩展到 4 个 Consumer 实例并行消费，当前单实例则所有分区由一个 Reader 轮询。

---

## Q3: VidLens 的 Consumer 为什么要手动提交 offset？失败时不 commit 会怎样？

**源码位置**: `internal/mq/consumer.go:134-161`

```go
// consumer.go:134
CommitInterval: 0,   // 手动提交（不自动提交）

// consumer.go:151-158
if err := c.handleAnalyze(context.Background(), msg); err != nil {
    log.Printf("[Kafka] 分析任务失败: %v", err)
    // 消费失败不 commit offset，下次会重新消费
    // 这就是 Kafka 的 at-least-once 语义
} else {
    if err := r.CommitMessages(context.Background(), msg); err != nil {
        log.Printf("[Kafka] commit offset 失败: %v", err)
    }
}
```

**参考答案**:

`CommitInterval: 0` 禁用了 Kafka Reader 的自动提交。VidLens 采用手动提交策略：只有当 `handleAnalyze` 返回 `nil`（业务成功）时才调用 `CommitMessages`。如果消费失败（返回 error），offset 不会被提交，Kafka 会在下次 Poll 时重新投递这条消息，实现 at-least-once 语义。

**追问链**:

- Q: at-least-once 不会导致消息重复消费吗？
  - A: 会。但 VidLens 在业务层做了两层幂等保障：① 分布式锁（同一 MD5 同时只有一个实例处理）；② 状态机检查（`UpdateStatusIf` 只在 `Queued/Failed` 状态下才更新为 `Running`，已完成的任务会跳过）。即使消息被重复消费，业务逻辑也不会重复执行。
- Q: 为什么不用 exactly-once？
  - A: Kafka 的 exactly-once 需要事务性 Producer + 幂等性 Consumer，对 `segmentio/kafka-go` 的支持不完善，且实现复杂。VidLens 用 at-least-once + 业务层幂等的组合，代码更简单、可靠性也足够。
- Q: 如果 `CommitMessages` 本身失败怎么办？
  - A: 消息会被重复消费一次，但幂等校验保证不会重复执行。日志会记录 commit 失败便于排查。

---

## Q4: VidLens 的六步消费流程是什么？每一步的作用是什么？

**源码位置**: `internal/mq/consumer.go:393-461`

```go
// consumer.go:393-461 handleAnalyze 六步流程
func (c *Consumer) handleAnalyze(ctx context.Context, msg kafka.Message) error {
    // 第 1 步：解析消息
    var payload AnalyzePayload
    json.Unmarshal(msg.Value, &payload)

    // 第 2 步：基于 MD5 获取分布式锁
    lockKey := fmt.Sprintf("vidlens:lock:%s", payload.MD5)
    distLock := lock.NewRedisLock(c.rdb, lockKey)
    acquired, _ := distLock.TryLock(ctx, 5*time.Second)
    if !acquired { return fmt.Errorf("同一视频正在处理中") }
    defer distLock.Unlock(ctx)

    // 第 3 步：幂等校验
    task, _ := c.repo.Task.FindByID(payload.TaskID)
    if task.Status == model.TaskStatusCompleted { return nil }

    // 第 4 步：更新状态为处理中
    updated, _ := c.repo.Task.UpdateStatusAndStageIf(payload.TaskID,
        []int8{model.TaskStatusPending, model.TaskStatusQueued,
               model.TaskStatusFailed, model.TaskStatusCompleted},
        model.TaskStatusRunning, model.TaskStageSummarizing, "")
    if !updated { return nil }

    // 第 5 步：核心业务
    c.processVideo(ctx, task)

    // 第 6 步：更新状态为已完成
    c.repo.Task.UpdateStatusAndStage(payload.TaskID,
        model.TaskStatusCompleted, model.TaskStageNone, "")
}
```

**参考答案**:

| 步骤 | 作用 | 失败后果 |
|------|------|----------|
| 1. 解析消息 | 反序列化 Kafka 消息得到 taskID/MD5/traceID | 返回 error，不 commit，等重投 |
| 2. 分布式锁 | 用 MD5 作为锁 Key，防止同一视频被多实例并发处理 | 抢锁失败返回 nil，静默跳过 |
| 3. 幂等校验 | 检查任务是否已完成（有 Summary 则跳过） | 防止重复处理已完成的任务 |
| 4. 更新状态 | CAS 更新 `Queued→Running`，抢占任务所有权 | 状态已变则返回 nil，其他实例已接管 |
| 5. 业务逻辑 | FFmpeg 提取音频 → ASR 转写 → LLM 摘要 | 调用 `recordTaskFailure` 记录失败，可重试 |
| 6. 更新完成 | 设置 `Status=Completed, Stage=None` | 返回 error，下次重投时幂等检查会兜底 |

**追问链**:

- Q: 第 2 步和第 4 步都在做并发控制，为什么需要两层？
  - A: 分布式锁是"互斥"——同一时间只有一个实例能处理同一 MD5 的任务。状态机更新是"幂等"——即使锁释放后消息被重复消费，已完成的任务不会被再次处理。两层保障覆盖了不同场景：锁防止并发，状态防重复。
- Q: 第 4 步的 `UpdateStatusIf` 和普通 `Update` 有什么区别？
  - A: `UpdateStatusIf` 是 CAS（Compare-And-Swap）操作，只有当前状态在允许列表中才会更新。这防止了两个实例同时将同一个任务从 `Queued` 更新为 `Running` 的竞态条件。

---

## Q5: VidLens 的重试系统是如何设计的？指数退避策略是什么？

**源码位置**: `internal/mq/retry.go:20-49`

```go
// retry.go:20-24
type TaskRetryPolicy struct {
    MaxRetries     int
    BackoffSeconds []int   // [60, 300, 900]
    Now            func() time.Time
}

// retry.go:39-49
func (p TaskRetryPolicy) backoffForRetry(retryCount int) time.Duration {
    p = p.normalized()
    if retryCount <= 0 {
        retryCount = 1
    }
    idx := retryCount - 1
    if idx >= len(p.BackoffSeconds) {
        idx = len(p.BackoffSeconds) - 1
    }
    return time.Duration(p.BackoffSeconds[idx]) * time.Second
}
```

**参考答案**:

VidLens 的重试系统由三部分组成：

1. **TaskRetryPolicy**: 定义最大重试次数（默认 3 次）和退避间隔（默认 `[60, 300, 900]` 秒，即 1 分钟、5 分钟、15 分钟）。
2. **recordTaskFailure**: 消费失败时调用，判断错误是否可重试（`isRetryableError`），可重试则计算下次重试时间写入 DB，不可重试则标记为终态。
3. **RetryScheduler**: 定时扫描到期的重试任务（`FindDueRetryTasks`），重新投递到 Kafka。

退避策略是**阶梯式**而非指数式：第 1 次重试等 60s，第 2 次等 300s，第 3 次等 900s。超过最大次数后标记为 `TaskStatusDead`。

**追问链**:

- Q: `isRetryableError` 怎么判断错误是否可重试？
  - A: 先检查黑名单（`nonRetryable`，如"请先配置 AI 服务"、"video unavailable"），命中则不可重试。再检查白名单（`retryable`，如"timeout"、"connection refused"、"HTTP 503"），命中则可重试。都不命中默认不可重试。
- Q: 为什么用阶梯式退避而不是指数退避？
  - A: 阶梯式退避更可控：运维可以精确配置每级间隔。对于 Kafka 消费场景，60s/300s/900s 的阶梯已经足够覆盖大部分临时故障（如 AI 服务过载、网络抖动），同时不会让任务等太久。
- Q: `RetryScheduler` 的 `ClaimRetryTask` 是做什么的？
  - A: 这是一个 CAS 操作，确保同一个重试任务不会被多个 Scheduler 实例重复投递。类似于乐观锁：只有一个实例能成功"认领"任务。

---

## Q6: VidLens 的长音频 ASR 转写流程是什么？为什么要做分片？

**源码位置**: `internal/mq/consumer.go:579-624`

```go
// consumer.go:579-624
func (c *Consumer) transcribeAudio(ctx context.Context, taskID int64,
    audioPath string, strategy ai.Strategy) (string, error) {
    // 1. 音频分片：按 300 秒切片
    chunks, _ := splitAudio(ctx, c.ffmpegPath, audioPath, 300)

    // 2. 逐片转写
    parts := make([]string, 0, len(chunks))
    for i, chunk := range chunks {
        // 2a. 检查该片是否已完成（断点续传）
        if completed := c.completedTranscriptionChunk(taskID, i); completed != "" {
            parts = append(parts, completed)
            continue
        }
        // 2b. 标记该片为处理中
        c.markTranscriptionChunkRunning(taskID, i, chunk)
        // 2c. 调用 ASR
        text, _ := strategy.Transcribe(ctx, chunk)
        // 2d. 保存该片结果
        c.markTranscriptionChunkCompleted(taskID, i, chunk, text)
        parts = append(parts, text)
    }

    // 3. 合并所有片
    transcript := strings.Join(parts, "\n\n")
    return transcript, nil
}
```

**参考答案**:

长音频 ASR 转写流程：

1. **FFmpeg 提取音频**: 从视频中提取 MP3（单声道、16kHz、32kbps）
2. **SplitAudio 分片**: 按 300 秒（5 分钟）切成多个片段
3. **逐片转写**: 每片独立调用 ASR API，通过 `VideoTranscriptionChunk` 记录每片状态
4. **断点续传**: 如果某片已完成（`completedTranscriptionChunk`），直接复用结果
5. **合并结果**: 所有片的转写文本用 `\n\n` 拼接

分片的原因：ASR API 通常有单次请求的音频时长限制（如 600 秒），且长音频转写容易超时。分片后每片独立处理，失败只需重传该片，不用从头开始。

**追问链**:

- Q: 如果第 3 片转写失败了，前 2 片的结果怎么办？
  - A: 前 2 片的结果已通过 `markTranscriptionChunkCompleted` 写入 `video_transcription_chunks` 表。下次重试时，`completedTranscriptionChunk` 会检查并复用已完成的片段，实现断点续传。
- Q: 300 秒这个值是怎么确定的？
  - A: `ffmpeg.DefaultAudioSegmentSeconds = 300`（`ffmpeg.go:14`）。5 分钟是大多数 ASR API 的安全上限，同时能保证单片转写在 30 秒内完成，避免超时。
- Q: 分片拼接时用 `\n\n` 会不会丢失上下文？
  - A: 确实会丢失跨片的上下文。但 VidLens 的后续 LLM 摘要阶段会处理整个转写文本，LLM 的上下文窗口足够长（通常 128K tokens），能弥补分片带来的上下文断裂。

---

## Q7: VidLens 如何实现转写结果的复用？

**源码位置**: `internal/mq/consumer.go:531-539`

```go
// consumer.go:531-539
func (c *Consumer) processVideo(ctx context.Context, task *model.VideoTask) error {
    existingTranscription, _ := c.repo.Transcription.FindByTaskID(task.ID)
    if existingTranscription != nil &&
       strings.TrimSpace(existingTranscription.Content) != "" {
        log.Printf("[Kafka] 复用已有转录生成总结: taskID=%d", task.ID)
        return c.summarizeTask(ctx, task)  // 直接跳到摘要步骤
    }
    // ... 正常的 FFmpeg → ASR → 摘要流程
}
```

**参考答案**:

VidLens 在 `processVideo` 入口处检查 `video_transcriptions` 表是否已有该 taskID 的转写结果。如果有，直接跳过 FFmpeg 提取音频和 ASR 转写，进入 LLM 摘要步骤。这实现了两种场景的复用：

1. **同一任务重试**: 任务在摘要阶段失败后重试，转写结果已保存，无需重新转写。
2. **分步任务**: `transcribe` 任务完成后，`analyze` 任务可以直接复用转写结果。

**追问链**:

- Q: 这个复用检查会不会有并发问题？
  - A: 不会。前面的分布式锁和状态机已经保证了同一时间只有一个实例在处理同一任务。且 `FindByTaskID` 是只读操作，不存在竞态。
- Q: 如果转写结果是空的怎么办？
  - A: `strings.TrimSpace(existingTranscription.Content) != ""` 会过滤掉空内容，空转写会被视为"无结果"，触发完整的 FFmpeg → ASR 流程。

---

## Q8: VidLens 的 TraceID 传播机制是什么？如何实现全链路追踪？

**源码位置**: `internal/mq/trace.go:1-20`

```go
// trace.go:1-20
type traceIDContextKey struct{}

func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
    if traceID == "" { return ctx }
    return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
    if ctx == nil { return "" }
    traceID, _ := ctx.Value(traceIDContextKey{}).(string)
    return traceID
}
```

配合 Producer 中的使用（`producer.go:81-82`）：

```go
// producer.go:81-82
payload, _ := json.Marshal(AnalyzePayload{
    TaskID:  taskID,
    MD5:     md5,
    TraceID: TraceIDFromContext(ctx),  // 从 context 提取 traceID
})
```

**参考答案**:

VidLens 的 TraceID 传播分三层：

1. **HTTP 入口**: 用户上传时生成 TraceID，写入 `VideoTask.TraceID` 字段
2. **Kafka 消息**: Producer 通过 `TraceIDFromContext(ctx)` 将 TraceID 嵌入消息 Payload
3. **Consumer 恢复**: Consumer 解析消息后通过 `ContextWithTraceID(ctx, traceID)` 恢复到 context，后续所有操作（日志、DB 查询）都能拿到 TraceID

使用 `traceIDContextKey` 结构体作为 context key，避免与其他包的 key 冲突（Go context 最佳实践）。

**追问链**:

- Q: 为什么不用 HTTP Header 传播 TraceID？
  - A: Kafka 消息不是 HTTP 请求，没有 Header 的概念。VidLens 将 TraceID 序列化到消息 Payload 中（`AnalyzePayload.TraceID`），Consumer 解析后手动注入 context。
- Q: 如果 Payload 中的 TraceID 为空怎么办？
  - A: `traceIDForTask` 函数（`consumer.go:821-829`）会先检查 Payload 的 TraceID，为空则回退到 `task.TraceID`，确保日志中始终有 TraceID 可追踪。
- Q: TraceID 在哪些地方被使用？
  - A: 所有 Consumer 的日志输出都包含 TraceID（如 `[Kafka] 收到分析任务: traceID=%s taskID=%d`），`recordTaskFailure` 记录失败时也会保存 TraceID。运维可以通过 TraceID 在日志中搜索一个任务的完整处理链路。

---

## Q9: VidLens 的 Consumer 如何处理不同类型的错误？业务失败和基础设施失败有什么区别？

**源码位置**: `internal/mq/consumer.go:214-227`

```go
// consumer.go:214-227 (download consumer)
// 业务级失败（下载失败、上传失败等）已由 handleDownload 记入 task_job 表，
// 由 RetryScheduler 兜底，此时返回 nil 走 commit；
// 基础设施级失败（消息解析、DB 查询、回写）返回 err，不 commit，由 Kafka at-least-once 重投。
if err := c.handleDownload(context.Background(), msg); err != nil {
    log.Printf("[Kafka] 下载任务消息异常（不提交 offset，等待重投）: %v", err)
} else {
    r.CommitMessages(context.Background(), msg)
}
```

配合 `recordTaskFailure`（`retry.go:103-138`）：

```go
// retry.go:118-123
if !isRetryableError(err) {
    c.repo.Task.RecordTerminalFailure(taskID, jobType, stage,
        "non_retryable_error", errMsg, task.RetryCount, maxRetries,
        model.TaskStatusFailed)
    return c.repo.TaskJob.RecordTerminalFailure(...)
}
```

**参考答案**:

VidLens 将错误分为两类，处理策略完全不同：

| 错误类型 | 示例 | 处理策略 | offset 行为 |
|----------|------|----------|-------------|
| **基础设施级** | 消息解析失败、DB 查询失败 | 返回 error，Kafka 重投 | 不 commit |
| **业务级** | 下载失败、ASR 失败、LLM 超时 | 返回 nil，记入 DB，由 RetryScheduler 兜底 | commit |

业务级错误进一步分为：
- **可重试**（timeout、HTTP 503）：记录 `next_retry_at`，等 RetryScheduler 重新投递
- **不可重试**（video unavailable、API key 无效）：直接标记为 `TaskStatusFailed`

**追问链**:

- Q: 为什么要区分这两种错误？
  - A: 基础设施级错误通常是瞬时的（如 DB 连接池耗尽），Kafka 重投几秒后就能恢复。业务级错误可能需要更长的退避（如 AI 服务过载需等 1 分钟），用 RetryScheduler 的阶梯退避更合适。
- Q: 如果业务逻辑执行到一半（如 ASR 成功但 LLM 失败）怎么办？
  - A: `processVideo` 中的每一步都独立保存结果（转写存 `video_transcriptions`，摘要存 `ai_summaries`）。失败后重试时，`processVideo` 入口会检查已有转写结果并复用（Q7），从 LLM 步骤继续。
- Q: `isRetryableError` 的白名单策略安全吗？未知错误会怎样？
  - A: 默认不可重试。这是一个保守策略：宁可让运维手动重试一个未知错误，也不要无限重试一个永久性错误导致资源浪费。

---

## Q10: VidLens 的 Kafka 架选型理由是什么？与 RocketMQ/RabbitMQ 的对比？

**源码位置**: `internal/mq/producer.go:29-38`

```go
// producer.go:29-38
// 面试亮点（选型理由）：
//
//  为什么选 Kafka 而不是 RocketMQ / RabbitMQ？
//  1. Kafka 是 Go 后端生态中最主流的 MQ，社区活跃，Go 客户端成熟
//  2. 天然支持消息持久化（磁盘落盘），不怕宕机丢消息
//  3. 基于拉取模式消费，消费者按自己的节奏处理，天然削峰
//  4. 分区机制支持水平扩展，未来增加消费者实例就能提升吞吐
//  不选 RocketMQ 的理由：Go 客户端不够成熟，更偏 Java 生态
//  不选 RabbitMQ 的理由：海量消息堆积能力不如 Kafka，Erlang 底层不好排查问题
```

**参考答案**:

VidLens 选择 Kafka 的核心理由：

| 维度 | Kafka | RocketMQ | RabbitMQ |
|------|-------|----------|----------|
| Go 生态 | `segmentio/kafka-go` 成熟 | Go SDK 不成熟 | `amqp091-go` 可用但功能受限 |
| 消息堆积 | 磁盘顺序写，堆积不影响性能 | 支持但生态偏 Java | 堆积会导致内存问题 |
| 消费模式 | Pull（消费者主动拉取） | Push + Pull | Push（Broker 推送） |
| 分区扩展 | 原生支持，增加分区+消费者即可 | 支持 | 不支持原生分区 |
| 运维复杂度 | 中等 | 高（依赖 NameServer） | 低 |

VidLens 的视频处理场景特点：
1. **消息量大**: 每个视频产生 4 条消息（download/transcribe/analyze/rag-index）
2. **处理时间长**: 单个视频的 FFmpeg + ASR 可能需要 10 分钟
3. **需要堆积能力**: 高峰期可能积压数百个任务

这些特点完美匹配 Kafka 的 Pull 模型和磁盘堆积能力。

**追问链**:

- Q: Kafka 的 Pull 模式对 VidLens 有什么具体好处？
  - A: Consumer 按自己的节奏拉取消息，不会被 Broker 的推送速率压垮。当一个视频正在做 ASR（耗时 10 分钟），Consumer 不会收到新消息，避免了内存溢出和并发过高的问题。
- Q: `segmentio/kafka-go` 和 `confluent-kafka-go` 有什么区别？
  - A: `segmentio/kafka-go` 是纯 Go 实现，无 CGO 依赖，交叉编译方便。`confluent-kafka-go` 基于 librdkafka（C 库），性能更好但需要 CGO，部署复杂。VidLens 选前者是因为纯 Go 更适合容器化部署。
- Q: 如果未来消息量增长 100 倍，当前架构能支撑吗？
  - A: 当前 4 个分区 + 单 Consumer 是瓶颈。扩展方案：① 增加分区数到 16/32；② 部署多个 Consumer 实例加入同一 Consumer Group；③ 每个 topic 独立一个 Consumer Group。Kafka 的分区机制天然支持这种水平扩展。
