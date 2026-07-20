# 专题 9：PostgreSQL 数据模型与生命周期边界

## 1. 面试口语答案

> 我没有把视频文件、用户任务、AI 结果和向量都塞进一张表。`video_assets` 表示可被多个任务复用的内容级对象，`video_tasks` 表示某个用户的一次业务任务；这样同 MD5 可以复用 MinIO 对象，但用户状态和权限仍相互独立。下载、转写、分析和索引再由 `task_jobs` 分开记录，避免一个 status 无法解释具体阶段。
>
> PostgreSQL 是唯一正式关系数据库。ASR 全文、分片结果、摘要、RAG chunk、索引状态、聊天、AI profile/调用记录、usage ledger 和 cleanup intent 都有各自 owner。上传中的临时片号保存在 Redis，pgvector 表只是 `video_chunks` 的可重建向量投影。
>
> 关键并发不变量尽量落到数据库：资产 MD5 唯一约束、task/job 唯一组合、upload chunk 的 `(session_id, chunk_index)` 唯一约束、行锁和 token/version CAS。应用层判断用于给出业务语义，唯一约束和条件更新才是并发下的最后防线。

## 2. 模型分组

| 分组 | 主要模型 | Owner / 作用 |
|---|---|---|
| 用户与配置 | `users`、`user_ai_profiles` | 用户身份和 BYOK 配置 |
| 资产与任务 | `video_assets`、`video_tasks`、`task_jobs` | 内容对象、用户任务、阶段状态 |
| 删除恢复 | `task_cleanup_jobs` | durable cleanup intent 与 lease |
| ASR/摘要 | `video_transcriptions`、`video_transcription_chunks`、`ai_summaries` | 模型输出与分片复用 |
| RAG | `video_chunks`、`video_rag_indexes`、pgvector projection | 文本事实、索引状态、向量投影 |
| 会话 | `chat_sessions`、`chat_messages` | 全量聊天与 retrieval snapshot |
| AI 治理 | call log、retry budget/attempt、usage ledger、compensation、daily usage | 调用审计与用量生命周期 |
| 上传临时进度 | Redis `upload:chunks:<md5>` | 已上传片号、规格与 24 小时 TTL |

`internal/model.AllModels()` 是 GORM 自动迁移模型的代码事实，不应在简历中长期写死“固定 N 张表”。

## 3. 关键生命周期

### 资产与任务

```text
文件内容 -> VideoAsset（MD5 unique）
                   -> 多个 VideoTask（按用户隔离）
```

删除一个 task 不代表立即删除 asset。cleanup job 在行锁下统计 active references，只有最后引用者才能取得 asset deletion ownership。

### RAG

```text
transcription -> video_chunks（事实）
              -> pgvector rows（可重建投影）
              -> video_rag_indexes（状态）
```

同库减少部署和跨库同步成本，但索引编排仍分阶段，不能称为天然全链路强一致。

### 上传

```text
Redis chunk state
  -> MinIO chunk objects
  -> merge lock + ComposeObject
  -> PostgreSQL asset + task
```

Redis 不保存上传事实，MinIO 只保存字节。

## 4. 高频追问

### 为什么 task 和 job 都需要？

> task 方便列表页和对外兼容，job 让不同动作拥有独立状态、错误和 lease。当前是渐进式拆分，不是完全去掉聚合状态。

### 为什么向量表不作为唯一事实？

> 向量依赖模型和维度，可以重新生成；文本、chunk identity 和权限 scope 才是稳定业务事实。审计和 reindex 都以关系 chunk 为源。

### 为什么使用软删除？

> 用户请求删除后需要先隐藏任务，但 MinIO 和向量清理无法与 PostgreSQL 做单一 ACID 事务。先保存 cleanup intent 再软删除，后台可重试外部清理。

## 5. 代码证据

- `internal/model/model.go`：模型入口。
- `internal/model/asset.go`、`task.go`、`task_job.go`：核心生命周期。
- `internal/service/media_chunk_upload.go`：Redis 临时分片状态与 MinIO 合并，不属于 PostgreSQL 数据模型。
- `internal/repository/asset.go`、`task_lease*.go`：行锁、唯一约束和 CAS。
- `internal/vector/pgvector.go`：向量投影 schema 与 scope。

## 6. 当前限制

- `video_tasks` 与 `task_jobs` 仍是兼容双层状态，需要继续防止语义漂移。
- Redis 上传状态带 TTL，丢失后未完成上传需要重新检查或重传；当前没有 durable upload session。
- GORM AutoMigrate 适合当前项目，但生产级复杂 schema 变更应采用版本化 migration。
