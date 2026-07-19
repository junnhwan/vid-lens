# 上传、断点续传与 MinIO 拷打

> 目标：把“大文件上传”讲成状态一致性问题：临时分片、最终对象、MD5 资产、用户任务之间的边界要分清。

### 1. 为什么要做分片上传和断点续传？

- 题目：上传链路第一问。
- 面试官想听什么：大文件和弱网为什么不适合一次性上传。
- 简答：视频文件通常比普通表单大，一次性上传失败后重传成本高。分片上传把大文件拆成 chunk，后端记录已上传分片，断线后只补缺失部分，最后合并成一个 MinIO object。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 的业务对象是视频，本身就可能几十 MB 到几百 MB。一次性上传如果在 90% 失败，用户只能从头开始，体验和带宽都浪费。分片上传把“传输过程”和“最终视频资产”拆开：上传过程中每个 chunk 是临时对象，Redis 记录进度；合并成功后才生成正式视频对象和 `video_assets` 记录。

  这样前端可以调用 progress 接口知道哪些 chunk 已经上传，弱网恢复后只补缺失分片。后端 merge 时再检查所有分片是否齐全，避免合并出残缺视频。
  </details>

- 延伸追问：
  - 分片大小怎么选？
  - Redis 状态和 MinIO chunk 不一致怎么办？
  - 为什么不是后端自动每片上传后尝试 merge？
- 项目证据：
  - `README.md:259` 说明 Redis 记录总分片和 24h 过期。
  - `internal/service/media.go:550` 初始化 chunked upload。
  - `internal/service/media.go:557` 查询上传进度。
  - `internal/service/media.go:601` merge chunks。
- 当前边界：当前分片大小是配置化基础能力，没有做动态弱网自适应分片。

### 2. Redis Set 为什么适合记录上传分片？

- 题目：数据结构选择题。
- 面试官想听什么：Set 的幂等和 membership 检查。
- 简答：每个 chunk number 只需要记录是否上传成功，Set 天然去重，重复上传同一片不会重复计数。merge 时用 `SIsMember` 检查 0 到 totalChunks-1 是否都存在。
- 深答：

  <details>
  <summary>展开深答</summary>

  这里不需要保存复杂结构，核心状态就是“第 N 片是否已经成功落盘”。Redis Set 比 List 更适合，因为 List 会受到重复上传影响；Hash 也可以，但 Set 语义更直接。前端查询时 `SMembers` 能拿到已上传 chunk number，前端据此补传缺失分片。

  关键顺序是先把 chunk 写入 MinIO，成功后再 `SAdd`。如果反过来，Redis 已经显示某片完成，但 MinIO 实际没有对象，merge 时就会失败，排查也更困难。
  </details>

- 延伸追问：
  - Bitmap 会不会更省内存？
  - Redis Set 丢失后能不能从 MinIO 恢复？
  - totalChunks 存在哪里？
- 项目证据：
  - `internal/service/media.go:551` 使用 `upload:chunks:{md5}` 作为 Redis key。
  - `internal/service/media.go:569` `SMembers` 获取已上传分片。
  - `internal/service/media.go:596` `SAdd` 记录 chunk number。
  - `internal/service/media.go:629` merge 时按 Set 校验完整性。
- 当前边界：当前没有从 MinIO chunk 列表反向恢复 Redis progress 的接口。

### 3. 为什么先写 MinIO，再写 Redis？

- 题目：一致性顺序题。
- 面试官想听什么：状态记录不能先于真实数据。
- 简答：Redis progress 表示“这个分片已经可用于合并”。如果先写 Redis 再写 MinIO，MinIO 失败会导致进度虚假完成。当前实现先上传 chunk object，成功后才 `SAdd`，让 Redis 只记录有效分片。
- 深答：

  <details>
  <summary>展开深答</summary>

  这是一个很小但很关键的顺序。分片上传中，最终合并依赖 MinIO 中的 `chunks/{md5}/{chunkNumber}` 对象。如果 Redis 先记账，后面 MinIO 上传失败，前端和 merge 都会认为这片存在。merge 校验 Redis 通过后，ComposeObject 才发现源对象不存在，错误会延后。

  当前 `UploadChunk` 里先调用 storage.UploadFile，把 chunk 作为临时对象落到 MinIO；只有成功后才 `SAdd`，并设置 key TTL。这样 Redis 状态是 MinIO 成功的派生状态，不是乐观状态。
  </details>

