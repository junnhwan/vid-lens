# 简历主线：四个核心能力怎么讲

> 这页专门对应简历中的四条项目经历。建议先背每节的“30 秒回答”，再用“深答”应对追问。所有结论都以当前 VidLens 代码为准；如果代码只做到项目级验证，就主动说清边界。

## 四个深入专题

| 简历原句 | 深入准备 | 题量 |
|---|---|---:|
| Kafka 异步调度与失败重试 | [Kafka 异步与重试](/interview/resume-grill/resume-core/kafka-retry/) | 11 |
| 长视频分段 ASR 与结果复用 | [分段 ASR](/interview/resume-grill/resume-core/asr-chunks/) | 11 |
| 分片上传、Redis 状态与 MinIO 合并 | [分片上传](/interview/resume-grill/resume-core/chunk-upload/) | 10 |
| Milvus + BM25 + RRF 与引用 | [混合检索](/interview/resume-grill/resume-core/hybrid-rag/) | 12 |

本页用于面试前速背；四个专题页用于逐题训练。

## 先记住项目主线

### 两分钟项目介绍

**面试官：请介绍一下这个项目。**

**30 秒回答：**

VidLens 是一个 Go 写的 AI 长视频内容理解后端。用户上传视频或提交 URL 后，系统把文件存到 MinIO，创建任务并投递 Kafka；后台完成下载、音频提取、分段 ASR、摘要和基于 ASR 转写文本的 RAG 索引，最后提供带引用片段的视频问答。我的重点不是把几个模型 API 串起来，而是处理长任务的排队、状态、失败重试、分片复用和检索可追溯性。

**深答：**

这个项目的业务难点是视频处理链路很长，而且每一步都可能受外部服务影响。同步 HTTP 请求不适合等待下载、FFmpeg、ASR、Embedding 和索引写入，所以我把任务拆成 Kafka consumer 能处理的阶段；主任务表记录整体状态，`task_jobs` 记录 download、transcribe、analyze、rag_index 等子作业。ASR 又按约 300 秒切片，片段结果单独落库，失败后重新进入任务时，已完成片段直接复用，未完成片段才重新调用 ASR。问答侧不是把摘要当知识库，而是读取完整 ASR 转写、切 chunk、写 Milvus，同时在 Go 侧做 BM25 风格关键词召回，再用 RRF 融合，返回 citation 让答案能回到原始转写片段。

我会主动补一句：这是一个有真实故障复盘和测试的项目级后端，不会把它说成已有大规模生产流量；URL 下载安全、跨视频知识库和完整计费仍有边界。

**代码证据：**

- 任务状态和阶段：`internal/model/task.go:12-18`、`internal/model/task.go:31-38`。
- 子作业和重试字段：`internal/model/task_job.go:12-37`。
- RAG 的知识源是转写文本：`internal/service/rag_index.go:99-110`。
- Kafka consumer 完成转写后再投递 RAG：`internal/mq/consumer.go:625-635`、`internal/mq/consumer.go:1020-1077`。

---

## 一、Kafka 异步调度、任务状态与阶梯退避重试

### Q1：为什么一定要用 Kafka，不能在 HTTP 接口里同步做完吗？

**30 秒回答：**

不能。长视频处理会包含下载、FFmpeg、分段 ASR、摘要和向量索引，耗时和失败概率都不可控。如果同步做，HTTP 连接会长时间占用，客户端超时后服务端还可能继续执行，状态也不好恢复。当前接口只负责创建任务、记录 job 并投递消息，consumer 才执行具体阶段。

**深答：**

Kafka 在这里不是为了“显得分布式”，而是把请求接入和耗时处理解耦。`VideoTask` 保存用户可见的整体状态和 stage，`TaskJob` 保存具体 job 的状态、重试次数、下一次重试时间和最后错误。consumer 获取处理 lease 后执行任务，成功就推进阶段，失败则根据错误类型决定是否重试。这样用户可以通过状态接口看到是 downloading、transcribing 还是 indexing，而不是只能等一个 HTTP 请求。

**追问：消息重复消费怎么办？**

