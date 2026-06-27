# 最终简历针对性准备文档

这份文档只围绕最终简历版本准备，不再扩散到所有项目细节。目标是：简历上写出的每一句，都能说清背景、流程、实现、选型、边界和代码证据。

## 1. 最终简历版本

项目名称：AI 视频内容理解平台

技术栈：Go、Gin、MySQL、Redis、Kafka、MinIO、Milvus、FFmpeg、Vue3

项目简介：

一个集成用户鉴权、视频上传、公开视频链接解析、ASR 转写、AI 摘要和 RAG 问答的全链路视频内容理解平台。针对视频处理中“长耗时阻塞、大文件弱网传输、并发重复处理、外部 AI 服务不稳定”等痛点，基于 Kafka + Redis + MinIO + Milvus 重构核心链路，实现视频处理异步化、上传可靠性和问答可解释性。

项目经历：

- 引入 Kafka 将视频下载、ASR 转写、摘要生成和 RAG 索引构建等长耗时任务异步化，HTTP 接口仅负责任务落库和消息投递，避免分钟级视频处理阻塞请求链路
- 设计 Redis 分布式锁 + WatchDog，结合 MD5 内容指纹和视频文件复用，保护分片合并、分析任务消费等临界区，降低并发重复入库和重复处理风险
- 采取分片上传与断点续传机制，通过 Redis Set 记录分片状态，使用 MinIO 存储分片并通过 ComposeObject 合并，提升弱网环境下大文件上传可靠性
- 基于 Redis Lua 令牌桶限流，按用户和接口维度限制高成本 AI 请求，缓解恶意请求和重复点击带来的外部 AI 服务调用成本
- 基于 RAG 检索增强生成实现视频智能问答，将转写文本切分为 chunk 并生成向量写入 Milvus，结合 BM25 关键词召回与 RRF 融合排序，返回引用片段提升答案可解释性
- 设计任务失败治理机制，基于错误分类和阶梯退避重试处理外部依赖异常，对可恢复错误延迟重投递，对不可恢复错误快速失败，避免重试风暴和 AI 成本浪费

## 2. 接下来要做什么

建议按这四步准备：

1. 先背项目主线：上传 / URL 入库 -> Kafka -> FFmpeg / ASR -> 摘要 -> RAG 索引构建 -> 问答。
2. 再逐条准备 6 个简历点：每条都能讲背景、流程、代码、选型、兜底。
3. 然后模拟连续追问：从 Kafka 问到重试，从 Redis 锁问到幂等，从 RAG 问到幻觉和召回率。
4. 最后做代码走读：能从 `cmd/server/main.go` 讲到 `internal/service`、`internal/mq`、`internal/pkg`、`internal/vector`。

不要先背一堆八股。面试官看项目时，最先判断的是“这个项目是不是你真的做过”。所以回答要先有业务链路，再把八股嵌进去。

## 3. 总体讲法

### 30 秒版本

这个项目是一个 AI 视频内容理解平台，用户可以上传本地视频或提交公开视频链接。后端不会在 HTTP 请求里同步处理视频，而是用 Kafka 把下载、ASR、摘要和 RAG 索引构建拆成异步任务。大文件上传用 Redis Set 记录分片状态、MinIO 存储并合并；相同视频用 MD5 内容指纹、视频文件复用和 Redis 分布式锁降低重复处理；高成本 AI 请求用 Redis Lua 令牌桶限流；问答部分基于 ASR 转写全文做 RAG，用 Milvus 向量召回、BM25 关键词召回和 RRF 融合，并返回引用片段。

### 2 分钟版本

VidLens 的核心问题是视频处理链路很长：用户上传视频后，后端可能要下载远程视频、提取音频、调用 ASR、生成摘要、做 Embedding、写 Milvus，最后还要支持问答。如果这些都同步做，接口很容易阻塞甚至超时。

