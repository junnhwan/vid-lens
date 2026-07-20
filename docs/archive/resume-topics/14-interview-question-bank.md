# VidLens 面试题库（当前事实版）

> 用途：临面前自测，不复制各专题实现正文。答不清时沿链接回到对应专题和代码证据。

## 1. Kafka 异步任务

专题：[`01-kafka-async.md`](01-kafka-async.md)

- 为什么视频处理不能放在 HTTP 请求内？
- Kafka、`video_tasks` 和 `task_jobs` 分别负责什么？
- processing lease 为什么要有 token、expiry 和 version？
- 重复消息、过期 owner 和 poison message 分别怎么处理？
- RetryScheduler enqueue 失败为什么还能恢复？
- 当前首次 DB 写入与 enqueue 有什么缺口？为什么不能声称 outbox/exactly-once？
- Kafka 与 RocketMQ 在这个 Go 项目里的取舍是什么？

## 2. Redis lock、资产复用与限流

专题：[`02-redis-lock-md5-reuse.md`](02-redis-lock-md5-reuse.md)、[`04-redis-lua-rate-limit.md`](04-redis-lua-rate-limit.md)

- Redis owner lock 当前真正在哪条业务链路使用？
- 为什么数据库唯一约束仍是资产并发创建的最后防线？
- MD5 用于内容复用时能否承担安全校验？
- WatchDog 如何防误续租、误删除？锁失效后靠什么兜底？
- 令牌桶为什么需要 Lua？key 为什么包含路由和用户/IP？
- Redis 限流 fail-open 的收益和风险是什么？
- 请求限流、AI usage ledger 和计费系统有什么区别？

## 3. Redis 分片状态与断点续传

专题：[`03-chunk-upload-resume.md`](03-chunk-upload-resume.md)

- 为什么旧“全局 MD5 + Redis Set”协议已经退役？
- immutable manifest、exact chunk size 和 SHA-256 ledger 分别防什么？
- 同 index 同内容与不同内容重传的语义是什么？
- completion token/lease 如何处理进程崩溃和 stale reclaim？
- 为什么合并时使用 `io.Pipe`？它仍有什么网络带宽代价？
- PostgreSQL 最终事务失败后如何释放 claim？
- completed session 关联 task 被 cleanup 删除后还有什么缺口？

## 4. PostgreSQL + pgvector RAG

专题：[`05-rag-hybrid-retrieval.md`](05-rag-hybrid-retrieval.md)

- 为什么知识源使用 ASR 原文而不是摘要？
- `video_chunks` 与 pgvector rows 谁是事实、谁是投影？
- 向量检索为什么必须带 `user_id + task_id + embedding_model`？
- Go 侧 BM25-style 与专业搜索引擎 BM25 有什么区别？
- RRF 解决什么，不能解决什么？
- citations 是实时重查还是回答时保存的 retrieval snapshot？
- 为什么从 MySQL + Milvus 收敛到 PostgreSQL + pgvector？
- 同库是否意味着索引全链路强一致？
- Milvus 为什么仍在代码里？线上是否启用了模型 rerank？

## 5. 失败、删除与可观测性

专题：[`06-task-failure-governance.md`](06-task-failure-governance.md)、[`10-delete-task-cleanup-consistency.md`](10-delete-task-cleanup-consistency.md)、[`11-task-observability-debugging.md`](11-task-observability-debugging.md)

- 可恢复错误、确定性错误和 retry exhausted 如何区分？
- provider `Retry-After` 与本地阶梯退避如何合并？
- 为什么删除必须先保存 cleanup intent？
- 多个 task 共享 asset 时，最后删除者如何确定？
- PostgreSQL 与 MinIO 之间为什么采用最终一致而不是伪分布式事务？
- 用户报告失败时，task/job/chunk/AI log/metrics 的排查顺序是什么？
- 当前 Prometheus 做到了什么？为什么仍不能说完整 OTel/APM？

## 6. 数据、安全与测试

专题：[`09-data-model-lifecycle.md`](09-data-model-lifecycle.md)、[`12-auth-user-isolation.md`](12-auth-user-isolation.md)、[`13-testing-strategy.md`](13-testing-strategy.md)

- asset 和 task 为什么拆表？task 和 job 为什么又拆开？
- 哪些并发不变量由唯一约束、行锁或 CAS 保证？
- JWT 中的 userID 如何进入 repository 查询？
- 密码为什么 bcrypt，BYOK Key 为什么 AES-GCM？
- RAG 如何防止跨用户访问？当前 Redis 分片协议的用户隔离边界是什么？
- SQLite/miniredis/sqlmock 分别能证明什么、不能证明什么？
- 哪些行为必须由真实 PostgreSQL/pgvector 集成测试验证？
- 当前最大的 E2E 和部署验证缺口是什么？

## 7. 自测要求

每题至少能说出：

```text
具体业务问题
-> 当前代码路径
-> 状态 owner / 事务或 lease 边界
-> 一个失败窗口
-> 当前限制
```

如果回答里出现“生产级、完全一致、绝不丢、性能大幅提升”，必须马上给出测试、指标或代码证据；给不出就降低表述。
