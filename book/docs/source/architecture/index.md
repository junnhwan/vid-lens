# VidLens 当前架构与启动流程：源码维护入口

> 本章只描述当前代码。历史 MySQL + Milvus 架构、迁移过程和故障记录分别保留在迁移文档与 troubleshooting 文档中，不应混入当前运行时说明。

## 1. 当前架构边界

VidLens 是以 Go 后端为主体的 AI 视频理解项目。前端主要用于验证上传、任务状态、转写、总结和问答链路，不是项目的架构重点。

```text
HTTP / SSE
    |
Gin Handler -> Service -> Repository -> PostgreSQL
                    |              \-> pgvector projection
                    |-> Redis
                    |-> MinIO
                    |-> Kafka -> Consumer -> FFmpeg / AI Provider
```

当前本地正式运行时：

- PostgreSQL 是唯一业务关系数据库；
- pgvector 是 PostgreSQL 扩展，保存可重建的 embedding 投影；
- `video_chunks` 是 RAG 文本事实源，`vidlens_rag_vectors` 是检索投影；
- Redis 保存锁、限流、上传临时状态和最近对话，不保存不可重建的业务事实；
- Kafka 调度 download、transcribe、analyze 和 rag-index 长任务；
- MinIO 保存视频、音频等对象；
- MySQL 与 Milvus 只保留为迁移观察期资产，不参与默认运行时；
- 远端 PostgreSQL 切换尚未由当前仓库证据证明，不能把本地迁移结果外推为线上事实。

## 2. 维护事实源

不要把本章中的文字或代码片段当成独立实现。判断当前行为时按以下顺序读取：

| 问题 | 事实源 |
|---|---|
| 应用启动与关闭 | `cmd/server/main.go`、`cmd/server/wiring.go` |
| 数据库连接 | `cmd/server/database.go`、`internal/database/postgres.go` |
| 路由 | `cmd/server/router.go` |
| 配置 schema 与校验 | `internal/config/config.go`、`loader.go`、`validation.go` |
| 数据模型 | `internal/model/`、`internal/model/model.go` |
| 事务与持久化状态机 | `internal/repository/` |
| 上传与任务业务 | `internal/service/media*.go` |
| Kafka 生命周期 | `internal/mq/consumer*.go`、`producer.go`、`retry.go` |
| RAG 构建与检索 | `internal/service/rag_*.go`、`retrieval_fusion.go` |
| 向量后端 | `internal/vector/factory.go`、`pgvector.go`、`milvus.go` |
| 当前维护边界 | `docs/backend-maintenance-map.md` |
| 当前路线图 | `docs/backend-optimization-roadmap.md`（本地维护文件） |

这里刻意不复制大段生产代码，也不记录容易漂移的行号和文件行数。后续 AI 应直接打开对应事实源。

## 3. 启动流程

`cmd/server/main.go` 的当前启动顺序可以概括为：

1. 解析 `--config`；
2. 建立可被 SIGINT / SIGTERM 取消的根 context；
3. 初始化 JSON 日志、Prometheus registry 和独立 metrics server；
4. 严格加载并按 server 场景校验配置；
5. 打开 PostgreSQL 连接池并执行 GORM migration；
6. 连接 Redis，初始化 AI admission 与 quota reconciler；
7. 初始化 MinIO；
8. 初始化默认 AI strategy；
9. 创建 Kafka topics 与 producer；
10. 通过 `internal/vector/factory.go` 创建配置指定的向量后端；
11. 组装 Repository、Service、Handler、Consumer 与后台 scheduler；
12. 注册 Gin 路由、liveness/readiness；
13. 启动四类 Kafka consumer、RetryScheduler 和 TaskCleanupScheduler；
14. 收到退出信号后取消根 context，关闭 HTTP server，等待后台组件退出，再关闭 producer、向量 store、Redis 和 PostgreSQL 连接。

关键维护规则：

- `main.go` 只负责生命周期和基础设施边界；
- `wiring.go` 负责对象组装；
- `router.go` 只注册路由，不自行构造 service；
- 新后台组件必须接受 context，并提供明确的等待或关闭语义；
- 新依赖应加入 readiness 的前提是它确实阻止核心服务工作，而不是机械增加检查。

## 4. 分层职责

### Handler

Handler 只负责：

- 解析路径、查询、JSON 或 multipart 参数；
- 从认证上下文获取 user ID；
- 调用 service；
- 将领域错误映射为统一 HTTP 响应。

不要在 Handler 中写 GORM、Kafka 状态机或对象存储补偿逻辑。

### Service

Service 负责用例编排和业务规则，例如：

- 文件上传、分片合并和任务创建；
- 删除任务与 durable cleanup intent；
- RAG 索引、检索融合和问答；
- AI profile 解密和 provider 调用。

Service 可以协调多个依赖，但跨边界失败必须有明确恢复语义，不能靠“按顺序调用所以大概一致”。

### Repository

Repository 是 PostgreSQL 状态和事务的 owner。以下逻辑必须优先落在 repository transaction 中：