所以我把系统拆成几层。入口层只做鉴权、参数校验、文件入库和任务创建。长耗时处理通过 Kafka 异步消费，任务状态和失败原因落到 MySQL 的 `video_tasks` 和 `task_jobs`。文件层用 MinIO 存储视频和分片，分片上传状态放 Redis Set，合并时用 ComposeObject。并发和成本层用 Redis 锁保护分片合并、分析任务消费等临界区，用 Redis Lua 令牌桶限制高成本 AI 请求。AI 问答层不是直接用摘要，而是把 ASR 全文切 chunk，生成向量写入 Milvus，同时在 MySQL 保留 chunk 元数据，问答时做向量召回、BM25 关键词召回和 RRF 融合，最后返回 citations。

这个项目的重点不是单纯调 AI API，而是把文件、异步任务、外部依赖失败、成本控制和 RAG 问答整合成一个可解释、可恢复的后端链路。

## 4. 简历点 1：Kafka 异步化

### 简历原句

引入 Kafka 将视频下载、ASR 转写、摘要生成和 RAG 索引构建等长耗时任务异步化，HTTP 接口仅负责任务落库和消息投递，避免分钟级视频处理阻塞请求链路

### 为什么放第一条

这是项目主线。视频处理天然长耗时，Kafka 引入以后，整个系统从“请求里处理视频”变成“请求创建任务，后台异步推进状态”。这条能自然引出：

- MQ 选型。
- 消费语义。
- 任务状态表。
- 重复消费和幂等。
- 失败重试和最终一致性。
- 前端任务状态轮询。

### 当前实现事实

- 使用 `segmentio/kafka-go`。
- topic 包括下载、转写、分析、RAG 索引构建。
- producer 同步发送，配置 `RequiredAcks=All` 和 `MaxAttempts=3`。
- consumer 处理下载、转写、摘要和 RAG 索引构建。
- 业务失败后不只靠 Kafka offset 重放，而是写 MySQL 失败状态，由 retry scheduler 重新投递。
- `cmd/server/main.go` 启动时初始化 Kafka topics、producer、consumer 和 retry scheduler。

### 推荐讲法

先讲业务问题：视频下载、ASR、LLM、Embedding 都可能超过 HTTP 请求可接受时间。

再讲改造方式：HTTP 请求只创建任务并投递 Kafka，真正耗时处理放到 consumer。

最后讲可靠性：Kafka 负责异步解耦，MySQL 负责业务状态，失败治理负责可恢复。

### 面试必须掌握

- Kafka 为什么适合长任务削峰。
- 为什么不能只用 goroutine。
- Kafka 至少一次语义为什么需要幂等。
- offset 提交和业务成功不是一回事。
- topic 拆分的好处。
- Kafka 和 MySQL 状态表如何配合。

### 代码证据

- `cmd/server/main.go`：Kafka 初始化和 consumer 启动。
- `internal/mq/producer.go`：producer 配置。
- `internal/mq/consumer.go`：下载、转写、分析、RAG 索引构建消费逻辑。
- `internal/mq/retry.go`：重试策略和失败分类。
- `internal/model/task.go`、`internal/model/task_job.go`：任务状态模型。

### 不要这么说

- 不要说用了 RocketMQ。
- 不要说 Kafka 提供严格 exactly-once。
- 不要说 Kafka 自带业务延迟重试。
- 不要说接口响应一定压缩到某个具体毫秒数，除非你有压测数据。

## 5. 简历点 2：Redis 分布式锁和 MD5 去重

### 简历原句

设计 Redis 分布式锁 + WatchDog，结合 MD5 内容指纹和视频文件复用，保护分片合并、分析任务消费等临界区，降低并发重复入库和重复处理风险

### 为什么写这条

它解决的是 AI 成本和并发幂等问题。相同视频如果重复转写、重复摘要、重复 RAG 索引构建，会浪费外部 AI 服务调用和向量存储资源。

### 当前实现事实

- 当前是 Go 自实现 Redis 锁，不是 Redisson。
- 用 `SETNX` + TTL 获取锁。
- value 中带 owner，释放锁时用 Lua 校验 owner。
- watchdog 每隔 `ttl / 3` 续期。
- MD5 用作视频内容指纹。
- `video_assets` 表示真实视频资产，`video_tasks` 表示用户任务。
- 删除任务时会处理引用关系，避免共享 asset 被误删。

### 推荐讲法

这条一定要主动澄清：我这里说 WatchDog 是锁续期思路，不是 Java 里的 Redisson。

