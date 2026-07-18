# 可靠性与系统设计拷打

> 回答结构固定为：失败类型 → 当前状态 owner → 恢复方式 → 未完成边界。避免用“重试、降级、最终一致性”几个空词覆盖所有故障。

## 1. 外部 AI 服务不稳定，怎么处理？

外部错误先分类。Timeout、429、部分 5xx 和网络抖动可能重试；缺 BYOK、鉴权失败、配置非法、embedding 维度不符通常等待也不会恢复，应直接失败。Provider error 会被包装成 typed error，并结合 Retry-After 与 retry budget 决定下一次调度。

Consumer 将失败写入 task/job，包括 stage、retry count、next retry time 和 last error；Observed client 记录 provider、model、耗时与用量 metadata。重试到期后由 RetryScheduler 使用 dispatch lease 补投，不在 consumer 内长时间 sleep。

**证据：** `internal/ai/retry_policy.go`、`internal/mq/retry.go`、`internal/service/ai_observer.go`。

## 2. BYOK 如何保护 API Key？

公开部署不能默认消耗维护者额度。用户分别配置 ASR、LLM 和 Embedding provider；Key 用 AES-GCM 加密后保存，model ciphertext 字段禁止 JSON 输出，列表接口只返回 mask，真正创建 client 前才解密。

这能保护存储和接口边界，但不是专业 KMS：服务端主密钥管理、轮换、审计权限和内存驻留仍需要更完整方案。

**证据：** `internal/model/ai_profile.go`、`internal/pkg/secret/crypto.go`、`internal/service/ai_profile.go`。

## 3. URL 下载 SSRF 做到哪一层？

当前仅维护第一层边界：http/https、允许 host、拒绝 localhost，DNS 后拒绝 private/loopback/link-local/multicast，执行前复检，并清洗日志 URL。Redirect chain、DNS rebinding、硬下载体积/耗时和 cookies 隔离仍不足。

该功能不进入简历主线，后续只修明确安全问题，不继续为了它扩充技术栈。不能描述成生产级下载平台。

**证据：** `internal/pkg/remoteurl/`、`internal/service/remote_video_url.go`、`internal/mq/consumer_download.go`。

## 4. AI 调用审计和 quota/billing 有什么区别？

审计回答“谁在何时调用了哪个 provider/model、耗时多少、成功或失败、估算输入输出量”；quota 回答“用户今天还能否继续调用”；billing 还需要价格版本、预扣/结算、余额、退款和对账。

VidLens 已有 call log 和 daily usage aggregation，但没有完整额度拒绝、价格表和资金事务，因此只能说有成本观察基础，不是计费系统。

**证据：** `internal/model/ai_usage_ledger.go`、`internal/repository/ai_call_log.go`、`internal/service/ai_usage_governor.go`。

## 5. 服务重启后哪些状态能恢复？

- PostgreSQL：task/job、retry lease、cleanup intent、转写、摘要、RAG source/index、chat、profile 和调用审计，是 durable state；
- MinIO：正式媒体和临时 chunk objects；
- Kafka：已确认写入且未消费完成的消息可继续由 group 消费；
- Redis：锁、限流桶、上传进度和最近聊天是易失协调状态；
- pgvector：可由 `video_chunks` source 审计和重建的派生 projection。

风险点是首次 task/job 已 commit 但 Kafka enqueue 未成功；当前还没有 outbox。上传进度也尚无 durable upload session。这两项比笼统“服务可恢复”更值得主动说明。

## 6. PostgreSQL、Redis、Kafka、pgvector 某一个挂了怎么办？

**PostgreSQL：** 核心事实源不可用，多数写操作应失败，不能 fail-open。

**Redis：** 限流可能 fail-open，最近聊天可回源；lock 和分片进度能力退化，需要告警和明确错误。

**Kafka：** 新异步任务无法可靠投递；RetryScheduler enqueue 失败会恢复 dispatch 状态，但首次投递窗口仍待 outbox/intention。

**pgvector extension/projection：** 上传、转写和摘要仍有价值，RAG query/indexing 失败应记录可见状态；projection 可由 source audit/reindex。因为当前业务表和向量在同一个 PostgreSQL 实例，实例整体故障时不能假装两者独立可用。

**MinIO：** 新上传和媒体读取受影响；durable cleanup job 应保留并等待重试，不能先丢 intent。

## 7. 如何支持更大规模和大视频？

先定义业务限制和 SLO，再按链路容量做：

1. durable upload session、对象存储直传、并发与临时空间配额；
2. Kafka partition 与按 job type worker pool；
3. ASR/Embedding 用户级并发和预算控制；
4. PostgreSQL pool、慢查询、pgvector 索引和备份容量；
5. 固定 RAG 评测集，必要时替换 BM25 candidate source；
6. stage metrics、队列深度、provider 错误率和告警。

没有压测、成本和故障注入结果前，不能声称支持 1000 用户或 10GB 全链路。

## 8. 从简历项目推进到更可靠版本，当前优先级是什么？

已经完成的高风险闭环包括 RetryScheduler enqueue 恢复、durable task cleanup、本地 PostgreSQL + pgvector 迁移、RAG audit/reindex 和基础 readiness。

接下来优先：

1. 服务端生成并绑定用户的 durable upload session，补 manifest、hash、终态和恢复矩阵；
2. 审计首次 task/job commit 与 Kafka enqueue，选择专用 dispatch intent 或小型 outbox；
3. 保持 source/projection audit 和固定 RAG evaluation case；
4. 远端 PostgreSQL 迁移完成后经过观察期，再清理 legacy MySQL/Milvus 资产；
5. URL 下载只维持现有安全边界。

**事实入口：** `docs/backend-maintenance-map.md`、`docs/superpowers/audits/2026-07-17-plan-completion-audit.md`。
