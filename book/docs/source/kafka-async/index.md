# Kafka 异步处理 -- 源码走读

> 基于 VidLens 项目 `internal/mq/` 包的真实源码，覆盖 Producer、Consumer、重试系统、TraceID 传播四个核心模块。

<div class="diagram-container">

![Kafka 异步处理流程](/diagrams/kafka-async.svg)

</div>

---

## 文件总览

| 文件 | 行数 | 职责 |
|------|------|------|
| `internal/mq/producer.go` | 185 | Kafka 生产者：封装 4 个 topic 的消息投递 |
| `internal/mq/consumer.go` | 852 | Kafka 消费者：4 个独立 goroutine 消费 + 六步处理流程 |
| `internal/mq/retry.go` | 252 | 重试系统：失败分类、退避策略、定时扫描重投 |
| `internal/mq/trace.go` | 21 | TraceID 传播：context 注入/提取，全链路追踪 |
| `internal/pkg/ffmpeg/ffmpeg.go` | 119 | FFmpeg 封装：音频提取、分片、时长获取 |
| `internal/pkg/lock/redis_lock.go` | 141 | 分布式锁：Redis SetNX + UUID owner + WatchDog |
| `internal/model/task.go` | 78 | 任务模型：6 种状态 + 6 种阶段的状态机 |
| `internal/model/task_job.go` | 37 | 子任务模型：download/transcribe/analyze/rag_index 四种作业类型 |
| `internal/model/transcription_chunk.go` | 31 | 转写分片模型：记录每片音频的转写状态 |

---

## 核心结构体

### Producer

```go
// producer.go:12-27 -- 消息载荷定义
type AnalyzePayload struct {
    TaskID  int64  `json:"task_id"`
    MD5     string `json:"md5"`
    TraceID string `json:"trace_id"`
}
type DownloadPayload struct {
    TaskID  int64  `json:"task_id"`
    Key     string `json:"key"`
    TraceID string `json:"trace_id"`
}
type RAGIndexPayload struct {
    TaskID  int64  `json:"task_id"`
    TraceID string `json:"trace_id"`
}

// producer.go:39-44 -- 生产者实例
type Producer struct {
    analyzeWriter    *kafka.Writer  // video-analyze topic
    transcribeWriter *kafka.Writer  // video-transcribe topic
    downloadWriter   *kafka.Writer  // video-download topic
    ragIndexWriter   *kafka.Writer  // video-rag-index topic
}
```

**设计要点**: 每个 topic 一个独立的 `kafka.Writer`，配置完全相同（`RequiredAcks=All`, `Async=false`, `MaxAttempts=3`），通过 `newWriter` 闭包统一创建。

### Consumer

```go
// consumer.go:44-63 -- 消费者实例
type Consumer struct {
    repo        *repository.Repositories   // 数据访问层
    storage     *storage.MinIOStorage      // 对象存储
    ai          ai.Strategy                // AI 策略（ASR + LLM）
    aiFactory   *ai.Factory                // AI 工厂（按用户 Profile 创建策略）
    aiRecorder  ai.CallRecorder            // AI 调用日志记录器
    profiles    profileResolver            // 用户 AI Profile 解析器
    rdb         redis.Cmdable              // Redis 客户端（分布式锁）
    ffmpegPath  string                     // FFmpeg 可执行文件路径
    ytdlpPath   string                     // yt-dlp 路径（URL 下载）
    cookiesPath string                     // yt-dlp cookies 文件
    proxyURL    string                     // 代理 URL
    splitAudio  splitAudioFunc             // 音频分片函数（可 mock）
    ragIndex    ragIndexFunc               // RAG 索引函数（可 mock）
    ragProducer ragIndexProducer           // RAG 索引消息投递器
    retryPolicy TaskRetryPolicy            // 重试策略
    downloadVideo   downloadVideoFunc      // 视频下载函数（可 mock）
    uploadLocalFile uploadLocalFileFunc    // 文件上传函数（可 mock）
}
```

