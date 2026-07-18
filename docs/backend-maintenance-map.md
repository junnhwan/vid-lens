# VidLens 后端维护地图

> 这份文档服务于后续开发者和 AI 接手代码，不是产品介绍，也不替代测试。任何描述以当前代码和验证结果为准。

## 1. 先看哪些入口

| 目的 | 首先阅读 |
|---|---|
| 了解服务如何组装 | `cmd/server/main.go`、`cmd/server/wiring.go` |
| 查看 HTTP 路由、参数和鉴权 | `cmd/server/router.go`、`internal/handler/` |
| 查看任务异步链路 | `internal/mq/producer.go`、`internal/mq/consumer_*.go`、`internal/mq/retry.go` |
| 查看任务状态和 lease 状态机 | `internal/repository/task_lease*.go`、`internal/repository/task_job.go` |
| 查看普通/URL 上传 | `internal/service/media*.go`、`internal/storage/minio.go` |
| 查看耐久化分片上传 | `internal/handler/upload_session.go`、`internal/service/upload_session*.go`、`internal/repository/upload_session.go`、`internal/model/upload_session*.go` |
| 查看任务删除与资源清理 | `internal/service/task_cleanup*.go`、`internal/repository/task_cleanup_job.go`、`internal/model/task_cleanup_job.go` |
| 查看聊天和 RAG 问答 | `internal/service/chat*.go`、`internal/service/rag_*.go` |
| 查看向量后端选择 | `internal/vector/factory.go`、`internal/vector/backend.go` |
| 查看关系数据库迁移 | `cmd/mysql-to-postgres/`、`internal/dbmigration/`、`docs/postgresql-single-database-migration.md` |
| 查看 pgvector 重建和迁移边界 | `cmd/rag-reindex/`、`cmd/rag-audit/`、`docs/pgvector-migration.md` |
| 查看部署门禁和 release 回滚 | `.github/workflows/deploy-server.yml`、`deploy/server-deploy.sh`、`deploy/server-deploy_test.sh` |
| 查看线上迁移前只读证据 | `deploy/server-preflight-audit.sh`、`deploy/server-preflight-audit_test.sh`、`docs/postgresql-single-database-migration.md` |

建议不要从最大的文件开始通读。先根据要修改的能力找到上表入口，再沿调用关系阅读。

## 2. 运行时主链路

```text
HTTP handler
  ├─ 普通文件上传 -> MinIO asset -> PostgreSQL VideoTask -> 用户主动投递分析/转写消息
  ├─ 分片上传 -> PostgreSQL session/chunk ledger + MinIO bytes -> 流式校验/合并 -> PostgreSQL asset/task
  └─ URL 上传 -> PostgreSQL downloading task/job -> Kafka download topic

Kafka consumer
  -> ClaimTaskProcessing(processing lease)
  -> 下载 / ASR / 摘要 / RAG 索引
  -> PostgreSQL 持久化阶段结果
  -> CompleteTaskProcessing 或 FailTaskProcessing
  -> 只有业务完成或失败已可靠移交后才提交 Kafka offset

任务删除
  -> PostgreSQL transaction: task_cleanup_jobs intent + task soft-delete
  -> immediate best-effort cleanup
  -> TaskCleanupScheduler 扫描失败/过期 lease
  -> vector projection / MinIO 清理
  -> PostgreSQL task-owned rows + asset 收尾

RAG 问答
  -> PostgreSQL task/chunk 事实数据
  -> vector.NewStore 选择当前后端
  -> 向量检索 + 关键词检索/融合
  -> ChatService 组装引用和上下文
  -> AI 回答 -> PostgreSQL chat message + Redis 最近消息
```

`cmd/server/main.go` 负责运行时组装；业务服务不应自行读取配置、重复创建 Kafka producer 或复制向量后端 switch。

## 3. 当前文件职责边界

### 3.0 Server composition

