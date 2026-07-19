# 简历主线：四个核心能力怎么讲

> 本页只保存四条主线的口述摘要和稳定证据入口，避免复制专题页与源码行号。当前数据库口径是 PostgreSQL + pgvector；MySQL + Milvus 只用于讲第一版架构和迁移取舍。

## 四个深入专题

| 简历主线 | 深入准备 | 当前状态 |
|---|---|---|
| Kafka 异步任务、状态与重试 | [Kafka 异步与重试](/interview/resume-grill/resume-core/kafka-retry/) | 已实现；首次投递 outbox 待完善 |
| 长视频分段 ASR 与结果复用 | [分段 ASR](/interview/resume-grill/resume-core/asr-chunks/) | 已实现 |
| 分片上传、Redis 状态与 MinIO 合并 | [分片上传](/interview/resume-grill/resume-core/chunk-upload/) | 现有协议已实现；服务端 upload session 待完善 |
| pgvector + BM25 + RRF 与引用 | [混合检索](/interview/resume-grill/resume-core/hybrid-rag/) | 已实现并完成本地迁移验证 |

## 两分钟项目介绍

**面试官：请介绍一下这个项目。**

VidLens 是一个 Go 编写的 AI 视频理解后端。用户上传视频后，系统将大文件保存到 MinIO，创建任务并投递 Kafka；后台完成 FFmpeg 音频处理、分段 ASR、摘要和基于 ASR 转写的 RAG 索引，最后提供带引用片段的视频问答。

我主要解决的不是模型 API 怎么调用，而是长时间、高成本链路的工程问题：大文件如何断点上传，HTTP 如何快速返回，后台任务如何记录阶段和重试，长视频如何按片段复用结果，以及检索结果如何回到原始转写证据。当前本地正式关系数据库统一为 PostgreSQL，pgvector 保存向量投影，Redis 做锁、限流与临时状态，Kafka 做异步传递，MinIO 保存媒体对象。

我会主动说明边界：这是有测试、迁移审计和故障复盘的项目级后端，不是大规模生产平台；远端 PostgreSQL 切换、首次 Kafka 投递 outbox、durable upload session、完整计费和生产级 URL 下载安全仍不能说已经完成。

**稳定证据入口：**

- 运行时装配：`cmd/server/main.go`、`cmd/server/wiring.go`
- 核心模型：`internal/model/`
- 媒体主链路：`internal/service/media_*.go`
- 异步处理：`internal/mq/consumer_*.go`、`internal/mq/retry.go`
- RAG：`internal/service/rag_*.go`、`internal/vector/pgvector.go`
- 当前维护边界：`MEMORY.md`、`docs/backend-maintenance-map.md`

## 一、Kafka 异步任务、状态与重试

### 30 秒回答

视频处理会经过 FFmpeg、ASR、摘要和索引，耗时与失败概率不可控，所以 HTTP 层只做校验、创建 task/job 和投递消息。Consumer 根据 job type 执行阶段，PostgreSQL 保存用户可见状态、stage、retry count 和 next retry time；processing lease 与幂等写入约束重复消费。

RetryScheduler 使用 dispatch token/version 认领到期任务；认领后 Kafka enqueue 失败时，事务恢复 task/job 的可调度状态，避免任务永久卡在 running。Kafka 只提供异步传递，不应说成 exactly-once 或完整业务工作流。

### 追问防守

- **为什么 Kafka，不是 RocketMQ？** Go 客户端和现有链路成熟，业务重试由 PostgreSQL 状态实现；承认 RocketMQ 在 Java 延时重试场景的优势。
- **消息重复怎么办？** 依赖 task/job 状态、lease、stable identity 和幂等落库，不依赖“消息绝不重复”。
- **还有什么窗口？** 首次创建 task/job 后的 DB commit 与 enqueue 尚无 durable outbox，这是后续审计项。

### 项目证据

- `internal/model/task.go`
- `internal/model/task_job.go`
- `internal/mq/producer.go`
- `internal/mq/consumer_*.go`
- `internal/mq/retry.go`
- `internal/repository/task_lease_*.go`

## 二、长视频分段 ASR 与结果复用

### 30 秒回答

长视频音频一次提交容易触发 provider 大小、时长或超时限制。VidLens 使用 FFmpeg 将音频按配置时长切片，以 `task_id + chunk_index` 保存片段状态和文本。任务重试时已完成片段直接复用，只调用未完成片段，最后按顺序拼接完整转写。

