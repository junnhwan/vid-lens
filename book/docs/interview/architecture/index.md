# VidLens 当前架构：面试问答稿

> 回答时只讲当前代码已经实现并有证据的部分。MySQL + Milvus 属于第一版架构和迁移历史；当前本地正式架构是 PostgreSQL + pgvector，远端切换尚未完成。

## 问题一：请先介绍一下 VidLens 的整体架构

### 可以直接说的回答

VidLens 是一个 AI 视频理解后端。用户上传视频后，后端不会在 HTTP 请求里同步跑完整的 FFmpeg、ASR 和总结链路，而是先把对象保存到 MinIO、把任务状态保存到 PostgreSQL，再通过 Kafka 调度转写、分析和 RAG 索引。Consumer 处理长任务时会认领数据库 processing lease，完成后再一起推进 task 和 task_job 状态；进程退出或者外部 AI 暂时失败时，调度器可以根据数据库状态恢复。

当前数据层已经收敛到 PostgreSQL。普通业务表、ASR 转写、RAG 文本 chunk 都在 PostgreSQL，embedding 通过 pgvector 扩展存储。`video_chunks` 是事实源，向量表是可以重新生成的检索投影。问答时会分别取向量候选和 BM25 候选，用 RRF 融合，再做邻居 chunk 扩展并返回引用。Redis 主要负责分布式锁、令牌桶、上传临时状态和最近对话，MinIO 保存大文件，Kafka 负责长任务调度。

这个项目现在仍然是模块化单体，不是微服务。我更关注任务状态、失败恢复和数据边界，而不是为了技术栈数量拆服务。

### 可能追问

- 为什么 HTTP 请求不能一直等 AI 处理完成？
- Kafka 和数据库之间如何保证一致？
- 为什么 Redis 不保存任务事实？
- RAG 为什么不用 summary？

### 防守回答

HTTP 请求只适合接收请求和返回任务标识，长视频的 FFmpeg、分段 ASR 和 LLM 调用可能持续数分钟。如果同步执行，客户端断开、网关超时或进程重启都会让结果和状态难以恢复。Kafka 解决的是异步调度，不自动解决所有一致性问题；真正的任务状态仍在 PostgreSQL，Consumer 也必须做幂等和 lease 校验。

RAG 使用 ASR transcription，因为它包含原始事实细节；summary 已经经过模型压缩，可能省略时间点和实体，适合作展示，不适合作检索事实源。

### 项目证据

- `cmd/server/main.go`
- `cmd/server/wiring.go`
- `internal/mq/consumer*.go`
- `internal/repository/task_lease*.go`
- `internal/service/rag_index*.go`
- `internal/service/retrieval_fusion.go`

### 当前限制

首次 task/job 数据库提交与 Kafka 投递之间仍有一致性窗口，不能说已经实现 transactional outbox 或 exactly-once。

---

## 问题二：应用是怎么启动和退出的？

### 可以直接说的回答

启动时先解析配置并创建可取消的根 context，然后初始化结构化日志和 Prometheus metrics server。配置通过严格 YAML schema 加载，并按 server 场景校验。之后连接 PostgreSQL、Redis、MinIO，初始化 AI provider、Kafka producer 和向量后端，再组装 Repository、Service、Handler、Consumer 和后台 scheduler。路由注册与依赖组装已经从 `main.go` 拆到 `router.go` 和 `wiring.go`，这样修改路由不会同时碰基础设施初始化。

进程收到 SIGINT 或 SIGTERM 后会取消根 context。HTTP server、Kafka consumer、RetryScheduler 和 TaskCleanupScheduler 都基于这个 context 停止接收新工作，并通过 `Wait` 等待已启动 goroutine 退出，最后再关闭 Kafka producer、向量 store、Redis 和 PostgreSQL 连接池。

### 可能追问

- 为什么先取消 context，再关闭连接？
- Redis 连接失败为什么现在会阻止启动？
- liveness 和 readiness 有什么区别？

### 防守回答

如果先关闭数据库或 Kafka，再通知 worker 停止，后台 goroutine 可能继续用已经关闭的依赖，产生额外失败。根 context 是生命周期信号，资源关闭放在 worker 退出之后。

`/healthz` 只说明进程还活着；`/readyz` 用带超时的依赖检查判断当前能不能承接核心请求。Redis 在这个项目里参与锁、限流、治理和调度相关状态，当前启动路径会主动 Ping，失败时不把实例标成可用。

### 项目证据

