# 面试作战手册

> 这部分不是八股清单，而是面试现场可以直接说的项目回答稿。每个回答都只讲 VidLens 已经实现的事实，并明确当前限制和未来优化。

## 1. 你这个项目一句话是什么？

### 直接回答

VidLens 是一个 Go 后端为主的 AI 视频理解项目。用户上传视频或提交视频 URL 后，后端先把视频文件放到 MinIO，再通过 Kafka 异步处理下载、音频提取、ASR 转写、LLM 摘要和 RAG 索引。真正的核心不是“调了一个 AI 接口”，而是把长时间、高成本、容易失败的视频 AI 流程做成可追踪、可重试、可解释的后台任务。

我会把它定位成“AI 视频理解后端”，不是单纯的 MQ/Redis demo。Kafka、Redis、MinIO、Milvus 这些组件都是为这个业务链路服务的：Kafka 解决 HTTP 请求不能等待几分钟的问题，Redis 做分布式锁和限流，MinIO 存视频和音频对象，Milvus 存转写文本的向量索引。

### 项目证据

- 启动时创建 Kafka topic、连接 Milvus、装配 ChatService/MediaService/Consumer：`cmd/server/main.go:110`, `cmd/server/main.go:135`, `cmd/server/main.go:169`, `cmd/server/main.go:177`, `cmd/server/main.go:194`
- 任务状态枚举覆盖 pending/queued/running/completed/failed/dead，stage 覆盖 downloading/transcribing/summarizing/indexing：`internal/model/task.go:12`, `internal/model/task.go:27`
- RAG 问答检索、最近聊天上下文和回答保存都在 ChatService 中完成：`internal/service/chat.go:129`, `internal/service/chat.go:144`, `internal/service/chat.go:192`, `internal/service/chat.go:208`

### 当前限制

不要把它说成大规模生产系统。当前更准确的说法是：它已经覆盖了后台任务、RAG、BYOK、安全校验和部署验证这些后端项目核心面，但流量规模、计费配额、完整监控指标和生产级 URL 下载安全还没有完全做完。

## 2. 为什么用 Kafka 做异步，而不是 HTTP 同步处理？

### 直接回答

视频下载、FFmpeg 提取音频、ASR、摘要和 RAG 建索引都可能持续几十秒甚至几分钟。如果把这些步骤放在 HTTP 请求里，连接会一直挂着，用户刷新页面后状态也不清楚，服务端线程和上下文也会被长时间占住。

VidLens 的做法是：HTTP 接口只负责创建任务、记录状态并投递 Kafka；后台 consumer 消费消息后按阶段处理，并把状态写回 MySQL。前端拿到 taskID 后轮询任务状态。这样失败可以落到 task/job 状态里，而不是变成一次模糊的 HTTP 超时。

### 项目证据

- Producer 使用 Kafka writer，`RequiredAcks: kafka.RequireAll`，并按 topic 投递 analyze/transcribe/download/rag_index 消息：`internal/mq/producer.go:47`, `internal/mq/producer.go:56`, `internal/mq/producer.go:77`, `internal/mq/producer.go:91`, `internal/mq/producer.go:104`, `internal/mq/producer.go:117`
- URL 上传创建 downloading 任务后立即投递下载消息，投递失败会把任务和 TaskJob 标为失败：`internal/service/media.go:150`, `internal/service/media.go:174`, `internal/service/media.go:178`, `internal/service/media.go:181`
- Consumer 分别启动 analyze、transcribe、download、rag_index 消费循环：`internal/mq/consumer.go:124`, `internal/mq/consumer.go:164`, `internal/mq/consumer.go:194`, `internal/mq/consumer.go:230`

### 容易被追问

**追问：Kafka 没有 RocketMQ 那种业务延迟重试，你怎么处理失败？**

我没有把 Kafka 说成 RocketMQ。VidLens 用的是 DB 侧 retry state：失败时记录 `retry_count`、`next_retry_at`、`last_job_type`，RetryScheduler 定期扫描到期任务再重新投递 Kafka。Kafka 负责异步缓冲，重试语义主要在业务表里做。

证据：`internal/mq/retry.go:118`, `internal/mq/retry.go:133`, `internal/mq/retry.go:193`, `internal/repository/task.go:211`

## 3. 你的重试和最终一致性怎么保证？

### 直接回答

VidLens 的重试不是简单 catch 后 sleep。Consumer 处理失败时先判断是不是可重试错误：比如网络抖动、超时这类可以进入 retry；配置缺失、鉴权失败这类非重试错误直接失败，不浪费外部模型调用。