回答时先说为什么要去重：AI 处理成本高，同一视频重复处理不划算。

再说怎么去重：MD5 标识内容，asset 和 task 拆开，同一 asset 可以被多个 task 引用。

最后说为什么要锁：并发上传时，两个请求可能同时发现 asset 不存在，所以要用 Redis 锁保护分片合并和入库临界区；分析任务消费侧也按 MD5 加锁，降低同一视频重复分析的风险。

### 面试必须掌握

- Redis 分布式锁的基本写法。
- 为什么需要 TTL。
- 为什么释放锁要校验 owner。
- WatchDog 解决什么问题。
- 分布式锁和 MySQL 唯一约束的区别。
- MD5 碰撞风险怎么回答。

### 代码证据

- `internal/pkg/lock/redis_lock.go`：锁、watchdog、Lua 解锁。
- `internal/service/media.go`：上传、合并、asset 复用。
- `internal/model/task.go`：`VideoAsset` 和 `VideoTask`。

### 不要这么说

- 不要说用了 Redisson。
- 不要说 MD5 绝对不会冲突。
- 不要说分布式锁保证强一致。
- 不要把锁说成唯一兜底，最终还要靠 DB 状态和幂等。

## 6. 简历点 3：分片上传与断点续传

### 简历原句

采取分片上传与断点续传机制，通过 Redis Set 记录分片状态，使用 MinIO 存储分片并通过 ComposeObject 合并，提升弱网环境下大文件上传可靠性

### 为什么写这条

这条是大文件工程能力。视频文件大、上传时间长，弱网中断后如果重传整个文件，体验很差。

### 当前实现事实

- 分片上传接口在 media handler。
- 每个分片先上传到 MinIO 临时对象。
- Redis Set 记录已上传分片编号。
- 合并前校验分片完整性。
- 合并使用 MinIO `ComposeObject`。
- 合并阶段用 Redis 锁防止重复合并。

### 推荐讲法

先说单文件上传的问题：请求长、失败重传成本高。

再说分片设计：每片独立上传，Redis Set 记录已完成编号。

最后说合并：全部到齐后服务端通过 MinIO ComposeObject 合并，不把所有分片读回应用内存。

### 面试必须掌握

- Redis Set 为什么适合记录分片状态。
- 断点续传如何知道哪些片不用传。
- MinIO 为什么比本地磁盘更适合。
- ComposeObject 和应用层拼接的区别。
- 合并并发怎么防。
- Redis 状态丢失或分片垃圾怎么处理。

### 代码证据

- `internal/handler/media.go`：分片上传入口。
- `internal/service/media.go`：`UploadChunk`、合并逻辑。
- `internal/storage/minio.go`：`ComposeObject`。
- `internal/pkg/lock/redis_lock.go`：合并锁。

### 不要这么说

- 不要说实现了完整云厂商 multipart upload。
- 不要说支持 GB 级文件，除非自己测试过。
- 不要说弱网成功率提升了多少，除非有数据。

## 7. 简历点 4：Redis Lua 令牌桶限流

### 简历原句

基于 Redis Lua 令牌桶限流，按用户和接口维度限制高成本 AI 请求，缓解恶意请求和重复点击带来的外部 AI 服务调用成本

### 为什么写这条

这条体现成本意识和服务保护。AI 应用和普通 CRUD 不一样，很多请求会产生外部服务成本。

### 当前实现事实

- 使用 Redis Hash 存 token 和 last time。
- Lua 脚本原子执行令牌补充、判断和扣减。
- key 维度按 route + user 或 IP。
- Redis 异常时当前策略是 fail-open，优先保证主业务可用。

### 推荐讲法

先说限流目标不是为了炫技，而是保护高成本 AI 接口。

再说为什么令牌桶：限制平均速率，同时允许合理突发。

然后说为什么 Lua：多步读写必须原子，否则并发下会超发。

最后说边界：这不是完整计费系统，生产可以结合日额度和套餐。

### 面试必须掌握

- 固定窗口、滑动窗口、漏桶、令牌桶区别。
- Redis Lua 原子性。
- route + user 限流和 IP 限流区别。
- fail-open 和 fail-close 权衡。
- 限流和 Kafka 削峰的区别。

### 代码证据

