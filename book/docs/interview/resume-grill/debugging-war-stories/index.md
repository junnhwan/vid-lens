# Debug 复盘拷打

> 目标：把真实故障讲成可复现、可定位、可修复的工程经历。不要只说“我加日志解决了”。

### 1. 长视频 ASR 结果过短，你怎么定位？

- 题目：长视频处理故障。
- 面试官想听什么：从现象到链路分段验证。
- 简答：先确认用户看到的摘要短，是 ASR 文本本身短还是 LLM 总结短；再看音频提取、压缩、ASR 请求体大小和每段返回字符数。最后定位到 MiMo ASR base64 体积限制，改成 300 秒切片逐段识别再合并。
- 深答：

  <details>
  <summary>展开深答</summary>

  这类问题不能直接怪 LLM。VidLens 的处理链路是视频到音频、音频到 ASR 文本、ASR 文本到摘要。如果 ASR 本身只返回一小段，后面的总结再怎么调 prompt 都没用。

  排查时看到了长音频 base64 超过接口限制，导致 ASR 只处理了部分内容或失败不清晰。修复不是简单把超时时间调大，而是把音频按 300 秒切片，每片单独 ASR，记录 chunk index、状态、字符数和错误，最后合并转写文本。这样长视频失败也能定位到具体片段。
  </details>

- 延伸追问：
  - 为什么不是调大请求体限制？
  - 分片后怎么保证顺序？
  - 某一片失败怎么办？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:34` 长视频 ASR 记录。
  - `docs/troubleshooting-and-interview-notes.md:96` 根因分析。
  - `docs/troubleshooting-and-interview-notes.md:134` 300 秒切片。
  - `docs/troubleshooting-and-interview-notes.md:225` 口语化回答。
- 当前边界：当前主要按固定时长切片，还没有按语义或静音边界智能切分。

### 2. 为什么“加日志”不是一句空话？

- 题目：可观测性。
- 面试官想听什么：日志要能定位阶段、任务和数据量。
- 简答：VidLens 后来补的是任务级和 chunk 级日志：task id、trace id、stage、chunk count、每段 ASR 字符数、失败错误。它解决的是异步链路里 HTTP、Kafka consumer 和 DB 状态断开的排障问题。
- 深答：

  <details>
  <summary>展开深答</summary>

  异步系统最怕“接口返回成功，但后台不知道跑到哪”。早期只看 HTTP 日志，很难判断任务是否投递成功、consumer 是否消费、失败在哪个阶段。后来给任务加 trace id，Kafka payload 也带 trace id，consumer 日志优先用 payload trace id。

  chunk 级日志的价值更直接：长视频 ASR 如果最后文本短，需要知道总共切了几段、每段输出多少字符、哪一段失败。这样的日志不是为了堆输出，而是让排查路径从“猜模型问题”变成“按阶段验证数据是否正常流动”。
  </details>

- 延伸追问：
  - trace id 和 OpenTelemetry 有什么区别？
  - 日志会不会泄露转写内容？
  - 怎么从日志定位单个任务？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:314` 记录日志不足根因。
  - `docs/troubleshooting-and-interview-notes.md:370` 口语化说明日志修复。
  - `docs/troubleshooting-and-interview-notes.md:2178` trace id 串联异步链路。
  - `docs/troubleshooting-and-interview-notes.md:2180` stage 时间为后续统计打基础。
- 当前边界：当前是应用层 trace id，不是完整分布式 tracing/metrics 平台。

### 3. RAG 状态接口为什么曾经返回 HTML？

- 题目：前后端契约排障。
- 面试官想听什么：能识别路由、部署和 fallback 问题。
- 简答：前端请求 RAG 状态时拿到 HTML，说明请求没有命中后端 API，可能被前端静态路由 fallback 接住。定位方式是看 Network 的 URL、状态码、响应内容，再核对后端路由和部署代理配置。
- 深答：

  <details>
  <summary>展开深答</summary>

  这个问题表面看是“JSON parse error”，但响应体是 HTML，说明错误不在 JSON 序列化。通常是前端路径打错、base path 错、代理没转发，或者后端根本没有对应 API，最后被 VitePress/Vue 静态页面 fallback 返回了 index HTML。

  修复这类问题的关键是按网络链路定位：浏览器实际请求什么 URL、返回哪个 status、响应头是什么、后端日志有没有收到。这个复盘适合说明我不是只看报错文案，而是能从响应类型判断边界。
  </details>