- 延伸追问：
  - 如果 Redis `SAdd` 失败怎么办？
  - 如果 MinIO 成功但 Redis 失败，用户会怎样？
  - 是否需要分布式事务？
- 项目证据：
  - `internal/service/media.go:591` 生成 chunk object name。
  - `internal/service/media.go:592` 先上传 MinIO。
  - `internal/service/media.go:596` 上传成功后写 Redis Set。
  - `internal/service/media.go:597` 设置 Redis TTL。
- 当前边界：MinIO 成功但 Redis 写失败时，当前体验会偏保守，可能要求用户重传或后续补恢复逻辑。

### 4. 合并分片为什么要加锁？

- 题目：并发合并题。
- 面试官想听什么：merge 是临界区。
- 简答：同一个 MD5 的多个 merge 请求如果同时执行，可能重复 ComposeObject、重复创建 asset 或互相清理 chunk。VidLens 在 merge 阶段用 Redis lock，并在拿锁前后都检查 MD5 asset 是否已存在。
- 深答：

  <details>
  <summary>展开深答</summary>

  分片上传的并发问题不在单个 chunk，而在 merge。前端可能因为重试或多标签页触发多次合并；多个用户也可能上传同一个 MD5 文件。如果没有锁，两个请求都看到 asset 不存在，都开始 ComposeObject，最后就会产生重复对象或 DB 写冲突。

  当前流程先查 asset，存在则秒建 task；不存在才抢 `vidlens:merge:{md5}` 锁。拿到锁后校验所有分片存在，调用 MinIO ComposeObject 服务端合并，创建 `video_assets`。创建失败会删除合并产物，避免产生孤儿对象。
  </details>

- 延伸追问：
  - 合并失败后怎么重试？
  - 清理 chunk 失败影响结果吗？
  - 为什么拿不到锁时还要再查 asset？
- 项目证据：
  - `internal/service/media.go:610` merge 前查已有 asset。
  - `internal/service/media.go:618` merge 使用 Redis lock。
  - `internal/service/media.go:651` 调用 ComposeObject。
  - `internal/service/media.go:663` asset 创建失败时删除合并对象。
- 当前边界：任务删除和对象删除之间仍不是分布式事务，后续可用 cleanup job 补偿。

### 5. MinIO 为什么比本地磁盘或 MySQL BLOB 更合适？

- 题目：对象存储选型题。
- 面试官想听什么：大文件和业务服务解耦。
- 简答：视频是大二进制对象，不适合放 MySQL BLOB，也不适合依赖单机本地磁盘。MinIO 作为对象存储可以把文件生命周期和业务 DB 状态拆开，后端只保存 object name、MD5、大小和 content type。
- 深答：

  <details>
  <summary>展开深答</summary>

  如果把视频放 MySQL，数据库备份、查询、连接和存储都会被大文件拖累。放本地磁盘也有问题：服务迁移、扩容、多实例部署和故障恢复都很麻烦。对象存储更符合这个场景，业务表只保存元数据，大文件由 MinIO 管。

  VidLens 里 `video_assets` 记录文件 MD5、object name、大小和类型；`video_tasks` 通过 asset 或 file_url 引用它。后续获取视频时走 presigned URL，而不是把文件通过业务服务长期公开。
  </details>

- 延伸追问：
  - MinIO 和云 OSS/S3 有什么关系？
  - 为什么桶要私有？
  - 预签名 URL 有什么风险？
- 项目证据：
  - `internal/storage/minio.go:61` MinIO 上传文件。
  - `internal/storage/minio.go:79` 生成预签名下载 URL。
  - `internal/storage/minio.go:93` 预签名 URL 有 5 分钟有效期。
  - `README.md:54` 说明 MinIO 私有桶 + 5 分钟预签名 URL。
- 当前边界：预签名 URL 只是临时访问控制，不等于完整 DRM 或防转发体系。

### 6. MD5 秒传/复用解决什么问题？

- 题目：去重与成本题。
- 面试官想听什么：内容级资产复用，而不是用户级文件名复用。
- 简答：同一个视频内容重复上传时，MD5 可以复用已有 `video_assets`，避免重复存储和重复合并。对于 AI 处理链路，它也为后续减少重复转写/摘要成本提供基础。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 把底层视频文件抽成 `video_assets`，同一个 MD5 可以被多个 `video_tasks` 引用。这样用户 A 和用户 B 上传同一个内容，或者同一用户重复上传，不需要每次都生成新的 MinIO 对象。普通上传时会先计算文件 MD5，再查已有 asset；分片 merge 前也查 MD5 asset。

  但我会谨慎说“秒传”。当前更准确的说法是内容级资产复用和重复存储规避。是否跳过 ASR/摘要，还要看业务是否允许跨用户复用 AI 结果、权限和隐私边界。当前简历里可以讲 MD5 资产复用，不要夸成完整内容缓存系统。
  </details>