- `internal/middleware/ratelimit.go`：令牌桶和 Lua。
- `internal/config/config.go`：限流配置。
- `cmd/server/main.go`：middleware 注册。

### 不要这么说

- 不要说实现了完整计费系统。
- 不要说 Redis 挂了也能严格限流。
- 不要说令牌桶解决所有成本问题，它只是入口保护。

## 8. 简历点 5：RAG 检索增强问答

### 简历原句

基于 RAG 检索增强生成实现视频智能问答，将转写文本切分为 chunk 并生成向量写入 Milvus，结合 BM25 关键词召回与 RRF 融合排序，返回引用片段提升答案可解释性

### 为什么写这条

这是 AI 应用差异化亮点。参考文档里的 RAG 更像扩展规划，而你项目里 RAG 已经是实现链路。

### 当前实现事实

- RAG 知识源是 ASR 转写全文，不是 AI 摘要。
- 文本按 chunk 切分，默认 chunk size 800、overlap 120。
- Embedding 后写入 Milvus。
- MySQL 保存 chunk 元数据。
- Milvus 搜索按 `user_id + task_id + embedding_model` 过滤。
- BM25 关键词召回基于 MySQL chunk 表实现，核心入口是 `SearchByBM25`。
- RRF 融合向量召回和关键词召回。
- 返回 citations。
- 问答消息和检索快照落库。

### 推荐讲法

先说为什么不用摘要：摘要会丢细节，RAG 知识源要尽量贴近原始资料。

再说为什么不用整段转写塞 prompt：长视频文本太长，成本高，也容易超过上下文。

然后说检索链路：问题向量化，Milvus 召回，MySQL 关键词补召回，RRF 融合，组 prompt，返回答案和 citations。

最后说效果和边界：目前没有 rerank，后续可基于 Recall@K / MRR 评估后再加。

### 面试必须掌握

- RAG 是检索 + 生成，不是训练模型。
- chunk size 和 overlap 的作用。
- 向量召回和关键词召回各自优缺点。
- RRF 为什么比直接加权分数更稳。
- citations 如何降低幻觉。
- Milvus 为什么要按 user/task/model 过滤。
- embedding 维度不匹配为什么不可重试。

### 代码证据

- `internal/service/rag_index.go`：RAG 索引构建。
- `internal/service/chunk_splitter.go`：chunk 切分。
- `internal/vector/milvus.go`：Milvus collection 和 search filter。
- `internal/repository/video_chunk.go`：关键词召回。
- `internal/service/retrieval_fusion.go`：RRF。
- `internal/service/chat.go`：问答、prompt、citations、消息保存。
- `internal/service/rag_eval.go`：Recall@K / MRR 评估。

### 不要这么说

- 不要说训练了模型。
- 不要说已经有 rerank。
- 不要说接入 Elasticsearch。
- 不要说 RAG 完全解决幻觉。
- 不要说跨视频知识库，当前主线是单视频问答。

## 9. 简历点 6：任务失败治理

### 简历原句

设计任务失败治理机制，基于错误分类和阶梯退避重试处理外部依赖异常，对可恢复错误延迟重投递，对不可恢复错误快速失败，避免重试风暴和 AI 成本浪费

### 为什么写这条

这是最像参考文档“指数退避重试”的工程点，但你的实现不能写 RocketMQ 原生延迟和死信队列。你项目的重点是 Kafka + MySQL retry scheduler。

### 当前实现事实

- `TaskRetryPolicy` 默认最大重试 3 次。
- 默认退避时间是 60s、300s、900s，但简历正文不建议写具体时间。
- 可重试错误包括 timeout、429、5xx、MinIO、Milvus 等临时异常。
- 不可重试错误包括 AI 配置缺失、API Key 解密失败、未授权、文件不存在、Embedding 维度不匹配、ASR 空结果等。
- 失败状态写入 `video_tasks` 和 `task_jobs`。
- DB retry scheduler 到期后重新投递 Kafka。

### 推荐讲法

先说为什么不能所有错误都重试：配置缺失、密钥错误、文件不存在重试再多次也不会好。

再说为什么不能立刻重试：429、5xx、网络抖动如果立刻重放，会造成重试风暴。

