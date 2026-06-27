# VidLens 简历与面试准备稿

> 这份文档按 `D:\zjh\dev\chat\小红书Nix-java项目准备文档.md` 的思路改写，但只使用当前 VidLens 项目已经能在代码和文档中支撑的内容。
> 重点不是把技术名词堆满，而是把“视频长任务 + AI 应用后端 + RAG 工程化”的主线讲清楚。

## 1. 简历推荐写法

### 项目名称

镜知 VidLens - AI 驱动的视频内容理解平台

### 技术栈

`Go` `Gin` `GORM` `MySQL` `Redis` `Kafka` `MinIO` `Milvus` `FFmpeg` `yt-dlp` `JWT` `Vue3`

### 项目简介

VidLens 是一个面向视频内容理解场景的全栈项目。用户上传本地视频或提交 B 站 / YouTube 视频链接后，系统异步完成 URL 下载、对象存储、音频提取、ASR 转写、LLM 摘要，并基于视频转写全文构建 RAG 索引，支持对单个视频进行带引用片段的多轮问答。

项目重点解决视频处理中的长耗时阻塞、大文件传输、外部 AI 服务不稳定、重复处理和公开部署成本归属问题，围绕 Kafka 异步任务、Redis 分布式锁和限流、MinIO 分片合并、Milvus 向量检索、用户级 BYOK 模型配置、任务重试和 AI 调用审计做了工程化设计。

### 简历项目经历版本

以下为当前最终简历版，和 `docs/resume-final-draft.md`、`docs/final-resume-targeted-prep.md` 保持一致：

- 引入 Kafka 将视频下载、ASR 转写、摘要生成和 RAG 索引构建等长耗时任务异步化，HTTP 接口仅负责任务落库和消息投递，避免分钟级视频处理阻塞请求链路
- 设计 Redis 分布式锁 + WatchDog，结合 MD5 内容指纹和视频文件复用，保护分片合并、分析任务消费等临界区，降低并发重复入库和重复处理风险
- 采取分片上传与断点续传机制，通过 Redis Set 记录分片状态，使用 MinIO 存储分片并通过 ComposeObject 合并，提升弱网环境下大文件上传可靠性
- 基于 Redis Lua 令牌桶限流，按用户和接口维度限制高成本 AI 请求，缓解恶意请求和重复点击带来的外部 AI 服务调用成本
- 基于 RAG 检索增强生成实现视频智能问答，将转写文本切分为 chunk 并生成向量写入 Milvus，结合 BM25 关键词召回与 RRF 融合排序，返回引用片段提升答案可解释性
- 设计任务失败治理机制，基于错误分类和阶梯退避重试处理外部依赖异常，对可恢复错误延迟重投递，对不可恢复错误快速失败，避免重试风暴和 AI 成本浪费

### 更短的简历版本

如果简历空间不够，保留 5 条：

- 基于 Kafka 将视频下载、ASR、摘要和 RAG 索引构建拆分为异步任务，结合 MySQL 任务状态表记录阶段、错误和重试信息，避免长耗时视频处理阻塞请求链路。
- 基于 Redis 分布式锁、MD5 内容指纹、视频文件复用和 Lua 令牌桶实现并发治理与接口限流，降低重复上传、重复 AI 调用和恶意高频请求带来的资源消耗。
- 实现分片上传和断点续传，使用 Redis Set 记录分片状态、MinIO 存储分片并通过 ComposeObject 合并，提升弱网环境下大视频上传成功率。
- 基于 ASR 全文构建 RAG 索引，使用 Milvus 向量检索、BM25 关键词召回和 RRF 融合排序，支持带引用片段的视频问答。
- 设计任务失败治理机制，基于错误分类和阶梯退避重试处理外部依赖异常，对可恢复错误延迟重投递，对不可恢复错误快速失败，提升任务可恢复性和排障效率。

### 不建议写进简历的说法

- 不要写 `RocketMQ`、`Redisson`。本项目使用的是 `Kafka` 和自实现的 Redis 分布式锁。
- 不要写“生产级计费系统”。当前是 AI 调用审计和每日用量聚合，没有真实扣费、套餐和 token 级精确账单。
- 不要写“跨视频知识库”。当前 RAG 是单视频 task 维度问答。
- 不要写“接入 Elasticsearch / 专业 BM25 引擎”。当前关键词召回是 Go 侧 BM25 风格实现。
- 不要写“已实现 rerank”。当前做的是向量召回、关键词召回和 RRF 融合。
- 不要写“ASR 并行处理”。当前分片转写是串行流程，已支持分片级状态复用。
- 不要写“WebSocket 流式对话”。当前是 HTTP SSE，且 provider 支持 streaming 时才是真正 token streaming。
- 不要写固定性能指标，除非你自己实测过。例如“60s 压到 50ms”可以换成“核心接口投递任务后立即返回，避免等待分钟级视频处理”。

## 2. 项目介绍背诵版

### 30 秒版本

VidLens 是我做的一个 AI 视频内容理解项目，核心场景是把本地视频或公开视频链接转成可检索、可总结、可追问的知识内容。后端用 Go + Gin 实现，视频入库后通过 Kafka 异步执行下载、FFmpeg 音频提取、ASR 转写、LLM 摘要和 RAG 索引。RAG 的知识源不是总结，而是 ASR 转写全文，切 chunk 后写入 Milvus，问答时按用户和视频过滤检索，并返回引用片段。

这个项目我主要想解决的不是简单调用 AI，而是视频处理长耗时、AI 服务不稳定、模型调用成本和公开部署安全边界这些后端工程问题。

### 2 分钟版本

VidLens 的起点是视频学习和回看效率问题。很多课程、会议或者公开视频时长很长，直接看很低效，所以我做了一个平台，让用户上传本地视频或者提交视频链接，后端自动完成音频提取、语音转写、AI 摘要，并支持对视频内容继续问答。

项目做到后面我发现真正难点不在 AI 调接口，而在视频和大模型能力的工程化包装。视频处理是典型长耗时、CPU 和 IO 都比较重的任务，如果放在 HTTP 请求里同步执行，用户体验和服务稳定性都会很差。所以我把 URL 下载、文字提取、AI 总结和 RAG 索引都拆成 Kafka 异步任务，用 MySQL 记录任务状态和重试信息。

另一个问题是长视频 ASR。最开始长音频会因为 base64 请求体过大失败，即使压缩后也可能只识别前面一小段。我最后把音频统一转成 16k 单声道低码率格式，再按 300 秒切片逐段 ASR，并把每个分片的结果和状态持久化，这样失败重试可以复用已经完成的分片。

RAG 这块我没有用 AI 总结作为知识库，因为总结是二次生成结果，可能丢细节。后端会把 ASR 全文切 chunk，调用用户配置的 Embedding 模型写 Milvus。提问时先做向量召回，再合并 Go 侧 BM25 风格关键词召回，用 RRF 做融合，最后把检索片段和最近会话上下文交给 LLM，并通过 SSE 流式返回。

公开部署方面，我也做了用户级 BYOK 配置。用户自己配置 ASR、LLM、Embedding 的 Key，后端加密保存，任务执行时按用户读取，不会默认消耗服务端自己的 Key。同时加了 AI 调用日志和每日用量聚合，用于排障和后续额度控制。

## 3. 全链路架构怎么讲

### 第一阶段：用户和 AI 配置

用户注册登录后通过 JWT 访问接口。使用 AI 能力前，用户需要配置自己的 ASR、LLM、Embedding provider、endpoint、model 和 API Key。后端用 AES-GCM 加密 Key，接口返回只展示脱敏值。任务执行时按 `task.UserID` 读取用户默认 profile，用户未配置则明确失败为“请先配置 AI 服务”。

### 第二阶段：视频入库

视频来源有三种：

- 普通上传：后端边读文件边计算 MD5，上传到 MinIO，写入 `video_assets` 和 `video_tasks`。
- 分片上传：前端按 5MB 分片，后端每片先写 MinIO，再用 Redis Set 记录成功分片，合并时校验分片完整性并用 MinIO `ComposeObject` 服务端合并。
- URL 入库：后端先校验 URL scheme、域名白名单和 DNS 解析结果，防止内网地址访问；然后创建 `download` 子任务并投递 Kafka，真正下载由消费者执行。

MD5 用于内容级去重。多个用户或任务可以引用同一个 `video_asset`，删除任务时只有没有其他活跃引用，才删除 MinIO 对象。

### 第三阶段：任务投递与异步消费

用户点击文字提取、AI 总结或提交 URL 后，HTTP 层只做权限校验、状态条件更新和 Kafka 投递，然后立即返回任务 ID。Kafka topic 按动作拆分：

- `video-download`
- `video-transcribe`
- `video-analyze`
- `video-rag-index`

生产者同步发送，`RequiredAcks=All`，失败最多重试 3 次；消费者使用消费者组，业务成功后手动 commit offset。

### 第四阶段：长视频处理

消费者下载视频文件到临时目录，调用 FFmpeg 提取音频：

```text
-ac 1 -ar 16000 -acodec libmp3lame -b:a 32k
```

然后按 300 秒切片。每个 chunk 转写前先查 `video_transcription_chunks`，如果对应 `task_id + chunk_index` 已完成，就复用已有文本；否则调用 ASR，成功后保存分片文本，失败则记录错误。

AI 总结会优先复用已有 `video_transcriptions.content`，不重复跑 ASR，减少成本和失败点。

### 第五阶段：RAG 索引与问答

转写完成后不在 ASR consumer 里同步构建 RAG，而是投递独立 `rag_index` Kafka job。RAG consumer 调用 `BuildTaskIndex`：

```text
读取转写全文
-> 字符级 chunk 切分，默认 chunk_size=800，overlap=120
-> 调用用户配置的 Embedding
-> 校验向量维度等于系统 Milvus collection 维度
-> 重建前删除同 task/model 旧向量
-> MySQL 保存 chunk 元数据
-> Milvus upsert 向量
-> video_rag_indexes 记录 indexed / failed
```

问答时：

```text
用户问题
-> question embedding
-> Milvus 按 user_id + task_id + embedding_model 检索候选
-> MySQL chunk 做 Go 侧 BM25 风格关键词召回
-> RRF 融合排序
-> 读取 Redis 最近 N 轮会话
-> 组装 RAG prompt
-> LLM 生成答案
-> SSE 返回 citations、answer delta、done
-> MySQL 保存 user / assistant 消息和 retrieval snapshot
```

### 第六阶段：失败治理和可观测性

Kafka 的 offset 重放不直接承担业务重试。消费者失败后先判断错误类型：

- 可重试：网络超时、连接失败、AI 5xx / 429、MinIO / Milvus 临时错误。
- 不可重试：用户未配置 AI 服务、权限错误、文件不存在、Embedding 维度不匹配、B 站 412 风控。

可重试错误写入 `retry_count` 和 `next_retry_at`，超过上限进入 `dead`；不可重试直接 failed。DB retry scheduler 扫描到期任务，按 `last_job_type` 重新投递对应 topic。

可观测性上，项目有：

- `trace_id` 贯穿任务日志和 Kafka payload。
- `video_tasks.status + stage` 表达整体状态和当前阶段。
- `task_jobs` 表达每类子任务的独立状态。
- `ai_call_logs` 记录模型调用元数据。
- `user_usage_daily` 做用户每日调用聚合。
- RAG 离线评估可计算 Recall@K、MRR、无结果率和来源分布。

## 4. 核心面试问答

### Q1：这个项目最大的难点是什么？

最大的难点不是 AI 调用本身，而是把不稳定、长耗时、成本敏感的外部能力包装成后端可控流程。

视频处理会同时遇到长耗时、文件大、CPU/IO 重、外部 ASR/LLM 不稳定、用户重复提交等问题。所以我做了几层治理：HTTP 只负责投递任务，真正处理放到 Kafka consumer；业务失败不靠卡住 Kafka offset，而是落库后由 DB retry scheduler 按退避重试；长视频 ASR 用 FFmpeg 压缩和 300 秒切片；AI Key 做用户级 BYOK 和加密保存；RAG 索引失败也拆成独立 job，不影响转写结果可见。

### Q2：为什么用 Kafka？

我的项目是 Go 后端，Kafka 的 Go 客户端成熟，消费者组和分区模型适合视频任务这种异步削峰场景。Kafka 是拉取式消费，消费者可以按自己的处理能力拉取消息，分区也方便后续通过增加消费者实例扩展吞吐。

这里我没有选择 RocketMQ，主要是因为 RocketMQ 更偏 Java 生态；也没有选择 RabbitMQ，因为我更看重日志型持久化、消息堆积和消费者组扩展能力。

但我也不会说 Kafka 天然解决所有重试问题。Kafka 提供的是 at-least-once 消费语义，业务级错误分类、延迟重试、dead 状态和前端可见的失败原因，是我在 MySQL 状态机和 retry scheduler 里补的。

### Q3：为什么消费失败后还 commit offset？

如果业务失败已经被记录到数据库，并且后续由 retry scheduler 接管，那么继续不 commit 会让同一条消息反复卡住分区。尤其用户没配置 API Key、B 站 412、权限错误这类不可重试错误，不提交 offset 没有意义。

所以我的策略是：无法解析消息这种系统级异常可以认为消息有问题；进入业务处理后，失败先分类并落库，返回 nil 让 offset 提交。真正的业务重试由数据库状态和定时调度驱动。

