# 项目定位与总览拷打

> 目标：把项目讲成“AI 视频理解后端”，而不是“Kafka、Redis、MinIO 技术堆叠”。回答时先说业务链路，再说工程手段。

### 1. 你这个项目到底解决什么问题？

- 题目：面试官想确认你是不是只做了一个上传页面加 AI 接口。
- 面试官想听什么：视频处理为什么长、贵、容易失败，以及后端怎么把这些问题拆开。
- 简答：VidLens 解决的是上传或提交视频 URL 后，异步完成下载、音频提取、ASR、摘要和基于转写文本的 RAG 问答。重点不是“调 AI”，而是把长耗时、高成本、外部服务不稳定的处理链路做成可排队、可重试、可观察的后端流程。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会先把它定义成一个 AI 视频理解后端。用户上传视频或提交 URL 后，系统先把视频文件落到 MinIO，再通过 Kafka 进入后台处理。后续会用 FFmpeg 提取和压缩音频，ASR 生成转写文本，LLM 生成摘要，同时用转写文本构建 RAG 索引，让用户可以围绕视频内容提问。

  这个项目里真正麻烦的地方不是 API 调用本身，而是视频链路的工程约束。比如长视频 ASR 可能因为单次请求体积、识别时长或 provider 不稳定而失败；URL 下载可能卡住 HTTP 请求；用户重复上传同一个视频会浪费存储和模型调用；RAG 如果直接用摘要做知识源会丢细节。所以后端需要 Kafka、任务状态、Redis 锁、对象存储、BYOK 和 RAG 状态，而不是简单同步调用。
  </details>

- 延伸追问：
  - 为什么说后端是主项目，前端只是验证面？
  - 如果只做短视频，还需要 Kafka 吗？
  - 为什么 RAG 的知识源不是摘要？
- 项目证据：
  - `README.md:17` 描述上传/URL 到 ASR、摘要、RAG 问答链路。
  - `README.md:46` 列出普通上传、分片续传、URL 下载、BYOK、RAG。
  - `README.md:247` 明确 RAG 知识源是 ASR 转写全文。
- 当前边界：它是简历项目和公开部署验证项目，不能包装成已有大规模生产流量。

### 2. 两分钟项目介绍怎么说？

- 题目：面试开场最常见，让你自然带出核心技术。
- 面试官想听什么：业务、链路、技术、难点、结果，顺序清楚。
- 简答：VidLens 是一个 Go 写的 AI 视频理解后端，支持视频上传和 URL 下载。后端用 MinIO 存视频，用 Kafka 处理下载、转写、摘要和 RAG 索引，用 Redis 做锁、限流和分片上传状态，用 Milvus 存转写 chunk 向量，最后让用户基于视频内容问答。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会这样说：VidLens 是一个面向视频学习和内容理解的 Go 后端项目。用户可以上传本地视频，也可以提交 B 站或 YouTube 这类公开视频链接。HTTP 接口不会同步等完整个视频处理，而是创建 task，投递到 Kafka，由后台 consumer 下载视频、提取音频、分片 ASR、生成摘要和构建 RAG 索引。

  存储上，大文件不进 MySQL，而是放到 MinIO 私有桶，业务表保存 object name 和状态。Redis 用在三个地方：分布式锁防止同一视频并发合并或重复处理，Lua 令牌桶保护高成本接口，分片上传用 Set 记录已上传 chunk。RAG 部分把 ASR 转写切成 chunk，生成 embedding 后写入 Milvus，提问时按 `user_id + task_id + embedding_model` 过滤检索，再把引用片段拼进 prompt。

  我会补一句限制：这个项目有混合检索和审计基础，但不是完整商业计费系统，也不是跨视频知识库。
  </details>

- 延伸追问：
  - 你负责最核心的模块是哪几个？
  - 你遇到过最真实的 bug 是什么？
  - 如果只能保留一个亮点，你讲哪个？