可重试失败会写入 `next_retry_at`，RetryScheduler 到时间后先 claim 任务，把任务恢复到对应 running stage，再投递 Kafka。如果 claim 后 Kafka 投递失败，代码会把任务恢复回 failed，并重新设置下一次 retry 时间，避免任务被“夹在中间”永远不再调度。

### 项目证据

- 非重试错误列表和 retryable 分支：`internal/mq/retry.go:51`, `internal/mq/retry.go:118`
- 可重试失败记录 `next_retry_at`：`internal/mq/retry.go:133`, `internal/repository/task.go:176`
- RetryScheduler 扫描、claim、投递；投递失败后恢复 retry 状态：`internal/mq/retry.go:193`, `internal/mq/retry.go:202`, `internal/mq/retry.go:210`, `internal/mq/retry.go:212`
- 对应回归测试覆盖“claim 后 Kafka enqueue 失败恢复 next_retry_at”：`internal/mq/consumer_test.go:974`

### 当前限制

这套设计适合当前单体 Go 后端和 MySQL 状态表。它不是完整的工作流引擎，没有 Temporal 那种跨版本 workflow replay、补偿事务和可视化 DAG。如果将来任务步骤更多，可以把任务编排进一步抽象，或者引入专门的 durable workflow。

## 4. RAG 用的是什么数据？为什么不是 AI 摘要？

### 直接回答

VidLens 的 RAG 知识源是 ASR 转写全文，不是 AI 摘要。原因很直接：摘要已经是模型压缩过的二手信息，很多细节、时间点、原话都会丢。如果用户问的是“视频里某个细节怎么说的”，应该从更接近原始内容的转写文本里检索。

当前实现是：RAG 索引服务把转写文本切成 chunk，调用用户配置的 Embedding 模型生成向量，写入 Milvus；问答时先把问题向量化，再按 `user_id + task_id + embedding_model` 做隔离检索，同时合并 Go 侧 BM25 关键词结果，用 RRF 排序后拼 prompt。

### 项目证据

- BuildTaskIndex 读取转写文本、切 chunk、生成 embedding、写向量和本地 chunk：`internal/service/rag_index.go:118`, `internal/service/rag_index.go:150`, `internal/service/rag_index.go:179`, `internal/service/rag_index.go:190`
- Milvus collection 字段包含 user_id、task_id、chunk_index、embedding_model、embedding：`internal/vector/milvus.go:67`, `internal/vector/milvus.go:83`, `internal/vector/milvus.go:92`
- 检索时向量召回后合并 BM25，再用 RRF 融合：`internal/service/chat.go:160`, `internal/service/chat.go:174`, `internal/service/chat.go:258`, `internal/service/retrieval_fusion.go:14`
- Go 侧 BM25 搜索按 user_id、task_id、embedding_model 限定 chunk：`internal/repository/video_chunk.go:47`, `internal/repository/video_chunk.go:59`

### 当前限制

不要说已经有专业搜索引擎、rerank 或跨视频知识库。当前是单视频范围内的 Milvus 向量检索 + Go 侧 BM25 风格检索 + RRF 融合。更强的方向是引入评估集、邻近 chunk 扩展、query rewrite、可选 rerank，以及当 chunk 数量变大时考虑 OpenSearch/Bleve/MySQL FULLTEXT。

## 5. BYOK 解决了什么？API Key 怎么存？

### 直接回答

公开部署时不能默认消耗维护者自己的 AI Key，所以 VidLens 让每个用户配置自己的 ASR、LLM、Embedding provider、baseURL、model 和 key。这样不同用户可以用不同服务商，也能把成本归到自己的账号。

API Key 不会明文返回给前端，也不会直接存明文。请求进来后，Service 用 AES-GCM codec 加密成 ciphertext 存到 `user_ai_profiles`；展示列表时只返回 mask 后的 key；真正调用模型前才解密成运行时 profile。

### 项目证据

- UserAIProfile 的三类 API key 字段都 `json:"-"`，避免序列化泄露：`internal/model/ai_profile.go:7`, `internal/model/ai_profile.go:13`, `internal/model/ai_profile.go:17`, `internal/model/ai_profile.go:21`
- AES-GCM 加密、随机 nonce、base64 payload 和解密逻辑：`internal/pkg/secret/crypto.go:40`, `internal/pkg/secret/crypto.go:47`, `internal/pkg/secret/crypto.go:60`
- 创建/更新 profile 时加密或保留原密文，返回时 mask，使用时 decrypt：`internal/service/ai_profile.go:229`, `internal/service/ai_profile.go:263`, `internal/service/ai_profile.go:303`, `internal/service/ai_profile.go:311`
- AI Factory 按 profile 创建 ASR/LLM/Embedding 客户端：`internal/ai/factory.go:34`, `internal/ai/factory.go:47`, `internal/ai/factory.go:58`