### Q4：为什么还需要 Redis 分布式锁？

Kafka 用 MD5 作为 key 可以让同一视频尽量进入同一分区，但这不能替代业务幂等。比如用户重复点击、重试任务、服务扩缩容或不同动作之间仍可能触发重复处理。

所以在分析任务消费时会基于视频 MD5 获取 Redis 锁。锁释放和续期都校验 owner，防止误删其他任务的锁；watchdog 按 TTL/3 自动续期，适配视频处理这种耗时不确定的任务。

### Q5：长视频 ASR 失败是怎么排查和解决的？

我遇到过 15 分钟左右的视频，第一次失败是因为音频 base64 超过 ASR 单次请求限制；后来即使压缩后任务完成，转写结果也只有几百字，说明单次长音频可能只识别部分内容。

修复不是简单调大限制，而是把长音频拆成可控的小段：先用 FFmpeg 转成 16k 单声道 32kbps MP3，再按 300 秒切片逐段 ASR，最后合并文本。后续又加了分片状态表，记录每段 running/completed/failed 和文本内容，失败重试可以跳过已完成片段。

### Q6：为什么用 ASR 转写全文做 RAG，不用 AI 总结？

总结是模型二次生成的压缩结果，会丢细节，也可能带模型自己的表达。RAG 的知识源应该尽量接近原始资料，所以我用 ASR 全文切 chunk。总结适合给用户快速浏览，但不适合作为唯一知识库。

### Q7：RAG 检索链路怎么设计？

第一步把用户问题 embedding，去 Milvus 按 `user_id + task_id + embedding_model` 检索向量相似 chunk；第二步从 MySQL 的 `video_chunks` 做 Go 侧 BM25 风格关键词召回，补充专有名词、英文缩写和数字类问题；第三步用 RRF 做融合，因为向量分数和关键词分数不是同一尺度，直接相加不稳定。

最后把融合后的 topK chunk、最近几轮会话和用户问题一起组装 prompt，让 LLM 只基于视频片段回答，并返回 citations。

### Q8：如果 topK 没召回关键片段怎么办？

我会从几个方向优化：

- 增大 candidateK，先宽召回，再融合或重排。
- 调整 chunk_size 和 overlap，避免语义被切断。
- 引入关键词召回和 RRF，解决向量语义相似但关键词漏召的问题。
- 后续可以接 rerank 模型或更专业的检索引擎。
- 用离线评估集看 Recall@K、MRR 和无结果率，而不是只凭人工感觉。

当前项目已经做了 candidateK、Go 侧 BM25 风格召回、RRF 融合和 RAG 离线评估核心，但还没有接 rerank。

### Q9：为什么选择 SSE，不用 WebSocket？

这个场景是大模型回答从服务端单向推给浏览器，不需要客户端和服务端高频双向通信。SSE 基于 HTTP，实现、调试和鉴权都更简单，也符合 LLM token streaming 的输出模型。

WebSocket 更适合在线协作、双向实时聊天、游戏状态同步这类双向场景。VidLens 的问答请求仍然是一次用户提问触发一次服务端流式输出，所以 SSE 足够。

需要注意的是，我第一版 SSE 只是把完整回答切块返回，后来才把 AI client 层升级为 provider 级 `StreamChat`，真正逐行解析 OpenAI-compatible SSE delta 并转发给前端。

### Q10：公开部署为什么要做 BYOK？

如果服务端配置自己的 ASR、LLM、Embedding Key，陌生用户注册后就能消耗服务端 token，成本和滥用风险都不可控。所以我改成用户级 BYOK：每个用户自己配置模型服务，Key 加密入库，后端只在调用模型前解密使用。

并且 ASR、LLM、Embedding 是三类不同能力，可能来自不同 provider，不能假设共用一个 baseURL 或 API Key。用户未配置时任务明确失败，不回退到服务端 Key。

### Q11：AI 调用审计为什么不保存完整 prompt 和 response？

因为 prompt 和 response 可能包含用户视频内容、个人信息或业务敏感数据，API Key 和 Authorization header 更不能保存。审计表只记录排障和额度控制需要的元数据：kind、provider、model、状态、耗时、输入输出字符数和错误摘要。

这不是完整计费系统，但能回答“哪个用户今天调用了多少次 LLM/Embedding/ASR”“失败集中在哪个 provider”“一次问答慢在哪里”等问题。

### Q12：任务状态为什么从单表 status 拆到 task_jobs？

`video_tasks.status` 早期同时表达下载、转写、总结、RAG 索引，语义会混。比如 ASR 已成功，但 RAG 索引失败，如果只把主任务标 failed，用户会误以为整个视频处理失败。

所以我保留 `video_tasks` 作为兼容状态源，又加了 `task_jobs` 记录每个动作自己的状态、阶段、重试次数和错误原因。第一版 `(task_id, job_type)` 唯一，只保留当前状态，不做完整历史事件表。后续如果要做更完整审计，可以再加 `task_job_attempts`。

### Q13：URL 下载怎么防 SSRF？

我做了几层限制：

- 只允许 `http / https`。
- 域名必须命中白名单，例如 bilibili、b23、youtube、youtu.be。
- 禁止 localhost。
- 如果 host 是 IP，直接判断是否内网、回环、链路本地、多播等不安全地址。
- 如果 host 是域名，先 DNS 解析，再检查解析出的所有 IP 是否安全。
- 日志中去掉 query 和 fragment，避免把敏感参数写入日志。

这不能说是完整生产级 SSRF 防护，但比直接把用户 URL 交给 yt-dlp 安全很多。

### Q14：项目中 AI Coding 怎么讲？

可以这样讲：

> 我确实使用 AI 辅助编码，但不是让 AI 直接当项目经理。我会先自己定义业务边界和要解决的问题，再让 AI 帮我展开方案空间、生成局部代码和测试。关键选型和质量把关还是我自己做。
>
> 比如长视频 ASR 失败这个问题，AI 可以给出压缩、切片、重试等方向，但真正定位到“base64 体积限制”和“长音频单次识别不完整”两层问题，是通过数据库状态、错误信息、转写长度和日志证据分析出来的。最终我选择统一按时长切片，并补了分片状态持久化和测试。

如果被问“是不是 AI 都帮你写的”，回答重点是：AI 提高了实现效率，但需求拆解、风险判断、方案取舍、测试验证、排障复盘是自己做的。

## 5. 项目真实性材料

### 能拿出来支撑的代码路径

- 服务入口和路由：`cmd/server/main.go`
- Kafka 生产和消费：`internal/mq/producer.go`、`internal/mq/consumer.go`
- 业务重试：`internal/mq/retry.go`
- Redis 分布式锁：`internal/pkg/lock/redis_lock.go`
- Redis Lua 限流：`internal/middleware/ratelimit.go`
- 上传和 MinIO 分片合并：`internal/service/media.go`
- FFmpeg 音频处理：`internal/pkg/ffmpeg/ffmpeg.go`
- BYOK 配置：`internal/service/ai_profile.go`、`internal/pkg/secret/crypto.go`
- RAG 索引：`internal/service/rag_index.go`
- RAG 问答和 SSE：`internal/service/chat.go`、`internal/handler/chat.go`
- 向量库：`internal/vector/milvus.go`
- AI 调用审计：`internal/ai/observed.go`、`internal/service/ai_observer.go`
- 排障复盘：`docs/troubleshooting-and-interview-notes.md`

### 能拿出来支撑的测试方向

项目测试覆盖了这些关键点：

- 环境变量配置解析。
- AI profile 加密、脱敏、默认配置隔离。
- OpenAI-compatible Chat / Embedding 请求和 streaming delta 解析。
- 长音频切片参数和分片转写复用。
- RAG 索引构建、维度校验、旧向量删除失败保护。
- RAG 问答存储消息、Redis 最近记忆、向量 + 关键词融合。
- RAG 离线评估 Recall / MRR / no result。
- Kafka retry scheduler、可重试 / 不可重试错误分类、dead 状态。
- URL 下载白名单、DNS 私网地址校验和日志脱敏。
- 删除任务时共享 asset 不被误删。

## 6. 一页背诵版

### 项目主线

VidLens 不是普通 AI wrapper，而是一个围绕视频长任务和大模型应用后端治理的项目。

主线可以概括为：

```text
视频入库
-> Kafka 异步下载 / 转写 / 总结 / RAG 索引
-> FFmpeg 低码率音频 + 300 秒切片 ASR
-> ASR 全文切 chunk 写 Milvus
-> 向量召回 + 关键词召回 + RRF
-> SSE token streaming 返回答案和引用
-> BYOK、AI 审计、重试、task_jobs 保证公开部署和排障边界
```

### 最推荐讲的 4 个故事

1. 长视频 ASR 从失败 / 过短，到 FFmpeg 压缩、300 秒切片、分片状态持久化。
2. Kafka 只解决异步削峰，不解决全部业务重试，所以补 DB retry scheduler 和 dead 状态。
3. RAG 不用总结做知识库，而是 ASR 全文切 chunk，Milvus 过滤隔离，再用 BM25 风格关键词召回和 RRF 提升召回。
4. 公开部署不能消耗服务端 Key，所以做 BYOK、Key 加密、AI 调用审计和每日用量聚合。

### 面试边界

可以说：

- 已实现单视频 RAG。
- 已实现 provider 级 SSE token streaming。
- 已实现 Go 侧 BM25 风格关键词召回 + RRF。
- 已实现 AI 调用审计和每日用量聚合。
- 已实现分片级 ASR 状态复用。

不要说：

- 已经做成跨视频知识库。
- rerank 模型已经落地。
- 已经具备生产级计费系统。
- ASR 分片已经并行处理。
- 已经接入 Elasticsearch。
- 已经做成完整工作流引擎。

## 7. 面向简历的全方位解析

### 项目定位怎么定

这个项目不要包装成“高并发视频平台”，也不要包装成“套壳 AI 应用”。更稳的定位是：

```text
面向视频内容理解的大模型应用后端项目。
核心亮点是长任务异步化、长视频 ASR 稳定性、RAG 检索问答、用户级模型配置和 AI 调用可观测。
```

这样写的好处是：

- 比普通 CRUD 更有辨识度。
- 比秒杀、优惠券、商城这类项目少一些同质化。
- 能自然引出 Kafka、Redis、MinIO、Milvus、FFmpeg、大模型 API、RAG、SSE、AI 成本控制。
- 被追问时有真实代码和排障文档支撑。

### 简历亮点排序

如果投后端开发，亮点优先级建议：

1. Kafka 异步任务和业务级重试。
2. 长视频 ASR 切片和分片级恢复。
3. RAG 检索链路和混合召回。
4. 用户级 BYOK 和 API Key 加密。
5. task_jobs、trace_id、AI 调用审计这类可观测设计。
6. 分片上传、MinIO、MD5 去重。
7. 前端 Vue 展示和 SSE 体验。

如果投 AI 应用开发，亮点优先级建议：

1. RAG 使用 ASR 全文作为知识源，而不是 AI 总结。
2. Milvus 向量检索 + Go 侧 BM25 风格关键词召回 + RRF。
3. OpenAI-compatible Chat / Embedding 抽象和 provider 级 streaming。
4. BYOK 模型配置，ASR / LLM / Embedding 分离。
5. RAG 离线评估 Recall@K / MRR / no result rate。
6. prompt 约束、引用片段、上下文记忆和幻觉控制。

### 一份简历可以怎么写

```text
镜知 VidLens - AI 视频内容理解平台
技术栈：Go、Gin、GORM、MySQL、Redis、Kafka、MinIO、Milvus、FFmpeg、yt-dlp、Vue3

项目描述：VidLens 是一个面向视频内容理解的大模型应用后端项目。用户上传本地视频或提交公开视频链接后，系统异步完成视频下载、音频提取、ASR 转写、LLM 摘要，并基于视频转写全文构建 RAG 索引，支持带引用片段的单视频多轮问答。

职责与亮点：
1. 引入 Kafka 将视频下载、ASR 转写、摘要生成和 RAG 索引构建等长耗时任务异步化，HTTP 接口仅负责任务落库和消息投递，避免分钟级视频处理阻塞请求链路。
2. 设计 Redis 分布式锁 + WatchDog，结合 MD5 内容指纹和视频文件复用，保护分片合并、分析任务消费等临界区，降低并发重复入库和重复处理风险。
3. 采取分片上传与断点续传机制，通过 Redis Set 记录分片状态，使用 MinIO 存储分片并通过 ComposeObject 合并，提升弱网环境下大文件上传可靠性。
4. 基于 Redis Lua 令牌桶限流，按用户和接口维度限制高成本 AI 请求，缓解恶意请求和重复点击带来的外部 AI 服务调用成本。
5. 基于 RAG 检索增强生成实现视频智能问答，将转写文本切分为 chunk 并生成向量写入 Milvus，结合 BM25 关键词召回与 RRF 融合排序，返回引用片段提升答案可解释性。
6. 设计任务失败治理机制，基于错误分类和阶梯退避重试处理外部依赖异常，对可恢复错误延迟重投递，对不可恢复错误快速失败，避免重试风暴和 AI 成本浪费。
```

### 这份简历的风险点

面试官最可能追问这些：

