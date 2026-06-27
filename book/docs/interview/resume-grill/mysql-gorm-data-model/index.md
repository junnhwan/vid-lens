# MySQL/GORM 数据模型拷打

> 目标：把表结构讲成任务状态、幂等、成本和排障的工程设计，而不是泛泛地背 GORM 模型。

### 1. 为什么任务表不能什么都塞进去？

- 题目：任务主表边界。
- 面试官想听什么：主表服务高频查询，详情和大字段应该拆开。
- 简答：`video_tasks` 主要承载列表、状态、阶段、重试和溯源字段，不适合塞转写全文、摘要、RAG chunk 或聊天快照这类大文本。大字段拆到 transcription、summary、chat、video_chunks 等表，能让任务列表和状态轮询更轻。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 里用户最频繁看的不是完整 ASR 文本，而是任务列表、任务详情里的 status/stage，以及当前失败原因。如果把转写全文、总结、RAG 片段都塞到 `video_tasks`，列表查询会反复扫大行，索引和缓存命中也会变差。

  所以当前模型把主任务表定位成“状态协调表”：记录用户、asset、文件名、来源、source URL、status、stage、retry、trace id、错误码和时间戳。大文本结果和检索片段放到独立表。面试里可以说这不是为了炫技拆表，而是让高频状态查询和低频详情读取分离。
  </details>

- 延伸追问：
  - 拆表会不会增加 join 成本？
  - 状态轮询为什么不能直接查所有结果？
  - 什么字段应该留在主任务表？
- 项目证据：
  - `internal/model/task.go:40` `VideoTask` 主任务模型。
  - `internal/model/task.go:48` status 索引。
  - `internal/model/task.go:49` stage 索引。
  - `internal/model/video_chunk.go:6` RAG chunk 独立模型。
- 当前边界：当前是单库内的垂直拆分，不是分库分表架构。

### 2. `status` 和 `stage` 为什么要同时存在？

- 题目：状态机设计。
- 面试官想听什么：终态和业务阶段不是一回事。
- 简答：`status` 表示任务整体状态，比如 queued/running/failed/dead/completed；`stage` 表示当前卡在哪个业务阶段，比如 downloading、transcribing、summarizing、indexing。只有 status 看不出失败环节，只有 stage 又无法表达是否终止。
- 深答：

  <details>
  <summary>展开深答</summary>

  视频处理链路很长，下载、音频提取、ASR、摘要、RAG 索引都可能失败。如果只有 `failed`，用户和开发者都不知道该看 yt-dlp、FFmpeg、AI key、Milvus 还是 MinIO。`stage` 的价值就是保留“失败发生在哪一步”。

  反过来，只有 `stage=transcribing` 也不够，因为它不能说明任务是正在转写、等待重试，还是已经 dead。VidLens 用 `status + stage + last_error` 组合，前端能展示用户可理解的状态，后端 retry scheduler 也能根据最后 job type 和阶段重新投递。
  </details>

- 延伸追问：
  - stage 会不会和 job_type 重复？
  - 前端应该展示 status 还是 stage？
  - 为什么需要 last_error_code？
- 项目证据：
  - `internal/model/task.go:48` status index。
  - `internal/model/task.go:49` stage index。
  - `docs/troubleshooting-and-interview-notes.md:1545` 记录只有 failed 难以排障。
  - `docs/troubleshooting-and-interview-notes.md:1642` 失败记录保留具体 stage。
- 当前边界：当前状态机主要靠常量和 service/consumer 约束，还没有单独的状态机引擎。

### 3. 为什么后来新增 `task_jobs`，不继续扩 `video_tasks`？

- 题目：任务和子作业拆分。
- 面试官想听什么：一个视频任务里会有多个动作，主状态和子动作状态不能混在一起。
- 简答：`video_tasks` 是兼容前端和主流程的任务视图；`task_jobs` 记录 download/transcribe/analyze/rag_index 等具体动作的状态、重试和错误。这样同一个 task 上多个动作不会互相覆盖状态语义。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的一个视频不是只跑一次任务。URL 下载是一种 job，用户之后可能触发提取文字、总结，也可能单独重建 RAG 索引。如果继续把所有状态都压进 `video_tasks`，就会出现“summary 失败把主任务改 failed，但 transcription 其实可用”这种混乱。

  `task_jobs` 用 `task_id + job_type` 做唯一约束，每个动作有自己的 status、stage、retry_count、next_retry_at 和 last_error。主任务表仍保留总体状态，是为了兼容原有接口和列表展示。面试里可以强调：这是从真实迭代里拆出来的，不是预先过度设计。
  </details>

- 延伸追问：
  - task_jobs 和 Kafka message 是什么关系？
  - 为什么不每个步骤都建一张表？
  - 主任务状态和 job 状态冲突怎么办？
