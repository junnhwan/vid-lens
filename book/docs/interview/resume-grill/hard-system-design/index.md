# 系统设计压力面拷打

> 面对“规模变大怎么办”，先拆资源瓶颈和故障边界，再给基于指标的演进方案。下面都是未来设计题，不能反向声称已有对应流量。

## 1. 1000 个用户同时上传和问答，先扩哪里？

我不会先回答“加机器”，而会把链路拆成上传带宽与 MinIO、Kafka backlog、FFmpeg/ASR worker、Embedding/LLM 外部额度、PostgreSQL 状态与 pgvector 查询。它们的扩容方式不同。

第一步是建立可观测基线：入口速率、队列深度、各 stage latency/失败率、provider 429/5xx、对象存储吞吐、DB pool 和慢查询。之后按瓶颈处理：入口限流，Kafka 增 partition，download/transcribe/rag worker 分组扩容，外部 AI 加用户级额度和并发上限。没有这些数据时，水平扩 API 可能只会更快压垮下游。

**当前边界：** 没有 1000 用户压测证据，只能讲设计方法。

## 2. 10GB 视频怎么支持？为什么现在不能说支持？

10GB 不是改 `MaxFileSize`，它会同时影响上传恢复、服务端临时空间、hash 计算、MinIO multipart、FFmpeg、处理时长、备份和删除成本。VidLens 是内容理解，不是网盘，应先判断是否需要长期保存原视频，很多场景只需音频、低清预览和转写。

要正式支持，需要 durable upload session、动态 chunk、并发和磁盘配额、对象存储直传或 multipart、流式服务端 hash、处理时长限制、对象生命周期、全链路压测和故障注入。当前只有现有分片协议和 ASR 分段，不满足这些证明条件。

## 3. Kafka topic、partition 和 consumer 怎么扩？

Download、transcribe、analyze、rag_index 的资源模型不同，应该按 job type 隔离 topic/consumer 和并发预算。增加 consumer 实例前必须保证 partition 数允许并行；增加 worker 也必须受下游 ASR/Embedding 配额约束，否则只会放大 429 和重试风暴。

同一 task 的阶段顺序不靠 partition 假设，而靠 task/job 状态、lease 和允许的状态转换。Kafka 可能重复投递，consumer 仍需幂等。首次 task/job commit 与 enqueue 的一致性窗口尚待 durable dispatch intent 或小型 outbox 解决。

**证据：** `internal/mq/producer.go`、`internal/mq/consumer_*.go`、`internal/repository/task_lease_*.go`。

## 4. Redis 挂了，哪些功能受影响？

Redis 是协调和缓存，不是核心事实源：

- token bucket 当前可 fail-open，但需要告警，否则成本保护失效；
- lock 失效会降低并发合并和重复处理保护，PostgreSQL unique/条件更新仍是物理兜底；
- 最近聊天可以从 PostgreSQL 回源；
- 上传临时进度丢失会影响续传体验，这正是 durable upload session 要解决的问题；
- 已完成 task、转写、摘要、RAG index 和 chat 不应因 Redis 丢失而消失。

回答时不能笼统说“Redis 挂了数据全丢”或“完全无影响”。

## 5. PostgreSQL 是单点怎么办？

PostgreSQL 是业务事实源，挂掉后 task、profile、chat、RAG source 与 projection 基本无法可靠读写，不能像缓存一样 fail-open。生产演进应覆盖备份恢复演练、托管 HA 或主从、连接池、慢查询与锁等待监控、schema migration、容量规划，以及写入幂等。

pgvector 与业务表同库减少了组件数量，但也意味着需要共同规划 CPU、内存、I/O 和备份。若向量查询真正影响 OLTP，再依据监控评估只读副本、索引策略或专用向量系统，而不是提前拆库。

**当前边界：** 本项目证明了本地单库迁移和集成，不代表已有 PostgreSQL HA。

## 6. MinIO 或向量删除失败怎么补偿？

当前已经不是“请求里同步删完所有资源”。删除接口先在 PostgreSQL transaction 中创建 `task_cleanup_jobs` durable intent 并 soft-delete task；scheduler 用 token/lease 认领，删除 pgvector projection，争取 shared asset owner，再清 MinIO 和 Redis，最后完成关系数据收尾。

任一步失败会保存 last error 和 next retry time；lease 过期后可重新认领；外部 delete 需要幂等，资源不存在可视为成功。这是针对 task cleanup 的有限 durable workflow，不是全局 Saga engine。

**证据：** `internal/service/task_cleanup*.go`、`internal/repository/task_cleanup_job.go`、`internal/repository/asset.go`。

## 7. RAG 数据量变大，Go 侧 BM25 怎么替换？

当前 BM25 读取单视频 chunks 后在 Go 内计算，优点是简单、可测、少依赖。跨视频或大语料后，扫描成本和中文检索能力会成为限制。

演进顺序应由评测驱动：先固定 Recall@K、MRR、citation hit 和 latency 基线，再比较 PostgreSQL Full Text Search/扩展、Bleve 或 OpenSearch。RRF 可继续作为融合层，只替换 keyword candidate source。不能因为简历想多写组件就直接上 Elasticsearch。

## 8. 从单体到微服务，最先拆什么？

当前不建议先拆用户或配置服务。真正可能有独立资源边界的是 worker：download 依赖网络和 yt-dlp，transcribe 依赖 FFmpeg/ASR，rag index 依赖 Embedding 和数据库向量写入。只有当 Web 与 worker 的部署、扩容或故障隔离需求真实出现时，才考虑将 worker 独立进程化。

拆分后 PostgreSQL 仍可暂时作为协调事实源，Kafka 传任务，worker 通过 lease claim 并回写状态。首先要解决首次投递一致性、配置分发和可观测性，而不是把单体目录机械拆成多个服务。

**当前边界：** 目前仍是模块化单体，consumer 与 API 在同一部署单元启动。