- Kafka 为什么不直接用不提交 offset 重试？
- 业务级 retry scheduler 怎么避免重复投递？
- Redis 锁和 Kafka key 路由是不是重复？
- 长音频为什么按 300 秒切片？有没有上下文断裂？
- RAG 为什么不用总结？chunk 怎么切？
- Go 侧 BM25 是不是伪 BM25？为什么不直接上 ES？
- BYOK 的 Key 怎么加密？密钥丢了怎么办？
- AI 调用审计是不是会泄露 prompt？
- SSE 和 WebSocket 怎么选？
- Milvus collection 维度固定，用户换 embedding 维度怎么办？

这份文档后面就是按这些问题展开。

## 8. 项目真实性准备

### Q1：你这个项目完整架构是什么？

可以分六层讲：

第一层是用户和模型配置。用户注册登录后，使用 JWT 鉴权。AI 能力不默认使用服务端 Key，而是要求用户配置自己的 ASR、LLM、Embedding 服务。Key 会用 AES-GCM 加密入库，接口只返回脱敏值。

第二层是视频入库。用户可以普通上传、分片上传，也可以提交 B 站或 YouTube 链接。普通上传会边读边计算 MD5；分片上传用 Redis Set 记录已上传分片，最终通过 MinIO ComposeObject 合并；URL 上传会先做白名单和 DNS 安全校验，再投递 Kafka 下载任务。

第三层是异步任务。视频下载、转写、总结、RAG 索引分别对应 Kafka topic。HTTP 请求只做校验、状态更新和消息投递，不等待真实处理完成。消费者处理成功才提交 offset。

第四层是视频处理。消费者从 MinIO 下载视频到临时文件，FFmpeg 提取 16k 单声道低码率音频，并按 300 秒切片。每个音频片段单独调用 ASR，结果写入分片表和最终转写表。

第五层是 RAG。转写完成后投递独立 RAG 索引 job。索引服务将 ASR 全文切 chunk，调用用户配置的 Embedding，写入 MySQL chunk 元数据和 Milvus 向量。问答时先检索相关片段，再结合 Redis 最近会话上下文调用 LLM。

第六层是失败治理和可观测。任务有 status、stage、task_jobs、trace_id、retry_count、next_retry_at、dead 状态；AI 调用有 ai_call_logs 和 user_usage_daily，方便定位模型调用失败和成本趋势。

### Q2：这个项目的核心业务流程是什么？

以用户点击“提取文字”为例：

```text
前端点击提取文字
-> 后端校验 JWT、任务归属和任务状态
-> video_tasks 条件更新为 queued/transcribing
-> task_jobs 写入 transcribe queued
-> Kafka 投递 video-transcribe 消息
-> HTTP 返回 task_id
-> 前端轮询任务详情或展示状态
-> consumer 读取消息
-> 条件更新为 running/transcribing
-> 从 MinIO 下载视频
-> FFmpeg 提取音频并切片
-> 按用户 AI profile 调用 ASR
-> 写 video_transcription_chunks
-> 合并写 video_transcriptions
-> 投递 rag_index job
-> task_jobs 标记 transcribe completed
-> video_tasks 回到 completed/none
```

如果用户点击“AI 总结”，流程类似，但总结前会先检查是否已有转写。如果已有转写，就复用转写文本直接调用 LLM；如果没有，先走 ASR，再总结。

### Q3：这个项目有什么真实问题，不是纸上设计？

有几个真实排障记录：

1. 长视频 ASR 失败。15 分钟视频因为 base64 后超过 10MB 限制失败，压缩后又出现“完成但只有几百字”的问题，最后改为固定时长切片。
2. RAG 接入时状态查询缺失。前端调用 GET 状态接口，被 SPA fallback 返回 HTML，HTTP 200 但 Content-Type 不对，导致前端误判。
3. Milvus 容器端口监听但服务未 ready。后端启动时加 5 秒初始化超时，RAG 不可用时基础功能仍可运行。
4. B 站链接在服务器上返回 412。最后区分为平台风控，给出 cookies 配置和本地上传兜底。
5. RAG 重建索引旧向量未清理。后来重建前按 user/task/model 删除旧向量，删除失败则不替换 MySQL chunks。
6. Kafka 业务失败不能只靠 offset 重放。后来补了错误分类、退避重试和 dead 状态。

这些都记录在 `docs/troubleshooting-and-interview-notes.md`，不是临时编的话术。

### Q4：你项目里的难点体现在哪里？

我会把难点分成三类：

第一类是长任务稳定性。视频下载、FFmpeg、ASR、Embedding、LLM 都可能很慢或失败，不能放在 HTTP 请求里同步执行，也不能失败后让用户一直转圈。

第二类是大模型应用的不确定性。ASR 可能有请求体限制，LLM 输出可能不稳定，Embedding 维度可能不匹配，RAG 可能检索不到正确上下文。需要做切片、维度校验、prompt 约束、引用片段和评估。

第三类是公开部署的成本和安全边界。不能让所有用户默认消耗服务端 Key，也不能把用户 API Key、prompt、视频链接 query 等敏感信息写进日志或明文数据库。

### Q5：项目价值怎么讲？

从产品角度讲，它解决的是长视频内容消费效率低的问题。用户不用完整观看一段长视频，可以先看转写和总结，再基于视频内容提问。

从工程角度讲，它是一个把大模型能力接入真实后端流程的例子。不是只调一个 chat API，而是处理文件、异步任务、失败恢复、向量检索、权限隔离、成本归属和可观测。

从求职角度讲，它能体现三个能力：

- 后端工程化：Kafka、MySQL 状态机、Redis、MinIO、任务重试。
- AI 应用开发：ASR、LLM、Embedding、RAG、SSE streaming。
- 问题排查：日志、数据库状态、测试、复盘文档。

### Q6：项目目前有哪些不足？

要主动说边界，反而更可信：

- 当前是单体后端，不是微服务。
- 当前 RAG 是单视频问答，不是跨视频知识库。
- 当前没有 rerank 模型，只有向量召回、关键词召回和 RRF。
- 当前 ASR 分片是串行处理，还没有并发控制和片段重叠去重。
- 当前 AI 调用审计只记录字符数，不是 provider token 级精确计量。
- 当前 task_jobs 只保留每类动作当前状态，不保留每次 attempt 的完整历史。
- 当前没有完整管理后台，AI 调用日志和用量聚合还没有可视化。

## 9. Kafka 异步任务专项

### 背景痛点

视频处理天然不适合同步 HTTP：

- URL 下载可能受网络和平台风控影响。
- FFmpeg 是 CPU/IO 密集操作。
- ASR 和 LLM 都依赖外部 API，耗时和失败不可控。
- RAG 索引还要调用 Embedding 并写 Milvus。

如果同步处理，接口可能几十秒甚至几分钟才返回，还会占用 Gin handler、数据库连接、临时文件和外部 API 请求资源。

所以 VidLens 的思路是：HTTP 只负责“接单”，Kafka consumer 负责“干活”，MySQL 负责“记录状态”。

### 具体实现

Kafka topic 按动作拆分：

```text
video-download    URL 下载
video-transcribe  ASR 转写
video-analyze     AI 总结
video-rag-index   RAG 索引
```

生产者：

- `segmentio/kafka-go` writer。
- `RequiredAcks=All`。
- `MaxAttempts=3`。
- 同步发送，确保投递结果可知。
- 同一视频用 MD5 或 taskID 作为 key。

消费者：

- 每个 topic 一个 reader。
- 配置同一个 consumer group。
- `CommitInterval=0`，手动提交 offset。
- 业务成功或业务失败已落库后 commit。

### Q1：为什么不用同步接口？

同步接口适合短平快的请求，不适合视频处理。用户上传或提交链接后，真正耗时的是下载、转码、ASR、LLM 和 Embedding。同步等待会导致接口超时、用户体验差，也会让服务端资源被长时间占用。

异步化后，接口只需要返回任务 ID，前端通过轮询或任务详情查看状态。用户不需要等待真实处理完成，后端也可以通过消费者数量控制处理速度。

### Q2：Kafka 和任务状态之间怎么配合？

Kafka 负责消息传递，MySQL 负责业务状态。

举例：用户提交转写时，后端先把任务从 pending 更新为 queued/transcribing，再投递 Kafka。如果投递失败，会把状态恢复或标记失败。consumer 拿到消息后再条件更新为 running/transcribing，处理成功写 completed，处理失败写 failed 或 dead。

这样即使 Kafka 消息重复消费，业务也可以通过状态条件更新和幂等检查避免重复执行。

### Q3：为什么不能只靠 Kafka offset 重试？

因为 Kafka offset 重试无法区分业务错误类型。

如果是网络 timeout，不提交 offset 可以让消息重来；但如果是用户没配置 API Key、权限错误、B 站 412 这种不可重试错误，不提交 offset 会让同一分区一直卡住，后面的正常任务也无法处理。

所以我把“消息消费进度”和“业务任务重试”拆开：业务失败先落库，记录错误类型和 next_retry_at，然后提交 offset。后续由 DB retry scheduler 重新投递。

### Q4：你怎么处理消息重复消费？

有三层：

第一层是 Kafka key 路由。同一视频使用 MD5 作为 key，尽量进入同一分区。

第二层是任务状态条件更新。只有 pending、queued、failed 等允许状态才能更新为 running，状态不匹配说明任务已经被处理或正在处理。

第三层是 Redis 分布式锁。分析任务按视频 MD5 加锁，避免重复 ASR / LLM 消耗。

这三层不是互相替代，而是分别处理消息路由、数据库状态和并发执行。

### Q5：消息会不会丢？

生产侧使用同步写入和 `RequiredAcks=All`，发送失败会返回错误，HTTP 层可以把任务状态写失败。单机 Kafka 的 replication factor 是 1，所以不能夸成多副本高可用；它在本地 demo 环境里保证的是 broker 正常情况下的持久化。

消费侧是业务处理成功后手动 commit。业务已经落库失败并交给 retry scheduler 时，也会 commit，避免阻塞分区。

### Q6：消息积压怎么办？

先判断积压发生在哪里：

- URL 下载慢：可能是外网、平台风控、yt-dlp。
- ASR 慢：可能是 FFmpeg、ASR provider、长视频切片太多。
- RAG 慢：可能是 Embedding 或 Milvus。
- LLM 慢：可能是模型响应或 prompt 太长。

优化方式：

- 增加消费者实例，让消费者组分摊分区。
- 增加 topic 分区数。
- 对不同动作拆 topic，避免 RAG 索引拖住 ASR。
- 对 AI provider 做限速和重试。
- 对长视频做切片级状态复用。

当前项目已经拆了 topic 和 job，但没有做动态扩缩容。

### Q7：RAG 索引为什么拆成独立 Kafka job？

因为 ASR 和 RAG 的失败边界不同。ASR 成功后，用户应该能看到转写文本；Embedding 或 Milvus 失败只影响问答索引，不应该让文字提取被认为失败。

所以转写 consumer 保存 transcription 后只投递 `rag_index` 消息，然后完成转写任务。RAG consumer 独立构建索引，失败进入 rag_index 的 job 状态和 retry。

### Q8：DB retry scheduler 怎么避免重复调度？

调度器扫描 `next_retry_at <= now` 的失败任务后，不是直接投递，而是先用条件更新 claim 任务，把它改成 queued 或 running。只有 claim 成功的实例才投递 Kafka。

如果投递 Kafka 失败，会恢复 failed 状态并设置一个短退避时间，但不增加业务 retry_count，因为业务逻辑还没有真正重跑。

### Q9：为什么不用 Kafka 延迟消息？

Kafka 本身不像 RocketMQ 那样开箱支持业务级延迟消息和死信队列。可以用延迟 topic、时间轮或外部调度器实现，但对这个项目来说，MySQL 已经是任务状态源，用 DB scheduler 可以把 retry_count、next_retry_at、last_error 和前端展示打通。

这个选择不一定适合所有项目，但适合 VidLens 当前单体后端和任务状态可见性的需求。

### Q10：如果让你继续优化 Kafka 链路？

我会做：

- 给每个 topic 配独立 consumer group 和并发配置。
- 增加消费者处理耗时、积压数量、失败率指标。
- 任务 job 增加 attempt 表，记录每次尝试耗时和错误。
- 对 ASR / Embedding / LLM provider 增加并发和速率控制。
- RAG 索引支持批量 embedding，减少 per chunk 请求次数。

## 10. Redis 分布式锁与去重专项

### 背景痛点

视频处理和 AI 调用成本高，同一个视频被重复提交会浪费：

- FFmpeg 计算资源。
- ASR 调用成本。
- LLM token。
- Embedding token。
- 存储空间和任务记录。

所以项目做了两类去重：

- 内容级去重：基于 MD5 和 `video_assets` 复用文件对象。
- 执行级防重：基于 Redis lock 防止同一视频并发处理。

### 当前实现事实

当前 `internal/pkg/lock/redis_lock.go`：

- 加锁使用 `SetNX(key, value, ttl)`。
- value 是当前时间纳秒字符串，用作 owner 标识。
- watchdog 每 `ttl/3` 续期。
- 续期用 Lua 校验 owner 后 `expire`。
- 释放用 Lua 校验 owner 后 `del`。

所以面试里不要说“用了 Redisson”，应该说“参考 Redisson WatchDog 思路，自实现 Redis 分布式锁”。

### Q1：为什么不用本地锁？

本地锁只能保证单进程内互斥。VidLens 的消费者后续可以多实例部署，如果两个实例同时消费同一视频任务，本地锁互相看不见。

Redis 锁是跨进程共享的，适合当前单体多实例的互斥需求。