- 项目证据：
  - `internal/model/task_job.go:15` `TaskJob` 模型。
  - `internal/model/task_job.go:17` `task_id + job_type` 唯一索引。
  - `internal/service/media.go:174` URL 下载创建 download job。
  - `docs/troubleshooting-and-interview-notes.md:3898` `task_jobs` 解决混合语义。
- 当前边界：当前还没有把所有前端展示完全迁移到 job 维度，`video_tasks` 仍是兼容状态源。

### 4. `video_assets` 和 `video_tasks` 为什么分开？

- 题目：MD5 复用和资源模型。
- 面试官想听什么：物理文件和用户任务不是同一个概念。
- 简答：`video_assets` 表示 MinIO 里的物理视频对象，按 MD5 唯一；`video_tasks` 表示某个用户对这个资产发起的一次处理任务。相同视频可以复用同一个 asset，但不同用户、不同文件名和不同任务状态要独立记录。
- 深答：

  <details>
  <summary>展开深答</summary>

  如果用户重复上传同一个视频，后端没必要重复存储和重复计算 MD5 对应的文件对象。`video_assets` 用 `file_md5` 做唯一约束，记录 object name、content type、size 等物理资源信息。`video_tasks` 再引用 asset，记录用户维度的文件名、来源、处理状态和结果。

  这个拆分让去重有物理落点，也避免一个 asset 的存在被某个任务状态绑死。删除任务时还要判断这个 asset 是否仍被其他 active task 引用，不能因为一个用户删任务就把别人还在用的对象删掉。
  </details>

- 延伸追问：
  - MD5 碰撞怎么办？
  - 相同视频不同用户是否共享 AI 结果？
  - 删除 task 时 asset 怎么处理？
- 项目证据：
  - `internal/model/asset.go:11` `VideoAsset` 模型。
  - `internal/model/asset.go:13` `FileMD5` 唯一索引。
  - `internal/service/media.go:478` 删除任务时统计 asset active refs。
  - `internal/repository/task_test.go:26` 相同 MD5 可支持多个用户任务。
- 当前边界：当前主要复用物理文件，不应声称已经做了跨用户 AI 结果复用。

### 5. GORM soft delete 对删除逻辑有什么影响？

- 题目：软删除边界。
- 面试官想听什么：软删除不是物理删除，计数和清理要理解默认作用域。
- 简答：`VideoTask` 带 `gorm.DeletedAt`，删除任务默认是软删除。GORM 普通查询会自动过滤 deleted_at 非空记录，所以统计 active refs 时能避免把已删除任务算作仍在使用 asset，但物理资源清理仍要额外处理。
- 深答：

  <details>
  <summary>展开深答</summary>

  软删除的好处是保留审计和恢复空间，坏处是你必须明确“删除业务数据”和“清理物理资源”不是同一件事。VidLens 删除任务时会在事务里删关联的 chat、RAG index、video chunks、task jobs，再软删除 task；asset 是否删除取决于是否还有 active task 引用。

  GORM 的默认查询会过滤软删除记录，所以 `CountActiveByAssetID` 这类计数应该统计未删除任务。面试里要注意不能说“删 task 就一定删 MinIO 文件”，因为如果 asset 被多个任务引用，立即删物理对象会破坏其他任务。
  </details>

- 延伸追问：
  - 软删除数据会不会越来越多？
  - 什么时候需要 Unscoped？
  - MinIO 删除失败怎么办？
- 项目证据：
  - `internal/model/task.go:66` `DeletedAt` 软删除字段。
  - `internal/service/media.go:452` DeleteTask 使用 repository transaction。
  - `internal/service/media.go:478` 删除前统计 active refs。
  - `docs/troubleshooting-and-interview-notes.md:2599` 资源清理补偿是未来工作。
- 当前边界：当前删除链路还不是完整分布式事务，MinIO/Milvus 清理失败需要后续补偿 job。

### 6. MySQL JSON 空字符串 bug 怎么定位和修复？

- 题目：真实数据库故障复盘。
- 面试官想听什么：知道 MySQL JSON 类型不能写空字符串。
- 简答：聊天记录的 `retrieval_snapshot` 是 JSON 字段，用户消息没有检索快照时不能写 `""`，因为空字符串不是合法 JSON。修复方式是模型改成 `*string`，用户消息写 NULL，助手消息写 `json.Marshal(citations)` 后的 JSON 数组。
- 深答：

  <details>
  <summary>展开深答</summary>

  这个问题不是 GORM 语法问题，而是 MySQL JSON 类型校验。第一版用 string 表达可空 JSON 字段，默认空值会写成空字符串。MySQL 会拒绝，因为 JSON 字段只能接受合法 JSON，比如 `null`、`[]`、`{}` 或字符串 JSON `"text"`，不能接受裸空字符串。

  修复时我没有把字段改成普通 text 来绕过，而是保留 JSON 语义：用户消息没有 retrieval snapshot 就写 NULL；助手消息有 citations 时先 marshal 成 JSON 数组再保存。这样后续排查 RAG 时还能直接把当次检索证据还原出来。
  </details>

