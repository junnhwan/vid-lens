# VidLens 简历项目当前版本

> 本文件是简历表述的唯一维护入口。实现证据以代码、`MEMORY.md` 和 `docs/backend-maintenance-map.md` 为准；历史简历草稿不得覆盖这里的当前事实。

## 推荐版本

### 项目名称

VidLens —— AI 长视频内容理解与智能问答后端

### 技术栈

Go、Gin、GORM、PostgreSQL、pgvector、Redis、Kafka、MinIO、FFmpeg、OpenAI-compatible AI API、Vue 3

MySQL 和 Milvus 仅作为迁移观察期回滚资产，不写入正式技术栈。

### 项目简介

面向长视频的内容理解后端，支持分片上传与断点续传、分段 ASR、AI 摘要和带引用的视频问答。系统使用 PostgreSQL 持久化业务状态和 RAG 事实数据，通过 pgvector、关键词检索与 RRF 构建混合检索，并用 Kafka、任务 lease 和失败重试处理分钟级、依赖外部 AI 服务的异步流程。

### 项目经历

- 使用 Kafka 将视频下载、ASR、摘要和 RAG 索引等长耗时步骤移出 HTTP 请求，并在 PostgreSQL 中保存 task/job 阶段、processing lease、失败原因与重试状态，支持重复消息下的状态校验和异常恢复。
- 采用分片上传与断点续传机制，使用 Redis Set 记录已上传片号和 TTL，前端恢复时只补传缺失分片，并通过 MinIO 服务端合并避免上传中断后整文件重传。
- 将长音频按时长切分后逐片调用 ASR，单独保存片段状态和文本；任务恢复时复用已完成片段，仅重跑缺失或失败片段，避免外部 ASR 调用和处理中间结果全部重做。
- 对分析消费使用带 owner 校验与 WatchDog 续租的 Redis 分布式锁，并结合 PostgreSQL processing lease、唯一约束和业务幂等降低同一视频并发重复处理风险；分片合并使用按文件 MD5 加锁的 Redis 锁避免并发合并。
- 以 ASR 转写原文作为 RAG 数据源，将 `video_chunks` 作为 PostgreSQL 事实表、pgvector 作为可重建向量投影，结合 Go 侧 BM25-style 关键词召回和 RRF 融合，按用户、任务和 embedding model 隔离并返回引用片段。
- 基于 Redis Lua 令牌桶限制用户级高成本 AI 请求，并记录任务阶段、AI 调用、重试、ASR chunk 和 RAG 检索等 Prometheus 指标，便于定位外部依赖失败、重试放大和调用成本问题。

## 精简版（简历版面不足时）

- 基于 Kafka 与 PostgreSQL task/job 状态机异步执行视频下载、分段 ASR、摘要和 RAG 索引，结合 processing lease、错误分类和退避重试处理重复消费与外部依赖失败。
- 使用 Redis Set 保存临时分片进度并通过 MinIO 服务端合并，支持断点续传、缺片补传和合并后文件大小校验。
- 使用 PostgreSQL + pgvector 保存 RAG 事实数据和向量投影，结合 BM25-style 关键词召回、RRF 融合与 citations 实现单视频可追溯问答。
- 通过 Redis owner lock、Lua 令牌桶、用户级 BYOK 和 Prometheus 指标控制并发重复处理、AI 调用成本并辅助故障定位。

## 面试边界

可以陈述：

- 当前本地正式架构是 PostgreSQL + pgvector 单库；server 不连接 MySQL，也没有双写。
- pgvector 与业务表同库，降低了两套关系数据库的部署、备份和迁移成本。
- RAG source 与 projection 仍按阶段提交；“同库”不等于索引全链路处于同一事务。
- Kafka 适合当前 Go 异步处理链路，但单 broker、本地规模不能包装成生产高并发或高可用。
- URL 下载只是辅助能力，不作为核心简历卖点。

不能陈述：

- 远端环境已经完成 PostgreSQL 迁移；
- 首次 task/job 创建和 Kafka enqueue 已由 transactional outbox 保证；
- Kafka exactly-once、生产级零停机迁移或全链路强一致；
- MySQL/Milvus 已彻底下线或删除；
- RAG 引用可以消除模型幻觉；
- 已上线模型 rerank、Cross-Encoder 或专业搜索引擎 BM25。

## 证据导航

- 异步任务：`internal/mq/`、`internal/repository/task_lease*.go`
- 分片上传：`internal/service/media_chunk_upload.go`、`internal/handler/media.go`、`internal/storage/minio.go`
- PostgreSQL：`cmd/server/database.go`、`internal/database/postgres.go`
- RAG：`internal/service/rag_*.go`、`internal/repository/video_chunk.go`
- pgvector：`internal/vector/pgvector.go`、`internal/vector/factory.go`
- 指标：`internal/observability/metrics.go`、`cmd/server/metrics_server.go`
- 真实故障：`docs/troubleshooting-and-interview-notes.md`