我不会声称 Kafka 天然 exactly-once。代码层面用任务/job 状态、processing lease 和幂等落库来约束重复执行；例如 consumer 先 claim 当前 job，遇到 stale 或 terminal 状态可以直接结束。真正的副作用仍需要以数据库状态为准，而不是只相信消息只会到达一次。

**追问：Kafka 投递成功就代表任务成功吗？**

不代表。投递成功只是把执行权交给后台，任务仍可能在下载、ASR 或索引阶段失败。当前 `handleTranscribe` 会在转写完成后写入转写结果，再把父任务推进到 indexing 或 completed，并单独处理 RAG enqueue 失败。

**代码证据：**

- 状态机：`internal/model/task.go:12-18`、`internal/model/task.go:54-75`。
- 子作业拆分：`internal/model/task_job.go:5-37`。
- consumer 的处理流程和 lease：`internal/mq/consumer.go:561-592`。
- 转写完成后推进状态并投递 RAG：`internal/mq/consumer.go:615-641`、`internal/mq/consumer.go:1020-1077`。

### Q2：你的“阶梯退避”具体怎么实现？哪些错误不应该重试？

**30 秒回答：**

默认重试等待是 60 秒、300 秒、900 秒，重试次数超过配置后进入失败或 dead 状态。错误不是一律重试：网络超时、连接失败、429、5xx、MinIO/Milvus 临时错误可重试；配置缺失、无权限、文件不存在、ASR 空结果、Embedding 维度错误等确定性错误不应反复打。

**深答：**

`TaskRetryPolicy` 把最大次数、退避数组和时间函数集中管理；`backoffForRetry` 根据 retry count 取对应档位，超过数组长度时使用最后一个档位。`isRetryableError` 先匹配不可重试标记，再匹配 timeout、network、429、5xx 等临时错误。这样做的重点是区分“等一会儿可能恢复”和“再试也不会改变输入”的失败，否则会浪费模型调用和队列容量。

**追问：重试会不会导致重复扣费或重复写入？**

有这个风险，所以不能只靠 Kafka 重投。任务状态、job 状态和 chunk 状态都要持久化；对外部 AI 调用要接受可能发生重复调用，并通过完成结果复用、幂等写入和审计日志降低影响。当前项目还不是带账单扣费事务的商业计费系统，我不会把 retry 说成“绝对不重复”。

**代码证据：**

- 默认退避：`internal/mq/retry.go:24-53`。
- 可重试与不可重试错误分类：`internal/mq/retry.go:55-100`。
- 任务中持久化 `retry_count`、`next_retry_at`、`last_error`：`internal/model/task.go:59-64`、`internal/model/task_job.go:23-28`。

### Q3：Kafka enqueue 成功、数据库状态更新失败，或者反过来，怎么办？

**回答：**

这是典型的消息和数据库双写问题，我不会声称当前已经实现完整 outbox。当前代码对关键投递失败会把任务/job 记为失败；RAG enqueue 失败还会走 handoff，记录下一次重试时间和目标 job。它能覆盖常见的“已经 claim 但子消息没投出去”场景，但仍存在进程在两个外部操作之间崩溃的窗口。后续可以引入 outbox 表或可靠事件表，由调度器扫描补投，而不是把这部分包装成已经完成。

**代码证据：** `internal/service/media.go:174-183`、`internal/mq/consumer.go:1044-1074`。

---

## 二、长视频分段 ASR 与失败片段复用

### Q4：为什么要把长视频 ASR 切片？切多长？

**30 秒回答：**

我把音频按默认约 300 秒切片，再逐片调用 ASR。单次把整段长音频送给 provider，容易触发请求体积、时长、超时和返回截断问题；切片后每片有独立状态，失败影响范围更小，也便于统计每片耗时和结果长度。

**深答：**

`transcribeAudio` 先通过 FFmpeg 切片，再按 chunk index 顺序处理。每片调用 ASR 前写 running，成功写 completed 和文本，失败写 failed。最后按原顺序拼接成完整转写。这里的关键不是“切片本身”，而是 chunk index 成为恢复和拼接的稳定坐标。

**追问：切片边界会不会把一句话截断？**

会，所以 300 秒只是当前实现的工程折中，不是理论最优值。后续可以增加重叠窗口、按静音切分或基于 provider 限制动态调整，并在合并时去重重叠文本。当前面试中应说“降低单请求风险”，不要说“完全解决语义边界问题”。