- 项目证据：
  - `README.md:70` 技术栈中列出 Kafka、Redis、MinIO、Milvus。
  - `README.md:220` 单独说明 Kafka 异步架构。
  - `README.md:249` 单独说明 Redis 分布式锁。
- 危险回答：不要上来背“提升性能、保证稳定性”，要先讲视频处理链路为什么会失败。

### 3. 为什么不是 AI 接口套壳？

- 题目：面试官怀疑项目只是调用模型。
- 面试官想听什么：你能说出模型之外的后端状态、一致性、失败处理。
- 简答：如果只是 AI 套壳，HTTP 同步调用就结束了。VidLens 需要处理大文件存储、长视频分片 ASR、任务状态、重试、URL 下载安全、BYOK、RAG 索引一致性和引用返回，这些都是模型调用外面的后端工程问题。
- 深答：

  <details>
  <summary>展开深答</summary>

  我不会否认项目依赖外部 AI 服务，但模型只是链路的一部分。以长视频为例，最开始短视频能转写，长视频会失败或只得到几百字。后来定位到两个问题：音频 base64 后超出 MiMo 单次请求限制，以及长音频单次 ASR 可能返回不完整。所以修复不是换提示词，而是用 FFmpeg 压缩音频、按 300 秒切片、逐段 ASR 后合并，并且补 chunk 级日志。

  RAG 也是类似。不能直接把摘要当知识库，因为摘要已经压缩过，可能漏掉细节。代码里是读取 `video_transcriptions.content`，切成 chunk，保存 MySQL chunk 和 Milvus vector；聊天时还会保存 retrieval snapshot。这里涉及数据源选择、向量库隔离、检索融合、状态记录和失败边界。
  </details>

- 延伸追问：
  - 长视频 ASR 具体怎么修？
  - RAG 索引失败会不会影响转写？
  - 哪些地方跟 AI 无关但很重要？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:47` 记录 MiMo ASR 超 10MB 错误。
  - `docs/troubleshooting-and-interview-notes.md:134` 记录 300 秒切片方案。
  - `internal/service/rag_index.go:78` RAG 构建读取 transcription。
  - `internal/model/chat.go:24` 聊天记录保存 retrieval snapshot。
- 当前边界：不要说模型能力是自己训练的；项目是工程化调用和编排外部 AI 服务。

### 4. 技术栈为什么选 Go、Gin、GORM、MySQL？

- 题目：面试官看你是否理解栈选择，而不是只会罗列。
- 面试官想听什么：语言和业务链路的匹配度，以及没有过度设计。
- 简答：Go 适合写并发后端和长任务 worker，Gin/GORM/MySQL 是简历项目里足够清晰的 Web 与持久化组合。项目重点不在炫框架，而在任务状态、Kafka consumer、Redis/Milvus/MinIO 集成和失败恢复。
- 深答：

  <details>
  <summary>展开深答</summary>

  我选 Go 是因为这个项目有比较多的后台处理和外部组件集成：Kafka consumer、FFmpeg/yt-dlp 命令调用、MinIO 文件流、Milvus 检索、Redis 锁和限流。Go 的 goroutine 和标准库 context 适合组织这些 I/O 密集型链路。

  Gin 负责 HTTP API，GORM 负责常规业务表。MySQL 存任务、转写、摘要、RAG index、chat message、AI profile 和调用审计。大文件不放 MySQL，而是用 MinIO；向量不放 MySQL，而是用 Milvus。也就是说 MySQL 负责状态和元数据，专门存储负责二进制对象和向量。
  </details>

- 延伸追问：
  - 为什么不用 Java/SpringBoot？
  - GORM 有什么风险？
  - 哪些数据不应该放 MySQL？
- 项目证据：
  - `README.md:195` 项目结构说明 `cmd/server/main.go` 初始化 DB、Redis、MinIO、Kafka、Milvus。
  - `README.md:204` 说明 model 层包含任务、转录、摘要、AI 配置、RAG chunk、聊天模型。
  - `README.md:72` 对象存储使用 MinIO。
- 当前边界：不要把技术栈选择说成绝对最优，只说适合当前 Go 后端项目。

### 5. 这个项目的核心难点有哪些？

- 题目：面试官要判断你是否真的踩过坑。
- 面试官想听什么：具体失败模式，而不是空泛“高并发、高可用”。
- 简答：核心难点是长视频 ASR 的完整性、异步任务状态与重试、RAG 索引和转写的失败边界、用户自带 AI key 的安全、URL 下载安全，以及大文件上传/复用/删除时的数据一致性。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会按真实故障讲。第一类是长视频 ASR：短视频正常，15 分钟视频失败或转写过短，所以改成音频压缩加 300 秒分片，并补日志。第二类是异步任务：下载、转写、总结、RAG 索引耗时长，不能绑在 HTTP 请求里，所以用 Kafka 和 DB 状态记录。第三类是 RAG 边界：ASR 成功后用户应该能看到转写，Embedding 或 Milvus 失败只应该影响问答索引，所以后来把 RAG 索引拆成独立 Kafka job。

  还有安全和成本问题。公开部署不能用服务端 Key 替所有用户买单，所以实现 BYOK，并把 API Key 加密入库。URL 下载是服务端代用户访问网络，所以做了平台白名单、DNS 私网 IP 拒绝和脱敏 URL，但我不会说这是完整生产级 SSRF 防护。
  </details>

- 延伸追问：
  - 这些难点里哪个最能体现后端能力？
  - 你怎么验证长视频修复有效？
  - RAG 失败为什么不删除 ASR 结果？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:225` 长视频 ASR 的口语化复盘。
  - `docs/troubleshooting-and-interview-notes.md:3121` RAG 索引拆成独立 Kafka job。
  - `docs/troubleshooting-and-interview-notes.md:3570` AI 调用审计的背景。
  - `docs/troubleshooting-and-interview-notes.md:2783` URL 下载 SSRF 风险背景。