- 延伸追问：
  - 为什么不存 `{}`？
  - JSON 字段和 text 字段怎么选？
  - 这个 bug 怎么写测试？
- 项目证据：
  - `internal/model/chat.go:24` `RetrievalSnapshot *string gorm:"type:json"`。
  - `internal/service/chat.go:228` 保存 retrieval snapshot。
  - `docs/troubleshooting-and-interview-notes.md:620` 根因是 JSON 空字符串。
  - `docs/troubleshooting-and-interview-notes.md:713` 修复为 pointer/null 和 JSON array。
- 当前边界：当前 snapshot 是保存检索证据，不是自动判断答案可信度。

### 7. RAG chunks 为什么 MySQL 和 Milvus 都要存？

- 题目：向量库和关系库职责。
- 面试官想听什么：Milvus 管向量检索，MySQL 管状态、文本和可排障数据。
- 简答：Milvus 用来做向量 Top-K；MySQL `video_chunks` 保存 chunk 文本、索引、embedding_model 等结构化信息，便于状态查询、关键词召回、重建索引和删除清理。两者不是重复造轮子。
- 深答：

  <details>
  <summary>展开深答</summary>

  向量库适合相似度检索，但不适合承载所有业务状态。VidLens 的 RAG 不只是查向量，还要知道某个 task 是否 indexed、chunk 数量是多少、失败原因是什么、当前 embedding model 是什么。MySQL 表能把这些信息以事务和索引的方式管理。

  另外当前有 Go 侧 BM25 风格关键词召回，需要从 MySQL 读取 task 范围内的 chunk 文本打分。重建索引时也会先替换 MySQL chunks，再写 Milvus vectors，并在 `video_rag_indexes` 里记录状态。面试里可以说这是“向量检索 + 关系型元数据”的组合。
  </details>

- 延伸追问：
  - MySQL 和 Milvus 不一致怎么办？
  - 为什么不只存 Milvus content 字段？
  - chunk 表会不会太大？
- 项目证据：
  - `internal/model/rag_index.go:12` `VideoRAGIndex` 状态模型。
  - `internal/model/video_chunk.go:8` chunk 关联 user/task/model。
  - `internal/repository/video_chunk.go:26` `ReplaceTaskChunks` 删除后创建。
  - `internal/repository/video_chunk.go:47` MySQL chunk 上做 BM25 风格召回。
- 当前边界：当前一致性主要靠重建流程和删除流程维护，还没有专门的后台一致性巡检。

### 8. Repository 层为什么要包一层事务？

- 题目：分层和事务边界。
- 面试官想听什么：不是为了套架构，而是为了集中 DB 操作和复用事务上下文。
- 简答：Repository 把 GORM 查询集中起来，service 负责业务编排。像删除任务、替换 chunks、恢复 retry claim 这类操作需要多个表一致更新，Repository 提供 transaction wrapper 和条件更新，避免 service 到处散落 DB 细节。
- 深答：

  <details>
  <summary>展开深答</summary>

  如果所有 service 都直接写 GORM，早期很快，但后面会出现重复查询、事务上下文难传、条件更新不统一的问题。VidLens 的 repository 层并不厚，它主要提供按业务命名的查询和更新方法，比如 `FindDueRetryTasks`、`ClaimRetryTask`、`RestoreRetryAfterDispatchFailure`、`ReplaceTaskChunks`。

  事务边界按业务一致性选：删除任务要同时清 chat、RAG index、chunks、jobs 和 task，所以放在一个 DB transaction 里；但 MinIO/Milvus 属于外部资源，不能和 MySQL 做本地事务，只能记录边界并规划补偿。
  </details>

- 延伸追问：
  - Repository 会不会变成贫血包装？
  - 事务应该放 service 还是 repository？
  - 外部资源怎么保证最终一致？
- 项目证据：
  - `internal/repository/repository.go:41` transaction wrapper。
  - `internal/service/media.go:452` DeleteTask 使用事务。
  - `internal/repository/task.go:226` `ClaimRetryTask` 条件更新。
  - `internal/repository/task.go:243` claim 后投递失败恢复。
- 当前边界：Repository 解决数据库事务和查询复用，不解决跨 MySQL、MinIO、Milvus 的分布式事务。