最后说怎么做：按错误分类，可恢复错误延迟重投递，不可恢复错误快速失败，超过次数进入终态。

### 面试必须掌握

- 可重试和不可重试错误怎么区分。
- 为什么失败后可以 commit Kafka offset。
- DB retry scheduler 和 Kafka 重放的区别。
- 什么是最终一致性。
- 幂等怎么保证。
- 重试风暴如何避免。

### 代码证据

- `internal/mq/retry.go`：错误分类和退避策略。
- `internal/mq/consumer.go`：失败处理和 commit。
- `internal/repository/task.go`：任务失败状态。
- `internal/repository/task_job.go`：job 失败和重试状态。
- `cmd/server/main.go`：retry scheduler 启动。

### 不要这么说

- 不要说用了 RocketMQ 死信队列。
- 不要说 Kafka 原生支持延迟重试。
- 不要说所有失败都会重试。
- 不要把具体退避时间写成最优参数，它只是工程默认值。

## 10. 和参考文档的对应关系

参考文档的亮点可以借鉴结构，但不能照搬技术结论。

| 参考文档写法 | 你的简历对应写法 | 注意点 |
| --- | --- | --- |
| RocketMQ 异步化 | Kafka 异步化 | 你是 Go + Kafka，不是 Java + RocketMQ |
| Redisson + WatchDog | 自实现 Redis 锁 + watchdog 续期 | 不要说 Redisson |
| 分片上传 + 断点续传 | Redis Set + MinIO ComposeObject | 你的实现是服务端合并 |
| Redis 令牌桶限流 | Redis Hash + Lua 令牌桶 | 可以讲原子性和 fail-open |
| 指数退避重试 | Kafka + MySQL retry scheduler | 不要说死信队列 |
| RAG / SSE 追问 | RAG 主亮点，SSE 可补充 | 简历主线先写 RAG，SSE 放追问 |

## 11. 一天准备路线

### 第 1 小时：背主线

背熟：

上传 / URL 入库 -> Kafka -> 下载 / 转写 / 摘要 / RAG 索引构建 -> RAG 问答。

能说清：

- 哪些是同步接口做的。
- 哪些是 Kafka consumer 做的。
- 哪些状态落 MySQL。
- 哪些临时状态放 Redis。
- 哪些文件进 MinIO。
- 哪些向量进 Milvus。

### 第 2 小时：看代码路径

按这个顺序看：

1. `cmd/server/main.go`
2. `internal/service/media.go`
3. `internal/mq/producer.go`
4. `internal/mq/consumer.go`
5. `internal/mq/retry.go`
6. `internal/pkg/lock/redis_lock.go`
7. `internal/middleware/ratelimit.go`
8. `internal/service/rag_index.go`
9. `internal/service/chat.go`
10. `internal/service/retrieval_fusion.go`

### 第 3 小时：准备 6 个 30 秒回答

每个简历点准备一段 30 秒话术。标准结构：

背景痛点 -> 怎么做 -> 为什么这样做 -> 边界。

### 第 4 小时：准备压力追问

重点准备：

- 你这是不是只是调 API？
- Kafka 失败后为什么 commit offset？
- Redis 锁过期怎么办？
- 分片上传到 99% 断了怎么办？
- Redis 限流挂了怎么办？
- RAG 为什么不用摘要？
- 任务失败为什么不都重试？

## 12. 最后背诵版

这个项目我会按两条线讲：

第一条是视频处理工程化。视频下载、ASR、摘要和 RAG 索引构建都是长耗时任务，所以我用 Kafka 拆成异步 job，HTTP 接口只创建任务和投递消息。任务状态和失败原因落 MySQL，外部依赖异常通过错误分类和退避重试治理。

第二条是 AI 应用落地。为了避免重复 AI 成本，我用 MD5 内容指纹、视频文件复用和 Redis 分布式锁降低重复入库和重复处理风险；为了处理大文件，使用 Redis Set 和 MinIO ComposeObject 做分片上传和断点续传；为了控制高成本请求，用 Redis Lua 令牌桶限流；问答部分不用摘要当知识库，而是基于 ASR 全文做 RAG，融合 Milvus 向量召回、BM25 关键词召回和 RRF 排名，并返回引用片段。