- `cmd/server/main.go`：启动参数、配置加载、基础设施连接、vector backend 初始化、HTTP server 和进程级关闭顺序；不再直接堆放 service/handler 构造和路由细节。
- `cmd/server/wiring.go`：把已经连接好的 PostgreSQL/Redis/MinIO/Kafka/vector 依赖组装成 service、handler、consumer、业务 retry scheduler 和资源 cleanup scheduler；这里不创建网络连接，也不启动 goroutine。
- `cmd/server/router.go`：Gin middleware、API 路由、健康检查路由和前端 SPA fallback；不负责业务对象构造。
- `cmd/server/health.go`：liveness/readiness 的检查模型和响应策略。
- `cmd/server/options.go`：server 命令行参数解析。

这样拆分的目的不是引入 DI 框架，而是把“连接基础设施”“组装应用”和“注册 HTTP 路由”分成可单独阅读、测试和替换的边界。

### 3.1 Kafka 和任务 lease

- `internal/mq/consumer.go`：`Consumer` 依赖和构造，不放具体阶段处理逻辑。
- `internal/mq/consumer_lifecycle.go`：reader 生命周期、手动 commit、poison message 持久化隔离和消费者启动。
- `internal/mq/consumer_download.go`：下载阶段。
- `internal/mq/consumer_transcribe.go`：音频提取、分段 ASR、chunk 状态。
- `internal/mq/consumer_analyze.go`：摘要和标题。
- `internal/mq/consumer_rag.go`：RAG index topic 的消费和投递失败 handoff。
- `internal/mq/consumer_helpers.go`：跨阶段共享的 lease、trace 和状态辅助。
- `internal/mq/retry.go`：扫描到期失败任务并投递重试消息；不要在这里复制 processing lease 的数据库更新。

`internal/repository/task_lease*.go` 按状态转换拆分：

- `task_lease.go`：请求对象、结果和状态类型。
- `task_lease_processing.go`：消费者获取 processing lease。
- `task_lease_dispatch.go`：RetryScheduler 获取/恢复 dispatch lease。
- `task_lease_terminal.go`：当前 processing job 完成/失败落库。
- `task_lease_ownership.go`：所有权检查、续租、带行锁副作用和跨阶段 handoff。

### 3.2 MediaService

`MediaService` 仍然是 handler 使用的一个 façade，但实现按能力拆分：

- `internal/service/media.go`：依赖、构造、共享类型。
- `media_file_upload.go`：普通文件读取、内容 hash、asset 创建。
- `media_url_upload.go`：URL 校验后的下载任务创建；实际下载不在 HTTP 请求中执行。
- `media_tasks.go`：分析/转写投递、任务查询、删除和预签名 URL。
- `internal/service/remote_video_url.go`：URL host allowlist、解析和目标地址校验。

耐久化分片上传故意不塞回 `MediaService`：`UploadSessionService` 独立负责 PostgreSQL 会话状态机和 MinIO 字节边界。不要让 handler 直接操作 MinIO、上传表或 task 状态。

### 3.3 任务删除与资源清理

- `internal/service/task_cleanup.go`：删除请求事务、cleanup lease 执行器、向量/MinIO/PostgreSQL 收尾顺序；不要把它改成通用 Saga 引擎。
- `internal/service/task_cleanup_scheduler.go`：扫描到期 intent，并复用 `TaskCleanupService.ExecuteJob`；一个 job 失败不能阻塞同批次后续 job。
- `internal/model/task_cleanup_job.go`、`internal/repository/task_cleanup_job.go`：durable intent 与 `pending/running/failed/completed` 状态机。`lease_token` 是完成/失败 CAS 的所有权凭证。
- `internal/model/asset.go`、`internal/repository/asset.go`：asset 的 `active/deleting` 生命周期和删除 owner。`deleting` asset 不得被新上传任务复用。
- `cmd/server/wiring.go`：创建唯一的共享 `TaskCleanupService`，同时注入 `MediaService` 和 `TaskCleanupScheduler`。`MediaService` 不提供隐式 fallback，避免即时路径与后台路径使用不同配置。

