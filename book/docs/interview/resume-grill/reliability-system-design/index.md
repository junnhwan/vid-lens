# 可靠性与系统设计拷打

> 目标：面试官从单点模块追到系统边界时，你要能讲清“现在做到哪、为什么这样做、下一步怎么补”。

### 1. 外部 AI 服务不稳定，VidLens 怎么处理？

- 题目：外部依赖可靠性。
- 面试官想听什么：错误分类、重试、审计和用户可见状态。
- 简答：VidLens 把外部 AI 调用放在后台任务里，通过 task status/stage、retry_count、next_retry_at 和 last_error 记录失败；网络、timeout、429、5xx 等重试，配置缺失、key 解密失败、embedding 维度错误等不重试。AI 调用还会写审计日志和每日聚合。
- 深答：

  <details>
  <summary>展开深答</summary>

  AI 服务的失败不是单一类型。比如用户没配置 BYOK，重试没有意义；embedding 维度和 Milvus collection 不一致，也不会因为等一会儿自动恢复。相反，timeout、429、5xx、网络抖动、MinIO/Milvus 临时不可用就可以重试。

  VidLens 的处理方式是任务化和可观察。consumer 出错后调用 `recordTaskFailure`，根据错误内容分类，写 `video_tasks` 和 `task_jobs`。AI client 外面还有 observed wrapper，记录 ASR、LLM、Embedding 的 provider、model、耗时、输入输出字符数和失败信息，方便排查到底是哪个模型环节失败。
  </details>

- 延伸追问：
  - 为什么不能所有错误都重试？
  - retry 后还是失败怎么办？
  - 审计是不是计费？
- 项目证据：
  - `internal/mq/retry.go:56` non-retryable marker。
  - `internal/mq/retry.go:72` retryable marker。
  - `internal/ai/observed.go:51` ObservedChatClient。
  - `internal/service/ai_observer.go:22` 记录 AI 调用。
  - `docs/troubleshooting-and-interview-notes.md:3715` AI 调用审计口语化回答。
- 当前边界：当前是审计和每日聚合，不是完整计费系统。

### 2. BYOK 怎么保护用户 API Key？

- 题目：安全和成本边界。
- 面试官想听什么：公开部署为什么不能用服务端 key，以及 key 怎么存。
- 简答：公开部署不能默认消耗服务端 key，所以用户配置自己的 ASR、LLM、Embedding provider。API Key 使用 AES-GCM 加密入库，模型调用前才解密，接口响应只返回 masked 值，model 字段里 ciphertext 不通过 JSON 返回。
- 深答：

  <details>
  <summary>展开深答</summary>

  BYOK 解决两个问题。第一是成本归属：陌生用户不能随便消耗维护者的模型 token。第二是灵活性：ASR、LLM、Embedding 可能来自不同 provider，不能强行共用一个 baseURL 和 key。

  代码层面，`UserAIProfile` 里 LLM/ASR/Embedding 三类 key 都是 ciphertext 字段，并且 `json:"-"`；`AIProfileService` 创建或更新时调用 codec encrypt，读取默认 profile 给 consumer 或 chat service 用时再 decrypt。返回给前端时只给 masked key，不返回明文。
  </details>

- 延伸追问：
  - AES-GCM secret 放在哪里？
  - key 会不会写日志？
  - 用户更新 profile 时不填 key 怎么处理？
- 项目证据：
  - `internal/model/ai_profile.go:13` LLM key ciphertext 且 JSON 不返回。
  - `internal/model/ai_profile.go:17` ASR key ciphertext。
  - `internal/model/ai_profile.go:21` Embedding key ciphertext。
  - `internal/pkg/secret/crypto.go:47` AES-GCM encrypt。
  - `internal/service/ai_profile.go:263` encrypt or keep key。
  - `internal/service/ai_profile.go:311` decrypt profile。
- 当前边界：BYOK 保护 key 存储和使用边界，但不等于完整密钥管理平台。

### 3. URL 下载 SSRF 防到了哪一层？

