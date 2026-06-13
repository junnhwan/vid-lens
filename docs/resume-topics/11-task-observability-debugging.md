# 专题 11：异步任务可观测性、TraceID 和排障路径

> 面试高频问题："用户说视频处理失败，你怎么定位？"
> 这类问题不是问你会不会看日志，而是看你能不能把 HTTP、Kafka、MySQL 状态、AI 调用和外部依赖串成一条可排查链路。

## 1. 先给总答案

推荐先这样答：

> 我会先从用户的 taskID 入手，看 `video_tasks` 的 status、stage、last_error_msg、retry_count 和 trace_id，再看 `task_jobs` 判断失败发生在 download、transcribe、analyze 还是 rag_index。然后顺着 traceID 和 taskID 查 Consumer 日志、AI 调用日志、转写分片状态和 Kafka topic。这个项目里用户可见状态落 MySQL，Kafka 只负责异步推进；排障时 MySQL 是业务事实来源，日志和 AI call log 是证据补充。

一句话：

> 先定位失败阶段，再定位外部依赖，再判断可重试还是不可重试。

## 2. 当前可观测数据有哪些

### 2.1 `video_tasks`

核心字段：

- `status`
- `stage`
- `trace_id`
- `retry_count`
- `max_retries`
- `next_retry_at`
- `last_error_code`
- `last_error_msg`
- `last_job_type`
- `stage_started_at`
- `stage_finished_at`

它回答：

```text
任务整体现在是什么状态？
卡在哪个阶段？
最近一次错误是什么？
还会不会重试？
什么时候重试？
```

### 2.2 `task_jobs`

按处理动作拆分：

- download
- transcribe
- analyze
- rag_index

它回答：

```text
是下载失败，还是转写失败？
RAG 索引失败是否影响已完成转写？
某个 job 重试了几次？
最后一次错误是什么？
```

### 2.3 `trace_id`

任务创建时生成 traceID，Kafka payload 会带上 traceID，Consumer 日志也会输出。

它回答：

```text
怎么把 HTTP 请求、Kafka 消息和 Consumer 日志串起来？
```

### 2.4 转写分片状态

```text
video_transcription_chunks
```

它回答：

```text
长视频切了几段？
哪一段 running / completed / failed？
已完成分片能不能复用？
```

### 2.5 AI 调用日志

```text
ai_call_logs
user_usage_daily
```

它回答：

```text
失败发生在 ASR、Embedding 还是 LLM？
provider 和 model 是什么？
耗时多少？
错误摘要是什么？
用户当天调用量大不大？
```

### 2.6 Consumer 日志

当前 Consumer 会记录：

- 收到任务。
- 下载开始/完成/失败。
- ASR 切片数量。
- 每个音频片段转写字符数。
- 最终转写字符数。
- RAG 索引开始/完成/失败。

它回答：

```text
数据库结果之外，任务执行过程发生了什么？
```

## 3. 标准排障路径

面试可以按这个顺序讲：

### 第一步：拿 taskID 查业务状态

看：

```text
video_tasks.status
video_tasks.stage
video_tasks.last_error_msg
video_tasks.retry_count
video_tasks.next_retry_at
video_tasks.trace_id
```

先判断：

- 任务是否还在跑。
- 是否失败。
- 是否等待重试。
- 是否 dead。

### 第二步：查 `task_jobs`

判断失败 job：

```text
download
transcribe
analyze
rag_index
```

例子：

```text
download failed: B站 412 / YouTube 网络不可达
transcribe failed: 第 N 段 ASR 失败
analyze failed: LLM 5xx 或配置缺失
rag_index failed: Embedding 维度不匹配 / Milvus 不可用
```

### 第三步：查 Consumer 日志

用 `trace_id` 或 `taskID` 搜：

```text
[Kafka] URL 下载开始
[Kafka] 音频切片转写已切片
[Kafka] 音频切片转写片段完成
[Kafka] RAG 索引任务失败
```

### 第四步：查外部依赖

按阶段定位：

- 下载失败：yt-dlp stderr、目标平台风控、cookies/proxy。
- 转写失败：ASR provider、音频大小、base64 限制、分片。
- 摘要失败：LLM provider、API Key、429/5xx。
- RAG 失败：Embedding 维度、Milvus 连接、collection。

### 第五步：判断恢复方式

用错误分类判断：

- timeout / 429 / 5xx / MinIO / Milvus 临时异常：退避重试。
- 配置缺失 / API Key 解密失败 / 文件不存在 / 维度不匹配：快速失败。