### Q2：为什么普通 SETNX 不够？

普通 SETNX 如果不加过期时间，进程崩溃会死锁；如果加固定过期时间，视频处理时间不确定，锁可能在任务还没完成时过期，另一个消费者又拿到锁。

所以项目加了 watchdog 续期。只要持锁任务还活着，就定期续期；如果进程崩溃，watchdog 停止，锁最终会自然过期。

### Q3：释放锁为什么要校验 owner？

因为可能出现这种场景：

1. A 拿到锁。
2. A 因为暂停或网络问题，锁过期。
3. B 拿到同一个 key 的新锁。
4. A 恢复后执行 unlock。

如果 unlock 不校验 owner，A 会把 B 的锁删掉。Lua 脚本把 “get 判断 owner + del” 做成原子操作，可以避免误删。

### Q4：watchdog 会不会造成死锁？

正常不会。watchdog 只在当前进程存活且没有调用 unlock 时续期。如果进程崩溃，goroutine 停止，Redis key 仍有 TTL，最终会过期。

风险是：如果业务逻辑卡死但进程还活着，watchdog 会持续续期。生产环境可以加任务级最大执行时间、context timeout 和人工 dead 状态处理。

### Q5：Redis 锁和 MySQL 唯一索引有什么区别？

MySQL 唯一索引适合做数据层兜底，例如 `video_assets.file_md5` 保证同一个内容只落一个资产记录。

Redis 锁适合做执行层互斥，例如避免两个消费者同时对同一视频跑 ASR / LLM。

两者解决的问题不同。唯一索引不能阻止两个线程同时开始高成本处理，只能在写库时发现冲突；Redis 锁能在执行前拦截。

### Q6：如果 Redis 挂了怎么办？

当前实现里获取锁失败会让任务失败，后续可以进入可重试链路。Redis 是这个项目分布式锁、限流、分片状态和会话热记忆的重要依赖。

如果生产化，需要：

- Redis 高可用。
- 锁获取失败进入可重试错误。
- 对非关键缓存例如 chat memory 可以降级到 MySQL。
- 对上传分片状态需要更谨慎，因为 Redis 丢失会影响断点续传。

## 11. 上传、MinIO 与对象存储专项

### 上传链路

VidLens 支持三种入库方式：

1. 普通上传。
2. 分片上传 + 断点续传。
3. URL 下载后入库。

本地上传和 URL 下载最终都会形成：

```text
video_assets  视频物理资产
video_tasks   用户维度任务记录
MinIO object  实际视频对象
```

### Q1：为什么用 MinIO，不直接存本地？

本地磁盘会把业务服务和文件存储耦合在一起：

- 多实例部署时文件不可共享。
- 服务迁移或重启时文件管理麻烦。
- 预签名下载、对象合并、生命周期管理都要自己做。

MinIO 提供对象存储语义，和 S3 兼容，适合存视频、分片和临时合并对象。业务数据库只保存 object name，不保存大文件。

### Q2：分片上传怎么实现？

流程：

```text
前端计算文件 MD5
-> 调用 check-upload 查询已上传分片
-> 上传缺失 chunk
-> 每个 chunk 落 MinIO chunks/{md5}/{index}
-> Redis Set 记录 chunk index
-> 所有分片成功后调用 merge-chunks
-> 后端校验 Redis Set 是否包含 0..total-1
-> MinIO ComposeObject 合并为 videos/{uuid}.ext
-> 写 video_assets 和 video_tasks
```

### Q3：为什么分片状态用 Redis Set？

因为只需要回答“某个分片是否已经上传”。Set 的 `SAdd`、`SIsMember`、`SMembers` 很适合这个场景。

相比 MySQL：

- Redis 更轻量，适合临时进度状态。
- 分片状态有 24h 过期，不需要长期保存。
- MySQL 更适合保存最终任务和资产记录。

### Q4：合并时怎么防并发？

`MergeChunks` 会对 `vidlens:merge:{fileMD5}` 加 Redis 锁。这样即使前端超时重试或用户重复点合并，也只有一个请求执行 MinIO ComposeObject。

合并前还会检查 `video_assets` 是否已有同 MD5 资产。如果已有，直接复用资产创建任务。

### Q5：同一个视频多个用户上传，会发生什么？

会共享同一个 `video_assets`，但创建各自的 `video_tasks`。

这样做的好处是节省 MinIO 存储。删除任务时不能直接删对象，要先统计是否还有其他任务引用这个 asset。当前 `DeleteTask` 已经做了 active refs 统计，最后一个引用删除时才删 MinIO 对象和 asset 记录。

### Q6：分片上传到 99% 网络断了怎么办？

前端重新进入上传流程后，再调用 `check-upload?file_md5=xxx`。后端从 Redis Set 返回已上传分片列表，前端只补传缺失分片。

当前 Redis 分片状态 TTL 是 24h。如果超过 TTL，进度可能丢失，需要重新上传。后续可以用 MinIO list objects 兜底恢复分片状态，或者把分片元数据持久化到 MySQL。

### Q7：碎片文件怎么处理？

当前代码主要解决上传和合并链路，没有完整实现 MinIO lifecycle 清理。可以这样回答：

当前 Redis 状态 24h 过期，但 MinIO 的 `chunks/{md5}/...` 临时对象还需要后续加生命周期策略或定时清理。这个是我不会夸大的点。生产化时可以：

- MinIO bucket lifecycle 自动清理 chunks 前缀。
- 合并成功后异步删除分片对象。
- 定时任务扫描超过 TTL 的 chunks。

### Q8：URL 下载为什么也入 Kafka？

URL 下载会受外部网站、带宽、代理、cookies、平台风控影响，时间不可控。早期如果放在 HTTP 里同步下载，接口会卡住。

现在 URL 提交只创建 downloading 任务并投递 Kafka，下载成功后回写 asset 和 task，失败后记录错误并可重试。

### Q9：B 站 412 怎么处理？

B 站在服务器公网 IP 上可能触发风控，yt-dlp 返回 412。这个不是后端上传逻辑错误，也不是 AI 配置问题。

当前处理：

- 错误信息明确提示 B 站风控。
- 支持配置 cookies_path。
- 用户可以改用本地视频上传。
- 不把 412 当作可无限重试错误。

## 12. Redis 令牌桶限流专项

### 背景

AI 接口有成本，Embedding、LLM、ASR 都可能按量计费。公开部署时，即使用户自带 Key，也不能让接口被恶意刷爆；对服务端来说，Kafka、MySQL、Redis、MinIO 也需要保护。

所以对转写、总结、RAG 问答、RAG 索引等接口加了 Redis 令牌桶限流。

### 实现方式

`internal/middleware/ratelimit.go` 使用 Redis Lua 脚本：

- Redis Hash 保存 `tokens` 和 `last_time`。
- 每次请求按当前时间惰性补充令牌。
- 如果 tokens >= 1，则扣减并放行。
- 否则返回 429。
- key 60 秒过期。

限流 key 会带上 route 和 userID：

```text
rate_limiter:/api/v1/chat/sessions/:session_id/messages:user:123
```

未登录时可以按 IP。

### Q1：为什么用 Lua？

令牌桶一次请求涉及读 tokens、计算补充、扣减、写回。如果拆成多个 Redis 命令，在并发请求下会有竞态。

Lua 在 Redis 内部单线程执行，能把这几个步骤变成原子操作，同时减少多次网络往返。

### Q2：为什么限流器异常时放行？

当前代码 Redis 出错时返回 true，也就是 fail-open。这是一个取舍：避免 Redis 短暂异常导致核心业务全部不可用。

但生产上如果 AI 成本风险更高，可以对 AI 调用接口改成 fail-closed，或者按接口分级：

- 登录、查询类 fail-open。
- AI 调用类 fail-closed 或降级。

### Q3：限流和 Kafka 削峰有什么区别？

限流是在入口拦截请求，控制进入系统的速度。

Kafka 削峰是请求已经被接收后，把后续处理异步化，平滑消费者压力。

两者配合：

- 限流防恶意流量和成本爆炸。
- Kafka 防正常流量下的长任务阻塞。

### Q4：如果用户很多，按 route + user 限流够吗？

第一版够做基础保护，但还不完整。生产化还需要：

- 全局维度限流。
- provider 维度限流。
- 用户套餐或每日额度。
- Kafka consumer 侧按 provider TPM/RPM 控速。
- AI 调用失败率高时熔断。

## 13. 长视频 ASR 与 FFmpeg 专项

### 背景

长视频 ASR 有两个典型问题：

- 请求体过大。音频 base64 后体积会膨胀约三分之一。
- 单次音频太长，provider 可能只识别部分内容或超时。

项目真实遇到过 15 分钟视频失败和转写过短，所以这一块是最值得讲的真实案例。

### 当前流程

```text
MinIO 下载视频临时文件
-> FFmpeg 提取低码率音频
-> FFmpeg 按 300 秒切片
-> 每片调用 ASR
-> 每片写 video_transcription_chunks
-> 合并写 video_transcriptions
```

音频参数：

```text
-vn              去掉视频流
-ac 1            单声道
-ar 16000        16k 采样率
-acodec libmp3lame
-b:a 32k         32kbps
```

### Q1：为什么 16k 单声道？

语音识别主要需要人声信息，不需要音乐级音质。16k 采样率和单声道足够覆盖大多数语音识别场景，同时能显著降低文件体积和请求体大小。

### Q2：为什么按 300 秒切片？

300 秒是一个工程折中：

- 太长，仍然可能超出 ASR provider 的体积或时长稳定性边界。
- 太短，请求次数增加，成本和延迟上升。
- 5 分钟一段便于定位问题，也适合 15 到 60 分钟这类视频。

我不会说 300 秒是最优值。后续可以把它做成配置，并根据 provider 限制、音频大小和失败率动态调整。

### Q3：分片会不会切断句子？

会，这是风险。当前第一目标是完整性和可用性，先避免整段失败或只识别前几分钟。

后续优化：

- 切片加 5 到 10 秒 overlap。
- 文本层去重。
- 保存片段时间戳。
- 前端按片段展示进度。

### Q4：为什么分片结果要持久化？

因为如果第 3 段 ASR 失败，前 2 段已经成功，不应该重试时全部重跑。持久化后，下次重试可以先检查 `task_id + chunk_index`，completed 的片段直接复用。

这让长视频处理从“任务级重试”向“分片级恢复”演进。

### Q5：为什么总结要复用已有转写？

ASR 是成本高、耗时长、失败点多的步骤。如果用户先做了文字提取，再点击 AI 总结，系统应该直接使用已有转写文本，而不是重新下载、提音频、ASR。

这样减少成本，也让“文字提取”和“AI 总结”两个动作边界更清晰。

### Q6：ASR provider 怎么选？

项目支持 MiMo 和 SiliconFlow 这类 provider，也有 OpenAI-compatible 的 Chat / Embedding 抽象。面试里可以说：

我没有把 provider 写死，而是通过用户 AI profile 动态解析。ASR、LLM、Embedding 可分开配置，因为真实使用中这三类能力可能来自不同服务。

不要说：

- 所有 provider 都完全兼容。
- ASR 效果已经做过系统 benchmark。

当前更准确的说法是：项目做了 provider 接入抽象和配置校验，具体效果还需要按模型和数据集评估。

## 14. RAG 专项完整问答

### RAG 链路一句话

VidLens 的 RAG 是“基于单个视频 ASR 转写全文”的检索增强问答：先把转写文本切 chunk 并向量化写入 Milvus，用户提问时检索相关 chunk，再把检索片段、最近会话和问题交给 LLM，答案返回引用来源。

### Q1：为什么不是直接把整篇转写喂给 LLM？

因为长视频转写可能很长，直接放进 prompt 会遇到：

- 上下文窗口限制。
- token 成本高。
- LLM 处理慢。
- 无关内容干扰回答。

RAG 的价值是只召回和问题相关的少量片段，控制上下文长度。

### Q2：chunk 怎么切？

当前按 rune 字符数切：

- `chunk_size=800`
- `chunk_overlap=120`

这样实现简单，对中文转写文本比较直观。overlap 用来降低语义被边界切断的风险。

不要说当前使用了复杂 tokenizer。当前没有。

### Q3：为什么 Milvus collection 要固定 embedding_dim？

Milvus 的 FloatVector 字段维度是 collection schema 的一部分，不能同一个 collection 混用不同维度向量。

当前系统配置 `rag.embedding_dim=1536`，构建索引时会校验用户 profile 里的 embedding_dim 和实际返回向量长度。如果不匹配，会标记 RAG index failed。

后续如果支持多维度，有两种方案：

- 按维度拆 collection，例如 `vidlens_video_chunks_1024`、`vidlens_video_chunks_1536`。
- 统一要求用户使用系统指定维度。

当前选择第二种，复杂度更低。

### Q4：为什么检索要带 user_id 和 task_id？

这是权限隔离。

同一个 Milvus collection 里可能保存多个用户、多个视频的 chunks。如果检索 filter 不带 user_id，就可能跨用户召回；不带 task_id，就会变成跨视频问答。

当前项目定位是单视频问答，所以 filter 是：

```text
user_id == {userID}
and task_id == {taskID}
and embedding_model == "{model}"
```

### Q5：为什么加关键词召回？

纯向量检索适合语义相似问题，但对专有名词、英文缩写、数字、代码片段、固定术语可能不稳定。

