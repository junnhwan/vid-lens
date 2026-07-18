# 源码走读总览

> 从这里理解当前实现，不要从旧的行号、文件行数或整段复制代码开始。架构事实发生变化时，优先更新本页和 `docs/backend-maintenance-map.md`。

## 当前技术边界

| 维度 | 当前选择 |
|---|---|
| 语言与 HTTP | Go 1.24、Gin |
| 数据访问 | GORM |
| 正式关系数据库 | PostgreSQL |
| 向量检索 | PostgreSQL + pgvector |
| 消息队列 | Kafka（segmentio/kafka-go） |
| 临时状态与协调 | Redis（go-redis） |
| 对象存储 | MinIO |
| 媒体处理 | FFmpeg；URL 辅助下载使用 yt-dlp |
| 前端 | Vue 3 + Vite，仅作为验证和展示面 |

MySQL 与 Milvus 都不是默认运行时：MySQL 只保留迁移审计和回滚资产，Milvus 只保留向量回滚适配。远端 PostgreSQL 切换尚未由当前仓库证据证明。

## 推荐阅读顺序

1. [当前架构与启动流程](/source/architecture/)
2. [数据模型](/source/data-model/)
3. [Repository 与事务](/source/repository/)
4. [媒体上传](/source/media-upload/)
5. [Kafka 异步处理](/source/kafka-async/)
6. [RAG 索引与检索](/source/rag-pipeline/)
7. [安全边界](/source/security/)

遇到实现与文档冲突时，以源码和测试为准，并修正文档，不要为兼容旧文档去恢复已经淘汰的 API。

## 当前分层

```text
HTTP / SSE
  -> Handler：协议解析、认证上下文、错误映射
  -> Service：用例编排、业务规则、跨依赖恢复语义
  -> Repository：PostgreSQL 查询、事务和持久化状态机
  -> Adapter：Kafka / Redis / MinIO / AI / FFmpeg / pgvector
```

关键入口：

| 关注点 | 代码入口 |
|---|---|
| 生命周期 | `cmd/server/main.go` |
| 依赖组装 | `cmd/server/wiring.go` |
| HTTP 路由 | `cmd/server/router.go` |
| 严格配置 | `internal/config/loader.go`、`validation.go` |
| 数据模型 | `internal/model/model.go` |
| 任务状态 | `internal/repository/task_lease*.go` |
| 上传 | `internal/service/media*.go` |
| Kafka | `internal/mq/consumer*.go`、`retry.go` |
| RAG | `internal/service/rag_*.go`、`retrieval_fusion.go` |
| 向量后端 | `internal/vector/factory.go`、`pgvector.go` |

## 主链路

```text
文件上传
  -> MinIO 保存对象
  -> PostgreSQL 保存 asset / task / task_job
  -> Kafka 调度 analyze 或 transcribe
  -> Consumer 认领 processing lease
  -> FFmpeg / ASR / LLM
  -> PostgreSQL 保存 transcription / summary
  -> Kafka 调度 rag-index
  -> video_chunks + embedding
  -> pgvector projection
```

问答链路：

```text
用户问题
  -> query rewrite（按配置）
  -> pgvector 向量候选 + PostgreSQL chunk 关键词候选
  -> RRF 融合
  -> 邻居 chunk 扩展
  -> 带引用的 prompt
  -> LLM 回答
```

`video_chunks` 是事实源，pgvector 是可重建投影；两者当前分两个 PostgreSQL transaction 提交，不要描述成整体强一致。

## 维护原则

- PostgreSQL 是不可重建业务事实的 owner；
- Redis 只保存可过期或可重建状态；
- Handler 不写持久化状态机；
- Repository 管理需要事务和并发条件更新的逻辑；
- 外部副作用失败必须有 retry、intent、lease 或显式人工恢复入口；
- 不为没有真实第二实现的调用创建通用 interface；
- 不复制大段源码到文档，避免源码与说明双重维护；
- 后端变更至少运行 `go test -count=1 ./...`，阶段收尾再运行 race、vet、staticcheck、deadcode 和命令构建。