- 当前边界：不要把这些难点包装成“高并发生产级系统”，重点是链路完整、问题真实、边界清楚。

### 6. 如果面试官说“你这个像拼组件”，怎么回应？

- 题目：压力测试你的项目价值。
- 面试官想听什么：组件之间为什么必须配合，以及你如何处理边界。
- 简答：我会承认用了成熟组件，但项目价值在于把组件放到视频 AI 链路里解决具体问题：Kafka 承接长任务，Redis 处理锁和限流，MinIO 存大文件，Milvus 存向量，MySQL 存状态和审计。关键不是组件名，而是每个组件负责什么、失败后怎么恢复。
- 深答：

  <details>
  <summary>展开深答</summary>

  后端项目一定会使用基础设施组件，不能因为用了 Kafka、Redis、MinIO 就说是拼装。真正要问的是：没有它会坏在哪里？在 VidLens 里，如果没有 Kafka，下载和 ASR 这种长任务会占住 HTTP 请求；没有 Redis 锁，同一个 MD5 视频合并或重复处理会出现竞态；没有 MinIO，大文件会压垮业务服务和数据库；没有 Milvus，RAG 只能做关键词匹配；没有 MySQL 状态表，用户只能看到一个模糊的“处理中”。

  我更愿意把这个项目讲成视频 AI 链路的状态机和数据流。组件只是工具，核心是用它们表达任务边界、数据边界和失败边界。
  </details>

- 延伸追问：
  - 哪个组件可以替换？
  - Kafka 换成本地队列会怎样？
  - Milvus 换 MySQL 全文检索行不行？
- 项目证据：
  - `cmd/server/main.go:110` 启动时创建 Kafka topics。
  - `internal/pkg/lock/redis_lock.go:31` 自定义 Redis lock。
  - `internal/storage/minio.go:61` MinIO 上传文件。
  - `internal/vector/milvus.go:67` Milvus collection 初始化。
- 危险回答：不要说“用了很多中间件所以架构高级”，要说“某个业务失败模式需要某个组件”。