- 题目：安全追问。
- 面试官想听什么：你不会把第一版校验吹成生产级。
- 简答：当前做了第一层防护：只允许 http/https，拒绝 localhost，按白名单域名后缀匹配，DNS 解析后拒绝 private/loopback/link-local/multicast 等 IP，入库和日志使用脱敏 URL，并限制 yt-dlp 最高 720p。但还不能说是完整生产级 SSRF 防护。
- 深答：

  <details>
  <summary>展开深答</summary>

  URL 下载的风险在于服务端代用户访问网络。如果直接把用户输入交给 yt-dlp，可能访问内网地址、metadata 服务或带 token 的私有链接。VidLens 当前先 parse URL，限制 scheme，检查 host 不能为空且不是 localhost，再做平台白名单匹配。之后通过 DNS 解析 host，任何解析结果落到内网、loopback、link-local、multicast 都拒绝。

  同时，source_url 只保存脱敏 URL，去掉 userinfo、query 和 fragment，避免分享链接里的 token 入库或进日志。yt-dlp 参数也限制最高 720p，避免拉超大视频。边界是：redirect-chain 校验、DNS rebinding 防护、硬下载大小/时间限制、用户级 cookies 加密处理还没完整做，所以不能说生产级。
  </details>

- 延伸追问：
  - DNS 解析后为什么还不够？
  - 去掉 query 会不会影响链接？
  - 为什么限制 720p？
- 项目证据：
  - `internal/service/remote_video_url.go:52` URL validate 入口。
  - `internal/service/remote_video_url.go:68` 拒绝 localhost。
  - `internal/service/remote_video_url.go:71` host allowlist。
  - `internal/service/remote_video_url.go:94` 返回 sanitized URL。
  - `internal/pkg/ytdlp/ytdlp.go:49` yt-dlp 限制 720p。
  - `docs/troubleshooting-and-interview-notes.md:2856` URL 安全口语化回答。
- 当前边界：需要继续补 redirect-chain validation、DNS rebinding review、硬资源限制和 user cookies 策略。

### 4. AI 调用审计和 quota/billing 有什么区别？

- 题目：成本控制边界。
- 面试官想听什么：不要把审计说成完整计费。
- 简答：审计记录发生了什么：谁调用了 ASR/LLM/Embedding、provider/model、成功失败、耗时、输入输出字符数，并聚合到每日用量。quota/billing 还需要额度规则、token 精确统计、扣减事务、价格和账单状态，当前没有完整实现。
- 深答：

  <details>
  <summary>展开深答</summary>

  BYOK 之后仍然需要可观察性。用户问答失败时，要知道是 embedding 失败、LLM 失败，还是 ASR 失败；公开部署时也想知道某个用户今天调用了多少次。VidLens 的 `AIObserver` 会写 `ai_call_logs`，再更新 `user_usage_daily`。

  但我不会把它说成计费系统。因为完整 billing 至少要有套餐、余额、价格、扣减事务、幂等扣费、退款/失败回滚、provider token usage 解析等。当前只是审计和每日聚合，为后续 quota 做数据基础。
  </details>

- 延伸追问：
  - 为什么不用 token 而用字符数？
  - streaming token 怎么统计？
  - quota 要怎么补？
- 项目证据：
  - `internal/model/ai_call_log.go:16` `AICallLog` 模型。
  - `internal/model/ai_call_log.go:37` `UserUsageDaily` 模型。
  - `internal/repository/ai_call_log.go:47` 增量聚合每日用量。
  - `internal/service/ai_observer.go:49` 创建 AI call log。
  - `docs/troubleshooting-and-interview-notes.md:3723` 明确不是完整计费系统。
- 当前边界：没有套餐和扣费事务，不能在简历里写“计费系统”。

### 5. 如果服务重启，哪些状态能恢复？