用户 DELETE 的成功边界是“cleanup intent 与 task soft-delete 已在同一 PostgreSQL 事务提交”，不是所有外部系统已经同步删除。即时清理失败后 job 进入 `failed` 并由 scheduler 恢复。queued/running task 当前返回 409，因为项目还没有 worker cancellation/tombstone 协议。

### 3.4 ChatService

- `internal/service/chat.go`：公共类型、依赖、构造和共享准备结果。
- `chat_sessions.go`：会话和标题相关操作。
- `chat_prepare.go`：strict RAG、video assistant、摘要/转写兜底和检索管线准备。
- `chat_ask.go`：非流式问答、消息持久化和 AI 调用观测包装。
- `chat_stream.go`：provider streaming 与非 streaming 适配。
- `chat_recent.go`：Redis 最近消息缓存与数据库回源。
- `chat_messages.go`：prompt 消息和视频概览问题判断。
- `chat_memory.go`：RedisChatMemoryStore 的基础实现，不要与 ChatService 的问答编排混在一起。

当前 `ChatModeVideoAssistant` 和 `ChatModeStrictRAG` 是两个有意保留的业务模式，不要用一个“万能 prompt”替代它们，除非先补充模式级测试。

### 3.5 RAG 和向量后端

- PostgreSQL `video_chunks` 是转写 chunk、文本 hash、模型/维度等事实源。
- `internal/vector/` 只负责向量存储后端适配和工厂，不负责 chunk 业务状态。
- `vector.NewStore` 和 `vector.BackendConfigFromApplication` 是运行时选择后端的唯一入口；空 `rag.store` 也归一化为 pgvector，Milvus 回滚必须显式配置。
- `cmd/rag-eval` 和 server 都应复用同一个 factory，不要各自写 Milvus/pgvector 判断。
- `cmd/rag-reindex` 是专门写 pgvector 的迁移工具，故意不跟随 `rag.store`，避免回滚配置时误写其他后端。
- `cmd/rag-reindex/checkpoint.go` 的 `checkpointLifecycle` 是 execute 状态编排的唯一入口，`main.go` 只选择当前执行阶段并调用它：开始写 `running`，成功写 `completed`，失败只写 `failed + failure_stage` 稳定枚举；不要在 `run` 中重新散落 `Completed` 或 `UpdatedAt` 赋值。
- checkpoint `running` 可能是仍在运行，也可能是进程异常退出；恢复前先排除同 scope 活跃进程。`failed` 修复后沿原 checkpoint 继续，`completed` 后仍必须执行 `rag-audit --all` 和固定 RAG 评测。
- v1 checkpoint 由 loader 在内存中兼容升级，v2 loader 拒绝未知版本和不一致生命周期。原始 provider/driver error 只能返回 stderr，不能加入 checkpoint JSON。
- pgvector 当前使用独立投影表 `vidlens_rag_vectors`；不要把 pgvector 行当成 PostgreSQL chunk 的第二个事实源。
- `rag_index.go`：索引服务公共类型、构造、admission wait 和状态查询。
- `rag_index_build.go`：索引构建编排、状态落库、embedding 和 chunk/向量投影写入阶段。
- `rag_artifact.go`：稳定 evidence ID、chunk manifest 和 PostgreSQL chunk ID 到向量投影的绑定校验。

- `rag_eval_config.go`：检索实验配置、严格校验和单变量消融约束。
- `rag_eval_query.go`：确定性查询预处理适配，不负责 LLM rewrite。
- `rag_eval.go`：评测输入/报告类型、指标聚合和执行器。

详细迁移边界、checkpoint 和评测证据见 [`docs/pgvector-migration.md`](pgvector-migration.md)。

## 4. 不可破坏的不变量

### 4.1 Kafka offset