- task 与 task_job 的 processing lease；
- RetryScheduler dispatch 的 claim / restore；
- task cleanup intent 的创建、认领与终态；
- 需要数据库约束兜底的幂等写入。

不要在 service 中重新拼一套并发状态判断。

### 外部适配层

- `internal/storage/`：MinIO；
- `internal/vector/`：pgvector / Milvus 回滚适配；
- `internal/ai/`：ASR、LLM、Embedding provider；
- `internal/pkg/ffmpeg/`、`internal/pkg/ytdlp/`：外部进程；
- `internal/mq/`：Kafka producer / consumer。

只有存在真实替换需求或测试边界时才引入 interface。不要为单个调用方创建没有第二个实现的通用抽象。

## 5. PostgreSQL 与 pgvector

业务表和向量表位于同一个 PostgreSQL 实例，但当前并不是一个覆盖整个 RAG 构建流程的事务：

```text
transaction A: replace video_chunks relational source
transaction B: replace pgvector projection
```

`PGVectorStore.ReplaceTaskChunks` 能保证一个 task/model scope 内的“删旧投影 + 写新投影”原子提交。它不能把前一个事实源事务也纳入同一个原子提交。

因此当前恢复模型是：

- `video_chunks` 为事实源；
- pgvector 为可重建投影；
- projection 发布失败时 RAG index 记录 `failed`；
- 重试或 `cmd/rag-reindex` 重新构建；
- `cmd/rag-audit` 对账 source-only、target-only 和 metadata mismatch。

面试中不能把这描述成跨阶段强一致、two-phase commit 或 exactly-once。

## 6. MySQL 与 Milvus 的观察期边界

### MySQL

API Server 不读取 `legacy_mysql.*`，也不双写 MySQL。以下资产暂时保留：

- `cmd/mysql-to-postgres`；
- `internal/dbmigration`；
- Compose 的 `legacy-mysql` profile；
- 已核验的离线备份。

远端迁移、数据核验和观察期结束前不要删除它们。观察期结束后再统一移除配置、driver、迁移命令和 Compose service。

### Milvus

`internal/vector/milvus.go` 与 Compose 的 `milvus` profile 是独立的向量回滚路径。是否删除 Milvus 不应与是否删除 MySQL 捆绑决定。

当前默认配置必须显式使用：

```yaml
rag:
  store: pgvector
```

`internal/vector` 中的空配置兼容逻辑仍保留历史 Milvus 默认，只用于迁移兼容；新增正式配置不要依赖空值。

## 7. 关键运行链路

### 上传与任务处理

```text
上传文件
  -> MinIO 保存对象
  -> PostgreSQL 创建 asset / task / task_job
  -> Kafka 投递
  -> consumer 认领 processing lease
  -> FFmpeg / AI provider
  -> PostgreSQL 完成阶段状态
```

现有分片协议仍由客户端提供 file hash 和 chunk manifest。下一阶段的目标是增加服务端 upload session，固化 user、filename、size、chunk count 和 expected hash，并在 merge 时由服务端流式校验哈希。

### RAG

```text
ASR transcription
  -> chunk split
  -> PostgreSQL video_chunks
  -> embedding
  -> pgvector projection
  -> vector + BM25 candidates
  -> RRF fusion
  -> neighbor expansion
  -> cited answer
```

RAG 的来源是 ASR 转写，不是 AI summary。

### 删除

任务删除先在 PostgreSQL 中创建 durable cleanup intent，再由 scheduler 使用 lease 恢复执行 pgvector、MinIO、Redis 和关系表清理。共享 asset 只允许最后一个引用 owner 删除外部对象。

## 8. 已知一致性窗口

以下问题不能被“已有 Kafka 重试”掩盖：

- 首次创建 task/job 后，Kafka enqueue 失败；
- Kafka 已接收消息但 HTTP response 丢失；
- 进程在数据库提交与投递之间退出；
- 用户重复请求或 Kafka 重复消息。

RetryScheduler 的补投 claim/restore 已闭环，但首次投递仍需要单独评估 durable dispatch intent 或小型 transactional outbox。不要直接引入 CDC、通用 workflow engine 或另一套 MQ。

## 9. 健康检查与关闭

- `/health` 与 `/healthz`：进程 liveness；
- `/readyz`：带超时的关键依赖 readiness；
- metrics server 与业务 HTTP server 使用根 context；
- consumer 与 scheduler 在根 context 取消后退出，并由 `Wait` 等待；
- 连接池和客户端由创建它们的启动层关闭。

这套边界的重点不是“优雅停机”四个字，而是避免关闭过程中继续认领新任务、半途丢弃 lease 或泄露连接。

## 10. 修改后的最低验证

后端逻辑变化至少运行：

```powershell
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
staticcheck ./...
deadcode -test ./...
```

涉及命令入口时再运行：

```powershell
go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit ./cmd/mysql-to-postgres
```

修改本书后运行：

```powershell
cd book
npm run build
```

测试通过只能证明被测试的行为；架构结论还必须对照当前源码、配置、Compose profile 和部署证据。
