# VidLens 简历项目面试防守总览

> 当前面试入口。先讲 VidLens 的具体问题，再讲技术；详细实现只链接专项文档，不在本文件复制另一套事实。

## 1. 一句话定位

VidLens 是一个 Go 实现的 AI 长视频内容理解后端：视频进入 MinIO 后，由 Kafka 驱动分段 ASR、摘要和 RAG 索引，PostgreSQL 保存业务事实与任务状态，pgvector 保存可重建向量投影，最终提供带引用的单视频问答。

## 2. 30 秒介绍

> 我做的不是一次模型调用，而是一条长视频处理链路。大文件上传通过 PostgreSQL durable session 和 MinIO 支持断点恢复；长音频切片调用 ASR，并持久化每片结果；分钟级处理通过 Kafka 异步执行，PostgreSQL task/job 状态和 lease 处理重复消费与失败恢复；问答使用 ASR 原文构建 pgvector + BM25-style + RRF 混合检索，并返回引用片段。项目重点是让耗时、昂贵且容易失败的 AI 流程具备可恢复状态，而不是包装成高并发生产系统。

## 3. 两分钟主线

> 上传方面，PostgreSQL 保存绑定用户的 session、不可变 manifest、chunk ledger 和完成 lease，MinIO 只负责保存字节。服务端校验每片大小和 SHA-256，合并时使用 `io.Pipe` 流式读写并重新计算完整 MD5。Redis 不参与上传正确性，旧 Redis Set 和 ComposeObject 协议已经退役。
>
> 处理方面，HTTP 只创建业务状态并触发后台流程，Kafka consumer 获取 PostgreSQL processing lease 后执行下载、转写、摘要或索引。长音频切片后逐片保存，重跑时可以复用已经成功的 ASR 片段。RetryScheduler 已有 dispatch lease 和 enqueue 失败恢复；首次 task/job 入库与 Kafka enqueue 的一致性窗口仍在专项治理，不能说已经有 outbox。
>
> 问答方面，知识源是 ASR 转写原文，不是摘要。`video_chunks` 是 PostgreSQL 文本事实表，pgvector 表是可以重建的向量投影；查询时向量召回与 Go 侧 BM25-style 关键词召回经 RRF 融合，再把片段作为 prompt context 和 citations 返回。正式运行只使用 PostgreSQL + pgvector，MySQL 和 Milvus 只保留作观察期回滚。

## 4. 常见质疑

### 这是不是 AI 套壳？

> 如果只是把视频发给模型再返回文本，那确实是套壳。这个项目的工程工作主要在模型调用之外：大文件的 durable upload session、分段 ASR 结果复用、Kafka 消费 lease、失败分类与重试、BYOK、RAG 数据隔离和可观测性。这些机制分别对应我实际遇到的长音频截断、状态缺失、外部服务失败和重复处理问题。

### 为什么需要 Kafka？

> HTTP 不适合等待几分钟的视频处理，也不能承担外部 AI 服务波动后的恢复状态。Kafka 在这里负责异步传递工作，PostgreSQL 负责业务事实和 lease。当前规模也可以选择 PostgreSQL 队列；我选择 Kafka 是为了实践清晰的生产消费边界和积压观测，不会据此声称项目有海量流量或生产高可用。

### 为什么从 MySQL/Milvus 改成 PostgreSQL + pgvector？

> 原方案需要同时维护业务关系库和独立向量库。当前数据量没有足够业务价值支撑两套持久化系统的部署、备份、迁移和一致性成本，所以迁移到 PostgreSQL 单库：业务表和 `video_chunks` 是事实数据，pgvector 是同库的向量投影。Milvus 的能力没有问题，但对这个项目偏重；MySQL/Milvus 目前只作为观察期回滚资产保留。

### Redis 挂了，上传进度会不会丢？

> 当前不会因为 Redis 丢失 upload session 事实。上传状态在 PostgreSQL，分片字节在 MinIO。Redis 故障会影响限流、分析锁和最近聊天缓存，但不应破坏已接受分片的 ledger。上传 complete 的并发所有权由 PostgreSQL token/lease 管理。

### 同库是不是 RAG 全链路强一致？

> 不是。`video_chunks` 和 pgvector 表虽然都在 PostgreSQL，但索引服务仍按“提交 chunk 事实、发布向量投影、更新索引状态”分阶段执行。投影失败后依靠 failed 状态、审计和 reindex 恢复，不能说全部步骤在一个事务里。

## 5. 当前限制

- 本地完成 PostgreSQL/pgvector 迁移和 smoke；远端部署迁移尚未被证明。
- 首次 task/job 创建与 Kafka enqueue 尚不能声称 transactional outbox 或原子提交。
- 单 broker 和单机依赖不是生产级高可用。
- 在线检索是 pgvector + Go 侧 BM25-style + RRF；没有专业搜索引擎 BM25 或模型 rerank。
- citations 提高可追溯性，但不能保证模型零幻觉。
- URL 下载只做辅助维护，不作为简历核心能力。

## 6. 权威导航

1. 当前简历：[`resume-final-draft.md`](./resume-final-draft.md)
2. 异步任务：[`resume-topics/01-kafka-async.md`](./resume-topics/01-kafka-async.md)
3. Redis 锁与内容复用：[`resume-topics/02-redis-lock-md5-reuse.md`](./resume-topics/02-redis-lock-md5-reuse.md)
4. durable upload session：[`resume-topics/03-chunk-upload-resume.md`](./resume-topics/03-chunk-upload-resume.md)
5. RAG：[`resume-topics/05-rag-hybrid-retrieval.md`](./resume-topics/05-rag-hybrid-retrieval.md)
6. 资源排查：[`resume-topics/07-cpu-memory-io.md`](./resume-topics/07-cpu-memory-io.md)
7. 真实故障：[`troubleshooting-and-interview-notes.md`](./troubleshooting-and-interview-notes.md)
8. 文档事实源：[`README.md`](./README.md)