- `cmd/server/main.go`
- `cmd/server/health.go`
- `cmd/server/metrics_server.go`
- `cmd/server/router.go`
- `cmd/server/wiring.go`

### 当前限制

项目使用业务关联 ID 和 Prometheus 指标，不是完整的 OpenTelemetry 分布式追踪。

---

## 问题三：为什么从 MySQL + Milvus 改为 PostgreSQL + pgvector？

### 可以直接说的回答

第一版使用 MySQL 保存业务数据、Milvus 保存向量。对于当前项目规模，两套独立持久化系统增加了配置、部署、备份和对账成本，但没有体现出 Milvus 在大规模 ANN 上的优势。pgvector 是 PostgreSQL 扩展，所以迁移后普通业务表、RAG 文本事实和向量投影可以放在同一个 PostgreSQL 实例中，项目默认运行时只需要一套关系数据库。

我没有把“同一个数据库实例”直接说成“整个 RAG 构建强一致”。目前 `video_chunks` 的替换和 pgvector projection 的替换仍然是两个事务。pgvector 能保证一个 task/model scope 内删旧向量和写新向量原子提交，但如果事实源事务成功、投影事务失败，仍然要把索引状态记成 failed，通过重试或 reindex 恢复，并用 audit 命令对账。

### 可能追问

- MySQL 是否还在双写？
- 为什么还保留 MySQL 和 Milvus 代码？
- pgvector 一定比 Milvus好吗？

### 防守回答

API Server 不连接也不双写 MySQL。`legacy_mysql`、迁移命令和 MySQL Compose profile 只是远端切换前的离线迁移、核验和回滚资产。Milvus 适配是另一条向量回滚路径，两者不能混为一谈。

我不会泛化说 pgvector 一定优于 Milvus。当前选择是因为数据量和团队规模下，单库维护价值更高。如果以后向量规模、查询并发和召回延迟真的超过 PostgreSQL 的合理边界，再依据基准测试考虑专用向量系统。

### 项目证据

- `cmd/server/database.go`
- `internal/database/postgres.go`
- `internal/vector/factory.go`
- `internal/vector/pgvector.go`
- `cmd/mysql-to-postgres/`
- `docs/postgresql-single-database-migration.md`
- `docs/pgvector-migration.md`

### 当前限制

当前仓库只证明本地迁移与本地运行时边界，不能声称远端线上已经切换。

---

## 问题四：Kafka 在这里解决了什么，没解决什么？

### 可以直接说的回答

Kafka 把耗时的视频处理从 HTTP 生命周期中拆出来，让上传接口可以先返回任务，Consumer 再执行 FFmpeg、ASR、总结和 RAG 索引。它还让不同阶段可以有独立 topic 和失败边界，例如 ASR 已经成功时，RAG embedding 失败不应该把转写结果一起判成失败。

但 Kafka 只提供消息系统能力，不会替我维护业务状态。项目把 task 和 task_job 放在 PostgreSQL，Consumer 处理前认领带 token、version 和 lease 的状态，完成时要求仍持有同一所有权。RetryScheduler 在补投前也先认领 dispatch lease，如果 enqueue 失败，再在 PostgreSQL transaction 中恢复 task 与 task_job。

### 可能追问

- 为什么不用 RocketMQ？
- 如何处理重复消息？
- enqueue 成功但响应丢失怎么办？

### 防守回答

Kafka 在 Go 生态里客户端成熟，而且项目的任务流适合按 topic 分阶段。RocketMQ 在 Java 业务消息、事务消息等场景有优势，但为了简历更换 MQ 没有业务价值。

消费语义仍是 at-least-once，重复消息需要数据库状态、唯一约束和 lease 所有权共同防守。首次投递的一致性窗口还在审计中，所以不能说 Kafka 与数据库已经 exactly-once。

### 项目证据

- `internal/mq/producer.go`
- `internal/mq/consumer*.go`
- `internal/mq/retry.go`
- `internal/repository/task_lease*.go`
- `internal/repository/task_job.go`

### 当前限制

还没有实现通用 transactional outbox、CDC 或 Kafka EOS。

---

## 问题五：Redis 在项目中是什么定位？

### 可以直接说的回答

Redis 是高频临时状态和协调工具，不是业务事实源。项目用它做分布式锁、用户与路由维度的令牌桶、上传分片临时状态、最近聊天记忆，以及部分 AI 调用治理。任务终态、重试 lease、cleanup intent 和长期聊天记录都要落 PostgreSQL。

