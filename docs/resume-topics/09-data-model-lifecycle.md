# 专题 9：数据库表设计、生命周期拆分和最终一致性

> 面试高频问题："你这个项目有哪些表？为什么这么拆？为什么不一张表存完？"
> 这类问题不是在考背表名，而是在看你是否理解视频资产、用户任务、异步子任务、RAG chunk、聊天记录和 AI 配置的生命周期差异。

## 1. 先给总答案

推荐先这样答：

> 我没有把视频所有信息都塞进一张 `video_tasks` 表，而是按生命周期拆分。真实视频文件用 `video_assets` 表示，用户的一次上传或处理用 `video_tasks` 表示，下载、转写、分析、RAG 索引用 `task_jobs` 表示子任务状态。转写、摘要、RAG chunk、聊天消息、AI 配置和 AI 调用审计都独立建表。这样做的目的不是为了表多，而是为了支持文件复用、异步任务状态、失败重试、RAG 检索、用户数据隔离和后续排障。

一句话：

> `video_assets` 管文件，`video_tasks` 管用户任务，`task_jobs` 管处理动作，RAG 和聊天数据按用途拆表。

## 2. 当前核心表怎么分工

### 2.1 用户和鉴权

```text
users
```

存用户账号、bcrypt 后的密码、昵称和角色。登录后 JWT 里带 `user_id`，后续所有用户资源都按 userID 校验。

### 2.2 文件资产和用户任务

```text
video_assets
video_tasks
```

`video_assets` 表示真实视频文件资产：

- `file_md5` 唯一。
- `object_name` 指向 MinIO 对象。
- 同一个真实视频只需要存一份。

`video_tasks` 表示某个用户的一次视频任务：

- 记录 `user_id`、`asset_id`、`status`、`stage`、`trace_id`。
- 多个 task 可以引用同一个 asset。
- 用户删除 task 不等于一定删除真实视频文件。

这两个表的拆分是面试重点：

> asset 是内容实体，task 是用户行为。相同视频可以复用 asset，但每个用户任务有自己的状态、转写、摘要和问答记录。

### 2.3 异步动作状态

```text
task_jobs
```

记录一个视频任务下的处理动作：

- `download`
- `transcribe`
- `analyze`
- `rag_index`

每个 job 有自己的：

- `status`
- `stage`
- `retry_count`
- `next_retry_at`
- `last_error_msg`
- `started_at`
- `finished_at`

它不是完整工作流引擎，而是当前项目里的轻量子任务状态表。

### 2.4 转写和摘要

```text
video_transcriptions
video_transcription_chunks
ai_summaries
```

转写全文和摘要都可能是大文本，所以不直接塞进 `video_tasks`。

`video_transcriptions` 存完整 ASR 文本。  
`video_transcription_chunks` 存 ASR 分片结果和状态，用于长视频分片转写、失败定位和结果复用。  
`ai_summaries` 存 LLM 摘要。

这样任务列表查询不需要带出大文本，避免列表接口被大字段拖慢。

### 2.5 RAG 索引

```text
video_chunks
video_rag_indexes
Milvus collection
```

`video_chunks` 存 chunk 元数据和文本：

- `user_id`
- `task_id`
- `chunk_index`
- `content`
- `content_hash`
- `embedding_model`
- `vector_id`

MySQL chunk 表支持：

- 展示引用片段。
- BM25 关键词召回。
- 删除任务时找 embedding model。

Milvus 存向量：

- 用于语义召回。
- 检索时按 `user_id + task_id + embedding_model` 过滤。

`video_rag_indexes` 记录某个用户、任务、embedding model 的索引状态：

- `not_indexed`
- `indexing`
- `indexed`
- `failed`

### 2.6 聊天和检索快照

```text
chat_sessions
chat_messages
```

`chat_sessions` 表示某个用户针对某个视频的会话。  
`chat_messages` 保存用户问题和 assistant 回答。

assistant 消息里保存 `retrieval_snapshot`，也就是当次问答用到的 citations。这样后续可以排查：

- 是检索片段不对。
- 还是 LLM 基于片段回答错了。

### 2.7 AI 配置和调用审计

```text
user_ai_profiles
ai_call_logs
user_usage_daily
```

`user_ai_profiles` 保存用户 BYOK 配置，ASR、LLM、Embedding 三类 provider 独立配置，API Key 加密存储。

`ai_call_logs` 记录每次 ASR / LLM / Embedding 调用的元数据：

- provider
- model
- status
- duration
- input/output chars
- error summary

`user_usage_daily` 做日级聚合，不是完整计费系统，但可以支撑排障和额度控制。

## 3. 为什么不能一张表存完

如果所有内容都塞进 `video_tasks`，会有几个问题：