- 题目：故障恢复题。
- 面试官想听什么：哪些状态在 DB/MinIO/Kafka/Redis，哪些会丢。
- 简答：任务主状态、子 job 状态、重试时间、转写、摘要、RAG index、chat、AI profile 都在 MySQL；视频和 chunk 对象在 MinIO；Kafka 中未消费消息可继续消费；Redis 里的锁、限流桶、上传临时进度和最近聊天缓存是易失辅助状态，丢了不应破坏已落库的最终结果。
- 深答：

  <details>
  <summary>展开深答</summary>

  我会按存储介质回答。MySQL 保存业务真相：`video_tasks`、`task_jobs`、转写、摘要、RAG index、video chunks、chat message、AI profile、AI call logs。MinIO 保存视频对象和临时分片对象。Milvus 保存向量，但 RAG index 状态和 MySQL chunks 也在 DB 里。

  Redis 在这个项目里更多是协调和缓存：分布式锁、令牌桶、分片上传 progress、最近 N 轮聊天。服务重启时锁自然过期，令牌桶可重建，最近聊天可从 MySQL 回源；分片上传 progress 丢失会影响续传体验，但最终正式 asset 不依赖 Redis。后续可以做从 MinIO chunk 反查进度或持久化临时状态。
  </details>

- 延伸追问：
  - Redis 丢上传进度怎么办？
  - Kafka 消息消费到一半服务挂了怎么办？
  - Milvus 和 MySQL 不一致怎么办？
- 项目证据：
  - `internal/model/model.go:11` AutoMigrate 包含 TaskJob 等核心模型。
  - `internal/model/model.go:20` AutoMigrate 包含 AICallLog。
  - `internal/service/chat.go:390` 最近消息可从 MySQL 查询。
  - `internal/service/chat.go:394` 查询后刷新 Redis recent memory。
  - `internal/service/rag_index.go:137` 重建索引前清理旧 Milvus vectors。
- 当前边界：Redis 上传 progress 丢失后的自动恢复还不完整。

### 6. 如果 MySQL、Redis、Kafka、Milvus 某一个挂了怎么办？

- 题目：系统设计故障场景。
- 面试官想听什么：不同依赖的降级策略不同。
- 简答：MySQL 是核心状态库，挂了大部分业务不可用；Redis 挂了限流 fail-open、锁和上传进度受影响；Kafka 挂了新异步任务无法投递，但已有 DB 状态可显示失败；Milvus 挂了 RAG 不可用或索引失败，但上传、转写、摘要等基础功能可以继续。
- 深答：

  <details>
  <summary>展开深答</summary>

  不能把所有中间件故障都回答成“重试”。MySQL 是系统事实来源，task、profile、转写、摘要都在里面，挂了只能返回错误或熔断。Redis 更偏辅助：限流代码选择 fail-open，锁和上传 progress 会受影响，但已经落库的任务不会丢。Kafka 挂了时，接口投递失败应该明确返回或写失败状态，不能让用户以为任务在后台执行。

  Milvus 更适合降级：如果启动时 Milvus 连不上，后端基础功能仍可提供，RAG 暂不可用；索引构建失败会写 RAG index failed，并可进入 retry。这个边界比“整个系统挂掉”更合理。
  </details>

- 延伸追问：
  - 为什么 MySQL 不 fail-open？
  - Milvus 挂了转写还能看吗？
  - Kafka 恢复后怎么补投递？
- 项目证据：
  - `internal/middleware/ratelimit.go:94` Redis 限流故障 fail-open。
  - `internal/mq/retry.go:212` Kafka retry 投递失败后恢复 `next_retry_at`。
  - `docs/troubleshooting-and-interview-notes.md:517` Milvus 未启动时后端健康检查仍可用。
  - `docs/troubleshooting-and-interview-notes.md:3261` RAG consumer 失败通过 DB retry scheduler。
- 当前边界：当前没有统一熔断器和指标告警体系。

### 7. 如果要支持 1000 用户和 10GB 视频，你会怎么扩展？