- 延伸追问：
  - MD5 碰撞怎么办？
  - 跨用户复用是否有隐私问题？
  - AI 结果能不能也复用？
- 项目证据：
  - `internal/service/media.go:98` 上传时计算临时文件 MD5。
  - `internal/service/media.go:105` 按 MD5 查 asset。
  - `internal/service/media.go:122` 从 asset 创建 task。
  - `docs/troubleshooting-and-interview-notes.md:2477` 删除生命周期复盘说明同一 asset 可被多个 task 复用。
- 当前边界：MD5 在简历项目足够实用，生产强校验可引入 SHA-256 或大小+hash 联合校验。

### 7. 删除任务时为什么不能直接删 MinIO object？

- 题目：资源生命周期题。
- 面试官想听什么：task 和 asset 的生命周期不同。
- 简答：因为同一个 asset 可以被多个 task 复用。删除一个任务时只能清理这个 task 的转写、摘要、RAG、聊天和 job 状态；只有最后一个 active task 引用消失时，才能删除对应 MinIO object 和 asset 记录。
- 深答：

  <details>
  <summary>展开深答</summary>

  这是 MD5 复用带来的后续问题。旧逻辑如果删除 task 就直接删 MinIO object，会破坏其他引用同一个 asset 的任务：数据库记录还在，但底层视频文件没了。

  当前 DeleteTask 会先清理 task 自己的数据，包括 transcription、transcription chunks、summary、video chunks、RAG index、chat、task_jobs 和 task。然后统计同一个 asset 还有多少 active task 引用。只有引用数为 0 时，才删除 MinIO object 和 asset 记录。
  </details>

- 延伸追问：
  - MinIO 删除失败怎么办？
  - Milvus vector 删除失败怎么办？
  - 这里需要事务吗？
- 项目证据：
  - `internal/service/media.go:427` `DeleteTask` 入口。
  - `internal/service/media.go:443` 删除 task 前清理 Milvus vectors。
  - `internal/service/media.go:453` 删除转写等 task 级数据。
  - `internal/service/media.go:478` 统计 asset active refs。
  - `docs/troubleshooting-and-interview-notes.md:2583` 删除生命周期口语化复盘。
- 当前边界：MinIO/Milvus 和 MySQL 之间不是分布式事务，未来可加 resource cleanup jobs 补偿。

### 8. URL 上传和本地上传有什么不同？

- 题目：入口差异题。
- 面试官想听什么：URL 下载是异步和安全问题，本地上传是文件流问题。
- 简答：本地上传是用户把文件流传给后端，后端计算 MD5 后上传 MinIO；URL 上传是服务端代用户访问外部网络，必须先校验 URL，再创建 download task 投递 Kafka，由 worker 下载、计算 MD5、入库和复用 asset。
- 深答：

  <details>
  <summary>展开深答</summary>

  本地上传的风险主要是文件大小、网络中断和重复上传，所以用普通上传/分片上传处理。URL 上传更复杂，因为服务端会代表用户访问外部地址，存在 SSRF、平台风控、代理、下载超时和大文件问题。

  所以 `UploadByURL` 不直接在 HTTP 请求里跑 yt-dlp。它会先通过 URL validator 检查，再创建 `source_type=url`、`stage=downloading` 的任务，投递 download topic。download consumer 完成实际下载、MD5 计算、MinIO 上传和 asset 复用，成功后把任务更新为 uploaded/pending。
  </details>

- 延伸追问：
  - URL 下载失败前端怎么看？
  - 为什么要限制 720p？
  - source_url 为什么要脱敏？
- 项目证据：
  - `internal/service/media.go:150` `UploadByURL` 创建 URL 下载任务。
  - `internal/service/media.go:178` 投递 download topic。
  - `internal/mq/consumer.go:261` `handleDownload` 执行下载处理。
  - `internal/pkg/ytdlp/ytdlp.go:49` yt-dlp 限制最高 720p。
  - `docs/troubleshooting-and-interview-notes.md:1273` URL 下载任务化复盘。
- 当前边界：当前 URL 下载有第一层安全校验，但不是完整生产级下载沙箱。