`consumer_lifecycle.go` 的 `consumeReader` 只有在 handler 返回成功后才 commit。handler 返回错误时 reader 会关闭并由外层重建；poison message 只有先写入隔离表后才被转换为可提交的成功结果。

### 4.2 Processing lease

处理任务时至少同时关注：

- `processing_token`：当前 owner；
- `lease_kind`：processing 和 dispatch 不是同一种 lease；
- `lease_version`：数据库 CAS 版本；
- `lease_expires_at`：过期后才允许接管；
- `TaskJob` 对应阶段的状态和 token。

数据库副作用应使用 repository 提供的 lease 方法，不要在 consumer 中直接更新这些字段。外部 AI、MinIO、Kafka 调用不是数据库事务的一部分，必须依赖 token、幂等记录或已持久化结果降低重复执行影响。

### 4.3 Retry dispatch

RetryScheduler claim dispatch 后，如果 Kafka enqueue 失败，必须通过 `RestoreRetryDispatch` 或等价的恢复路径让任务重新可扫描；不能只把任务留在 queued + 未过期 dispatch lease 状态。

### 4.4 上传状态

- PostgreSQL `upload_sessions` 是上传生命周期、不可变 manifest、owner、完成 lease 和最终 task identity 的事实源；Redis 不参与上传正确性。
- PostgreSQL `upload_session_chunks` 以 `(session_id, chunk_index)` 唯一约束保存已接受分片的 size、SHA-256 和 MinIO object name；同内容重试幂等，不同内容重试冲突。
- MinIO 只保存字节。分片对象使用 `upload-sessions/<session_id>/chunks/<index>/<sha256>`，不能用全局 MD5 key 绕过用户边界。
- complete 必须先持有带 token 的 completion lease，再按 manifest 顺序流式读取分片，同时校验单片大小、整文件大小和服务端 MD5。
- asset/task 创建和 session 完成 CAS 位于同一个 PostgreSQL transaction；重复 complete 返回 session 保存的同一 task identity。
- 分片清理是 best-effort，不得让已经成功提交的完成事务回滚。废弃 session 的对象生命周期清理仍需独立策略。
- 资产可被多个 task 引用，删除 task 时只有没有活跃引用才删除对象。

### 4.5 删除与资源清理

- `task_cleanup_jobs` intent 与 task soft-delete 必须在同一 PostgreSQL transaction 内提交；intent 创建失败时 task 必须继续可见。
- PostgreSQL `video_chunks` / `video_rag_indexes` 在向量清理成功前保留 embedding model 恢复事实。不要先删事实源再尝试清理投影。
- 向量投影操作和 MinIO 不与删除请求的 PostgreSQL transaction 共享事务；依靠幂等 delete、持久化 job、退避重试和 lease token 达到可恢复的最终一致性。pgvector 虽与业务表位于同一数据库，当前清理编排仍不是一个覆盖外部资源的全局事务。
- 最后一个 active task 引用消失时，cleanup job 才能将 asset 从 `active` reserve 为 `deleting`。只有记录在 `delete_owner_job_id` 的 job 可以软删除 asset。
- MinIO 已删除但 PostgreSQL 收尾失败时，重试会再次执行幂等对象删除；不要把 object delete 调用次数当成业务执行次数。
- queued/running task 不得直接删除；在完整取消协议实现前保持 409。

### 4.6 RAG 数据边界

- ASR 转写是 RAG 的源内容，不是 LLM 摘要。
- PostgreSQL chunk 删除或 hash/模型变化后，向量投影需要通过重建或清理保持一致。写入向量后端前，每个向量必须绑定到正数的 PostgreSQL `video_chunks.id`，禁止静默写入 `chunk_id=0`。
- pgvector 实现了 task/model scope 内的事务性 replace（同一 PostgreSQL 事务中先删旧投影再写新投影）；Milvus 仍走显式的 delete + upsert 兼容路径。两者都不保证“业务事实写入 + 向量发布”处于同一事务；pgvector 虽位于同一个 PostgreSQL，当前服务仍分阶段提交，失败后应依赖 RAG failed 状态和可重建索引恢复。
- 向量后端切换前先做 manifest、固定评测集和 preflight；不要仅凭一次查询结果宣称性能提升。