关键词召回能补这类精确匹配。当前项目没有引入 ES，而是在单视频 chunk 数量有限的前提下，从 MySQL 读取 chunks 后做 Go 侧 BM25 风格打分。

这能作为第一版验证，不要夸成大型检索系统。

### Q6：RRF 是什么，为什么用？

RRF 是 Reciprocal Rank Fusion，基于排名做融合：

```text
score = 1 / (k + rank)
```

如果一个 chunk 同时被向量召回和关键词召回命中，它的融合分会更高。

我不用“向量分 + 关键词分直接相加”，因为两路分数不是同一尺度。RRF 更适合第一版混合检索，简单、稳定、可解释。

### Q7：如果检索不到结果怎么办？

当前会返回“未检索到足够相关的视频片段”，不让 LLM 硬答。

排查步骤：

1. 确认 RAG index 是否 indexed。
2. 确认 Milvus 是否可用。
3. 确认 embedding_dim 是否匹配。
4. 看 question embedding 是否成功。
5. 看 vector topK 是否为空。
6. 看 min_score 是否过高。
7. 看关键词 terms 是否提取合理。
8. 检查 chunk_size / overlap 是否导致内容被切断。

### Q8：检索结果和问题不匹配怎么办？

优化顺序：

1. 降低或调整 min_score，看是不是过滤太严格。
2. 增加 candidateK，宽召回后再融合。
3. 调整 chunk_size 和 overlap。
4. 优化关键词提取。
5. 引入 rerank。
6. 建评估集，用 Recall@K / MRR 对比。

当前项目已经有 RAG eval 核心，可以统计 Recall@K、MRR、无结果率和 source mix。

### Q9：RAG 怎么防幻觉？

几层：

- prompt 明确要求只能基于检索片段回答。
- 检索不到相关片段时直接返回错误，不让 LLM 编。
- 返回 citations，用户可以看到依据。
- retrieval snapshot 会和 assistant message 一起保存，方便复盘。
- 后续可以加答案覆盖率评估或 LLM-as-judge。

### Q10：检索知识和模型预训练知识冲突怎么办？

应该以检索片段为准。system prompt 里可以明确：

```text
当视频片段与通用知识冲突时，以视频片段为准。
如果片段不足以判断，直接说明无法从当前视频确认。
```

因为 VidLens 的场景是“问这个视频讲了什么”，不是问世界知识。

### Q11：为什么需要 citations？

RAG 问答如果只返回答案，用户不知道模型依据哪里回答，也无法判断有没有幻觉。

citations 至少包含：

- chunk_id
- chunk_index
- score
- content
- source: vector / keyword / hybrid

这样用户能追溯到视频转写片段。

### Q12：RAG 索引重建为什么要先删旧向量？

因为 MySQL chunks 和 Milvus vectors 是两套存储。

如果重建时只替换 MySQL，不删除 Milvus 旧向量，检索时仍可能召回旧内容。当前做法是重建前先按 `user_id + task_id + embedding_model` 删除旧向量；如果删除失败，就把索引状态标记 failed，不继续替换 MySQL chunks。

这个问题本质是关系型元数据和向量库数据的一致性。

### Q13：RAG 索引失败会不会影响转写？

当前设计尽量不影响。ASR 转写完成后保存 transcription，然后投递独立 rag_index job。RAG 失败只影响问答索引，不应该删除转写文本。

历史上因为 `video_tasks.status` 混合多个动作，RAG 失败可能会让主任务状态难解释；后续用 `task_jobs` 拆开动作状态来缓解。

### Q14：RAG 离线评估怎么做？

当前评估先不让 LLM 参与，而是评估检索质量：

每个 case 包含：

```text
question
expected_chunk_keywords
expected_answer_points
```

运行时调用 retriever，检查 topK citations 是否包含期望关键词，计算：

- Recall@K
- MRR
- no_result_rate
- average latency
- source_counts: vector / keyword / hybrid

这样能回答“有没有把正确上下文捞出来”，避免只看 LLM 最终回答是否顺眼。

### Q15：如果 top20 没命中，第 21 条才是关键怎么办？

先不要直接把 topK 喂给 LLM。可以：

- Milvus 先取 candidateK，比如 30 或 50。
- 关键词召回也取 candidateK。
- RRF 融合后再截 topK。
- 分数不稳定时动态扩大候选。
- 后续接 rerank 模型。

当前项目的 `candidate_k=30` 就是这个思路的第一版。

### Q16：上下文记忆怎么实现？

MySQL 保存完整 chat_messages，Redis 只缓存最近 N 轮：

```text
vidlens:chat:session:{sessionID}:recent
```

每次问答时，先读 Redis；miss 时从 MySQL 读取最近消息并回填。成功回答后保存 user 和 assistant 消息，再刷新 Redis。

这不是大模型真的有记忆，而是后端维护上下文窗口。

### Q17：多轮问答会不会污染检索？

当前检索 query 主要使用当前 question，不是把全部历史拼起来检索。历史主要参与 prompt，帮助 LLM 理解上下文。

后续可以做 query rewrite：结合历史把用户追问改写成完整问题，再做 embedding 检索。

### Q18：RAG 慢怎么排查？

拆阶段：

1. question embedding 耗时。
2. Milvus search 耗时。
3. MySQL keyword recall 耗时。
4. RRF 融合耗时。
5. Redis memory 读取耗时。
6. LLM 首 token 和总生成耗时。

优化：

- 缓存 query embedding。
- 减少 candidateK。
- 批量 embedding 索引。
- 使用更快的 embedding 模型。
- 限制 prompt 长度。
- 对 LLM 做 streaming，改善首屏体验。

### Q19：为什么现在没有 rerank？

rerank 会增加额外模型调用成本和延迟。对当前单视频 chunk 数量有限的项目，第一阶段先做向量 + 关键词 + RRF，并用评估集验证召回质量。

如果后续 Recall@K 仍不足，再加 rerank 更合理。

### Q20：RAG 和 Function Calling 有什么关系？

当前 VidLens 的 RAG 不是典型 Function Calling。它是后端固定流程：embedding、检索、prompt、LLM。

如果后续做 Agent，可以把“查询某视频任务信息”“构建 RAG 索引”“搜索视频片段”“生成总结”封装成 tools，由模型选择调用。但当前不要夸成 Agent 系统。

## 15. BYOK、AI 安全和调用审计专项

### Q1：为什么要做 BYOK？

公开部署时，如果所有用户都使用服务端 Key，成本不可控，也容易被滥用。BYOK 让用户自己承担模型调用成本，服务端只负责安全保存和调用。

### Q2：为什么 ASR、LLM、Embedding 要分开配置？

真实环境里这三类能力可能来自不同 provider：

- ASR 可能用 MiMo。
- LLM 可能用 DeepSeek 或 OpenAI-compatible router。
- Embedding 可能用另一个 endpoint。

如果强行共用 baseURL 和 Key，会限制用户选择，也容易请求路径拼错。

### Q3：API Key 怎么加密？

项目使用 AES-GCM：

- `security.api_key_secret` 作为密钥来源。
- 入库保存 ciphertext。
- 每次加密使用随机 nonce。
- 调用模型前解密。
- 返回前端只展示脱敏值。

### Q4：密钥丢了怎么办？

AES-GCM 是对称加密，服务端密钥丢失后，历史 API Key 无法解密。生产上需要：

- 使用稳定的 KMS 或安全密钥管理。
- 支持用户重新配置 Key。
- 做密钥轮换方案。

当前项目只实现了基础加密，不要说有完整 KMS。

### Q5：日志里怎么避免泄露？

原则：

- 不打印 API Key。
- 不打印 Authorization header。
- 不保存完整 prompt。
- 不保存完整模型响应。
- URL 日志去掉 query 和 fragment。
- AI call log 只保存字符数、耗时、状态、provider、model 和错误摘要。

### Q6：AI 调用审计表记录什么？

`ai_call_logs`：

- user_id / task_id / session_id
- kind: asr / llm / embedding
- provider / model
- status
- duration_ms
- input_chars / output_chars
- error_code / error_msg

`user_usage_daily`：

- asr_requests
- llm_requests
- embedding_requests
- failed_requests
- input_chars / output_chars
- asr_seconds 预留字段

### Q7：为什么不直接做 token 计费？

不同 provider 的 token 计算规则不同，ASR、Embedding、LLM 返回 usage 的格式也不完全一致。第一版先做字符数近似和请求次数聚合，解决排障和基础用量可见。

真正计费需要：

- provider usage 字段解析。
- token 单价配置。
- 用户套餐和余额。
- 扣减事务。
- 幂等账单。

当前不是完整计费系统。

### Q8：如果用户配置了恶意 endpoint 呢？

当前 BYOK 会请求用户填写的 LLM / Embedding / ASR endpoint，这里确实有安全边界问题。第一版主要面向用户自己配置可信 provider。

生产化可以：

- 限制 provider 类型和域名白名单。
- 禁止内网地址。
- 对 endpoint 做 SSRF 校验。
- 调用侧设置超时和响应体大小限制。

这个点不要隐瞒，主动说后续要做。

## 16. SSE 流式问答专项

### Q1：SSE 是怎么实现的？

HTTP handler 设置：

