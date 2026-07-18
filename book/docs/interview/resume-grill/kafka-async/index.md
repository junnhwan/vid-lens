# Kafka 异步任务拷打

> 目标：不要只说“削峰填谷”。VidLens 里 Kafka 解决的是 HTTP 不该等待下载、ASR、摘要、RAG 索引这类长耗时动作的问题。

### 1. 为什么要用 Kafka 异步，而不是 HTTP 同步处理？

- 题目：这是 MQ 第一问。
- 面试官想听什么：长任务为什么不适合占用请求线程，以及异步后状态如何回写。
- 简答：视频下载、FFmpeg、ASR、摘要和 RAG 索引都可能持续很久，还依赖外部服务。同步 HTTP 容易超时，也很难给用户展示阶段性失败。Kafka 让接口快速返回 task id，consumer 后台处理并把状态写回 PostgreSQL，前端轮询任务状态。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的处理链路不是一个短请求。URL 下载可能受网络和平台风控影响，ASR 会受音频长度和请求体大小限制，RAG 索引还要调用 embedding 并发布 pgvector projection。如果这些都放在 HTTP 请求里，请求会长时间占用连接；一旦客户端断开或服务重启，任务状态也很难恢复。

  当前实现是先创建 `video_tasks`，再按动作投递 Kafka。比如 URL 上传接口创建 downloading 状态后投递 download topic；转写和摘要也分别进入对应 topic；ASR 完成后再投递独立的 RAG index topic。consumer 处理完成后更新任务状态、保存转写/摘要/RAG 状态。用户拿 task id 查询，而不是等一个长 HTTP 响应。
  </details>

- 延伸追问：
  - 如果 Kafka 挂了，接口怎么处理？
  - 为什么不用 goroutine 直接跑？
  - 前端怎么知道失败原因？
- 项目证据：
  - `internal/service/media.go:150` URL 下载接口创建异步任务。
  - `internal/service/media.go:178` URL 下载投递 Kafka。
  - `internal/mq/consumer.go:194` 启动 download consumer。
  - `internal/model/task.go:12` 定义 pending/queued/running/completed/failed/dead 状态。
- 当前边界：不要泛泛说“削峰填谷”，要落到 VidLens 的长视频处理和任务状态。

### 2. 为什么 Kafka 而不是 RocketMQ、RabbitMQ 或本地线程池？

- 题目：技术选型比较题。
- 面试官想听什么：你知道不同 MQ 的差异，也知道项目不是 Java 克隆。
- 简答：VidLens 是 Go 栈，Kafka 的 Go client 成熟，适合 durable task buffering 和 consumer group 扩展。RocketMQ 在 Java 业务消息、延迟消息、重试/DLQ 上很强，但本项目用 DB retry state 补足业务重试语义。RabbitMQ 更偏队列路由和任务队列，本项目更看重 Kafka topic、consumer group 和后续可扩展性。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会承认 RocketMQ 在 Java 业务消息场景里有优势，尤其是延迟消息、事务消息、重试和 DLQ 这些能力。但 VidLens 不是机械复制 Java 项目，它是 Go 后端项目，当前使用 `segmentio/kafka-go`，启动时创建 topics，producer 投递不同动作，consumer group 处理后台任务。

  本项目没有直接依赖 MQ 自带的业务重试，而是把重试状态落在 PostgreSQL：失败时记录 `retry_count`、`max_retries`、`next_retry_at`、`last_job_type`，RetryScheduler 扫描 due task 后重新投递。这样即使 Kafka 本身不提供 RocketMQ 那种业务延迟重试语义，任务恢复也能通过 DB 状态解释清楚。
  </details>

- 延伸追问：
  - Kafka 没有天然延迟消息怎么办？
  - RabbitMQ 更简单，为什么不用？
  - 以后要不要换 RocketMQ？
- 项目证据：
  - `cmd/server/main.go:110` 启动时创建 Kafka topics。
  - `internal/mq/producer.go:47` 创建 Kafka producer。
  - `internal/mq/retry.go:193` RetryScheduler 扫描 due retry tasks。
  - `internal/repository/task.go:176` 记录 retryable failure。