### 当前限制

这还不是完整的计费系统。项目已经有 AI 调用记录和日聚合，但没有做用户 quota、价格表、余额扣费和账单。面试里可以说“有审计基础”，不要说“已经实现完整计费”。

## 6. URL 下载有什么安全风险？你做到了哪一层？

### 直接回答

URL 下载最大的风险是 SSRF：用户提交一个看起来像视频的链接，但实际让服务器访问内网地址、localhost 或云元数据服务。VidLens 做了第一层防护：只允许 http/https，要求 host 在白名单或默认视频站域名里，DNS 解析后拒绝 private、loopback、link-local、multicast 等 IP，并且会清洗 URL 的 query 和 fragment，避免日志里泄露 token。

我不会把它说成生产级 URL 下载安全，因为 yt-dlp 可能跟随重定向，DNS rebinding、下载大小硬限制、超时策略、用户级 cookies 管理都还需要继续补。

### 项目证据

- URL 校验入口：`internal/service/media.go:150`, `internal/service/media.go:152`
- 默认允许视频域名、http/https 限制、localhost 拒绝、host allowlist、DNS 私网 IP 检查和 URL 清洗：`internal/service/remote_video_url.go:13`, `internal/service/remote_video_url.go:52`, `internal/service/remote_video_url.go:61`, `internal/service/remote_video_url.go:68`, `internal/service/remote_video_url.go:71`, `internal/service/remote_video_url.go:89`, `internal/service/remote_video_url.go:94`
- 下载日志使用 sanitized URL：`internal/mq/consumer.go:282`, `internal/mq/consumer.go:286`
- yt-dlp 参数支持 cookies/proxy，并限制下载格式到最高 720p：`internal/pkg/ytdlp/ytdlp.go:45`, `internal/pkg/ytdlp/ytdlp.go:54`, `internal/pkg/ytdlp/ytdlp.go:57`

### 容易被追问

**追问：为什么还不能说生产级？**

因为当前校验在提交阶段做得比较多，但真正下载由 yt-dlp 执行，重定向链路、下载过程中的 DNS 变化、资源大小和耗时限制都还需要更严格地约束。面试里应该主动说出这个边界，反而更可信。

## 7. 你用 AI 写项目，怎么证明自己懂？

### 直接回答

我会坦诚说 AI 参与了实现加速，但项目不是“生成完就算”。真正让我理解项目的是不断用真实 bug 反推设计：长视频 ASR 一开始会因为音频过大失败，后来做了 FFmpeg 压缩和 300 秒切片；RAG 状态一开始前端不知道索引失败原因，后来加了 `video_rag_indexes` 状态；URL 下载上线后遇到 B 站 412、YouTube 网络问题，所以补了 cookies/proxy 配置和错误提示；重试调度也补过 claim 后 Kafka 投递失败的恢复。

面试里我不会背“AI 提升效率”这种空话，而是讲我怎么定位日志、读源码、补测试、区分已实现和未来优化。

### 项目证据

- 长视频 ASR 问题和修复记录：`docs/troubleshooting-and-interview-notes.md:38`, `docs/troubleshooting-and-interview-notes.md:47`, `docs/troubleshooting-and-interview-notes.md:61`
- README 记录 300 秒切片和复用已有转写：`README.md:226`, `README.md:234`, `README.md:239`
- URL 下载部署和代理/cookies 说明：`README.md:148`, `internal/pkg/ytdlp/ytdlp.go:63`
- 重试调度恢复测试：`internal/mq/consumer_test.go:974`

### 当前限制

AI 辅助项目最怕的是夸大。我的说法会保守：我能解释核心链路、读得懂关键源码、知道哪些是当前事实，哪些只是 roadmap。比如不会说 RocketMQ、Redisson、Function Calling、rerank、完整 quota 这些当前代码没有的东西。

## 面试前 15 分钟速刷顺序

1. 先背项目定位：AI 视频理解后端，核心是长任务和高成本 AI 流程的工程化。
2. 再背三条主线：Kafka 异步任务、RAG 基于 ASR 转写、BYOK + URL 安全边界。
3. 最后准备两个真实 bug：长视频 ASR 截断、RetryScheduler 投递失败恢复。
4. 被问未来优化时，优先讲 URL 下载安全和 RAG 评估，不要把未来工作说成已经完成。