### 7. 哪些点不能写进简历或不能强说？

- 题目：面试官看你是否会夸大。
- 面试官想听什么：你能主动说明边界。
- 简答：不能说使用 RocketMQ、Redisson、Function Calling、rerank、完整计费、生产级 URL 安全、跨视频知识库或大规模生产流量。当前真实实现是 Kafka、自定义 Redis 锁、单视频 RAG、Go 侧 BM25 风格召回加 RRF、AI 调用审计和 URL 下载第一层安全校验。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会主动区分已实现和未来计划。比如参考 Java 项目可能会提 RocketMQ 和 Redisson，但 VidLens 的 Go 版本用的是 Kafka 和自定义 Redis lock，不能照抄。RAG 现在有 Milvus 向量召回、Go 侧关键词 BM25 风格召回和 RRF，但没有接 Cross-Encoder rerank，也不是 Elasticsearch/OpenSearch。AI 调用有审计和每日聚合，但没有套餐、价格、扣费事务，所以不能叫完整计费。

  URL 下载也一样。当前已经做了 http/https、平台白名单、DNS 解析拒绝私网 IP、脱敏 URL 和 720p 限制，但 redirect-chain、DNS rebinding、硬下载大小/时间限制还不完整，所以只能说第一层安全校验。
  </details>

- 延伸追问：
  - 为什么不直接复制 Java 项目里的 RocketMQ？
  - 你怎么描述未来优化？
  - 当前 RAG 为什么还不能叫完整搜索系统？
- 项目证据：
  - `MEMORY.md:262` 开始列出不要过度声称的内容。
  - `MEMORY.md:266` 明确不要声称 VidLens 使用 RocketMQ。
  - `MEMORY.md:267` 明确不要声称 VidLens 使用 Redisson。
  - `docs/troubleshooting-and-interview-notes.md:2858` 明确 URL 下载不能说完全生产级。
  - `docs/troubleshooting-and-interview-notes.md:2985` 明确当前不是 Elasticsearch/OpenSearch。
  - `docs/troubleshooting-and-interview-notes.md:3723` 明确不是完整计费系统。
- 当前边界：保守不是减分，能把边界说清楚通常比硬吹更可信。

### 8. Vue3 前端在项目中是什么定位？

- 题目：面试官可能看到 Vue，但你投的是后端。
- 面试官想听什么：前端是验证和展示，不抢后端主线。
- 简答：Vue3 + Vite 前端主要是验证面，用来触发上传、查看任务状态、展示 ASR 文本、摘要和 RAG 问答效果。项目核心还是后端链路：任务状态、Kafka consumer、MinIO、Redis、Milvus、BYOK 和故障处理。
- 深答：

  <details>
  <summary>展开深答</summary>

  我不会把这个项目包装成前端产品。Vue3 前端的作用是让后端能力可见：上传视频后能看到 task 状态，转写后能看到文本，摘要和问答能验证 AI 链路是否跑通。真正值得讲的是状态从 HTTP API 到 Kafka topic，再到 consumer 落库和前端轮询展示的闭环。

  面试时如果被问前端，我会简单说明它是验证/display surface，然后马上回到后端，比如任务状态为什么要有 `status` 和 `stage`，RAG 为什么要有 `video_rag_indexes`，聊天为什么要保存 retrieval snapshot。
  </details>

- 延伸追问：
  - 前端轮询有什么风险？
  - 未来会不会用 WebSocket 或 SSE？
  - 用户怎么看到失败原因？
- 项目证据：
  - `internal/model/task.go:12` 定义任务状态。
  - `internal/model/task.go:27` 定义任务阶段。
  - `internal/model/rag_index.go:6` 定义 RAG 索引状态。
  - `internal/model/chat.go:24` 保存 retrieval snapshot。
  - `README.md:77` 说明展示界面是 Vue 3 + Vite，用于触发上传和查看结果。
- 当前边界：不要让前端设计盖过后端项目定位。
