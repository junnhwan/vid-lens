# 项目定位与总览拷打

> 本页回答“项目是什么、为什么不是 AI 套壳、技术选型是否合理”。详细实现进入各专题页，不在这里复制源码和易漂移行号。

## 1. 这个项目解决什么问题？

**直接回答：**

VidLens 面向长视频理解。用户真正需要的不是一次模型调用，而是把视频可靠地变成可查看、可检索、可追溯的内容：上传文件、后台处理、ASR 转写、摘要、RAG 建索引和带引用问答。

难点来自链路长且外部依赖多。大文件可能传输中断，FFmpeg 和 ASR 可能耗时几分钟，模型接口可能限流或鉴权失败，索引 source 与 projection 可能分阶段失败。项目围绕这些具体失败模式设计状态、重试、幂等和恢复能力。

**项目证据：** `internal/service/media_*.go`、`internal/mq/consumer_*.go`、`internal/service/rag_*.go`。

## 2. 两分钟项目介绍怎么说？

VidLens 是一个 Go 后端为主的 AI 视频理解项目。用户上传视频后，后端将对象保存到 MinIO，在 PostgreSQL 创建 task/job，并通过 Kafka 异步执行音频提取、分段 ASR、摘要和 RAG 索引。ASR 片段结果单独保存，失败重试时复用已完成片段；RAG 使用完整转写作为知识源，将 `video_chunks` 文本事实和 pgvector 向量投影保存到 PostgreSQL，查询时融合向量召回与 Go 侧 BM25，并返回 citations。

Redis 不保存核心业务事实，主要用于分布式锁、token bucket、上传临时状态和最近聊天缓存。用户可以通过 BYOK 配置自己的 ASR、LLM 和 Embedding 服务，Key 加密保存。前端只是触发和展示这些后端能力。

我会主动说明：本地 PostgreSQL + pgvector 迁移已经验证，远端尚不能声称完成切换；首次 Kafka 投递 outbox 和 durable upload session 仍是后续工作；URL 下载只是低优先级便利功能。

## 3. 为什么不是 AI 接口套壳？

如果只是套壳，请求进来后直接调用模型并返回即可；VidLens 的主要代码和故障都发生在模型调用前后：

- 大文件如何分片、合并、复用 asset；
- HTTP 如何快速返回，后台任务如何保存阶段；
- 重复消息和超时任务如何通过 lease 恢复；
- 长视频如何切片，并复用已完成的 ASR 结果；
- RAG source 与 projection 如何审计和重建；
- BYOK 如何加密、脱敏和按用户装配 provider；
- 删除 task 后如何最终清理向量、对象和 Redis 状态。

AI provider 是不稳定外部依赖，项目价值是围绕它构造可解释的业务状态和失败恢复，而不是中间件数量。

## 4. 为什么选 Go、Gin、GORM、PostgreSQL + pgvector？

**Go + Gin：** 适合当前单体 API、后台 goroutine 生命周期和 Kafka consumer；项目重点在后端，不需要为展示面引入更重框架。

**GORM：** 当前模型、事务和条件更新规模下可以提高开发效率，但关键 claim/lease 和 PostgreSQL 特性仍使用明确 SQL 或严格条件更新，不能只依赖 ORM 魔法。

**PostgreSQL + pgvector：** 第一版 MySQL + Milvus 要维护两套持久化系统。当前数据量没有证明需要专用向量集群，因此迁移到 PostgreSQL 单库，让业务表、RAG source 和向量 projection 共享一套部署、备份和审计边界。pgvector 是扩展，不是第二套数据库。

**Kafka：** 长任务需要持久异步传递和 consumer group；业务重试状态由 PostgreSQL 管理。它不是为了复制 Java 项目的 RocketMQ。

**Redis 与 MinIO：** Redis 负责短期协调，MinIO 负责大对象，各自解决关系数据库不适合承担的问题。

**项目证据：** `cmd/server/database.go`、`internal/database/postgres.go`、`internal/vector/factory.go`、`internal/vector/pgvector.go`。

## 5. 项目最值得讲的难点是什么？

1. **任务可靠性：** task/job 状态、processing lease、typed provider error、retry budget，以及 RetryScheduler enqueue 失败后的事务恢复。
2. **长视频 ASR：** 固定时长切片、片段状态、结果复用、最终拼接和日志定位。
3. **RAG 生命周期：** ASR 作为知识源、stable evidence ID、source/projection 分阶段发布、BM25 + RRF、citation snapshot、audit/reindex。
4. **资源生命周期：** task soft delete 先提交 durable cleanup intent，再由 scheduler 幂等清理外部资源。
5. **数据库迁移：** MySQL + Milvus 到 PostgreSQL + pgvector 的复制、独立审计、sequence 校准和停止 MySQL smoke。

这些内容都有源码、测试或故障记录，不需要用虚构并发量包装。

## 6. 如果面试官说“这只是拼组件”，怎么回应？

我会先承认组件都可替换，然后解释没有它时具体会坏在哪里：

- 没有持久异步队列，长任务会占住 HTTP，进程退出还会丢本地 goroutine；
- 没有关系状态和 lease，消息重复或 worker 崩溃后无法判断任务所有权；
- 没有对象存储，大视频会挤压业务数据库和单机磁盘；
- 没有 Redis 协调，并发合并和高成本接口保护会退化；
- 没有向量检索，只靠关键词很难覆盖语义表达；没有 BM25，又容易漏掉术语和数字；
- 没有 stable citation，回答无法回到原始转写证据。

组件不是卖点，组件对应的失败边界和取舍才是卖点。

## 7. 哪些内容不能强说？

- 不能说 Kafka exactly-once 或已经有 transactional outbox；
- 不能说 Redis lock 是 Redisson；
- 不能说所有 provider 都是真 token streaming；
- 不能说有 Cross-Encoder rerank、完整 quota/计费或大规模生产收益；
- 不能说 URL 下载已经生产级；
- 不能把第一版 MySQL + Milvus 说成当前默认架构；
- 不能从本地迁移结果推断远端已经切换 PostgreSQL；
- 不能把同一 PostgreSQL 实例中的 source/projection 说成一个 transaction。

事实边界见 `MEMORY.md` 和 `docs/backend-maintenance-map.md`。

## 8. Vue 前端是什么定位？

Vue 前端是验证和展示面：触发上传、查看任务阶段、展示转写与摘要、发起问答和显示 citations。它不是项目主线。面试被问到前端时，简要说明交互后应回到后端状态流、失败恢复和数据边界。
