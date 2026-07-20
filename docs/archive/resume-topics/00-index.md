# 最终简历专题准备索引

> 简历正文只维护在 `../resume-final-draft.md`。本目录负责面试追问，不再复制另一份简历和架构事实。

## 建议阅读顺序

1. `01-kafka-async.md`：异步链路、offset、processing lease 和失败窗口。
2. `06-task-failure-governance.md`：错误分类、RetryScheduler 和最终状态。
3. `03-chunk-upload-resume.md`：Redis 分片状态与 MinIO 合并，是上传唯一权威专题。
4. `02-redis-lock-md5-reuse.md`：分析消费锁、分片合并锁、内容指纹和数据库幂等。
5. `05-rag-hybrid-retrieval.md`：PostgreSQL + pgvector、BM25-style、RRF 和 citations。
6. `04-redis-lua-rate-limit.md`：高成本接口限流与 Redis 故障策略。
7. `08-large-video-handling.md`、`07-cpu-memory-io.md`：大视频边界和资源排查。
8. `09-data-model-lifecycle.md`、`10-delete-task-cleanup-consistency.md`：数据生命周期和耐久化清理。
9. `11-task-observability-debugging.md`、`13-testing-strategy.md`：证据、指标和测试边界。
10. `12-auth-user-isolation.md`：task、profile、session 和 RAG scope 隔离。

## 当前六个核心面试点

- Kafka + PostgreSQL task/job 状态与 processing lease；
- Redis Set 分片进度 + MinIO 字节存储与服务端合并；
- 分段 ASR、片段结果持久化与失败片段复用；
- Redis owner lock、WatchDog 与数据库幂等的职责边界；
- PostgreSQL + pgvector + BM25-style + RRF 混合检索；
- Redis Lua 限流、AI 调用治理与 Prometheus 可观测性。

## 临面前检查

- 能解释为什么 RAG 使用 ASR 原文而不是摘要。
- 能解释 PostgreSQL 是事实源、pgvector 是可重建投影，并承认分阶段提交边界。
- 能解释上传为什么不再依赖 Redis，以及 complete token/lease 如何防并发完成。
- 能解释 Kafka offset、processing lease、RetryScheduler 和首次 enqueue 窗口不是同一问题。
- 能说明 MySQL/Milvus 是观察期回滚资产，而不是当前正式技术栈。
- 不声称远端迁移、outbox、exactly-once、模型 rerank或生产级 URL 下载安全已经完成。

## 统一回答公式

具体问题 → 不处理会怎样 → 当前调用链 → 状态 owner / lease / 事务边界 → 故障恢复 → 代码证据 → 当前限制 → 下一步。

不要从“我用了 Kafka、Redis、pgvector”开场；先说明 VidLens 的哪个具体问题需要这项机制。
