# 专题 11：异步任务可观测性与排障路径

## 1. 面试口语答案

> 异步任务最怕只看到“失败”两个字。VidLens 用 `task_id` 定位业务对象，用 `trace_id` 串联 HTTP、Kafka payload 和结构化日志，再通过 `video_tasks` 看聚合阶段、`task_jobs` 看具体动作、ASR chunk 表看分片复用、AI call log 看 provider 调用。错误会保存 code、摘要、retry count 和 `next_retry_at`，因此能判断是等待自动恢复、已经 dead，还是确定性失败。
>
> 运行层还有 `/healthz` 和 `/readyz`：liveness 只判断进程，readiness 检查 PostgreSQL、Redis、MinIO、Kafka 和当前 vector backend。Prometheus `/metrics` 运行在独立管理监听器，指标覆盖 task stage/duration、retry/dead、Kafka job、ASR chunk、AI 调用/token/cost、RAG 和 rate-limit。它已经不是“完全没有指标”，但也不能夸大成完整 OpenTelemetry/APM。

## 2. 标准排障顺序

```text
1. 取得 user_id + task_id + trace_id
2. 查 video_tasks：status/stage/last_job/error/lease
3. 查 task_jobs：具体 job、retry_count、next_retry_at、token/lease
4. 若是 ASR：查 transcription_chunks 的分片状态与文本长度
5. 若是 AI：查 ai_call_logs、provider/model、HTTP 分类和 usage
6. 若是 RAG：查 video_chunks、rag index status、vector manifest/audit、citations
7. 查同 trace_id 的结构化日志和 Prometheus 指标
8. 判断：等待重试、人工修配置、重建索引或进入代码修复
```

## 3. 典型问题

### 长视频转写过短怎么查？

> 先比较视频时长、音频切片数量、完成分片数和最终文本长度，再查每段 ASR 调用。这个项目历史上确实遇到过“请求成功但只返回前一部分”，最终改成 16k 单声道低码率音频和 300 秒分段 ASR，而不是只调大请求上限。

### Kafka 消息为什么没有继续推进？

> 先看对应 job 是否 queued/running/failed，lease 是否过期，错误是否已经写入 retry 状态，再看 consumer group 和 broker。不能一上来就重发消息，否则可能与仍存活的 lease 并发执行。

### RAG 回答不准怎么查？

> 分四层：ASR 原文是否完整；chunk/hash/model 是否正确；向量和关键词候选及 RRF 排名是否合理；最终 prompt/citations 是否包含证据。`rag-audit` 用于检查关系事实和向量投影，不把 LLM 回答错误都归因于向量库。

### trace_id 能证明分布式追踪吗？

> 不能。它是应用级 correlation ID，能串联请求、消息和日志；当前没有完整 span、采样和跨组件 trace backend。

## 4. 代码证据

- `internal/observability/`：结构化上下文、日志脱敏和 Prometheus 指标。
- `cmd/server/health.go`：health/readiness 模型。
- `cmd/server/metrics_server.go`：独立管理监听器。
- `internal/model/task*.go`、`internal/model/ai_call_log.go`：可查询状态。
- `internal/mq/consumer_*.go`、`internal/mq/retry.go`：阶段指标和失败记录。
- `cmd/rag-audit/`：关系事实与向量投影审计。

## 5. 当前限制

- 没有完整 OTel tracing、集中日志平台、告警规则和 dashboard 交付物。
- `trace_id` 传播覆盖主要 HTTP/Kafka 路径，但不能声称所有外部命令和 provider 都有 span。
- Prometheus 指标说明“可抓取”，不等于远端已经部署 Prometheus server 和告警系统。