## 4. 高频追问

### Q1：用户说"处理失败"，你第一步看什么？

答：

> 先看 taskID 对应的 `video_tasks` 和 `task_jobs`，不要直接猜。`video_tasks` 给整体状态，`task_jobs` 能告诉我是下载、转写、分析还是 RAG 索引失败。

### Q2：怎么判断是 ASR 失败还是 LLM 失败？

答：

> 看 `task_jobs.job_type` 和 `stage`。如果是 transcribe 阶段失败，多半是 FFmpeg 或 ASR；如果是 analyze/summarizing 失败，多半是 LLM；如果是 indexing 失败，多半是 Embedding 或 Milvus。AI 调用日志还能看到 kind、provider、model 和错误摘要。

### Q3：长视频转写过短，怎么定位？

答：

> 先看转写分片日志和 `video_transcription_chunks`。重点看切了几段、每段返回多少字符、最终合并字符数。如果任务完成但转写明显过短，说明不能只看 status=completed，还要看 chunk 级指标。

### Q4：Kafka 消费失败后怎么查？

答：

> 看 Consumer 日志和 task job 状态。Kafka offset 只是消费进度，业务失败会写入 MySQL 的错误和重试时间。排障时以 DB 任务状态为主，Kafka lag 和日志为辅。

### Q5：traceID 有什么用？

答：

> traceID 用来串起一次任务从 HTTP 创建、Kafka 投递到 Consumer 处理的日志。如果没有 traceID，只靠 taskID 也能查，但跨组件关联会更麻烦。

### Q6：你有没有完整 Prometheus/OTel？

答：

> 当前项目还没有完整 Prometheus/OTel。现在主要靠 `video_tasks`、`task_jobs`、AI call log 和 Consumer 日志做业务可观测。生产上我会补指标系统，比如 Kafka lag、ASR 耗时、分片失败率、RAG 构建耗时和 provider 错误率。

### Q7：怎么判断任务是否还会自动恢复？

答：

> 看 `retry_count`、`max_retries`、`next_retry_at` 和 `last_error_code`。如果是 retryable error 并且没超过最大次数，会有下一次重试时间；如果是 non_retryable 或 retry_exhausted，就不会自动恢复。

### Q8：如果用户问为什么 RAG 问答答错了，怎么排查？

答：

> 先看 assistant message 的 retrieval snapshot。如果 citations 不相关，是检索问题；如果 citations 相关但答案错，是 prompt 或 LLM 生成问题。这个比只看最终回答更可解释。

## 5. 30 秒话术

> 异步任务排障我会先看 MySQL，不会先猜日志。`video_tasks` 看整体状态、stage、错误、重试次数和 traceID；`task_jobs` 看失败发生在 download、transcribe、analyze 还是 rag_index；再用 traceID/taskID 查 Consumer 日志、转写分片状态和 AI 调用日志。最后根据错误类型判断是退避重试还是快速失败。

## 6. 2 分钟话术

> 如果用户说视频处理失败，我第一步会拿 taskID 查 `video_tasks`，看 status、stage、last_error_msg、retry_count、next_retry_at 和 trace_id。这个表告诉我任务整体是否失败、是否还会重试。第二步查 `task_jobs`，因为同一个视频任务下有下载、转写、分析和 RAG 索引几个动作，job 表能定位失败阶段。
>
> 定位阶段后再查对应日志。下载失败看 yt-dlp 和平台风控；转写失败看 FFmpeg、ASR 分片状态和每段字符数；分析失败看 LLM 调用；RAG 失败看 Embedding 维度和 Milvus。AI 调用日志记录 provider、model、耗时和错误摘要，可以判断是 ASR、Embedding 还是 LLM 问题。
>
> 当前系统还不是完整 OTel，但已经有业务状态、job 状态、traceID、chunk 日志和 AI call log。生产上我会补 Prometheus 指标和告警。

## 7. 不要这么说

- 不要说只看日志就能定位。
- 不要说 Kafka offset 等于业务成功。
- 不要说已有完整链路追踪系统。
- 不要说 status=completed 就一定表示转写质量正确。
- 不要忽略 task_jobs 和 ai_call_logs 的排障价值。

## 8. 代码证据路径

```text
internal/model/task.go
internal/model/task_job.go
internal/model/transcription_chunk.go
internal/model/ai_call_log.go

internal/mq/trace.go
internal/mq/consumer.go
internal/mq/retry.go

internal/service/ai_observer.go
internal/ai/observed.go

internal/service/chat.go
```