**设计要点**: Consumer 通过函数类型字段（`splitAudio`、`downloadVideo` 等）实现依赖注入，测试时可替换为 mock 函数。

### 重试系统

```go
// retry.go:20-24 -- 重试策略
type TaskRetryPolicy struct {
    MaxRetries     int           // 最大重试次数，默认 3
    BackoffSeconds []int         // 退避间隔，默认 [60, 300, 900]
    Now            func() time.Time  // 时间函数（可 mock 测试）
}

// retry.go:147-153 -- 重试调度器配置
type RetrySchedulerConfig struct {
    BatchSize              int           // 每次扫描任务数，默认 20
    Interval               time.Duration // 扫描间隔，默认 30s
    DispatchFailureBackoff time.Duration // 投递失败后退避，默认 1min
    Now                    func() time.Time
}

// retry.go:154-158 -- 重试调度器
type RetryScheduler struct {
    repos    *repository.Repositories
    producer retryProducer
    config   RetrySchedulerConfig
}
```

### TraceID 传播

```go
// trace.go:5 -- context key（结构体类型避免冲突）
type traceIDContextKey struct{}

// trace.go:7-12 -- 注入 TraceID
func ContextWithTraceID(ctx context.Context, traceID string) context.Context

// trace.go:14-19 -- 提取 TraceID
func TraceIDFromContext(ctx context.Context) string
```

---

## 4 个 Topic 与对应 Payload

| Topic | Payload 结构体 | Key | 用途 |
|-------|---------------|-----|------|
| `video-analyze` | `AnalyzePayload{TaskID, MD5, TraceID}` | MD5 | 视频分析（ASR + LLM 摘要） |
| `video-transcribe` | `AnalyzePayload{TaskID, MD5, TraceID}` | MD5 | 纯转写（只 ASR 不摘要） |
| `video-download` | `DownloadPayload{TaskID, Key, TraceID}` | Key | URL 视频下载 |
| `video-rag-index` | `RAGIndexPayload{TaskID, TraceID}` | taskID | RAG 向量索引构建 |

**Key 路由规则**: `video-analyze` 和 `video-transcribe` 用 MD5 作为 Key，保证同一视频进入同一分区。`video-rag-index` 用 taskID 作为 Key，因为此时视频已去重完成。

---

## 关键函数走读

### 1. Producer.EnqueueAnalyze -- 消息投递入口

```
producer.go:77-88
```

**调用链**:
```
Service.UploadFile() / Service.SubmitURL()
  → Producer.EnqueueAnalyze(ctx, taskID, md5)
    → json.Marshal(AnalyzePayload{TaskID, MD5, TraceID})
    → TraceIDFromContext(ctx)       // 从 context 提取 traceID
    → kafka.Writer.WriteMessages()  // 同步写入 Kafka
```

**关键逻辑**:
- `TraceIDFromContext(ctx)` 将 HTTP 请求中的 TraceID 传播到 Kafka 消息
- `Key: []byte(md5)` 通过 MD5 哈希路由到固定分区
- `WriteMessages` 同步阻塞，等待 Kafka 所有 ISR 副本确认

### 2. Consumer.StartAnalyzeConsumer -- 消费者启动

```
consumer.go:127-162
```

**调用链**:
```
main() / app.Start()
  → Consumer.StartAnalyzeConsumer(brokers, topic, groupID)
    → kafka.NewReader(ReaderConfig{CommitInterval: 0})  // 手动提交
    → go func() {                                       // 独立 goroutine
        for {
          msg, err := r.ReadMessage(ctx)               // 阻塞拉取
          if err := c.handleAnalyze(ctx, msg); err != nil {
            // 失败：不 commit offset，等 Kafka 重投
          } else {
            r.CommitMessages(ctx, msg)                  // 成功：手动 commit
          }
        }
      }()
```

**关键配置**:
- `CommitInterval: 0` -- 禁用自动提交，手动控制 offset
- `MinBytes: 1e3, MaxBytes: 1e6` -- 控制每次拉取的数据量（1KB ~ 1MB）
- 每个 topic 一个独立 goroutine，互不阻塞

