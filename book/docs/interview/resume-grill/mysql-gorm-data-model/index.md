# PostgreSQL/GORM 数据模型拷打

> 路径名 `mysql-gorm-data-model` 为兼容旧链接保留。当前正式关系数据库是 PostgreSQL；MySQL 只在“第一版 JSON bug”和迁移历史中出现。

## 1. 为什么任务表不能什么都塞进去？

`video_tasks` 服务于列表、状态轮询和任务生命周期，不适合塞入大段转写、摘要、聊天或调用日志。大字段和不同更新频率的数据拆到 transcription、summary、chat、usage 等表，可以让主查询更清晰，也避免一个模型承载互不相关的职责。

这不是为了表多，而是按访问路径和生命周期拆分。证据在 `internal/model/`。

## 2. `status` 和 `stage` 为什么同时存在？

`status` 表示总体结果，例如 queued、running、completed、failed、dead；`stage` 表示当前或失败发生在哪一步，例如 downloading、transcribing、summarizing、indexing。

只有 status=failed 时，用户和维护者无法判断应查看 yt-dlp、FFmpeg、AI provider 还是 RAG projection。stage 提供排障上下文，但不替代 `task_jobs` 的具体作业记录。

**证据：** `internal/model/task.go`。

## 3. 为什么增加 `task_jobs`？

同一个视频任务包含 download、transcribe、analyze、rag_index 等动作，它们有独立状态、重试次数、next retry time 和 lease。若全部塞进 `video_tasks`，每加一个动作都要扩充主表字段和条件分支。

`video_tasks` 保留兼容的用户视图，`task_jobs` 保存 action 级执行状态。当前还不是任意 DAG 或 workflow engine。

**证据：** `internal/model/task_job.go`、`internal/repository/task_lease_*.go`。

## 4. `video_assets` 和 `video_tasks` 为什么分开？

asset 表示可复用的媒体对象和内容身份；task 表示某个用户对该对象发起的一次处理。相同内容可以复用 MinIO object，避免重复上传和重复 AI 成本，但不同用户任务、状态和权限不能合并成一条记录。

删除 task 时也不能直接删除共享 object。当前 cleanup job 会先争取 shared asset deletion ownership，只有没有其他活跃引用的 owner 才删除外部对象。

**证据：** `internal/model/asset.go`、`internal/service/task_cleanup.go`、`internal/repository/asset.go`。

## 5. GORM soft delete 对资源清理有什么影响？

GORM 默认查询会过滤 `deleted_at`，但 soft delete 只影响关系数据可见性，不会自动删除 MinIO、Redis 或向量 projection。若请求事务直接删除外部资源，任一步失败都可能留下半清理状态。

当前删除流程先在 PostgreSQL 事务中锁定 task、创建 durable cleanup intent 并 soft-delete task；scheduler 通过 token/lease 认领 job，幂等清理外部资源，失败保存 error 与 next retry time，最终再完成关系数据收尾。

这不是通用 Saga engine，而是针对 task 资源生命周期的 durable intent。

**证据：** `internal/service/task_cleanup*.go`、`internal/repository/task_cleanup_job.go`、`internal/model/task_cleanup_job.go`。

## 6. 第一版 MySQL JSON 空字符串 bug 怎么讲？

第一版聊天表在 MySQL 使用 JSON 字段保存 retrieval snapshot。用户消息没有 citations，却因 Go string 零值写入裸空字符串；MySQL JSON 不接受它，因此插入失败。修复是把模型改成可空指针：无快照写 NULL，有快照先 `json.Marshal` 后保存合法数组。

迁移到 PostgreSQL 后，这段仍是有效的历史 Debug 经历，但不能说成当前 PostgreSQL 故障。它说明“无值”“空字符串”和“合法 JSON 空数组”是不同语义。

**证据：** `internal/model/chat.go`、`internal/service/chat_messages.go`、`docs/troubleshooting-and-interview-notes.md`。

## 7. 为什么同一 PostgreSQL 里同时保存 chunks 和 pgvector projection？

`video_chunks` 保存稳定 evidence ID、文本、hash、索引和 embedding model，是 BM25、审计和重建使用的事实源；pgvector 表保存相似度查询需要的 embedding，是派生 projection。

同库降低了部署和备份成本，但当前 source replace 与 projection publish 仍是两个 transaction。projection 失败会记录 RAG failed 状态，`rag-audit` 发现差异，`rag-reindex` 从 source 重建。不能把同实例误说成整体原子提交。

**证据：** `internal/repository/video_chunk.go`、`internal/vector/pgvector.go`、`internal/service/rag_index_build.go`。

## 8. Repository 为什么包事务？

Repository 集中 GORM 查询、条件更新和 transaction wrapper，service 负责编排业务。价值不在于给每个 CRUD 套一层，而在于让 `ClaimRetryDispatch`、`RestoreRetryDispatch`、chunk replace、cleanup claim 等一致性操作有清晰 owner 和可复用事务边界。

PostgreSQL transaction 只能覆盖关系数据，不能同时提交 Kafka、MinIO 或 Redis。跨系统副作用必须依靠 durable state、幂等和重试恢复。

**证据：** `internal/repository/repository.go`、`internal/repository/task_lease_dispatch.go`、`internal/repository/task_cleanup_job.go`。

## 9. 为什么不保留 MySQL 作为业务主库？

技术上可以让 MySQL 保存业务、PostgreSQL 只存向量，但会维护两套关系数据库连接，产生跨库一致性、部署和面试解释成本。当前项目规模没有这样的业务价值，因此 API Server 只连接 PostgreSQL；`legacy_mysql` 只供迁移和观察期审计工具。

**证据：** `cmd/server/database.go`、`internal/config/config.go`、`cmd/mysql-to-postgres/`。