**代码证据：** `internal/mq/consumer.go:709-728`，其中使用 `ffmpeg.DefaultAudioSegmentSeconds`；逐片处理见 `internal/mq/consumer.go:730-792`。

### Q5：任务失败重试时，怎么做到只重跑失败片段？

**30 秒回答：**

ASR 不是只保存最终全文，而是以 `task_id + chunk_index` 保存片段状态和内容。重试重新生成当前临时音频切片后，遍历到某个 index 时先查数据库；如果状态是 completed 且内容非空，就直接复用，否则才调用 ASR。准确说是“跳过已完成片段的 ASR API 调用”，不是整个重试链路完全不重复。

**深答：**

代码在每次处理 chunk 前调用 `completedTranscriptionChunk`。命中完成结果就追加到 `parts` 并 continue；未命中才写 running、调用 `strategy.Transcribe`，成功写 completed，失败写 failed 并返回错误。这样如果第 7 片失败，下一次会跳过前 6 片的模型调用，只重做第 7 片及后续未完成片段。任务级重试仍可能重新下载视频、提取音频并生成临时切片，这是当前实现的边界。

**追问：为什么不能只把最终 transcript 存一行？**

只存最终全文无法知道哪一段已成功，也无法定位失败片段；重试时要么整段重跑，要么自己猜测边界。独立 chunk 记录让恢复依据变成数据库状态，而不是进程内存。

**代码证据：**

- 复用已完成片段：`internal/mq/consumer.go:735-742`、`internal/mq/consumer.go:795-806`。
- 片段 running/completed/failed 持久化：`internal/mq/consumer.go:744-765`、`internal/mq/consumer.go:773-833`。
- 最终按顺序拼接：`internal/mq/consumer.go:780-792`。

### Q6：ASR 结果过短或截断，你怎么排查？

**口述答案：**

我会先把问题拆成“切片是否完整、provider 是否真的返回完整、数据库是否截断、最终拼接是否丢片”四层。当前代码已经在切片开始、切片数量、每片输出字符数和最终输出字符数打日志；同时每片结果独立落库，所以可以对比 chunk index 和字符数，而不是只看最终全文。这个问题在长视频场景里很重要，因为“任务 completed”不等于内容完整。

**项目证据：** `internal/mq/consumer.go:716-728`、`internal/mq/consumer.go:767-792`；真实问题复盘见 `docs/troubleshooting-and-interview-notes.md:34-49`、`docs/troubleshooting-and-interview-notes.md:225-240`。

---

## 三、分片上传、断点续传与 MinIO 服务端合并

### Q7：分片上传的断点续传具体怎么做？Redis 里存什么？

**30 秒回答：**

客户端先用文件 MD5 初始化上传，Redis 以 `upload:chunks:{fileMD5}` 这个 Set 记录已经成功落盘的 chunk number，并额外记录 total 和 status。查询进度时返回已上传编号，客户端只补传缺失分片；分片对象本身落在 MinIO 的 `chunks/{fileMD5}/{chunkNumber}` 路径下。

**深答：**

上传分片时采用“先落盘、后记账”：先把 chunk 写入 MinIO，成功后再 `SAdd` 到 Redis，并设置 24 小时过期。这样 Redis 中出现的编号代表“至少完成过一次对象上传”，不会在对象写失败时提前显示完成。合并时重新检查 0 到 `totalChunks-1` 是否都在 Set 中，再调用 MinIO `ComposeObject` 服务端合并，业务表只保存最终 asset 的 object name、MD5 和大小。

**追问：Redis 记录丢了怎么办？**

当前实现把 Redis 当上传会话状态，不把它当最终文件事实来源；如果状态过期，客户端需要重新初始化并补传，已存在对象是否可复用还要结合实际对象检查。更高要求的生产实现会把上传会话状态持久化到数据库、给对象和会话做一致性校验，并增加过期分片清理任务。

**代码证据：**

- 进度查询：`internal/service/media.go:550-580`。
- 先落盘后记账：`internal/service/media.go:582-599`。
- 分片对象命名：`internal/service/media.go:591-597`。

### Q8：为什么用 MinIO 服务端合并，而不是 Go 服务把所有分片读回来再拼？