- 危险回答：不要说 RocketMQ 不好；只说 Go 栈下 Kafka 足够合适，重试语义由 DB 兜底。

### 3. 消息会不会丢？你怎么考虑投递可靠性？

- 题目：MQ 必考。
- 面试官想听什么：producer ack、DB 状态和失败返回的配合。
- 简答：producer 配置了 `RequiredAcks: kafka.RequireAll`，要求 ISR 副本确认。业务上，如果投递失败，接口或 consumer 会记录失败状态，而不是让用户以为任务已经在跑。RetryScheduler 重投时如果 Kafka enqueue 失败，会恢复 `failed + next_retry_at`，避免 claim 后任务丢在半路。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会分两层讲。Kafka 层面，producer 不是 fire-and-forget，而是配置了 RequireAll ack。业务层面，VidLens 不把“写了一条 Kafka 消息”当成唯一状态来源，任务状态仍在 PostgreSQL。比如 URL 下载创建任务后投递失败，接口会返回错误；RAG 索引投递失败，会记录 rag_index job 失败，并尝试写 `video_rag_indexes` failed。

  对 retry 来说更关键。RetryScheduler 先 claim task，把它从 failed 恢复到 queued/running，然后投递 Kafka。claim 会通过 `ClaimRetryDispatch` 在同一事务写入 task/job 的 token/version lease。如果投递失败，`RestoreRetryDispatch` 校验 token 后把两张表恢复成 failed 并重新设置 `next_retry_at`；如果进程崩溃，则由过期 lease 扫描接管。
  </details>

- 延伸追问：
  - DB 写成功但 Kafka 发失败怎么办？
  - Kafka 发成功但 consumer 处理失败怎么办？
  - 为什么需要 `next_retry_at`？
- 项目证据：
  - `internal/mq/producer.go:56` Kafka producer 使用 `RequireAll`。
  - `internal/mq/retry.go:202` retry 前 claim task。
  - `internal/mq/retry.go:211` enqueue retry 失败后计算下一次重试时间。
  - `internal/mq/retry.go:212` 恢复 failed 状态和 `next_retry_at`。
- 当前边界：这不是金融级 exactly-once，业务上按 at-least-once 加幂等处理。

### 4. 重复消费怎么处理？

- 题目：面试官看你是否理解 MQ 默认不是 exactly-once。
- 面试官想听什么：消费者幂等、任务状态、资产复用和唯一约束。
- 简答：Kafka consumer 要按可能重复消费设计。VidLens 的思路是用任务状态、MD5 asset 复用、分布式锁和数据库记录兜底。比如相同 MD5 的视频会复用 `video_assets`，分片合并有 Redis lock，RAG 重建前按 task/model 删除旧向量再写新向量。
- 深答：

  <details>
  <summary>展开深答</summary>

  我不会说 Kafka 帮我保证业务 exactly-once。VidLens 的 consumer 处理动作需要自己做幂等。上传链路按 MD5 查找 `video_assets`，已有资产时直接创建 task 复用；分片 merge 前先查 asset，再拿 Redis lock，拿不到说明其他请求在合并；RAG 重建时先事务性替换 PostgreSQL `video_chunks` source，再在独立事务中原子替换同一 `user_id/task_id/embedding_model` 下的 pgvector projection；两阶段之间不假装强一致，失败由 RAG 状态、审计和重建恢复。

  这些做法的共同点是：消息重复了也尽量不重复生成底层资源，或者用覆盖式/可重建的方式保证最终状态可解释。真正高等级的 exactly-once 需要事务 outbox、幂等表或更严格的状态机，当前项目先做到简历项目可防守的业务幂等。
  </details>

- 延伸追问：
  - Kafka offset 在什么时候提交？
  - ASR 重复跑会不会重复扣费？
  - RAG 重建为什么要先删旧向量？