- 相同视频无法优雅复用真实文件。
- 转写和摘要大文本污染任务列表查询。
- 下载、转写、分析、RAG 索引的状态互相覆盖。
- RAG chunk 无法独立检索、删除和重建。
- 聊天记录和检索快照没有清晰生命周期。
- 用户 AI 配置和调用审计无法复用。

推荐回答：

> 单表更适合 demo，但这个项目里的对象生命周期不同。真实文件、用户任务、异步动作、转写文本、向量索引、聊天消息和 AI 配置不是同一个生命周期，拆开后更容易做复用、幂等、删除和排障。

## 4. 高频追问

### Q1：`video_assets` 和 `video_tasks` 有什么区别？

答：

> `video_assets` 是真实视频文件，按 MD5 唯一，指向 MinIO 对象；`video_tasks` 是用户的一次上传或处理任务，记录用户、状态、阶段和结果关联。一个 asset 可以被多个 task 引用。

### Q2：为什么要有 `task_jobs`？`video_tasks.status` 不够吗？

答：

> `video_tasks.status` 表示用户可见的整体任务状态，但一个视频下面有下载、转写、分析、RAG 索引多个动作。RAG 索引失败不应该让用户误以为转写文本也没了。`task_jobs` 用来拆开每个动作的状态、重试次数和错误原因。

### Q3：为什么转写和摘要要分表？

答：

> 转写和摘要是大文本，任务列表只需要展示状态、文件名、大小和时间。如果大文本放在主表里，列表查询容易把无关大字段一起加载出来，影响性能和内存。分表后任务主表保持轻量。

### Q4：为什么 RAG chunk 既写 MySQL 又写 Milvus？

答：

> Milvus 负责向量检索，MySQL 负责 chunk 元数据、文本展示、BM25 关键词召回和删除清理。两者职责不同。向量库不适合承载所有业务元数据，MySQL 也不适合做高效向量搜索。

### Q5：MySQL 和 Milvus 怎么保证一致？

答：

> 当前不是分布式事务，而是最终一致。构建索引时先记录索引状态，删除旧向量，再写 MySQL chunk 和 Milvus 向量。如果失败，会把 `video_rag_indexes` 标记为 failed，后续可以重建。生产上可以进一步用 outbox 或补偿任务增强一致性。

### Q6：`task_jobs` 是完整工作流引擎吗？

答：

> 不是。它只是把当前固定的几个处理动作拆成独立状态行。没有 DAG 编排、历史 attempt 表、补偿事务和可视化工作流。当前项目阶段用轻量 job 表比引入工作流引擎更合适。

### Q7：为什么聊天消息要保存 retrieval snapshot？

答：

> RAG 问答出错时，需要判断是检索错了还是生成错了。保存 retrieval snapshot 后，可以回看当时给 LLM 的引用片段，而不是只看最终答案。

## 5. 30 秒话术

> 这个项目的表是按生命周期拆的。`video_assets` 表示真实视频文件，`video_tasks` 表示用户的一次任务，`task_jobs` 表示下载、转写、分析、RAG 索引这些异步动作。转写、摘要、RAG chunk、聊天记录、AI 配置和调用审计都单独建表。这样能支持相同视频复用、任务状态追踪、失败重试、RAG 检索和删除清理，而不是把所有东西塞进一张大表。

## 6. 2 分钟话术

> 我这个项目没有用一张视频表存所有东西，因为视频理解链路里的对象生命周期差异很大。真实文件是一个 asset，可以被多个任务复用；用户上传或处理是 task，有自己的状态、阶段和 traceID；下载、转写、摘要、RAG 索引又是 task 下的不同 job，各自会失败和重试。
>
> 转写和摘要是大文本，所以独立分表，避免任务列表查询加载大字段。RAG 部分又拆成 MySQL chunk 表和 Milvus 向量库：MySQL 保存 chunk 文本和元数据，用于 BM25、展示和删除；Milvus 保存向量，用于语义检索。聊天记录也独立保存，并且 assistant message 里有 retrieval snapshot，方便排查问答依据。
>
> 这个设计的代价是表多、链路复杂，但换来的是复用、幂等、失败恢复和可观测性。

## 7. 不要这么说

- 不要说 `task_jobs` 是完整工作流引擎。
- 不要说 MySQL 和 Milvus 有强事务一致性。
- 不要说删除任务时直接删除视频对象。
- 不要说单表不能做，只能说单表不适合当前生命周期和查询模式。

## 8. 代码证据路径

```text
internal/model/model.go
internal/model/asset.go
internal/model/task.go
internal/model/task_job.go
internal/model/transcription.go
internal/model/transcription_chunk.go
internal/model/summary.go
internal/model/video_chunk.go
internal/model/rag_index.go
internal/model/chat.go
internal/model/ai_profile.go
internal/model/ai_call_log.go
```