**30 秒回答：**

服务端合并能避免业务进程承担整文件的网络搬运和磁盘临时空间，尤其适合大视频。Go 服务只负责校验分片完整性、构造源对象列表和写入最终资产记录，MinIO 通过 `ComposeObject` 在对象存储侧完成合并。

**深答：**

`MergeChunks` 先查同 MD5 的已存在 asset，避免重复合并；然后用 Redis 锁 `vidlens:merge:{fileMD5}` 防止两个请求同时做同一份合并。拿到锁后检查每一片是否存在，再构造按编号排序的 `CopySrcOptions`，调用 `ComposeObject` 写入 `videos/{uuid}.ext`。合并成功后再创建 `VideoAsset`，将状态标记为 completed，并 best-effort 清理临时分片。

**追问：为什么合并还要加锁？MD5 去重不够吗？**

MD5 去重解决的是内容级复用，不能阻止两个并发请求同时发现“asset 还不存在”并一起合并。锁负责把合并过程串行化，数据库唯一性和二次查询则作为最终兜底。锁的实现还有 owner 校验和看门狗续期，避免固定 TTL 导致长视频合并时锁提前失效。

**代码证据：**

- 合并锁、完整性检查和服务端合并：`internal/service/media.go:601-671`。
- MinIO 的 `ComposeObject`：`internal/storage/minio.go:105-112`。
- owner、Lua 校验和 WatchDog：`internal/pkg/lock/redis_lock.go:13-21`、`internal/pkg/lock/redis_lock.go:76-139`。

### Q9：断点续传和内容级去重有什么区别？

**回答：**

断点续传解决“一次上传中断后补哪些分片”；内容级去重解决“同一个完整文件再次上传时复用哪一个 asset”。前者依赖 Redis chunk Set 和 MinIO 临时对象，后者依赖 `file_md5` 和 `VideoAsset`。普通上传路径也会先计算 MD5、查已有 asset，再创建任务，所以这两个机制是互补的，不是一个概念。

**代码证据：** `internal/service/media.go:98-119`、`internal/service/media.go:610-616`、`internal/model/task.go:45-54`。

---

## 四、Milvus + BM25 + RRF 混合检索与引用片段

### Q10：RAG 的知识源是什么？为什么不用 AI 摘要？

**30 秒回答：**

知识源是 ASR 转写文本，不是摘要。摘要适合快速浏览，但会压缩细节；用户问到某个时间点、术语、原句或例子时，摘要可能已经丢失。系统先检查转写存在，再按 chunk size 和 overlap 切分，生成 embedding 写入 Milvus，同时把 chunk 文本和元数据写入 MySQL，供向量和关键词两路检索。

**代码证据：** `internal/service/rag_index.go:99-110`、`internal/service/rag_index.go:188-210`。

### Q11：BM25、向量检索和 RRF 在你的代码里怎么串起来？

**30 秒回答：**

提问时先按 `user_id + task_id + embedding_model` 做 Milvus 向量召回；另一条路从同一个视频的 `video_chunks` 做 Go 侧 BM25 风格关键词召回。两路结果不是直接比较原始分数，而是按排名用 RRF 融合，得到统一的 citation 候选，再按 topK 返回。

**深答：**

`RetrievalPipeline.Retrieve` 对每个 query 分别执行 embedding/Milvus 搜索和 keyword search，然后调用 `FuseRetrievedChunks`。向量分数适合表达语义相似度，BM25 的词频、逆文档频率和文档长度归一化更擅长命中特定术语；两者原始分值不在同一尺度，所以用排名融合更稳。当前 BM25 是 Go 代码对单视频 chunk 集合计算，中文使用 2 到 4 字 n-gram，Latin 文本按 token 处理，并不是接了 Elasticsearch/OpenSearch。

**追问：为什么还要带 task_id 和 embedding_model 过滤？**

这是数据隔离和向量版本隔离。不同用户、不同视频不能混在一起回答；同一视频更换 embedding 模型后，向量维度和语义空间也可能不同。Milvus Search 明确把这三个条件拼进 filter，避免召回错误数据。

**代码证据：**