### 3. Consumer.handleAnalyze -- 六步处理流程

```
consumer.go:393-461
```

**调用链**:
```
handleAnalyze(ctx, msg)
  ├─ 1. json.Unmarshal(msg.Value) → AnalyzePayload
  ├─ 2. lock.NewRedisLock(rdb, "vidlens:lock:{md5}")
  │    └─ TryLock(ctx, 5s) → SetNX + WatchDog
  ├─ 3. repo.Task.FindByID(taskID)
  │    └─ if Status == Completed && Summary != nil → return nil (幂等跳过)
  ├─ 4. repo.Task.UpdateStatusAndStageIf(taskID, [Queued,Failed], Running)
  │    └─ CAS 操作，抢占任务所有权
  ├─ 5. processVideo(ctx, task)
  │    ├─ 复用检查: Transcription.FindByTaskID() → 有则跳过 ASR
  │    ├─ FFmpeg: ExtractAudio() → SplitAudio(300s)
  │    ├─ ASR: transcribeAudio() → 逐片转写 + 断点续传
  │    └─ LLM: summarizeTask() → AI.Summarize()
  └─ 6. repo.Task.UpdateStatusAndStage(taskID, Completed, None)
```

### 4. Consumer.transcribeAudio -- 长音频分片转写

```
consumer.go:579-624
```

**调用链**:
```
transcribeAudio(ctx, taskID, audioPath, strategy)
  ├─ ffmpeg.SplitAudio(ctx, ffmpegPath, audioPath, 300)
  │    └─ FFmpeg -f segment -segment_time 300 → chunk_000.mp3, chunk_001.mp3, ...
  ├─ for i, chunk := range chunks {
  │    ├─ completedTranscriptionChunk(taskID, i)    // 断点续传检查
  │    │    └─ repo.TranscriptionChunk.FindByTaskAndIndex()
  │    ├─ markTranscriptionChunkRunning(taskID, i)  // 标记处理中
  │    │    └─ repo.TranscriptionChunk.UpsertRunning()
  │    ├─ strategy.Transcribe(ctx, chunk)            // ASR 调用
  │    └─ markTranscriptionChunkCompleted(taskID, i) // 保存结果
  │         └─ repo.TranscriptionChunk.UpsertCompleted()
  └─ strings.Join(parts, "\n\n")                    // 合并所有片
```

**设计亮点**:
- 每片独立保存到 `video_transcription_chunks` 表，支持断点续传
- `completedTranscriptionChunk` 检查已完成的片，避免重复转写
- 失败的片通过 `markTranscriptionChunkFailed` 记录错误信息

### 5. RetryScheduler.RunOnce -- 重试扫描

```
retry.go:193-218
```

**调用链**:
```
RetryScheduler.Start(ctx)
  └─ go func() {
       ticker := time.NewTicker(30s)
       for {
         RunOnce(ctx)      // 每 30s 执行一次
         <-ticker.C
       }
     }()

RunOnce(ctx)
  ├─ repos.Task.FindDueRetryTasks(now, batchSize=20)
  │    └─ SELECT * FROM video_tasks WHERE next_retry_at <= now AND status IN (4,5)
  ├─ for _, task := range tasks {
  │    ├─ repos.ClaimRetryDispatch(task/job, expectedVersion, token, leaseUntil)
  │    │    └─ 一个 PostgreSQL transaction 同步写 task + task_job dispatch lease
  │    └─ enqueueRetry(contextWithClaimToken(ctx, token), task)
  │         ├─ TaskJobDownload   → producer.EnqueueDownload()
  │         ├─ TaskJobTranscribe → producer.EnqueueTranscribe()
  │         ├─ TaskJobAnalyze    → producer.EnqueueAnalyze()
  │         └─ TaskJobRAGIndex   → producer.EnqueueRAGIndex()
  └─ producer 失败: repos.RestoreRetryDispatch(token, nextRetryAt)
       └─ token CAS 后事务性恢复 task + task_job；进程崩溃则由过期 lease 扫描恢复
```

