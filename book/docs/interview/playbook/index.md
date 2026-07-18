# 面试作战手册

> 本页是面试现场可直接口述的当前实现稿。事实以源码和 `MEMORY.md` 为准；MySQL + Milvus 只代表第一版架构和迁移历史，远端 PostgreSQL 切换完成前不要外推线上状态。

## 1. 你这个项目一句话是什么？

### 直接回答

VidLens 是一个以 Go 后端为主的 AI 视频理解项目。用户上传视频后，后端将文件保存到 MinIO，通过 Kafka 异步执行音频提取、分段 ASR、摘要和 RAG 建索引，最后基于 ASR 转写提供带引用片段的视频问答。

项目重点不是简单串联模型 API，而是把耗时长、成本高、容易受外部服务影响的处理链路做成可追踪、可重试和可恢复的后台任务。PostgreSQL 保存业务数据、RAG 文本事实和 pgvector 向量投影；Redis 负责锁、限流和临时状态；Kafka 负责任务传递；MinIO 保存大文件。

### 项目证据

- 启动和依赖装配：`cmd/server/main.go`、`cmd/server/wiring.go`
- 任务和子作业状态：`internal/model/task.go`、`internal/model/task_job.go`
- 视频处理 consumer：`internal/mq/consumer_*.go`
- RAG 建索引和问答：`internal/service/rag_index*.go`、`internal/service/chat_*.go`

### 当前限制

这是经过本地集成、迁移和部署验证的项目级后端，不应描述成大规模生产平台。远端 PostgreSQL 切换、完整计费配额、跨视频知识库和生产级 URL 下载隔离仍不能声称完成。

## 2. 为什么用 Kafka，而不是在 HTTP 请求里同步处理？

### 直接回答

视频下载、FFmpeg、ASR、摘要和向量索引可能持续几十秒甚至几分钟。放在 HTTP 请求中会长期占用连接；客户端超时或刷新后，也很难判断服务端是否仍在执行。

VidLens 的 HTTP 层只负责校验请求、创建任务和投递消息，consumer 执行具体阶段并把状态写回 PostgreSQL。前端通过任务状态看到 downloading、transcribing、summarizing 或 indexing，而不是只得到一次模糊的 HTTP 超时。

Kafka 在这里负责持久异步传递，不代表 exactly-once，也不负责全部业务重试语义。重复执行和失败恢复仍由 task/job 状态、processing lease、幂等写入和重试调度共同约束。

### 项目证据

- Producer：`internal/mq/producer.go`
- Consumer 生命周期与分阶段处理：`internal/mq/consumer.go`、`internal/mq/consumer_*.go`
- Task/job 状态：`internal/model/task.go`、`internal/model/task_job.go`
- Lease 与重试：`internal/repository/task_lease_*.go`、`internal/mq/retry.go`

### 当前限制

首次创建 task/job 后投递 Kafka 仍存在 DB commit 与 enqueue 之间的一致性窗口。当前不能声称已经实现 transactional outbox；应把它作为下一阶段可靠性改进。

## 3. 重试和最终一致性怎么处理？

### 直接回答

Consumer 失败后先区分错误是否值得重试。网络抖动、超时和部分 provider 临时错误可以按 retry budget 进入重试；鉴权或必需配置错误通常直接失败，避免无意义地重复消耗外部调用。

可重试失败会在 PostgreSQL 保存 retry count、next retry time 和 job type。RetryScheduler 到期后先通过 dispatch token 和 version lease 认领，再投递 Kafka。如果认领后 enqueue 失败，`RestoreRetryDispatch` 会在事务中把 task 和 job 恢复到可再次调度的失败状态，避免任务永久卡在 running。

外部系统之间不是分布式事务。任务删除使用 durable cleanup intent 和可续租 scheduler；RAG 的 `video_chunks` 与 pgvector projection 分阶段发布，投影失败会记录 RAG failed 状态，并由重试、`rag-reindex` 和 `rag-audit` 恢复或发现不一致。

### 项目证据

- 重试调度：`internal/mq/retry.go`
- Dispatch claim/restore：`internal/repository/task_lease_dispatch.go`
- Durable cleanup：`internal/service/task_cleanup*.go`、`internal/repository/task_cleanup_job.go`
- RAG 发布与恢复：`internal/service/rag_index_build.go`、`internal/service/rag_reindex.go`、`internal/service/rag_projection_audit.go`

### 当前限制

这不是通用 workflow engine，也不是跨 PostgreSQL、Kafka、MinIO、Redis 的全局事务。项目采用的是“关系库状态作为协调事实 + 可重试外部副作用”的有限最终一致性设计。

## 4. RAG 用什么数据？为什么不是摘要？

### 直接回答

RAG 的知识源是 ASR 转写，不是 LLM 摘要。摘要本身已经压缩并改写过内容，可能丢掉时间点、术语、原句和局部例子；转写更接近原始视频证据。

建索引时先将转写切成带 overlap 的 chunk，保存到 PostgreSQL `video_chunks`，再把带稳定 evidence ID 的向量投影发布到同一 PostgreSQL 实例中的 pgvector 表。查询时按 `user_id + task_id + embedding_model` 隔离，执行 pgvector 语义召回和 Go 侧 BM25 关键词召回，再用 RRF 融合并返回 citations。

