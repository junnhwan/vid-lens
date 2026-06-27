# 系统设计压力面拷打

> 目标：面对“如果规模变大怎么办”的追问时，先讲瓶颈和边界，再讲演进方案。不要把未来方案说成当前已上线。

### 1. 1000 个用户同时上传和问答，你先扩哪里？

- 题目：容量设计。
- 面试官想听什么：先拆链路瓶颈，不要一句水平扩容。
- 简答：我会先按链路拆：上传入口、MinIO 存储、Kafka backlog、ASR/Embedding/LLM 外部配额、MySQL 状态查询、Milvus 检索。最先加的是限流、任务队列、worker 并发控制和可观测指标，而不是盲目扩后端实例。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的瓶颈不只在 HTTP QPS。上传会压带宽和对象存储；URL 下载会压服务器出口和临时磁盘；ASR/Embedding/LLM 会压第三方 API 额度和成本；问答会压 Milvus、MySQL chunk 查询和 LLM。不同链路要分别控制。

  当前已有 Kafka 异步和 Redis token bucket，可以作为基础。要面向 1000 用户，下一步应该补 stage latency、队列长度、失败率、AI 调用耗时和用户级 quota。扩容时，download/transcribe/rag consumer 可以分开扩，Kafka partition 要跟上，外部 AI 还要有额度和退避。
  </details>

- 延伸追问：
  - 为什么不是直接加机器？
  - 哪个组件最可能先爆？
  - 如何保护第三方 AI 配额？
- 项目证据：
  - `cmd/server/main.go:217` 启动 Kafka consumers。
  - `cmd/server/main.go:221` 启动 retry scheduler。
  - `internal/middleware/ratelimit.go:94` Redis 限流故障 fail-open。
  - `internal/config/config.go:107` 任务重试配置。
- 当前边界：当前没有真实 1000 用户压测数据，不能声称已经支撑该规模。

### 2. 10GB 视频支持怎么设计，当前为什么不能声称支持？

- 题目：大文件设计。
- 面试官想听什么：10GB 不是改配置，是全链路约束。
- 简答：10GB 会影响上传时长、断点续传、临时磁盘、MD5 计算、MinIO multipart、URL 下载时长、FFmpeg 提取和清理策略。当前项目有分片上传和 720p 限制，但没有完整 10GB 生产策略，所以只能说未来设计，不能说已支持。
- 深答：

  <details>
  <summary>展开深答</summary>

  大视频设计要先问业务目标：VidLens 是视频理解，不是网盘或高清视频平台。很多情况下可以只保留音频、低清预览和转写结果，原视频可以设置生命周期。否则 10GB 视频会让存储、下载、转码、备份和删除都变成成本问题。

  技术上要有动态分片大小、并发限制、断点恢复、服务端临时文件配额、对象存储 multipart、下载硬超时和文件大小上限。AI 处理也要分片、可恢复和可跳过已完成片段。当前具备一部分基础，但缺硬资源限制和压测验证。
  </details>

- 延伸追问：
  - 是否应该直接让前端直传 MinIO？
  - 只抽音频能不能节省成本？
  - 删除 10GB 资源失败怎么办？
- 项目证据：
  - `internal/config/config.go:103` upload max file size 配置。
  - `internal/config/config.go:104` chunk size 配置。
  - `README.md:148` URL 下载限制 720p。
  - `docs/troubleshooting-and-interview-notes.md:1108` 720p 对理解业务足够。
- 当前边界：当前没有 10GB 视频全链路压测、硬下载大小限制和完整生命周期策略。

### 3. Kafka topic、partition 和 consumer 怎么扩？

- 题目：消息队列扩展。
- 面试官想听什么：consumer 并发受 partition 和任务类型影响。
- 简答：先按任务类型拆 topic 或至少拆 consumer：download、transcribe、analyze、rag_index 的耗时和资源不同。扩 consumer 实例前要确认 topic partition 足够，否则同一个 consumer group 里多出来的 consumer 没有 partition 可消费。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的任务不是同质消息。download 受网络和 yt-dlp 影响，transcribe 受 FFmpeg 和 ASR 影响，rag_index 受 embedding 和 Milvus 影响。混在一个小并发池里会出现慢任务拖住快任务的问题，所以扩展时应该按 job 类型隔离队列和 worker 资源。

  Kafka 里一个 partition 同一时刻只能由 consumer group 里的一个 consumer 消费。要通过增加实例提升吞吐，需要 partition 数也允许并行。面试里还要补一句：扩 consumer 不能解决下游 AI 配额不足，如果 ASR 限流，盲目扩 worker 只会制造更多失败和重试。
  </details>