### 6. Consumer.recordTaskFailure -- 失败记录与重试决策

```
retry.go:103-138
```

**调用链**:
```
recordTaskFailure(taskID, jobType, stage, err)
  ├─ isRetryableError(err)
  │    ├─ 黑名单检查: "video unavailable", "api key 解密失败", ...
  │    └─ 白名单检查: "timeout", "connection refused", "HTTP 503", ...
  ├─ if 不可重试:
  │    └─ RecordTerminalFailure(taskID, ..., "non_retryable_error", Failed)
  ├─ if 可重试 && retryCount > maxRetries:
  │    └─ RecordTerminalFailure(taskID, ..., "retry_exhausted", Dead)
  └─ if 可重试 && retryCount <= maxRetries:
       └─ nextRetryAt = now + backoffForRetry(retryCount+1)
            └─ RecordRetryableFailure(taskID, ..., nextRetryAt)
```

**退避计算** (`retry.go:39-49`):
```
retryCount=1 → BackoffSeconds[0] = 60s   (1 分钟后重试)
retryCount=2 → BackoffSeconds[1] = 300s  (5 分钟后重试)
retryCount=3 → BackoffSeconds[2] = 900s  (15 分钟后重试)
retryCount>3 → 超过最大次数，标记为 Dead
```

---

## 调用链全景图

```
用户上传视频 / 提交 URL
  │
  ├─ HTTP Handler
  │    ├─ MediaService.UploadFile()
  │    │    ├─ MD5 去重 → MinIO 上传 → 创建 VideoTask (Status=Queued)
  │    │    └─ Producer.EnqueueAnalyze(ctx, taskID, md5)  ──┐
  │    └─ MediaService.SubmitURL()                          │
  │         ├─ 创建 VideoTask (Status=Running, Stage=Downloading)
  │         └─ Producer.EnqueueDownload(ctx, taskID, key)  │
  │                                                        │
  ▼                                                        ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Kafka Broker                             │
│  video-analyze    video-transcribe   video-download   video-rag │
│  (4 partitions)   (4 partitions)    (4 partitions)   (4 parts) │
└─────────────────────────────────────────────────────────────────┘
        │                │                 │                │
        ▼                ▼                 ▼                ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│  Consumer    │ │  Consumer    │ │  Consumer    │ │  Consumer    │
│  Goroutine   │ │  Goroutine   │ │  Goroutine   │ │  Goroutine   │
│  [analyze]   │ │  [transcribe]│ │  [download]  │ │  [rag_index] │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
       │                │                 │                │
       ▼                ▼                 ▼                ▼
  handleAnalyze()  handleTranscribe() handleDownload() handleRAGIndex()
       │                │                 │                │
       ├─ 分布式锁       ├─ 更新状态        ├─ yt-dlp 下载    ├─ 查询转录
       ├─ 幂等校验       ├─ FFmpeg 音频     ├─ MD5 去重       ├─ 文本分块
       ├─ 更新状态       ├─ ASR 转写        ├─ MinIO 上传     ├─ Embedding
       ├─ processVideo  ├─ 保存转录        ├─ 更新 Asset     ├─ pgvector projection
       │  ├─ 复用转录    ├─ 投递 rag-index  └─ 完成           └─ 完成
       │  ├─ FFmpeg     └─ 完成
       │  ├─ ASR
       │  └─ LLM 摘要
       └─ 完成
```

---

## 设计决策分析

### 决策 1: 为什么每个 topic 一个独立 goroutine 而不是一个 goroutine 轮询所有 topic？

**选择**: 4 个独立 goroutine，每个绑定一个 topic。

**原因**:
1. **隔离性**: 一个 topic 的消费阻塞（如 ASR 耗时 10 分钟）不影响其他 topic
2. **独立 offset**: 每个 `kafka.Reader` 独立管理 offset，互不干扰
3. **故障隔离**: 一个 goroutine panic 不影响其他三个