```text
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

Service 层先准备 RAG 上下文，然后 emit：

```text
event: citations
event: answer
event: done
```

如果 chat client 支持 `StreamingChatClient`，就调用 provider 的 stream 接口，每收到一个 delta 就转发给前端。如果不支持，就 fallback 到完整回答后切块。

### Q2：为什么第一版 SSE 不算真正 streaming？

因为第一版是先等 LLM 完整回答返回，再按 80 个 rune 切块发给前端。用户看到的是“后端分块”，不是“模型边生成边返回”。

后来在 AI client 层增加 `StreamChat`，对 OpenAI-compatible `/chat/completions` 设置 `stream=true`，逐行解析 `data: {...delta.content...}`，这才是 provider 级 token streaming。

### Q3：中途失败怎么办？

当前 StreamChat 成功结束后才保存 assistant message。如果上游中途失败，会发 error event，不保存半截 assistant message。

后续可以加 partial message 状态，记录已经生成的部分答案。

### Q4：为什么不用 WebSocket？

VidLens 的流式问答是服务端单向推送文本，不需要高频双向通信。SSE 基于 HTTP，简单、易调试、易复用现有鉴权和网关。

WebSocket 更适合多人协作、在线游戏、双向实时编辑这类场景。

### Q5：SSE 支持多轮吗？

支持。多轮不是 SSE 自身提供的，而是服务端保存会话历史。每次提问时读取最近几轮消息，加上检索片段和当前问题组装 prompt。

## 17. 数据库与表设计专项

### 核心表

```text
users
video_assets
video_tasks
task_jobs
video_transcriptions
video_transcription_chunks
ai_summaries
user_ai_profiles
video_chunks
video_rag_indexes
chat_sessions
chat_messages
ai_call_logs
user_usage_daily
```

### Q1：为什么有 video_assets 和 video_tasks 两张表？

`video_assets` 表示物理视频资产，按 MD5 去重；`video_tasks` 表示某个用户对这个视频发起的任务。

同一个视频可以被多个用户或同一用户多次引用。如果没有 asset/task 拆分，删除任务时容易误删共享视频对象，也不利于内容级去重。

### Q2：为什么转写和总结不放 video_tasks？

转写和总结是大文本，直接放在 task 主表会让任务列表查询变重。列表页通常只需要文件名、状态、时间和错误摘要。

所以大文本拆到：

- `video_transcriptions`
- `ai_summaries`
- `video_chunks`
- `chat_messages`

这是垂直拆表思路。

### Q3：为什么有 video_transcription_chunks？

记录 ASR 分片级状态：

- chunk_index
- audio_object / path
- content
- chars
- status
- error_msg

支持失败重试复用已完成片段，也方便后续展示进度。

### Q4：为什么有 video_rag_indexes？

仅看 `video_chunks` 是否存在，不足以表达 RAG 状态。索引可能正在构建、构建失败、Milvus 写入失败、旧向量删除失败。

`video_rag_indexes` 用来记录：

- not_indexed
- indexing
- indexed
- failed
- chunk_count
- last_error
- embedding_model
- embedding_dim

### Q5：task_jobs 为什么不是完整 workflow？

当前 `task_jobs` 是子任务当前状态表，不是工作流引擎。它按 `(task_id, job_type)` 唯一，记录 download/transcribe/analyze/rag_index 的当前状态和 retry 信息。

完整 workflow 还需要 DAG、依赖关系、attempt history、补偿事务、可视化编排。当前项目没有做到，所以不要夸大。

## 18. URL 下载和 SSRF 防护专项

### Q1：为什么 URL 下载有安全风险？

用户传入 URL，如果后端直接请求，可能被用来访问：

- localhost。
- 内网服务。
- 云厂商 metadata。
- Redis / MySQL 管理接口。
- 带敏感 query 的 URL 并写入日志。

这就是 SSRF 风险。

### Q2：当前做了哪些防护？

当前 `remote_video_url.go` 做了：

- 只允许 http / https。
- 域名白名单。
- 禁止 localhost。
- IP host 直接检查私网 / 回环 / multicast。
- 域名 host 先 DNS 解析，再检查解析结果。
- URL 日志脱敏，移除 query 和 fragment。
- YouTube watch URL 只保留 v 参数。

### Q3：还有什么不足？

DNS rebinding 仍然需要更严格处理。当前是在下载前解析并检查 IP，但 yt-dlp 实际请求时可能重新解析。

生产化可以：

- 代理层固定解析结果。
- 下载器运行在隔离网络。
- 对出站网络做防火墙限制。
- 更严格的 provider 白名单。

### Q4：为什么限制 720p？

业务核心是音频转写、总结和问答，不是高清视频存储。下载 4K 视频会增加带宽、磁盘、下载时间和转码成本，对 ASR 收益很小。

720p 对保留基本视频文件足够，也能避免 URL 下载接口和 worker 被大文件拖慢。

## 19. 前端和 AI Coding 怎么讲

### 前端定位

如果你的求职重点是后端，可以这样说：

前端是 Vue3 + Vite 的展示和联调界面，主要用于验证后端闭环：上传、任务列表、任务详情、AI 配置、转写、总结、RAG 问答和 SSE 展示。我主要精力在后端架构和接口稳定性上，前端更多是可视化验证平台。

### 前端生成流程

可以说：

1. 先整理后端接口。
2. 按模块生成：登录、任务列表、AI 配置、任务详情、RAG Chat。
3. 检查 API 路径、请求参数、响应 envelope。
4. 补异常状态，例如 401、任务失败、AI 未配置、RAG 未索引。
5. 加前端策略测试，例如 task polling、auth error、api envelope。

### AI Coding 怎么讲

不要说“都是 AI 写的”。可以说：

AI 主要帮助我提高编码效率和展开方案，但我自己负责：

- 识别真实问题。
- 判断技术方案。
- 限定边界。
- 看代码和测试。
- 跑验证。
- 写复盘文档。

举例：长视频 ASR 问题，AI 可以给出压缩、切片、重试等候选方向，但我通过数据库错误、转写长度和日志确认了两层根因，最后选择固定时长切片和分片状态持久化。

### AI Coding 不足怎么讲

- 长上下文容易遗忘，需要把关键设计写成文档。
- 容易编不存在的 API，要查官方文档或跑测试。
- 复杂联调问题仍然要靠日志、数据库和最小复现。
- 生成代码容易只覆盖 happy path，需要自己补异常和测试。

## 20. 高频八股和项目结合

### Kafka vs RocketMQ vs RabbitMQ

可以这样答：

Kafka 更适合日志型消息、堆积能力和消费者组扩展，Go 客户端也比较成熟。RocketMQ 延迟消息和事务消息能力更完整，但更偏 Java 生态。RabbitMQ 路由灵活，适合传统业务消息，但大规模堆积和流式消费不是它最强项。

VidLens 选择 Kafka，是因为当前是 Go 后端，重点是异步削峰、消费者组和任务日志式处理。Kafka 缺少业务级延迟重试这点，我用 DB retry scheduler 补上。

### Redis 数据结构怎么用

项目里：

- String：分布式锁。
- Hash：令牌桶 tokens 和 last_time。
- Set：上传分片状态。
- String JSON：最近会话记忆。

### MySQL 事务怎么用

删除任务时需要事务：

- 删除转写、分片、总结、RAG index、chat、task_jobs。
- 删除 task。
- 统计 asset 是否还有引用。
- 事务后再删除 MinIO 对象。

注意对象存储删除不放在数据库事务内，因为 MinIO 不是同一个事务资源。当前是先提交 DB，再删对象；失败时返回错误。

### 幂等怎么做

不同场景不同：

- 上传资产：MD5 + 唯一索引。
- 分片合并：Redis merge lock + asset 再查。
- Kafka 消费：状态条件更新 + Redis lock。
- RAG 重建：先删旧向量，再 replace chunks。
- AI 总结：已有 summary 或 transcription 时复用。

### 最终一致性怎么讲

视频任务涉及 MySQL、Kafka、MinIO、Milvus、外部 AI，不可能全局强事务。

项目采用最终一致性：

- 状态先落库。
- 消息投递失败写失败状态。
- 消费失败写 retry。
- RAG 失败独立记录，不删除转写。
- 删除任务时先清理业务元数据，再清理对象和向量。

## 21. 压力追问模拟

### 面试官：你这个项目是不是就是调 API？

不是。调 API 只是最后一步。项目里更核心的是把外部 AI 能力接入一个可恢复、可解释、成本可控的后端流程。

比如 ASR 不是直接上传整段音频，而是 FFmpeg 转码、切片、分片状态持久化、失败重试复用。RAG 不是把总结塞给 LLM，而是 ASR 全文切 chunk、Milvus 检索、关键词召回、RRF、引用片段和离线评估。公开部署也不是服务端统一 Key，而是用户 BYOK 和调用审计。

### 面试官：你为什么不用现成工作流引擎？

当前任务类型固定，只有 download、transcribe、analyze、rag_index，且都在单体后端内。引入 Temporal 这类工作流引擎会增加部署和学习成本。

我当前用 Kafka + MySQL 状态机 + task_jobs 解决最核心的问题：异步执行、状态可见、失败重试和动作边界。后续如果任务依赖变复杂、需要暂停恢复和完整 attempt history，再考虑工作流引擎。

### 面试官：你怎么证明 RAG 效果变好了？

不能只靠肉眼看回答顺不顺。我补了一个检索评估核心，case 里定义问题和期望命中的 chunk 关键词，跑检索后计算 Recall@K、MRR、无结果率和来源分布。

当前它证明的是检索质量，不直接证明最终回答质量。最终回答还需要 expected_answer_points、人工评审或 LLM-as-judge。

### 面试官：为什么不用 ES？

当前是单视频 RAG，每个 task 的 chunk 数有限，用 Go 侧 BM25 风格召回可以先验证混合检索和 RRF 链路，部署成本低，也方便测试。

如果后续做跨视频知识库、chunk 数量上升、需要复杂过滤和高性能关键词检索，我会把关键词召回迁移到 MySQL FULLTEXT/ngram 或 Elasticsearch。

### 面试官：Milvus 挂了怎么办？

启动时 Milvus 初始化有 5 秒超时。连不上时后端仍然启动，登录、上传、转写、总结等基础功能可用，但 RAG 索引和问答会明确提示向量数据库不可用。

运行中 Milvus 写入或检索失败，RAG index 会记录 failed，业务重试链路会对可重试错误设置 next_retry_at。

### 面试官：如果用户上传涉密视频，安全吗？

当前项目做了基础隔离：

- JWT 鉴权。
- 任务按 user_id 校验归属。
- Milvus 检索 filter 带 user_id。
- MinIO 使用私有 bucket 和预签名 URL。
- API Key 加密保存。
- AI 审计不保存完整 prompt/response。

但不能说已经达到企业级数据安全。生产还需要对象加密、审计后台、权限分级、日志脱敏策略、数据删除策略和合规评估。

## 22. 接口契约专项

### 为什么要准备接口契约

面试官如果怀疑项目真实性，很容易问：

```text
你这个功能具体哪个接口触发？
请求参数是什么？
前端怎么知道任务完成？
AI 配置怎么保存？
RAG 问答怎么发请求？
```

所以需要能讲出接口，而不是只讲架构。

### 用户接口

```text
POST /api/v1/user/register
POST /api/v1/user/login
GET  /api/v1/user/profile
```

注册和登录成功都会返回 JWT。后续接口通过：

```text
Authorization: Bearer <token>
```

鉴权。

可以这样讲：

用户体系不是项目核心，但它是后续 BYOK、任务归属、RAG 数据隔离的基础。没有用户维度，就无法回答“这个视频是谁的”“这个 API Key 属于谁”“Milvus 检索为什么不会跨用户”。

### AI 配置接口

```text
GET    /api/v1/ai/profiles
POST   /api/v1/ai/profiles
PUT    /api/v1/ai/profiles/:id
DELETE /api/v1/ai/profiles/:id
POST   /api/v1/ai/profiles/test
```

请求核心字段：

```json
{
  "name": "default",
  "llm_provider": "openai_compatible",
  "llm_base_url": "https://example.com/v1",
  "llm_api_key": "sk-xxx",
  "llm_model": "deepseek-chat",
  "asr_provider": "mimo",
  "asr_base_url": "https://token-plan-cn.xiaomimimo.com/v1",
  "asr_api_key": "tp-xxx",
  "asr_model": "mimo-v2.5-asr",
  "embedding_provider": "openai_compatible",
  "embedding_endpoint": "https://example.com/v1/embeddings",
  "embedding_api_key": "sk-xxx",
  "embedding_model": "text-embedding-3-small",
  "embedding_dim": 1536,
  "is_default": true
}
```

注意点：

- API Key 入库前加密。
- 更新时如果 key 为空，保留原密文。
- 返回前端的是 masked key。
- 同一个用户只应该有一个默认 profile。
- test 接口会验证 Chat、ASR、Embedding 是否可用，并检查 embedding 维度。

### 媒体任务接口

```text
POST   /api/v1/media/upload
POST   /api/v1/media/upload-url
POST   /api/v1/media/upload-chunk
GET    /api/v1/media/check-upload
POST   /api/v1/media/merge-chunks
GET    /api/v1/media/list
GET    /api/v1/media/task/:id
DELETE /api/v1/media/task/:id
POST   /api/v1/media/analyze/:id
POST   /api/v1/media/transcribe/:id
GET    /api/v1/media/task/:id/rag-index
POST   /api/v1/media/task/:id/rag-index
GET    /api/v1/media/download-audio/:id
```

核心链路：

```text
上传或 URL 入库 -> 得到 task_id
-> transcribe/analyze 投递 Kafka
-> 前端轮询 /media/task/:id
-> 任务完成后返回 transcription、summary、jobs、rag index 状态
```

### RAG Chat 接口

```text
POST /api/v1/chat/sessions
GET  /api/v1/chat/sessions?task_id=xxx
GET  /api/v1/chat/sessions/:session_id/messages
POST /api/v1/chat/sessions/:session_id/messages
POST /api/v1/chat/sessions/:session_id/messages/stream
```

普通问答响应：

```json
{
  "message_id": 101,
  "answer": "回答内容",
  "citations": [
    {
      "chunk_id": 12,
      "chunk_index": 3,
      "score": 0.82,
      "content": "被召回的视频片段",
      "source": "hybrid"
    }
  ],
  "model": "deepseek-chat"
}
```

SSE 流式事件：

```text
event: citations
data: [...]

event: answer
data: "delta text"

event: done
data: {"message_id": 101, "model": "..."}
```

### 统一响应格式怎么讲

项目有统一 response envelope：

```json
{
  "code": 200,
  "message": "success",
  "data": {}
}
```

错误时：

```json
{
  "code": 400,
  "message": "请先配置 AI 服务"
}
```

这能让前端统一处理错误，例如 401 跳登录、429 显示限流、400 显示业务错误。

### 面试追问：前端怎么知道异步任务完成？

当前主要是任务详情轮询。前端拿到 task_id 后定时请求：

```text
GET /api/v1/media/task/:id
```

后端返回 `status`、`stage`、`error_msg`、`jobs`、`transcription`、`summary` 等字段。前端根据状态显示处理中、失败原因或结果。

如果后续优化，可以加 WebSocket/SSE 任务通知，但当前任务状态轮询更简单，也足够支撑 demo。

### 面试追问：为什么 RAG 问答要先创建 session？

因为多轮问答需要会话边界。session 绑定 user_id 和 task_id，后续消息都在这个 session 下保存。这样：

- 能校验用户是否有权访问这个视频。
- 能按视频维度管理聊天历史。
- Redis recent memory key 可以按 session_id 缓存。
- retrieval snapshot 可以和 assistant message 绑定。

## 23. 用户鉴权与权限隔离专项

### 当前实现

用户注册时：

- 检查 username 是否存在。
- bcrypt 哈希密码。
- 默认 role 为 USER。
- 生成 JWT。

登录时：

- 根据 username 查用户。
- bcrypt 校验密码。
- 生成 JWT。

JWT claims：

```text
user_id
username
role
issuer = vidlens
expires_at
issued_at
```

中间件会校验：

- Authorization header 是否存在。
- 是否是 Bearer 格式。
- token 签名是否正确。
- issuer 是否是 vidlens。
- token 是否过期。

然后把 userID、username、role 写入 Gin context。

### Q1：为什么用 bcrypt？

密码不能明文存储，也不适合只做普通 hash。bcrypt 会自动加盐，并且有计算成本参数，可以提高暴力破解成本。

当前成本参数是 10，适合 demo 和普通服务。生产环境可以根据机器性能调整。

### Q2：JWT 有什么优点和风险？

优点：

- 无状态，后端不需要每次查 session。
- 前后端分离容易使用。
- token 内可以携带 user_id 和 role。

风险：

- token 签发后，在过期前默认难以主动失效。
- secret 泄露会导致伪造 token。
- token 存储在前端要注意 XSS。

生产化可以：

- 使用更短过期时间 + refresh token。
- Redis 维护黑名单或 token version。
- secret 通过环境变量或 KMS 管理。

### Q3：项目里的权限隔离有哪些？

至少有四层：

1. HTTP 层 JWTAuth。
2. Service 层校验 task.UserID == current userID。
3. Chat session 查询校验 user_id。
4. Milvus 检索 filter 带 user_id。

RAG 场景一定不能只靠前端传 task_id，因为恶意用户可以改 ID。后端每次都要查任务归属。

### Q4：AI profile 怎么防跨用户访问？

AIProfileRepository 里按 user_id 查询、更新和删除。即使用户知道别人的 profile id，也无法通过 `FindByIDForUser(userID, id)` 读到。

同时接口响应只返回脱敏 key，不返回明文。

### Q5：MinIO 里的视频是不是公开的？

不是。项目使用私有 bucket。用户下载音频或视频时，后端先校验任务归属，再生成短期预签名 URL。

预签名 URL 有时效性，适合临时下载，不需要把 bucket 设为 public。

### Q6：CORS 怎么配置？

当前 CORS 允许本地前端开发地址：

```text
http://localhost:8080
http://localhost:5173
http://127.0.0.1:5173
```

允许方法：

```text
GET, POST, PUT, DELETE, OPTIONS
```

允许 header：

```text
Origin, Content-Type, Authorization
```

生产环境不能直接放开 `*`，应该配置真实域名。

## 24. 部署、配置与启动专项

### 本地启动顺序

```text
docker-compose up -d
设置 VIDLENS_API_KEY_SECRET
检查 config.yaml 里的 FFmpeg / yt-dlp 路径
go run ./cmd/server
cd web && npm run dev
```

中间件：

- MySQL 8.0
- Redis
- MinIO
- Zookeeper
- Kafka
- Kafka UI
- Milvus
- Milvus Etcd

### 关键配置

```yaml
server:
  port: 8080