- 项目证据：
  - `internal/service/media.go:105` 上传时按 MD5 查找已有 asset。
  - `internal/service/media.go:618` 分片合并使用 Redis lock。
  - `internal/service/rag_index.go:137` RAG 构建前删除旧向量。
  - `docs/troubleshooting-and-interview-notes.md:3004` 记录 RAG 重建清理旧向量问题。
- 当前边界：ASR/LLM 这类外部调用如果重复触发仍可能产生成本，后续可以加强幂等缓存和任务状态门禁。

### 5. 为什么需要 `video_tasks` 和 `task_jobs` 两层状态？

- 题目：状态建模题。
- 面试官想听什么：主任务状态和子动作状态的区别。
- 简答：`video_tasks` 是兼容主状态，适合列表和整体进度；`task_jobs` 记录 download/transcribe/analyze/rag_index 这些子动作状态。因为一个视频可能 ASR 成功但 RAG 索引失败，只用主状态会让用户误解成整个任务失败。
- 深答：

  <details>
  <summary>展开深答</summary>

  旧问题是 `video_tasks.status` 混合了多个语义：下载、转写、摘要、RAG 索引都往同一个字段写。后来 RAG 从 ASR 完成路径里拆出来后，这个问题更明显：ASR 转写文本已经存在，但 RAG indexing 失败，用户仍然应该能看转写文本。只把主任务写成 failed/indexing 会让前端和面试解释都很别扭。

  所以后来补了 `task_jobs`。它按 `task_id + job_type` 记录某个动作的 queued/running/completed/failed、stage、retry_count、next_retry_at 等。这样主任务可以继续作为兼容入口，子任务状态可以表达更细的失败边界。
  </details>

- 延伸追问：
  - 为什么不一开始就设计 job 表？
  - `task_jobs` 保留历史 attempt 吗？
  - RAG 失败后主任务应该怎么展示？
- 项目证据：
  - `internal/model/task.go:40` 定义 `VideoTask`。
  - `internal/model/task_job.go:15` 定义 `TaskJob`。
  - `internal/model/task_job.go:6` 定义 analyze/transcribe/download/rag_index job 类型。
  - `docs/troubleshooting-and-interview-notes.md:3898` 说明 task_jobs 解决混合语义问题。
- 当前边界：当前 `task_jobs` 主要保留当前子任务状态，不是完整 attempt history。

### 6. RetryScheduler 怎么设计？哪些错误不重试？

- 题目：可靠性追问。
- 面试官想听什么：不是所有失败都适合重试。
- 简答：失败会先分类。网络、timeout、429、5xx、MinIO、PostgreSQL/pgvector 暂时不可用等临时错误可以重试；缺少 AI 配置、API key 解密失败、无权、文件不存在、embedding 维度错误、视频不可用等属于非重试。重试状态写入 DB，由 RetryScheduler 按 `next_retry_at` 扫描。
- 深答：

  <details>
  <summary>展开深答</summary>

  重试不是“失败就无限再来”。VidLens 的 `isRetryableError` 明确区分 retryable 和 non-retryable。比如用户没配 AI 服务，重试不会自动好；embedding 维度跟 pgvector schema 不一致，重试也不会好；视频被平台删除或 412 风控，也可能不是立刻可恢复。相反，网络 timeout、connection reset、429、5xx、MinIO/PostgreSQL 暂时不可用，才适合退避重试。

  失败时会更新 `retry_count`、`max_retries`、`next_retry_at` 和 `last_job_type`。RetryScheduler 扫描 due tasks，claim 后按 job type 重新投递对应 topic。超过最大重试后写 dead，避免任务一直占用调度。
  </details>

- 延伸追问：
  - 为什么不要无限重试？
  - `dead` 状态给谁处理？
  - 退避时间怎么定？
- 项目证据：
  - `internal/mq/retry.go:56` non-retryable marker 列表。
  - `internal/mq/retry.go:72` retryable marker 列表。
  - `internal/mq/retry.go:133` 计算 `nextRetryAt`。
  - `internal/model/task.go:17` 定义 `TaskStatusDead`。