## 5. 常见修改应该落在哪里

| 需求 | 修改位置 | 先补/运行的验证 |
|---|---|---|
| 新增 Kafka 阶段 | `producer.go`、对应 `consumer_*.go`、`retry.go`、`cmd/server/main.go` | `internal/mq` 测试、lease 测试、全量测试 |
| 修改任务重试状态 | `internal/repository/task_lease*.go`、`task_job.go` | repository lease 测试 + `go test -race ./internal/mq ./internal/repository` |
| 新增上传方式 | `media_*.go`、handler、必要的 repository/storage | media 测试、边界和清理测试 |
| 修改删除/资源清理 | `task_cleanup*.go`、cleanup repository、asset lifecycle、server wiring | intent 原子性、共享 asset、外部失败重试、lease 与 scheduler 测试 |
| 修改问答模式 | `chat_prepare.go`、`chat_messages.go`、`chat_ask.go` | strict/assistant/fallback/stream 测试 |
| 更换向量后端 | `internal/vector/`、配置、迁移文档 | factory、backend 集成测试、manifest 和 rag-eval |
| 改 AI provider 调用 | `internal/ai/` 和调用观测包装 | provider、治理、调用记录测试 |

## 6. 开发前后的最小检查清单

1. 先运行 `git status -sb`，不要覆盖工作区已有修改。
2. 先定位事实源和状态 owner，再修改调用方。
3. 不为一次性调用增加新的 interface；优先复用现有 package 边界。
4. 逻辑变更先补一个能复现失败路径的测试；纯文件拆分也要验证行为等价。
5. 后端变更至少运行：

   ```powershell
   go test -count=1 ./...
   go test -race -count=1 ./...
   go vet ./...
   staticcheck ./...
   deadcode -test ./...
   ```

6. 涉及 server 或命令入口时再运行：

   ```powershell
   go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit ./cmd/mysql-to-postgres
   ```

7. 涉及 PostgreSQL 方言、事务、行锁或 pgvector SQL 时，不能只依赖 SQLite/sqlmock；启动真实 PostgreSQL 后显式运行：

   ```powershell
   $env:VIDLENS_POSTGRES_INTEGRATION_DSN="<local-test-dsn>"
   go test -count=1 ./internal/model ./internal/repository -run '^TestPostgres' -v
   $env:VIDLENS_PGVECTOR_INTEGRATION="1"
   go test -count=1 ./internal/vector -run '^TestPGVectorStoreIntegration$' -v
   ```

   环境变量未设置导致的 skip 不是验证通过。不要在日志或文档中记录真实密码。

8. 不把 API key、cookie、profile key、checkpoint 产物和本地日志提交到 Git。

## 7. 当前技术栈观察窗口

当前正式配置使用 pgvector，Milvus 代码和 `milvus` Compose profile 暂时保留为回滚选项。迁移完成不等于已经证明 pgvector 全面优于 Milvus；在删除 Milvus 前仍需持续观察 readiness、检索错误和固定评测集结果，并根据真实数据量决定是否引入 HNSW/IVFFlat。

如果后续确认不再需要回滚，删除 Milvus 前应按顺序处理：

1. 更新配置和启动文档；
2. 删除 Milvus 专用 factory 分支、连接配置、compose 服务和测试；
3. 更新 README、架构图和迁移文档；
4. 运行全量测试、构建和部署前健康检查；
5. 记录一次可回滚的数据库/对象存储备份。

## 7.1 部署脚本与数据库迁移门禁