- 题目：扩展设计题。
- 面试官想听什么：先拆瓶颈和前提，不要直接吹水平扩容。
- 简答：我会先限制上传大小、下载时长和任务并发，再拆 worker 资源。上传走直传或分片并发控制，下载加硬大小/时间限制，Kafka topic 增加 partition，consumer 按 download/transcribe/rag 分组扩展，ASR/Embedding 做额度和队列控制，MinIO/Milvus/MySQL 加监控和清理策略。
- 深答：

  <details>
  <summary>展开深答</summary>

  10GB 视频不是简单把 `MaxFileSize` 改大。首先要确认业务是否需要保存 10GB 原视频，因为 VidLens 核心是内容理解，不是高清视频处理。可以考虑只提取音频、限制分辨率、限制时长、异步清理原视频。

  系统上，上传最好走对象存储直传或更强的分片并发控制；URL 下载必须有硬大小、时长、速率、content-type 和 redirect 限制；Kafka topic partition 要支持更多 consumer 并发；ASR 和 Embedding 是成本瓶颈，需要用户 quota、队列优先级和失败降级；RAG chunk 数量上来后，Go 侧 BM25 风格召回可能要换成 MySQL FULLTEXT/ngram、Bleve、OpenSearch 或 Elasticsearch 这类专门方案。
  </details>

- 延伸追问：
  - 先扩哪个组件？
  - 10GB 视频为什么可能不应该保存？
  - Go 侧 BM25 什么时候不够？
- 项目证据：
  - `README.md:148` URL 下载限制最高 720p 的动机。
  - `docs/troubleshooting-and-interview-notes.md:1108` 说明 720p 对业务足够。
  - `docs/troubleshooting-and-interview-notes.md:2937` 当前 Go 侧 BM25 适合单视频 chunk 有限场景。
  - `docs/troubleshooting-and-interview-notes.md:3000` 未来用评估比较检索方案。
- 当前边界：不要声称当前已经支持 10GB 生产级视频处理，这是扩展设计题。

### 8. 怎么把这个项目从简历项目推进到生产可用？

- 题目：路线图题。
- 面试官想听什么：风险优先级，而不是堆功能。
- 简答：我会优先补 URL 下载安全和 retry scheduler 可靠性，再补资源生命周期、指标告警、用户 quota、RAG 评估集和检索优化。顺序上先解决会造成安全事故、任务卡死或资源泄漏的问题，再做质量提升。
- 深答：

  <details>
  <summary>展开深答</summary>

  P0 我会放安全和一致性：URL 下载需要 redirect-chain 校验、DNS rebinding 防护、硬下载大小/时间限制、cookies 加密和日志脱敏策略；retry scheduler 需要测试 Kafka enqueue 失败后 claim 的恢复路径，避免任务卡死。资源生命周期也很关键，删除 task 后 MinIO、Milvus、MySQL 的清理失败要有补偿 job。

  P1 再做可观测和成本：Prometheus 指标、stage latency、失败率、AI 调用耗时和用户 quota。RAG 方面先扩大评估集，再做 chunking、query rewrite、邻居扩展和可选 rerank。这样讲不会把未来规划说成已完成，也体现优先级。
  </details>

- 延伸追问：
  - 为什么 URL 下载安全优先级最高？
  - retry scheduler 还有什么风险？
  - RAG 优化为什么不先上 rerank？
- 项目证据：
  - `AGENTS.md:121` 高优先级 future work 列出生产风险修复。
  - `AGENTS.md:123` 指向 URL download SSRF/domain whitelist/DNS safety。
  - `AGENTS.md:124` 指向 RetryScheduler claim 后 Kafka enqueue 失败恢复。
  - `docs/troubleshooting-and-interview-notes.md:2599` 资源删除不是分布式事务的边界。
  - `docs/troubleshooting-and-interview-notes.md:2872` URL 下载当前不是完整生产级 SSRF 沙箱。
  - `docs/troubleshooting-and-interview-notes.md:3381` RAG 评估先看有没有捞到正确上下文。
- 当前边界：未来优化只能说 roadmap，不能写成已完成能力。