### 项目证据

- 建索引：`internal/service/rag_index_build.go`、`internal/service/rag_artifact.go`
- pgvector：`internal/vector/pgvector.go`
- BM25：`internal/repository/video_chunk.go`
- RRF 与查询管线：`internal/service/retrieval_fusion.go`、`internal/service/rag_pipeline.go`
- 引用与检索快照：`internal/service/chat_prepare.go`、`internal/service/chat_stream.go`、`internal/model/chat.go`

### 当前限制

`video_chunks` 和 pgvector projection 虽然位于同一个 PostgreSQL 实例，但当前仍由两个 transaction 分阶段更新，不能说成整体强一致。BM25 目前按单视频读取 chunks 后在 Go 内计算，适合项目规模，不等于专业倒排搜索引擎。

## 5. 为什么从 MySQL + Milvus 改成 PostgreSQL + pgvector？

### 直接回答

第一版用 MySQL 保存业务表、Milvus 保存向量。对于当前数据量和单体后端，两套持久化系统带来的配置、部署、备份和对账成本，高于专用向量库带来的收益。

pgvector 是 PostgreSQL 扩展，因此迁移后普通业务表、`video_chunks` 和向量投影可以统一放在 PostgreSQL。API Server 只连接 PostgreSQL，不做 MySQL/PostgreSQL 双写。`legacy_mysql`、迁移命令和 Milvus adapter 只是观察期审计或回滚资产，不是默认运行时依赖。

### 项目证据

- Server 数据库入口：`cmd/server/database.go`
- PostgreSQL 连接和模型迁移：`internal/database/postgres.go`、`internal/model/model.go`
- 向量 backend factory：`internal/vector/factory.go`
- 数据迁移与审计：`cmd/mysql-to-postgres/`、`internal/dbmigration/`
- 迁移边界：`docs/postgresql-single-database-migration.md`、`docs/pgvector-migration.md`

### 当前限制

这不是说 pgvector 永远优于 Milvus。若未来向量规模、并发或 ANN 延迟经基准测试证明超出 PostgreSQL 的合理边界，再评估专用向量系统；目前不为简历堆叠两套数据库。

## 6. BYOK 解决了什么？API Key 怎么保存？

### 直接回答

公共部署不能长期消耗维护者自己的模型额度，因此用户可以配置 ASR、LLM 和 Embedding endpoint、model 与 API key。Key 使用服务端主密钥进行 AES-GCM 加密后保存，模型调用前才解密；模型字段禁止 JSON 序列化，列表接口只返回 mask 后的值。

### 项目证据

- Profile 模型：`internal/model/ai_profile.go`
- 加解密：`internal/pkg/secret/crypto.go`
- Profile 生命周期：`internal/service/ai_profile.go`
- Client factory：`internal/ai/factory.go`

### 当前限制

项目已经有调用日志和日聚合，但没有完整价格表、余额、扣费和 quota，不应说成计费系统。

## 7. URL 下载安全做到哪一层？

### 直接回答

URL 下载只作为自用便利功能维护。当前保留第一层边界：只允许 http/https、限制 host、DNS 解析后拒绝私网和特殊地址、执行前复检，并清洗日志中的 query 和 fragment。

我不会把它说成生产级 URL 下载平台。重定向链、DNS rebinding、下载体积和耗时硬限制、用户 cookies 隔离仍有缺口；因为该功能不进入简历主线，后续只修明确安全问题，不继续扩大投入。

### 项目证据

- URL 边界：`internal/pkg/remoteurl/`、`internal/service/remote_video_url.go`
- 下载执行：`internal/mq/consumer_download.go`、`internal/pkg/ytdlp/ytdlp.go`

## 8. 项目由 AI 辅助开发，怎么证明自己理解？

### 直接回答

我会坦诚 AI 参与了实现，但不会用“AI 提效”替代理解。项目的可信证据是：我能从真实故障解释设计变化，例如长视频 ASR 失败后改成分段处理和片段复用，RAG 状态缺失后补状态表，RetryScheduler 认领后投递失败再补事务恢复，数据库从 MySQL + Milvus 收敛为 PostgreSQL + pgvector 并做独立迁移审计。

回答时会区分三类内容：当前源码事实、历史故障、未来方向。RocketMQ、Redisson、Kafka exactly-once、完整 outbox、生产级 URL 安全和大规模收益都不会冒充已实现事实。

### 项目证据

- 长期事实源：`MEMORY.md`
- 维护入口：`docs/backend-maintenance-map.md`
- 故障记录：`docs/troubleshooting-and-interview-notes.md`
- 阶段审计：`docs/superpowers/audits/2026-07-17-plan-completion-audit.md`

## 面试前 15 分钟速刷

1. 项目定位：AI 视频理解后端，核心是长任务和高成本 AI 流程的工程化。
2. 四条主线：Kafka 状态与重试、分段 ASR、分片上传、pgvector + BM25 + RRF。
3. 两个边界：首次 Kafka 投递尚无 outbox；upload session 尚未服务端持久化。
4. 两个真实复盘：长视频 ASR 分段、RetryScheduler enqueue 失败恢复。
5. 数据库口径：当前本地 PostgreSQL 单库；MySQL/Milvus 仅迁移历史和观察期回滚；远端切换不能提前声称完成。