database:
  host: 127.0.0.1
  port: 3307

redis:
  host: 127.0.0.1
  port: 6379

minio:
  endpoint: localhost:9000

kafka:
  brokers:
    - localhost:19092
  analyze_topic: video-analyze
  transcribe_topic: video-transcribe
  download_topic: video-download
  rag_index_topic: video-rag-index

upload:
  max_file_size: 2147483648
  chunk_size: 5242880

rag:
  chunk_size: 800
  chunk_overlap: 120
  candidate_k: 30
  embedding_dim: 1536
```

### 环境变量

```text
VIDLENS_API_KEY_SECRET   用户 API Key 加密密钥
MIMO_API_KEY             本地兼容的服务端 MiMo Key
SILICONFLOW_API_KEY      本地兼容的服务端 SiliconFlow Key
```

公开部署时不要依赖服务端 AI Key。用户应该走 BYOK。

### Q1：Milvus 启动慢怎么办？

Milvus 容器 running 不代表服务 ready。项目启动时用 5 秒 context timeout 初始化 Milvus。超时后后端继续启动，基础功能可用，RAG 功能显示不可用。

这比直接 fatal 更适合本地开发和公开部署，因为向量库故障不应该影响登录、上传、转写等基础链路。

### Q2：FFmpeg 和 yt-dlp 为什么不放进 Go 代码？

FFmpeg 和 yt-dlp 是成熟的外部工具。Go 通过 `exec.CommandContext` 调用，配置里指定路径。

优点：

- 复用成熟生态。
- 避免自己实现复杂音视频处理。
- 通过 context 可以控制进程生命周期。

生产部署可以把它们打进 Docker 镜像，避免路径差异。

### Q3：端口为什么这样配？

MySQL 映射到 3307 是为了避开本机已有 MySQL 的 3306。Kafka 映射到 19092 是为了区分容器内外监听地址。MinIO 控制台是 9001，API 是 9000。Milvus 是 19530。

面试里不用背端口，但要知道每个中间件的角色。

### Q4：如果 docker-compose 起不来怎么排查？

按依赖顺序：

1. `docker compose config` 看配置是否有效。
2. `docker ps` 看容器是否运行。
3. 看 MySQL、Redis、MinIO、Kafka、Milvus logs。
4. 访问 `/health` 看后端是否启动。
5. 看后端日志里哪一个中间件初始化失败。

注意：后端对 MySQL、Redis、MinIO 是强依赖，对 Milvus 是可降级依赖。

### Q5：如何做生产部署？

当前项目主要是本地和演示部署。生产化建议：

- 后端和前端打 Docker 镜像。
- FFmpeg / yt-dlp 内置到后端镜像。
- MySQL、Redis、Kafka、MinIO、Milvus 使用托管服务或独立集群。
- 配置通过环境变量注入。
- 日志输出到 stdout，接日志系统。
- 增加 Prometheus metrics 和告警。
- 使用 HTTPS 和真实域名 CORS。

不要说当前已经是生产部署架构。

## 25. 监控告警与排障专项

### 为什么要准备这一章

参考文档里有“配置预警指标”的问题。VidLens 也很容易被问：

```text
你上线后看哪些指标？
任务失败了怎么知道？
AI provider 挂了怎么发现？
Kafka 积压怎么办？
磁盘满了怎么办？
```

当前代码有日志和审计表，但还没有完整 Prometheus。面试时要区分“已实现的可观测数据”和“生产建议指标”。

### 已实现的可观测数据

```text
trace_id
video_tasks.status/stage/error_msg
task_jobs.status/stage/retry_count/next_retry_at/last_error
video_transcription_chunks.status/error_msg/chars
video_rag_indexes.status/chunk_count/last_error
ai_call_logs.kind/provider/model/status/duration_ms
user_usage_daily.request counts
Kafka consumer 日志
```

### 推荐告警指标

#### 1. Kafka 消费积压

指标：

```text
每个 topic 的 consumer lag
```

告警：

```text
video-transcribe 或 video-rag-index lag 持续增长 5 分钟
```

含义：

- ASR provider 慢。
- Embedding 慢。
- 消费者数量不够。
- 某类任务失败重试过多。

#### 2. 任务失败率

按 job_type 统计：

```text
download_failed_rate
transcribe_failed_rate
analyze_failed_rate
rag_index_failed_rate
dead_task_count
```

如果 dead 任务上升，说明自动重试已经救不回来，需要人工或产品提示。

#### 3. AI provider 失败率

从 `ai_call_logs` 聚合：

```text
kind=asr/llm/embedding
provider
model
status=failed
duration_ms p95/p99
```

告警：

```text
某 provider 5xx / timeout 失败率超过阈值
LLM p95 超过 30 秒
Embedding 失败率持续上升
```

#### 4. RAG 检索质量

线上指标：

```text
no_result_rate
avg_citations_count
vector/keyword/hybrid source mix
```

离线指标：

```text
Recall@K
MRR
avg retrieval latency
```

#### 5. 存储和磁盘

要看：

- MinIO bucket 使用量。
- 临时目录空间。
- MySQL 磁盘。
- Kafka log 磁盘。
- Milvus 数据目录。

视频和音频临时文件很容易把磁盘打满。当前代码会 `defer os.Remove` 清理下载和音频临时文件，但异常退出仍可能留下文件，生产要加定时清理。

#### 6. Redis 健康

Redis 影响：

- 分布式锁。
- 令牌桶。
- 分片上传状态。
- chat recent memory。

指标：

- Redis 可用性。
- 命令延迟。
- 内存使用。
- key eviction。

### 面试答法

可以这样说：

> 当前项目已经把任务状态、子任务状态、RAG 索引状态、ASR 分片状态和 AI 调用元数据落库，能支持基本排障。真正上线时我会再补 Prometheus 指标，重点看 Kafka lag、各 job 失败率、AI provider 失败率和耗时、Milvus 检索失败率、MinIO 写入失败率、Redis 延迟和磁盘使用。

### 线上问题排查套路

用户反馈“问答不可用”：

1. 查用户是否有 AI profile。
2. 查 task 是否属于该用户。
3. 查 transcription 是否存在。
4. 查 video_rag_indexes 是否 indexed。
5. 查 Milvus 是否可用。
6. 查 ai_call_logs 里 embedding 是否失败。
7. 查 chat service 是否检索到 citations。
8. 查 LLM 是否失败。

用户反馈“任务一直处理中”：

1. 查 video_tasks status/stage。
2. 查 task_jobs 对应 job_type。
3. 查 retry_count 和 next_retry_at。
4. 查 Kafka consumer 日志。
5. 查外部 provider 日志或 ai_call_logs。
6. 查任务是否已进入 dead。

## 26. 测试策略专项

### 当前测试覆盖思路

项目测试不是只测工具函数，而是围绕风险点做：

- 配置解析。
- 用户 AI profile 加密和默认配置。
- OpenAI-compatible Chat / Embedding 请求格式。
- Streaming delta 解析。
- ASR 分片和分片状态复用。
- Kafka consumer 失败分类和 retry scheduler。
- RAG index 构建和维度校验。
- RAG 检索融合。
- ChatService 消息保存和 Redis recent memory。
- URL SSRF 校验。
- 删除共享 asset 的引用安全。
- 前端 API envelope、auth error、任务轮询策略。

### 后端测试命令

```powershell
go test ./...
```

重点定向命令：

```powershell
go test ./internal/mq -run Retry -v
go test ./internal/service -run RAGIndex -v
go test ./internal/service -run AskStream -v
go test ./internal/ai -run Stream -v
go test ./internal/service -run RAGEval -v
```

### 前端测试命令

```powershell
cd web
npm test
```

覆盖：

- auth error policy。
- API envelope。
- session 管理。
- task action policy。
- task polling policy。
- task detail policy。
- task list loading policy。

### Q1：为什么 repository 测试用 sqlite？

项目使用 GORM，很多 repository 逻辑可以用 sqlite in-memory 快速测试，比如唯一索引、CRUD、条件更新、软删除、关系预加载。

但要注意：sqlite 不能完全代表 MySQL。涉及 MySQL 特定语法、索引、字符集、锁行为时，仍需要集成测试。

### Q2：外部 AI 怎么测试？

不能在单元测试里真实调用 provider。当前使用 fake HTTP server 或 fake client：

- 验证请求路径和 body。
- 验证 header。
- 验证 streaming SSE delta 解析。
- 验证错误状态码处理。

这样测试稳定，也不消耗 token。

### Q3：Kafka 怎么测试？

当前大量 consumer 逻辑通过直接调用 handler 函数和 fake producer / fake repository 测试，避免单元测试依赖真实 Kafka。

真正 Kafka broker 的连通性属于集成测试或本地手动验证。

### Q4：RAG 怎么测试？

分层测：

- chunk splitter：短文本、长文本、overlap、空文本。
- RAGIndexService：chunks、embedding 维度、旧向量删除、失败状态。
- retrieval fusion：vector + keyword + RRF。
- ChatService：检索、prompt、消息保存、SSE。
- RAGEval：Recall@K、MRR、no result。

### Q5：测试还缺什么？

可以主动说：

- 缺少真实 MySQL/Kafka/Milvus 的集成测试流水线。
- 缺少 Playwright 端到端测试。
- 缺少真实视频样本的自动回归测试。
- 缺少 provider 级 token usage 兼容测试。
- 缺少压力测试和长时间稳定性测试。

## 27. 容量、性能与成本估算专项

### 为什么要准备

面试官可能问：

```text
一个 1 小时视频会产生多少 chunk？
Embedding 调多少次？
MinIO 存储会不会爆？
Kafka 分区怎么估？
AI 成本怎么控？
```

### 文件大小估算

配置：

```text
max_file_size = 2GB
chunk_size = 5MB
```

一个 2GB 文件最多约：

```text
2GB / 5MB = 409.6
```

即约 410 个上传分片。

如果面试官给 10GB 文件：

```text
10GB / 5MB = 2048
```

这时要考虑：

- 前端并发数限制。
- 服务端单 chunk 大小限制。
- MinIO 临时对象数量。
- Redis Set 大小。
- 合并耗时。
- lifecycle 清理。

当前项目 max_file_size 是 2GB，不要说已经支持 10GB 生产级上传。

### ASR 切片估算

默认 300 秒一段。

```text
15 分钟视频 -> 3 段
1 小时视频 -> 12 段
2 小时视频 -> 24 段
```

当前串行 ASR，耗时大约是每段 ASR 耗时之和。后续可以并发，但要受 provider 限流和成本控制约束。

### RAG chunk 估算

默认：

```text
chunk_size = 800 字符
overlap = 120 字符
step = 680 字符
```

如果 1 小时转写文本约 3 万字：

```text
30000 / 680 ≈ 44 chunks
```

Embedding 请求约 44 次。如果后续支持 batch embedding，可以减少 HTTP 往返。

### Prompt 成本估算

问答 prompt 包含：

- system prompt。
- topK citations。
- 最近 N 轮对话。
- 当前问题。

如果 topK=5，每个 chunk 约 800 字符，则仅检索上下文约 4000 字符。再加历史和问题，仍要控制在模型上下文和成本范围内。

优化：

- 限制 topK 最大 10。
- 限制 question 1000 字。
- recent_turns 默认 8。
- 检索不到结果不调用 LLM。

### Kafka 分区估算

当前 topic 创建 4 分区，适合本地和小规模 demo。分区数决定同一个 consumer group 内最大并行消费度。

如果后续用户量上升：

- ASR topic 可以增加分区。
- RAG topic 可以独立扩消费者。
- 不同 provider 可按用户或模型做限流。
- 注意同一视频 key 路由到同一分区，有利于顺序，但热点视频可能形成局部热点。

### 成本控制策略

当前已实现：

- BYOK，不默认消耗服务端 Key。
- Redis 令牌桶。
- 复用已有转写。
- RAG 检索不到结果不调用 LLM。
- AI call log 和 daily usage。

后续可做：

- 每日请求额度。
- provider usage token 统计。
- 账户余额。
- 按模型配置单价。
- ASR / Embedding / LLM 分项限额。
- 异常失败不重复扣费。

### 性能优化优先级

如果要优化响应体验：

1. LLM 使用 provider streaming，降低首字等待。
2. RAG 索引改 batch embedding。
3. ASR 分片并发，但要限流。
4. 热门 video chunks 缓存。
5. 任务进度更细粒度展示。

如果要优化系统吞吐：

1. 增加 Kafka 分区和消费者。
2. 拆不同 worker 类型。
3. 给 provider 调用做并发池。
4. MinIO、Milvus、MySQL 独立部署。

## 28. 代码走读路线

### 为什么要准备

面试官让你现场讲项目代码时，不要从文件树乱点。按主链路走，能显得更清楚。

### 路线一：从启动入口讲架构

从：

```text
cmd/server/main.go
```

讲：

1. 加载 config.yaml。
2. 初始化 MySQL 和 AutoMigrate。
3. 初始化 Redis、MinIO、Kafka。
4. 初始化 AI factory、AI profile、RAG index、Chat service。
5. Milvus 5 秒超时降级。
6. 启动 Kafka consumers。
7. 启动 retry scheduler。
8. 注册 Gin routes。

### 路线二：从“点击提取文字”讲业务

```text
handler/media.go
-> service/media.go RequestTranscribe
-> mq/producer.go EnqueueTranscribe
-> mq/consumer.go handleTranscribe
-> pkg/ffmpeg/ffmpeg.go
-> ai strategy
-> repository/transcription_chunk.go
-> repository/transcription.go
-> indexAfterTranscription
```

这条线最适合讲长任务、Kafka、FFmpeg、ASR 切片。

### 路线三：从“问问视频”讲 RAG

```text
handler/chat.go AskStream
-> service/chat.go AskStream
-> prepareRAGChat
-> embedding client
-> vector/milvus.go Search
-> repository/video_chunk.go SearchByBM25
-> retrieval_fusion.go FuseRetrievedChunks
-> buildRAGMessages
-> ai/chat.go StreamChat
-> saveChatExchange
```

这条线最适合讲 RAG、SSE、上下文记忆、citations。

### 路线四：从“公开部署”讲 BYOK

```text
handler/ai_profile.go
-> service/ai_profile.go
-> pkg/secret/crypto.go
-> ai/factory.go
-> mq/consumer.go strategyForTask
-> ai/observed.go
-> service/ai_observer.go
```

这条线最适合讲 Key 加密、用户级配置、AI 审计。

### 路线五：从“失败重试”讲可靠性

```text
mq/consumer.go recordTaskFailure
-> mq/retry.go isRetryableError
-> repository/task.go RecordRetryableFailure
-> repository/task_job.go RecordRetryableFailure
-> mq/retry.go RetryScheduler.RunOnce
-> producer.Enqueue*
```

这条线最适合讲 Kafka offset 和业务重试为什么拆开。

## 29. 不同岗位的讲法

### 后端开发岗位

重点：

- Kafka 异步任务。
- MySQL 状态机和 task_jobs。
- Redis 锁和限流。
- MinIO 分片上传。
- 重试、幂等、最终一致性。
- JWT 和权限隔离。

少讲：

- Transformer 原理。
- 太多 prompt 技巧。
- 前端 UI。

### AI 应用开发岗位

重点：

- ASR、LLM、Embedding 三类模型能力拆分。
- RAG 使用 ASR 全文，不用总结。
- Milvus filter 隔离。
- 向量 + BM25 风格关键词 + RRF。
- SSE token streaming。
- RAG eval。
- BYOK 和 AI 调用审计。

少讲：

- 纯上传细节过多。
- Kafka 八股过多。

### 全栈岗位

重点：

- Vue 前端作为后端闭环验证平台。
- 登录、AI 配置、任务列表、任务详情、RAG Chat。
- 前后端接口契约和错误处理。
- SSE 展示体验。

要主动说明：

前端不是主要亮点，主要用于完整展示业务流程。

### Java 后端岗位怎么迁移表达

如果面试官是 Java 技术栈，可以用对照说法：

```text
Gin 类似 Spring MVC 的 Web 层
GORM 类似 MyBatis-Plus / JPA 的 ORM 层
segmentio/kafka-go 对应 Java Kafka client
自实现 Redis lock 思路接近 Redisson WatchDog
Go context 类似请求级取消和超时传播
```

但不要把项目写成 Java，也不要说用了 SpringBoot 或 Redisson。

## 30. 面试前复习路线与自测清单

### 15 分钟速记版

如果临面前只剩 15 分钟，只背这 5 件事：

1. 项目一句话：VidLens 是 Go 后端的视频内容理解项目，核心是 Kafka 长任务、FFmpeg + ASR、RAG 问答、BYOK 和 AI 调用可观测。
2. 主链路：上传 / URL 入库 -> Kafka -> FFmpeg 切片 ASR -> 转写 / 总结 -> RAG 索引 -> SSE 问答。
3. 最硬案例：15 分钟视频 ASR 失败和转写过短，最后用低码率转码、300 秒切片、分片结果持久化解决。
4. RAG 亮点：ASR 全文做知识源，Milvus 向量检索 + Go 侧 BM25 风格关键词召回 + RRF，返回 citations。
5. 边界：没有跨视频知识库、没有 rerank、没有生产级计费、没有 ASR 并行、没有完整 workflow。

### 1 小时复习版

按这个顺序看文档：

1. `1. 简历推荐写法`：确认简历上写什么。
2. `2. 项目介绍背诵版`：背 30 秒和 2 分钟版本。
3. `9. Kafka 异步任务专项`：准备后端基础追问。
4. `13. 长视频 ASR 与 FFmpeg 专项`：准备真实排障案例。
5. `14. RAG 专项完整问答`：准备 AI 应用追问。
6. `15. BYOK、AI 安全和调用审计专项`：准备公开部署和成本问题。
7. `28. 代码走读路线`：准备现场看代码。
8. `30. 面试前复习路线与自测清单`：用问题反查薄弱点。

### 1 天准备版

上午：

- 先读 README，确认项目功能和截图。
- 读本文档第 1 到 8 章，整理自己的项目介绍话术。
- 打开 `cmd/server/main.go`，顺着启动流程画一遍架构图。

下午：

- 读 Kafka、Redis、MinIO、FFmpeg、RAG、BYOK 六个专项。
- 每个专项挑 3 个问题录音回答。
- 回答时强制说出代码路径，例如 `mq/consumer.go`、`service/chat.go`。

晚上：

- 跑一遍 `go test ./...` 和 `npm test`。
- 看 `docs/troubleshooting-and-interview-notes.md`，挑 3 个真实问题当 STAR 案例。
- 用下面的自测清单模拟连续追问。

### STAR 案例 1：长视频 ASR 失败

S：用户上传约 15 分钟视频，第一次提取文字失败，后续修复后任务完成但转写只有几百字。

T：需要保证长视频转写完整，并让失败可定位、可重试。

A：通过数据库 error_msg、转写长度和日志定位到两层问题：base64 请求体过大，以及单次长音频识别不完整。改用 FFmpeg 低码率转码，统一 300 秒切片逐段 ASR，并新增分片状态表复用已完成片段。

R：长视频从整段失败变成分片可恢复，AI 总结也能复用已有转写，降低重复 ASR 成本。

面试追问点：

- 为什么不是调大请求体限制？
- 为什么 300 秒？
- 分片切断句子怎么办？
- ASR 失败后怎么复用已完成片段？

### STAR 案例 2：Kafka 业务重试

S：Kafka 能提供 at-least-once，但业务失败类型复杂，不能都靠不提交 offset 重放。

T：要避免不可重试错误卡住分区，同时让可重试错误能延迟重试。

A：把 Kafka offset 和业务 retry 拆开。consumer 失败后先分类错误，可重试错误写 retry_count 和 next_retry_at，不可重试直接 failed，超过次数 dead。DB retry scheduler 到期后重新投递对应 topic。

R：失败任务状态前端可见，不会因为用户未配置 AI 或 B 站 412 这类错误阻塞同分区后续任务。

面试追问点：

- 为什么失败后还 commit？
- scheduler 怎么避免多实例重复投递？
- Kafka 延迟消息为什么不用？
- 投递重试消息失败怎么办？

### STAR 案例 3：RAG 从向量检索到混合召回

S：RAG 第一版纯向量检索，对术语、数字、英文缩写不一定稳定。

T：要提升单视频问答的召回稳定性，并保持实现可测试。

A：保留 Milvus 向量召回，同时从 MySQL chunks 做 Go 侧 BM25 风格关键词召回，再用 RRF 按排名融合。补 RAG eval，计算 Recall@K、MRR、无结果率和 source mix。

R：问答结果能返回 vector / keyword / hybrid 来源，检索优化不再只靠肉眼感觉。

面试追问点：

- 为什么不用 ES？
- RRF 为什么比直接加分稳？
- 没有 rerank 怎么办？
- 怎么证明检索效果提升？

### STAR 案例 4：公开部署 BYOK

S：如果公开部署使用服务端自己的 AI Key，陌生用户会消耗服务端 token。

T：需要把模型调用成本归属到用户，同时保护用户 Key。

A：设计 user_ai_profiles，ASR / LLM / Embedding 分开配置 provider、endpoint、model 和 Key。Key 使用 AES-GCM 加密保存，响应脱敏。模型调用前按 task.UserID 读取默认 profile，未配置则任务失败，不回退服务端 Key。补 AI call logs 和 daily usage。

R：公开部署时服务端不默认承担 AI 成本，也能按用户排查模型调用失败和用量趋势。

面试追问点：

- Key 怎么加密？
- 密钥丢了怎么办？
- 为什么不保存 prompt？
- 这是不是计费系统？

### 连续追问自测

用下面问题自测，答不上就回对应章节补：

1. 你这个项目一句话是什么？
2. 用户点击“提取文字”后，后端完整链路是什么？
3. Kafka consumer 为什么手动 commit？
4. 业务失败为什么不一直不提交 offset？
5. Redis 锁怎么避免误删别人的锁？
6. MD5 去重是在任务层还是资产层？
7. 分片上传合并时并发怎么处理？
8. Redis 分片状态丢了怎么办？
9. URL 下载怎么防 SSRF？
10. B 站 412 是什么问题？
11. 长视频为什么必须切片？
12. ASR 分片结果为什么要持久化？
13. RAG 为什么不用 AI 总结做知识库？
14. Milvus 为什么要带 user_id filter？
15. embedding 维度不匹配怎么办？
16. BM25 风格关键词召回解决什么问题？
17. RRF 怎么算，为什么不用直接加分？
18. SSE 和 WebSocket 怎么选？
19. BYOK 为什么 ASR、LLM、Embedding 分开配置？
20. API Key 怎么加密，日志怎么脱敏？
21. AI call log 记录什么，不记录什么？
22. task_jobs 解决什么问题？
23. 当前项目有哪些没有实现，不能夸大？
24. 如果线上任务一直 running，你怎么排查？
25. 如果用户说 RAG 问答胡说，你怎么排查？

### 回答节奏

每个问题尽量按这个结构：

```text
先说结论
-> 说项目里具体怎么做
-> 说为什么这么选
-> 说风险和后续优化
```

例子：

```text
为什么不用 WebSocket？