- 延伸追问：
  - 怎么区分后端 404 和前端 fallback？
  - 为什么接口要返回明确 status？
  - RAG 状态为什么需要后端持久化？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:566` RAG 状态和 MySQL JSON 问题记录。
  - `docs/troubleshooting-and-interview-notes.md:636` snapshot 字段可空问题。
  - `docs/troubleshooting-and-interview-notes.md:1847` 旧 RAG 状态只看 chunks 的问题。
- 当前边界：文档记录的是排障过程，不要把它讲成复杂网关治理经验。

### 4. MySQL JSON 空字符串 bug 怎么讲得像真实经历？

- 题目：数据库类型坑。
- 面试官想听什么：能从错误信息回到字段语义。
- 简答：我会说这是聊天记录落库时暴露的问题：用户消息没有 retrieval snapshot，却给 JSON 字段写了空字符串。MySQL JSON 不接受裸空字符串，所以改成 `*string`，无快照写 NULL，有快照写 JSON 数组。
- 深答：

  <details>
  <summary>展开深答</summary>

  面试里不要只说“我改了字段类型”。要讲清楚：RAG 问答会保存用户消息和助手消息，助手消息有 citations，可以作为 retrieval snapshot；用户消息没有检索结果，应该是 NULL。第一版用 string 导致零值 `""` 入库，MySQL 校验 JSON 时失败。

  这个修复体现的是类型语义：空字符串、NULL、空数组不是同一个意思。用户消息的 snapshot 不存在，用 NULL；助手消息即使没有 citation，也应该按合法 JSON 表达。这样数据含义清楚，后续排查也方便。
  </details>

- 延伸追问：
  - 为什么不用 text 绕过？
  - NULL 和 [] 的语义差别？
  - GORM 零值为什么危险？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:620` JSON 空字符串根因。
  - `docs/troubleshooting-and-interview-notes.md:662` `RetrievalSnapshot` 改为 `*string`。
  - `docs/troubleshooting-and-interview-notes.md:725` 解释空字符串不是合法 JSON。
  - `internal/model/chat.go:24` JSON pointer 字段。
- 当前边界：这是数据建模和落库修复，不是 MySQL JSON 性能优化案例。

### 5. RetryScheduler claim 后 Kafka enqueue 失败为什么危险？

- 题目：重试一致性。
- 面试官想听什么：claim 成功不等于重试已经投递成功。
- 简答：RetryScheduler 会先把到期任务 claim 成运行/投递状态，再投递 Kafka。如果 claim 后 Kafka enqueue 失败，又没有恢复 next_retry_at，任务可能卡在一个不会再被扫描的状态。
- 深答：

  <details>
  <summary>展开深答</summary>

  这个问题容易被忽略，因为“扫描到期任务”和“投递消息”不是一个原子操作。数据库条件更新成功后，调度器以为自己拿到了任务；但 Kafka producer 可能因为 broker 不可用、网络错误或 topic 问题投递失败。如果此时不恢复任务的 retry metadata，后续 scheduler 就不会再捞到它。

  VidLens 当前通过 `ClaimRetryDispatch` / `RestoreRetryDispatch` 闭环这个窗口：task 与 task_job 使用相同的 token/version lease 事务性认领，producer 失败时按 token CAS 恢复；进程崩溃则由过期 lease 接管。面试里可以讲“我没有假装 DB 和 Kafka 有原子事务，而是让半成功状态可检测、可恢复”。
  </details>

- 延伸追问：
  - 为什么不把 Kafka 投递放进 PostgreSQL 事务？
  - outbox 模式能不能解决？
  - 多实例 scheduler 会不会重复 claim？
- 项目证据：
  - `internal/repository/task_lease_dispatch.go` dispatch claim/restore 事务与 token CAS。
  - `internal/mq/retry.go` producer 失败主动恢复。
  - `internal/mq/reliability_review_test.go` 多实例、回滚、过期 lease 与 producer 失败测试。
- 当前边界：重试补投窗口已可恢复，但这不是完整 outbox；首次创建任务后的投递窗口仍需单独做故障矩阵。

### 6. RAG 旧向量不删会怎样？

- 题目：数据一致性。
- 面试官想听什么：重建索引时旧数据污染检索。
- 简答：如果重建 RAG 时只追加新向量，不替换旧 projection，pgvector 表里会同时存在旧 chunk 和新 chunk。用户提问可能召回过期内容，citation 对不上当前转写，甚至重复片段影响 RRF 排序。
- 深答：

  <details>
  <summary>展开深答</summary>

  RAG 索引不是只建一次。用户换 embedding model、修复转写、重建索引时，都可能生成新的 chunks。如果旧 pgvector projection 不替换，检索 scope 仍然可能命中旧 embedding model 或旧 task chunk，导致答案看起来像“模型胡说”，实际是检索数据污染。

  VidLens 当前先在一个 PostgreSQL transaction 中替换 `video_chunks` source，再在另一个 transaction 中原子替换 pgvector projection。这里的复盘价值是：RAG 质量问题不一定是 prompt，很多时候是索引生命周期没管好。两步不是同一个 transaction，projection 失败时会记录 failed，并通过 `rag-audit`、`rag-reindex` 恢复。
  </details>