- 当前边界：当前是基于错误文本 marker 的第一版分类，未来可以引入结构化错误码。

### 7. RAG 索引为什么拆成独立 Kafka job？

- 题目：面试官看你是否理解失败边界。
- 面试官想听什么：ASR 和 RAG 的成功条件不同。
- 简答：ASR 成功后用户应该能看到转写文本；Embedding 或 pgvector projection 发布失败只影响问答索引，不应该把转写任务也拖失败。拆成独立 RAG job 后，transcribe consumer 保存转写并投递 `video-rag-index`，RAG consumer 独立构建索引并支持重试。
- 深答：

  <details>
  <summary>展开深答</summary>

  旧流程把 RAG indexer 放在 transcribe consumer 后面同步执行。这样看起来链路简单，但实际会让 embedding 和第一版 Milvus 写入占用转写 worker；迁移到 pgvector 后，projection 发布仍然是独立的失败边界。如果向量后端短暂不可用，转写文本明明已经保存，用户不应该看到整个转写任务失败。

  现在的边界更清楚：`handleTranscribe` 完成 ASR 和转写保存后调用 `indexAfterTranscription`，先写 rag_index queued job，再投递 RAGIndexPayload。`handleRAGIndex` 独立检查 task、transcription 和 indexer，然后调用 `BuildTaskIndex`。失败后按 rag_index job 进入现有 DB retry scheduler。
  </details>

- 延伸追问：
  - RAG enqueue 失败怎么办？
  - RAG 失败后转写文本还在吗？
  - 为什么不做独立 RAG 微服务？
- 项目证据：
  - `internal/mq/producer.go:24` 定义 `RAGIndexPayload`。
  - `internal/mq/consumer.go:338` `handleRAGIndex` 独立处理 RAG topic。
  - `internal/mq/consumer.go:735` ASR 后写 RAG queued job。
  - `internal/mq/consumer.go:740` ASR 后投递 RAG index 消息。
  - `docs/troubleshooting-and-interview-notes.md:3121` 记录拆分背景。
- 当前边界：当前不是独立 RAG 微服务，而是 Go 单体内独立 Kafka consumer。

### 8. 消费者积压怎么排查？

- 题目：系统运行题。
- 面试官想听什么：先定位瓶颈，不是直接加机器。
- 简答：先看积压来自生产突增、consumer bug、外部 AI 慢、FFmpeg/yt-dlp 慢、embedding/pgvector 慢还是 DB 写入慢。VidLens 的日志和状态里有 task id、trace id、stage、chunk 数、错误信息，可以按 topic 和 job type 定位。
- 深答：

  <details>
  <summary>展开深答</summary>

  我不会一上来就说“扩容 consumer”。先要看哪个 topic 积压：download、transcribe、analyze 还是 rag_index。download 慢可能是平台网络、代理、yt-dlp 或 720p 下载；transcribe 慢可能是 FFmpeg 和 ASR provider；rag_index 慢可能是 embedding provider 或 pgvector projection 写入；analyze 慢可能是 LLM。

  然后看 DB 里的 `status/stage/last_job_type/last_error_msg` 和日志里的 trace id。长视频 ASR 复盘里已经补过 chunk 级日志，因为只看最终 completed/failed 不够。确认瓶颈后再决定是加 consumer、增加 partition、降级某些功能、提高 provider timeout，还是先修 bug。
  </details>

- 延伸追问：
  - 加 consumer 一定有用吗？
  - 如果是 ASR provider 慢怎么办？
  - 怎么避免用户一直看到处理中？
- 项目证据：
  - `cmd/server/main.go:217` 启动 analyze/transcribe/download/rag_index consumers。
  - `internal/model/task.go:58` `last_job_type` 字段帮助定位子动作。
  - `docs/troubleshooting-and-interview-notes.md:336` 记录 chunk 级 ASR 日志格式。
  - `docs/troubleshooting-and-interview-notes.md:398` 记录 trace id 贯穿任务的改进方向。
- 当前边界：当前没有 Prometheus 指标体系，主要依赖 DB 状态和日志。