- 延伸追问：
  - 如何保证同一个 task 的顺序？
  - Kafka backlog 上升怎么判断原因？
  - 为什么不用 consumer sleep 做延迟重试？
- 项目证据：
  - `cmd/server/main.go:217` 初始化 download/transcribe/analyze consumers。
  - `internal/mq/retry.go:238` retry 根据 job type 重新投递。
  - `docs/troubleshooting-and-interview-notes.md:1481` 重试不让 Kafka consumer sleep。
  - `docs/troubleshooting-and-interview-notes.md:1487` DB scheduler 和任务状态结合。
- 当前边界：当前没有按环境动态扩 partition 的自动化，也没有独立 worker 服务拆分。

### 4. Redis 挂了，哪些功能受影响，哪些能降级？

- 题目：缓存和协调故障。
- 面试官想听什么：Redis 在系统里不是唯一事实源。
- 简答：Redis 影响分布式锁、token bucket、分片上传进度和最近聊天缓存。限流当前 fail-open；最近聊天可从 MySQL 回源；锁失效会影响并发保护；上传临时进度丢失会影响续传体验，但不应破坏已落库的最终任务结果。
- 深答：

  <details>
  <summary>展开深答</summary>

  Redis 在 VidLens 里更多是协调层和缓存层。真正的任务状态、AI profile、转写、摘要、RAG index、chat message 都在 MySQL。Redis 挂了时，不能把所有业务都说成“数据丢了”，要按功能拆。

  令牌桶故障时当前选择 fail-open，是为了 Redis 短暂故障不把所有用户请求打死；但生产上要配合告警。最近聊天 Redis 缓存丢失可以查 MySQL 最近消息重建。风险更大的是锁和分片上传进度：锁没了会降低并发幂等保护，上传 progress 丢失会让前端续传体验变差。
  </details>

- 延伸追问：
  - 限流 fail-open 会不会被打爆？
  - 上传进度能不能从 MinIO 反查？
  - Redis 锁失效后 MySQL 唯一约束能兜底什么？
- 项目证据：
  - `internal/middleware/ratelimit.go:94` 限流 Redis 异常 fail-open。
  - `internal/service/chat.go:390` recent messages 可从 MySQL 查询。
  - `internal/service/chat.go:394` 查询后刷新 Redis recent memory。
  - `internal/pkg/lock/redis_lock.go` 自定义 Redis lock。
- 当前边界：当前上传 progress 自动恢复不完整，生产上应补持久化或从对象存储反查。

### 5. MySQL 是单点怎么办？

- 题目：核心状态库。
- 面试官想听什么：MySQL 不能随便 fail-open。
- 简答：MySQL 是 VidLens 的事实源，挂了任务、profile、chat、RAG 状态基本都不可写。生产演进要做备份、主从/高可用、连接池与慢查询监控、索引治理、迁移策略和关键写入幂等，而不是说靠缓存顶住。
- 深答：

  <details>
  <summary>展开深答</summary>

  Redis 可以丢缓存，Kafka 可以积压消息，但 MySQL 不能随便丢状态。上传任务创建、job 状态、重试时间、AI key ciphertext、chat history、RAG index 都依赖 MySQL。一旦 MySQL 写不可用，继续让用户提交任务只会制造不可追踪的后台状态。

  生产上应该先保证备份和恢复，再考虑主从、故障切换、读写分离和监控。对这个项目来说，索引也要围绕真实查询路径做：用户任务列表、状态查询、retry scheduler 到期扫描、chunk 检索。面试里不要空泛说分库分表，先把单库可靠性和可观测做好。
  </details>

- 延伸追问：
  - 哪些查询最需要索引？
  - AutoMigrate 上生产有什么风险？
  - MySQL 写失败后 Kafka 消息怎么办？
- 项目证据：
  - `internal/model/model.go:27` AutoMigrate 所有模型。
  - `internal/model/task.go:48` status index。
  - `internal/repository/task.go:211` 到期 retry 扫描。
  - `internal/repository/repository.go:41` repository transaction。
- 当前边界：当前是简历项目级 MySQL 部署，不是完整 MySQL HA 架构。

### 6. MinIO 或 Milvus 删除失败怎么补偿？