- 延伸追问：
  - `video_chunks` source 成功、pgvector projection 失败怎么办？
  - build_version 有什么用？
  - 怎么检测向量残留？
- 项目证据：
  - `internal/service/rag_index_build.go` 编排 source 持久化与 projection 替换。
  - `internal/repository/video_chunk.go` 事务性替换 PostgreSQL `video_chunks` source。
  - `docs/troubleshooting-and-interview-notes.md:3004` RAG 旧向量问题记录。
  - `docs/troubleshooting-and-interview-notes.md:3097` 旧向量污染口语化回答。
- 当前边界：当前已有 `rag-audit` 检查 source/projection 漂移，`rag-reindex` 可重建；但 source 与 projection 仍是两个 transaction，不是整体强一致。

### 7. RAG 索引为什么要拆成独立 job？

- 题目：异步链路演进。
- 面试官想听什么：不要让 ASR/总结和索引状态互相遮蔽。
- 简答：RAG 索引依赖 ASR，但它有自己的失败原因和重试策略。拆成 `rag_index` job 后，转写成功但索引失败时，用户仍能看到转写结果，也能单独重建索引，而不是把整个视频任务都说成失败。
- 深答：

  <details>
  <summary>展开深答</summary>

  第一版容易把“视频处理完成”和“RAG 可问答”混在一起。实际上 ASR 成功后，转写结果已经有价值；RAG 索引还要经过 chunk、embedding、PostgreSQL `video_chunks` source、pgvector projection publish，任何一步失败都不应该抹掉 ASR 成果。

  拆成独立 job 后，`video_rag_indexes` 能记录 indexing/indexed/failed、chunk count、embedding model 和 last_error，`task_jobs` 能记录 rag_index 的重试状态。面试里可以把它讲成对用户可见状态和排障粒度的改进。
  </details>

- 延伸追问：
  - RAG 失败后用户还能做什么？
  - 为什么不在 analyze 里同步做完？
  - RAG job 和 task status 怎么协调？
- 项目证据：
  - `internal/model/rag_index.go:12` `VideoRAGIndex` 模型。
  - `internal/mq/retry.go:231` RetryScheduler 支持 RAG index job。
  - `docs/troubleshooting-and-interview-notes.md:3121` RAG 独立 job 记录。
  - `docs/troubleshooting-and-interview-notes.md:3261` RAG consumer 失败进入 DB retry scheduler。
- 当前边界：当前 RAG 索引仍在同一后端进程内消费，不是独立微服务。

### 8. 这些 bug 怎么包装成“AI 辅助但自己能 debug”？

- 题目：AI 编程追问。
- 面试官想听什么：你不是只会让 AI 写代码。
- 简答：我会说 AI 帮我加快了实现，但真正让项目可面试的是后面的验证和复盘：长视频 ASR、RAG 状态、MySQL JSON、URL 下载、Milvus 部署、retry 半成功状态都需要看日志、读源码、跑命令和写测试固定。
- 深答：

  <details>
  <summary>展开深答</summary>

  面试官如果问“是不是 AI 写的”，不要躲。可以直接承认 AI 是编码助手，但强调自己做的是需求拆分、方案选择、边界验证和问题收敛。比如 B 站 412 不是让 AI 猜，而是在服务器上直接复现 yt-dlp；Milvus 不是看容器 running，而是看内部日志和环境变量；MySQL JSON 不是换库，而是理解 NULL、空字符串和 JSON 的差别。

  这种回答的重点是给出真实问题和证据路径。只说“我 review 了 AI 代码”很空；能讲出这些失败、根因、修复和当前限制，才说明项目不是一遍生成就结束。
  </details>

- 延伸追问：
  - AI 生成错代码你怎么发现？
  - 哪个 bug 最能证明你理解后端？
  - 如何避免下次再踩？
- 项目证据：
  - `MEMORY.md` 记录项目 AI 辅助和学习目标。
  - `docs/troubleshooting-and-interview-notes.md:225` 长视频 ASR 复盘。
  - `docs/troubleshooting-and-interview-notes.md:830` Milvus 部署复盘。
  - `docs/troubleshooting-and-interview-notes.md:2856` URL SSRF 复盘。
- 当前边界：不要把 AI 辅助包装成独立从零手写全部代码，重点讲验证能力。