结论：因为我的场景是服务端向浏览器单向推送 LLM 生成文本，SSE 足够。
项目实现：handler 设置 text/event-stream，service 先 emit citations，再转发 provider delta，最后 emit done。
选型理由：SSE 基于 HTTP，鉴权、调试、部署都简单；WebSocket 更适合双向实时协作。
边界：如果后续要做任务进度主动推送或多人协作，再考虑 WebSocket。
```

### 面试当天不要临时改的东西

- 不要临时把 Kafka 说成 RocketMQ。
- 不要把自实现 Redis lock 说成 Redisson。
- 不要为了显得高级说已经有 rerank、ES、生产计费。
- 不要背固定性能数据，除非你现场能拿出压测或日志。
- 不要说“AI 都生成的”，要说 AI 辅助编码但你负责架构决策和验证。
- 不要回避不足。明确边界比硬吹更可信。

## 31. 最后背诵模板

### 开场模板

这个项目我想重点讲两条主线：第一是视频处理长任务怎么工程化，第二是大模型应用怎么从 demo 变成有边界的后端系统。

视频处理这条线，我用 Kafka 把下载、转写、总结、RAG 索引拆成异步任务，用 MySQL 状态机和 task_jobs 记录每个动作的状态和重试信息。长视频 ASR 这块，我实际遇到过 15 分钟视频失败和转写过短的问题，最后用 FFmpeg 低码率转码、300 秒切片和分片状态持久化解决。

大模型应用这条线，我没有直接把 AI 总结当知识库，而是用 ASR 全文做 RAG。转写文本切 chunk 后写 Milvus，问答时做向量召回和关键词召回，再用 RRF 融合，最后通过 SSE 返回答案和引用。公开部署方面，我做了用户级 BYOK、API Key 加密和 AI 调用审计，避免服务端 Key 被默认消耗，也方便排查模型调用问题。

### 结尾模板

这个项目目前还不是生产级系统，比如没有跨视频知识库、rerank、完整计费和工作流引擎。但它的价值在于我围绕真实的视频长任务和 AI 调用问题，做了异步化、重试、状态拆分、RAG 检索、BYOK 和可观测这些工程化改造。面试里我也会明确区分已实现内容和后续演进，不把 demo 说成生产系统。