这个边界很重要：Redis 丢数据时可以通过 PostgreSQL 或客户端重新建立临时状态；如果把唯一的任务事实只放 Redis，故障后就没有可靠恢复依据。

### 可能追问

- Redis 故障时限流怎么处理？
- 为什么分布式锁不能只设置一个固定过期时间？

### 防守回答

当前限流选择 fail-open，因为限流是保护能力，不应该在 Redis 故障时把所有核心请求都变成不可用；同时会记录 fail-open 指标和结构化错误。长任务锁使用 lease 生命周期而不是依赖一个拍脑袋的固定 TTL，防止任务还没结束锁就提前失效。

### 项目证据

- `internal/middleware/ratelimit.go`
- `internal/pkg/lock/redis_lock.go`
- `internal/service/chat_memory.go`
- `internal/service/media_chunk_upload.go`

### 当前限制

现有分片上传状态仍以客户端 file hash 为核心标识，下一阶段会增加绑定用户和 manifest 的服务端 upload session。

---

## 问题六：为什么保持模块化单体，而不是微服务？

### 可以直接说的回答

当前项目的主要复杂度在长任务恢复、外部 AI 失败、对象生命周期和 RAG 一致性，不在团队协作或独立扩缩容。拆成微服务会立刻引入服务发现、跨服务鉴权、分布式追踪、更多部署单元和跨服务数据一致性，但当前没有真实业务收益。

我选择先在一个 Go 进程里维持清晰的 Handler、Service、Repository 和 adapter 边界，把 `main.go`、Consumer、media、chat 和 task lease 这些过大的文件按职责拆开。将来如果某个 worker 有独立伸缩或资源隔离需求，可以复用现有 package 边界拆进程，而不是现在提前制造网络边界。

### 可能追问

- 模块化单体如何防止再次变乱？
- 什么条件下你会真的拆服务？

### 防守回答

维护规则是每个持久化状态只有一个 owner，路由不构造依赖，Handler 不写数据库状态机，Repository 管事务，后台组件接受统一 context。新抽象必须有真实替换点，不能看到一个调用就创建 interface。

只有当独立部署、资源隔离、故障隔离或团队 ownership 的收益能够覆盖分布式成本时，我才会拆。例如 FFmpeg worker 的 CPU 资源与 API 节点差异足够大，并且实际出现独立扩缩容需求时，才值得拆进程。

### 项目证据

- `cmd/server/wiring.go`
- `cmd/server/router.go`
- `internal/service/media*.go`
- `internal/service/chat*.go`
- `internal/mq/consumer*.go`
- `docs/backend-maintenance-map.md`

### 当前限制

包边界清晰不代表所有跨组件一致性都已经解决，首次 Kafka 投递和上传 session 仍是下一阶段工作。

---

## 问题七：你认为当前最值得继续完善的地方是什么？

### 可以直接说的回答

第一是服务端 upload session。现有分片上传方便使用，但 session identity、文件 manifest 和最终 hash 仍过度信任客户端。下一步会让服务端生成 session ID、绑定 user ID，固化 filename、总大小、chunk size/count 和 expected hash，并在 merge 时流式计算实际 hash。Redis 适合高频分片状态，但 durable session 是否需要 PostgreSQL，要先根据失败矩阵决定。

第二是首次 Kafka 投递一致性。RetryScheduler 的补投失败已经可以 claim 后 restore，但新任务第一次数据库提交与 Kafka enqueue 之间仍有窗口。我会先写 DB rollback、commit 后 enqueue fail、message success response lost、process crash 和重复请求矩阵，再在专用 durable dispatch intent 与小型 outbox 中选最小方案。

URL 下载不是简历主链路，只维持已有 allowlist、DNS 安全检查和执行前复检，不继续投入复杂的平台化能力。

### 不能夸大的内容

- 不能说远端已经完成 PostgreSQL 迁移；
- 不能说实现了 exactly-once 或 transactional outbox；
- 不能说 pgvector 与 `video_chunks` 在一个跨阶段事务中；
- 不能说已经实现 rerank、Function Calling 或真正逐 token streaming，除非代码与测试之后发生变化；
- 不能把 URL 下载描述为生产级通用下载平台。

### 维护入口

- `AGENTS.md`
- `MEMORY.md`
- `docs/backend-maintenance-map.md`
- `docs/backend-optimization-roadmap.md`（本地维护文件）
- `docs/troubleshooting-and-interview-notes.md`