- 两路召回和融合：`internal/service/rag_pipeline.go:87-181`。
- BM25 计算：`internal/repository/video_chunk.go:67-151`。
- 中英文 tokenization：`internal/repository/video_chunk.go:182-235`。
- Milvus 过滤条件：`internal/vector/milvus.go:240-268`。
- RRF 实现：`internal/service/retrieval_fusion.go:17-82`。

### Q12：引用片段是怎么返回给用户的？如何避免答案看起来像编的？

**30 秒回答：**

检索结果本身保留 chunk index、evidence ID 和 content，问答服务先把 citations 发送给前端，再生成答案；完成后把问答和 retrieval snapshot 持久化。这样用户可以先看到模型依据的转写片段，后续也能复盘当时用了哪些证据。

**深答：**

在流式问答入口中，`prepareChatByMode` 先完成检索，随后 `AskStreamWithMode` 先 emit `citations`，再调用 streaming client 输出 answer；没有真正流式 client 时，代码会把完整答案切成块发送，这是接口层的 fallback，不应说成所有 provider 都是真 token streaming。保存聊天时还会写 retrieval snapshot，所以引用不是只存在前端内存里。

**追问：有引用是不是就等于答案一定正确？**

不是。引用只能说明答案使用了哪些召回片段，不能保证模型没有误读，也不能证明召回覆盖了全部上下文。当前可以继续做检索评估、邻居 chunk 扩展、查询改写和可选 rerank，但面试中要区分已实现的混合召回与后续优化。

**代码证据：**

- 引用先于答案发送及 fallback：`internal/service/chat.go:449-493`。
- retrieval snapshot 持久化：`internal/service/chat.go:364-397`。
- 引用数据结构：`internal/service/chat.go:45-65`；快照字段见 `internal/model/chat.go:18-27`。

---

## 综合追问：四条简历怎么串成一个闭环？

**面试官：这四点不是四个孤立 demo 吗？**

**推荐回答：**

它们对应同一条链路的四个故障边界。分片上传解决大文件进入系统时不能一次完成；Kafka 和任务状态解决进入系统后不能占住 HTTP、外部服务失败后要能恢复；分段 ASR 把长视频拆成可重试的最小处理单元；Milvus、BM25、RRF 和 citations 则把已经得到的转写变成可检索、可解释的问答结果。它们之间通过 `VideoTask`、`TaskJob`、`TranscriptionChunk`、`VideoChunk` 和 RAG index 状态关联，而不是简单把中间件堆在一起。

**一分钟链路：**

```text
上传/URL
  -> MinIO asset + VideoTask
  -> Kafka download/analyze/transcribe
  -> FFmpeg 分段
  -> TranscriptionChunk(task_id, chunk_index)
  -> 完整 ASR 转写
  -> RAG index job
  -> Milvus 向量 + MySQL chunk/BM25
  -> 向量召回 + BM25 + RRF
  -> citations + answer + retrieval snapshot
```

**综合证据：**

- 任务主线：`internal/model/task.go:40-83`。
- 分段 ASR：`internal/mq/consumer.go:709-792`。
- RAG 建索引：`internal/service/rag_index.go:87-210`。
- 混合检索与问答：`internal/service/rag_pipeline.go:87-181`、`internal/service/chat.go:449-493`。

## 面试时的四条边界

1. **可以说：** Kafka 异步调度、DB 状态、阶梯退避、分片状态、MinIO Compose、分段 ASR 结果复用、Milvus + Go 侧 BM25 风格召回 + RRF、带 citation 的问答。
2. **不要说：** 使用 RocketMQ 或 Redisson；代码实际是 Kafka 和自定义 Redis lock。
3. **不要说：** 重试完全不重复、URL 下载已经生产级安全、所有模型都是真 token streaming、已有完整计费和大规模生产流量。
4. **未来改进要单独说：** outbox/可靠事件表、持久化上传会话、ASR 重叠窗口、BM25 倒排或 RRF 评估集、邻居 chunk、检索指标和更严格的 URL 下载隔离。

相关复盘：

- [Kafka 异步任务](/interview/resume-grill/kafka-async/)
- [上传、断点续传与 MinIO](/interview/resume-grill/upload-minio/)
- [RAG 与 Milvus](/interview/resume-grill/rag-milvus/)
- [Debug 复盘](/interview/resume-grill/debugging-war-stories/)