- `.github/workflows/deploy-server.yml` 只负责编排测试、构建、上传和 SSH 调用；远程 release 激活与回滚必须集中在 `deploy/server-deploy.sh`，不要重新复制内联 shell 流程。
- `deploy/server-deploy_test.sh` 是部署行为的可执行规格。修改文件替换顺序、备份、路径清理、服务重启或健康检查时，先增加失败用例，再修改脚本。
- 工作流期望 `/opt/vidlens/.runtime-generation` 精确等于 `postgres-pgvector-v1`。脚本在创建 release backup 和替换文件前检查它；缺失或不匹配表示远程迁移尚未获准，必须停止。
- 迁移标记是人工授权，不是 readiness。只有远程旧库备份已验证、关系数据和向量投影迁移已对账、PostgreSQL 配置及依赖已准备好后，才能在维护窗口写入。
- 新 release 使用 `http://127.0.0.1:18083/readyz` 验证必需依赖；`/health`/`/healthz` 只是进程存活信号，不能证明 PostgreSQL 或 pgvector 可用。
- 脚本自动恢复的是 server 和前端 release。`config.yaml` 只备份、不自动改写；数据库也不做反向同步。数据库代际回滚必须使用维护窗口前单独验证的旧配置、MySQL 备份和 Milvus manifest。
- 所有递归清理只允许作用于经过绝对路径、规范化和目录不嵌套校验的精确 staging 路径。不要为了“简化脚本”删除这些路径约束。

`deploy/server-preflight-audit.sh` 是创建迁移标记之前的只读证据收集入口：

- 只读取 server/config 的元数据和 SHA-256、marker、数据目录大小、部署盘容量、systemd 摘要、VidLens 容器列表、指定端口计数和本机健康端点状态；
- 不读取 `config.yaml` 内容，不调用 `docker inspect`、`systemctl cat`，也不输出进程或容器环境变量；
- 输出是单行 `key=value`。`audit.collection_complete=true` 且退出码为 0 只表示采集器完整执行，**不表示迁移条件已通过**；缺少 marker、服务未就绪或端口仍存在都是需要人工解释的审计事实；
- 必需采集器缺失、执行失败或返回不可解析结果时，脚本仍尽量输出其他证据，最后写入 `audit.collection_complete=false` 并退出 2；参数或路径不安全等调用错误退出 1；
- `deploy/server-preflight-audit_test.sh` 是上述输出契约和“不泄漏配置内容”边界的可执行规格。新增采集项时先补正常、异常和脱敏测试，不要把配置内容加入报告。

本地修改预检或部署行为后至少运行：

```bash
bash -n deploy/server-preflight-audit.sh deploy/server-preflight-audit_test.sh
bash deploy/server-preflight-audit_test.sh
bash -n deploy/server-deploy.sh deploy/server-deploy_test.sh
bash deploy/server-deploy_test.sh
```

### 数据迁移命令所有权

- `cmd/mysql-to-postgres`：只迁 20 张关系表并审计关系/sequence；不允许重新加入向量连接或向量报告字段。
- `cmd/rag-reindex`：只从 PostgreSQL `video_chunks` 重建 pgvector；dry-run 与 execute 共用目标维度校验，checkpoint 在成功 upsert 后推进。
- `cmd/rag-audit --all`：最终全量向量 manifest 门禁；不负责修复。
- 顺序固定为“关系迁移与独立审计 → pgvector 重建 → 全量向量审计 → 固定 RAG 评测”。
- PostgreSQL advisory lock 只解决迁移进程互斥，不能替代维护窗口停写。

## 7.2 RAG 投影一致性维护

`internal/service/rag_projection_audit.go` 提供只读的、后端无关的 manifest 对账逻辑。它把 PostgreSQL `video_chunks` 视为事实源，把 pgvector/Milvus 视为可重建投影，按 `vector_id` 对齐并检查：