- 题目：最终一致性。
- 面试官想听什么：MySQL 事务不能覆盖外部资源。
- 简答：删除任务涉及 MySQL、MinIO 和 Milvus。MySQL 能用本地事务清关联表，但 MinIO/Milvus 删除是外部调用，失败不能回滚 MySQL。生产设计应记录待清理资源，后台补偿重试，并保证删除操作幂等。
- 深答：

  <details>
  <summary>展开深答</summary>

  这里不能说“用事务保证全部一致”。MySQL transaction 只能保证数据库里的 chat、chunks、rag index、task jobs、task 状态一致。对象存储和向量库不参与这个事务。如果 DB 删除成功后 MinIO 删除失败，就会产生孤儿对象；如果 Milvus 删除失败，就可能残留旧向量。

  更稳的设计是把资源清理拆成可重试 job：记录 resource type、resource key、owner task、next_retry_at 和 last_error。删除接口先完成用户可见的 task 删除，再由清理任务幂等删除外部资源。删除对象和向量都要允许重复调用，找不到也视为成功。
  </details>

- 延伸追问：
  - 删除失败用户应该看到什么？
  - 补偿 job 怎么避免重复删除？
  - 数据合规要求立即删除怎么办？
- 项目证据：
  - `internal/service/media.go:452` DeleteTask 使用 MySQL transaction。
  - `internal/service/media.go:511` 删除任务时遍历 embedding models 清理 vectors。
  - `docs/troubleshooting-and-interview-notes.md:2599` 资源清理不是分布式事务。
  - `docs/troubleshooting-and-interview-notes.md:2603` 未来补偿清理。
- 当前边界：当前没有独立资源清理补偿表和后台 worker。

### 7. RAG 数据量变大，Go 侧 BM25 怎么替换？

- 题目：检索扩展。
- 面试官想听什么：当前实现适合单视频有限 chunks，大规模要换检索基础设施。
- 简答：当前 Go 侧 BM25 风格召回适合单视频 chunk 数有限的场景。数据量变大后，可以先评估 MySQL FULLTEXT/ngram 或 Bleve，再到 OpenSearch/Elasticsearch；但要基于 Recall@K、MRR、latency 和运维成本比较。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 当前是单视频 RAG，chunk 数量可控，所以从 MySQL 拉 chunks 到 Go 里做关键词打分是简单、可测、少依赖的选择。如果以后做跨视频知识库、全站搜索或长视频海量 chunks，这个方案会变慢，也缺少专业中文分词、倒排索引和复杂查询能力。

  替换顺序不能上来就说 Elasticsearch。可以先用评估集比较当前 hybrid、MySQL FULLTEXT/ngram、Bleve 或 OpenSearch 的召回和延迟，再决定引入哪种依赖。RRF 可以继续作为融合层，变化的是 keyword retrieval 的候选来源。
  </details>

- 延伸追问：
  - 为什么不现在就上搜索引擎？
  - 中文分词怎么处理？
  - rerank 应该什么时候加？
- 项目证据：
  - `internal/repository/video_chunk.go:47` 当前 Go 侧 BM25 风格召回。
  - `docs/troubleshooting-and-interview-notes.md:2937` 当前没有引入专业搜索引擎。
  - `docs/troubleshooting-and-interview-notes.md:3001` 未来可替换为 FULLTEXT/ngram/search engine。
  - `docs/troubleshooting-and-interview-notes.md:3381` RAG 评估先看检索命中。
- 当前边界：当前没有接 Elasticsearch/OpenSearch/Bleve，也没有 rerank。

### 8. 从单体到微服务，最先拆什么？

- 题目：架构演进。
- 面试官想听什么：按资源边界和变更频率拆，不为拆而拆。
- 简答：我不会先拆用户模块，而会优先拆 worker 类能力：URL download、ASR/transcribe、RAG indexing。这些任务耗时长、资源依赖重、扩容方式和 Web API 不同，拆出去收益更明确。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 当前单体的好处是开发和部署简单，数据模型还在快速迭代。真正有拆分价值的是后台 worker，因为它们和 HTTP API 的资源模型不同：download 依赖 yt-dlp 和网络出口，transcribe 依赖 FFmpeg 和 ASR，rag_index 依赖 embedding 和 Milvus。

  拆分时可以保留 MySQL 作为状态源，通过 Kafka 传任务事件，worker 只负责消费、执行和回写 job 状态。先拆 worker 比拆用户、配置、聊天接口更实用，因为它解决的是耗时任务对 Web 进程的资源干扰和独立扩容问题。
  </details>

- 延伸追问：
  - 拆 worker 后事务怎么处理？
  - API 和 worker 共用数据库好吗？
  - 什么时候才需要拆用户服务？
- 项目证据：
  - `cmd/server/main.go:217` 当前 Web 进程内启动 consumers。
  - `internal/mq/consumer.go` consumer 执行下载、转写、总结、索引。
  - `internal/mq/retry.go:238` retry 按 job type 重新投递。
  - `docs/troubleshooting-and-interview-notes.md:3261` RAG consumer 可通过 DB retry 调度。
- 当前边界：当前仍是单体进程内启动消费者，不要声称已经微服务化。