**权衡**: goroutine 数量增加，但每个 goroutine 只做一件事，逻辑清晰。

### 决策 2: 为什么用函数类型字段（`splitAudio`、`downloadVideo`）而不是接口？

**选择**: 函数类型 + 可选覆盖，而非接口注入。

```go
// consumer.go:30-36
type splitAudioFunc func(ctx context.Context, ffmpegPath, inputPath string, segmentSeconds int) ([]string, error)
type ragIndexFunc func(ctx context.Context, task *model.VideoTask) error
type downloadVideoFunc func(ctx context.Context, sourceURL string) (string, error)
```

**原因**:
1. **简单性**: 只有一个方法的接口用函数类型更简洁
2. **可测试性**: 测试时直接赋值 mock 函数，不需要定义 mock 结构体
3. **可选性**: 字段为 nil 时走默认实现（如 `splitAudio == nil` 则用 `ffmpeg.SplitAudio`）

**权衡**: 多方法的接口（如 `ai.Strategy`）仍用传统接口，函数类型只用于单方法场景。

### 决策 3: 为什么重试系统用"阶梯退避"而不是"指数退避"？

**选择**: 固定阶梯 `[60, 300, 900]`，而非 `2^n * base`。

**原因**:
1. **可预测性**: 运维可以精确知道每次重试的等待时间
2. **可控性**: 不会因为指数增长导致任务"饿死"（如 2^10 = 1024 秒）
3. **业务适配**: 60s/300s/900s 的阶梯覆盖了大部分临时故障的恢复时间

**权衡**: 灵活性不如指数退避，但对 VidLens 的场景足够。

### 决策 4: 为什么 `handleAnalyze` 中业务失败返回 `nil` 而不是 `error`？

**选择**: 业务失败（ASR 超时、LLM 报错）返回 `nil` 并 commit offset，基础设施失败（消息解析、DB 查询）返回 `error` 不 commit。

**原因**:
1. **退避精度**: Kafka 重投是秒级的，不适合 ASR 超时等需要分钟级退避的场景
2. **状态持久化**: 业务失败通过 `recordTaskFailure` 写入 DB，`RetryScheduler` 按精确的退避策略重投
3. **offset 安全**: commit offset 后消息不会被 Kafka 重复投递，避免与 RetryScheduler 的重投产生冲突

**权衡**: 增加了 `recordTaskFailure` 和 `RetryScheduler` 的复杂度，但获得了精确的重试控制。

### 决策 5: 为什么 `VideoTranscriptionChunk` 要独立建表？

**选择**: 每片转写结果持久化到 `video_transcription_chunks` 表。

**原因**:
1. **断点续传**: 任务重试时复用已完成的片段，避免重复转写
2. **可观测性**: 可以查询每片的状态、耗时、字符数，便于排查问题
3. **成本控制**: ASR API 按调用次数计费，复用已完成的片段直接省钱

**权衡**: 增加了 DB 写入次数（每片一次），但对 PostgreSQL 来说微秒级写入可以忽略。

---

## 状态机流转图

```
VideoTask 状态机:

  Pending (0) ──→ Queued (1) ──→ Running (2) ──→ Completed (3)
       │              │              │                │
       │              │              ▼                │
       │              │          Failed (4) ◄─────────┘ (摘要失败重试)
       │              │              │
       │              │              ▼
       │              │          Dead (5) (超过最大重试次数)
       │              │
       └──────────────┘ (重新投递)

VideoTask 阶段流转:

  none → downloading → uploaded → transcribing → summarizing → indexing → none
                            ↑                        │
                            └────────────────────────┘ (转写复用跳过)
```

```
TaskJob 状态机:

  Pending (0) ──→ Dispatching ──→ Running (2) ──→ Completed (3)
                       │              │
                       │              ▼
                       │          Failed (4) / Dead (5)
                       │
                       └──→ (RetryScheduler 认领后重新投递)
```

```
VideoTranscriptionChunk 状态机:

  pending ──→ running ──→ completed
                 │
                 ▼
              failed (可重试该片)
```