- source-only：PostgreSQL 有 chunk，向量后端没有对应投影；
- target-only：向量后端有记录，PostgreSQL 没有对应来源；
- metadata mismatch：`user_id`、`task_id`、`chunk_id`、`chunk_index`、`content_hash` 或 embedding model 不一致；
- invalid/duplicate：缺少 evidence ID、非正 `chunk_id`、越界 scope 或重复 evidence ID。

该逻辑只读，不会自动删除向量，也不会自动触发付费 embedding。这样发现漂移后，可以人工确认范围，再使用 `cmd/rag-reindex` 的显式过滤条件做修复。

维护命令：

```powershell
# 日常定位：显式单 scope，可用于 pgvector 或回滚期 Milvus
 go run ./cmd/rag-audit --config config.yaml --user-id 5 --task-id 14 --model embed-v1

# 正式 pgvector 迁移门禁：审计 PostgreSQL/pgvector 两侧 scope 并集
 go run ./cmd/rag-audit --config config.yaml --all
```

单 scope 模式要求明确提供 user/task/model，避免日常维护误扩大范围。`--all` 只允许 pgvector，用于全量迁移门禁，并能发现只存在于目标端的孤儿 scope。任一 issue 都会使命令以非零状态结束。该命令只报告 manifest 差异，不写向量、不调用付费 embedding，也不代表事实表与向量投影处于同一事务或线上检索质量已经合格。

`cmd/rag-eval` 的 preflight 复用同一套对账逻辑，因此评测前的 evidence 检查与维护命令不会各自维护一份容易漂移的比较规则。

## 8. URL 下载安全边界

`internal/pkg/remoteurl/` 是 URL 上传入口和真实下载执行路径共用的策略包：

- 只允许 `http/https`；
- host allowlist；
- 对域名再次做 DNS 解析，并拒绝 loopback/private/unspecified/link-local/multicast 地址；
- 去除 userinfo、fragment 和不必要 query；YouTube watch URL 只保留 `v`；
- HTTP 创建任务时检查一次，Kafka consumer 进入真实 yt-dlp 路径前再检查一次。

`internal/mq/consumer_download.go` 的执行期检查解决的是“任务入队后 URL 被篡改、过期或解析结果变化时仍完全不检查”的缺口，但它不是网络沙箱。yt-dlp 是外部进程，可能自行处理重定向或再次解析域名，Go 进程不能仅凭当前校验控制其全部后续网络请求。因此面试和文档中只能表述为“入口 + 执行前 allowlist/DNS 校验”，不能表述为已经完成生产级 SSRF 防护。若要达到更强边界，应在独立下载 worker、受限 egress 网络或代理层再次实施域名/IP策略。

## 9. 配置加载、校验与维护命令边界