这降低了失败后的重复调用成本，也让日志能定位具体失败片段。它不是分布式 MapReduce；当前 consumer 仍以单任务顺序编排为主。

### 追问防守

- **为什么保存片段，不只保存最终全文？** 否则任一片段失败都要从头调用 ASR，无法证明断点恢复。
- **边界处会不会断句？** 当前固定时长切分简单稳定，但没有语音级重叠窗口和去重，这是可继续改进的准确率问题。
- **怎么防止混用旧结果？** 复用条件应绑定任务、片段索引及处理参数；不能只因为 index 相同就盲目复用。

### 项目证据

- `internal/model/transcription_chunk.go`
- `internal/mq/consumer_transcribe.go`
- `internal/pkg/ffmpeg/ffmpeg.go`
- `docs/troubleshooting-and-interview-notes.md`

## 三、Redis 分片进度与 MinIO 合并

### 30 秒回答

大文件上传先由前端计算完整文件 MD5，Redis Set 保存带 TTL 的已上传片号和上传规格，MinIO 保存分片并在完成时服务端合并。前端重进页面后只补传缺失片号；该协议仍以客户端 MD5 为会话标识，Redis 状态丢失会影响未完成上传的恢复。

### 当前边界

当前通过 MinIO `ComposeObject` 在对象存储侧按顺序合并，不把完整视频读入 Go 进程。Redis 状态带 TTL，不是耐久上传事实；TTL 到期、Redis 丢失和孤儿分片的后台回收仍是当前边界。

### 项目证据

- `internal/service/media_chunk_upload.go`
- `internal/handler/media.go`
- `internal/storage/minio.go`
- `cmd/server/router.go`
- `web/src/chunkedUpload.js`
- [上传专项](/interview/resume-grill/resume-core/chunk-upload/)

## 四、pgvector + BM25 + RRF 与引用

### 30 秒回答

RAG 使用完整 ASR 转写，不使用摘要作为知识源。系统将转写切成 chunks，保存到 PostgreSQL `video_chunks` 作为可审计、可重建的文本事实，再将向量发布到同库 pgvector projection。查询同时执行 pgvector 语义召回和 Go 侧 BM25 关键词召回，用 RRF 融合排名，并把 evidence ID、chunk index 和 content 作为 citations 返回和持久化。

`user_id + task_id + embedding_model` 同时承担授权范围、视频范围和向量模型空间隔离。第一版 MySQL + Milvus 已迁为本地 PostgreSQL 单库；Milvus 仅保留观察期回滚适配。

### 追问防守

- **为什么不用摘要？** 摘要会丢细节，引用应尽量回到原始转写。
- **为什么还要 BM25？** 补充术语、数字和原词匹配；当前是 Go 内单视频计算，不是专业搜索引擎。
- **为什么 RRF？** 向量分与 BM25 分尺度不同，排名融合比直接相加更稳。
- **同库是否强一致？** 不是。source chunks 与 vector projection 仍分两个 transaction，通过 failed 状态、reindex 和 audit 恢复。

### 项目证据

- `internal/service/rag_index_build.go`
- `internal/repository/video_chunk.go`
- `internal/vector/pgvector.go`
- `internal/service/rag_pipeline.go`
- `internal/service/retrieval_fusion.go`
- `internal/service/chat_*.go`

## 四条主线如何组成一个闭环？

```text
分片上传
  -> MinIO asset + PostgreSQL task/job
  -> Kafka 异步处理
  -> FFmpeg 分段 + ASR chunk 复用
  -> 完整转写
  -> RAG index job
  -> PostgreSQL video_chunks + pgvector projection
  -> pgvector + BM25 + RRF
  -> citations + answer + retrieval snapshot
```

它们对应同一条业务链路的四个失败边界：文件不能一次传完、处理不能占住 HTTP、长视频失败不能从头重做、模型回答不能脱离证据。技术组件为这些具体问题服务，不是四个孤立 demo。

## 面试时必须守住的边界

1. **可以说：** Kafka 异步任务、DB 状态与 retry lease、分段 ASR 复用、现有分片上传、PostgreSQL + pgvector、BM25 + RRF、citations 与检索快照。
2. **历史说法：** MySQL JSON bug 和 Milvus 部署问题是第一版真实故障，不是当前默认架构。
3. **不要说：** Kafka exactly-once、transactional outbox 已完成、完整 upload session 已完成、所有 provider 都是真 token streaming、已有完整计费或大规模生产收益。
4. **未来方向：** durable upload session、首次投递 intent/outbox、固定 RAG 评测基线；URL 下载只维护已有安全边界。