- `internal/config.Load` 负责读取 YAML、展开环境变量、检查已知弃用字段、使用 `KnownFields(true)` 严格解析，并归一化当前跨命令共享的 Kafka 默认值：`download_topic` 默认是 `video-download`，`rag_index_topic` 默认是 `video-rag-index`。
- 未知顶层或嵌套字段、多个 YAML document 都会在加载阶段失败，避免拼写错误被静默忽略。`rag.collection` 会得到迁移到 `milvus.collection` 的明确提示；旧 `rag.rerank_model` / `rag.rerank_endpoint` 会指向 `cmd/rag-eval` 的 legacy CLI 参数。不要为兼容旧草稿重新放宽解析。
- `cleanup` 独立配置扫描间隔、批量、lease 和失败退避，不复用 `task_retry`；两者分别处理资源回收与 Kafka 业务任务，失败语义不同。
- `rag` 只保存 chunk、检索和 embedding 维度等后端中立参数；Milvus collection 属于 `milvus.collection`，pgvector 表名属于 `rag.vector_table`。`internal/vector.BackendConfig` 不再保留第二个顶层 collection 来源。
- `Load` 故意不执行完整 server 校验：配置解析测试、RAG 评测和维护命令允许使用比常驻服务更窄的配置，不能因为缺少无关基础设施配置而失败。
- `internal/config/validation.go` 按使用场景提供轻量结构校验：`ValidateServer` 检查常驻服务启动项；`ValidatePostgres` 检查正式 PostgreSQL 连接字段；`ValidateMySQL` 只检查离线迁移工具的 `legacy_mysql.*`；`ValidateVectorBackend` 只检查当前向量后端；`ValidateRAG` 再叠加索引/检索参数；`ValidatePGVectorDestination` 检查 rag-reindex 计划使用的 pgvector 连接形状和正数维度。它们只校验配置形状，不代替启动时的真实连接和 readiness。
- server 在连接 PostgreSQL、Redis、MinIO、Kafka 或向量后端前执行 `ValidateServer`，不会读取或连接 `legacy_mysql`。`rag-audit` 和 `rag-eval` 组合正式 PostgreSQL + 当前向量后端校验；`rag-reindex` dry-run 与 execute 都校验正式 PostgreSQL 和计划目标维度，只有 execute 才初始化密钥、embedding client 和 pgvector writer。只有 `cmd/mysql-to-postgres` 使用 `ValidateMySQL`。
- `cmd/server/main.go` 不再在启动流程中临时补 Kafka 默认值；server、`cmd/rag-audit`、`cmd/rag-eval` 和 `cmd/rag-reindex` 看到的是同一份配置语义。
- 其他 Kafka topic 没有擅自补默认值，因为它们原本就是现有配置中的必填业务入口；不要仅为减少空值而改变启动失败语义。
- `internal/vector.BackendConfigFromApplication` 是应用配置到向量后端连接配置的唯一适配边界。`cmd/rag-reindex` 虽然明确写入 pgvector，但复用该适配器后再设置维护命令专用的较小连接池；这不表示它跟随当前 `rag.store` 选择。
- `rag.store` 为空时由 `internal/vector.NormalizeBackendName` 归一化为 pgvector；Milvus 只在显式配置 `rag.store: milvus` 时作为迁移观察期回滚路径。不要把保留回滚适配器误写成“Milvus 已物理下线”。


## 10. rerank 的运行边界

项目当前有三条容易被混淆、但用途不同的 rerank 路径：

1. **线上聊天**：`internal/service/chat_prepare.go` 创建检索流水线时使用 `service.DeterministicReranker`。`cmd/server/wiring.go` 只传入 TopK、CandidateK、MinScore、RecentTurns 等检索参数，不配置模型 rerank。
2. **strict 评测**：`cmd/rag-eval/strict_eval.go` 只接受检索实验配置中的 `reranker_mode: none` 或 `deterministic`，用来保证冻结证据和实验配置可复现；它拒绝 legacy 的模型 rerank CLI 参数。
3. **live/legacy 评测**：模型 rerank 是可选实验，必须显式传 `--rerank-model`；`--rerank-endpoint` 可选，省略时 `internal/ai.Factory` 只能在用户 embedding endpoint 以 `/embeddings` 结尾时推导 `/rerank`。API key 复用该用户的 embedding profile key。

正式 `RAGConfig` 和 `config.yaml` 不再包含 `rerank_endpoint` / `rerank_model`，因为它们此前没有进入 server wiring，只被 legacy 评测读取。未传 `--rerank-model` 时，legacy 评测不会创建模型客户端，也不会重复执行或输出“Model Rerank”模式。`ai.Profile` 仍保留两个运行时字段，作为 legacy 实验向 `ai.Factory.NewRerankClient` 传参的窄边界；用户 AI Profile 数据表、请求和响应没有持久化这两个字段。

因此当前可陈述的是“线上有确定性 rerank 基线，离线工具可显式运行模型 rerank 实验”；不能陈述“线上聊天已启用 Cross-Encoder/模型 rerank”。如果未来要上线模型 rerank，应单独设计 server 配置、超时/熔断、调用成本、降级指标和评测准入，而不是重新借用离线实验参数。
