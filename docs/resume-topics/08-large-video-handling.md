# 专题 8：超大视频文件怎么处理

> 面试高频问题："如果用户上传的视频很大，或者外链下载来的视频很大，怎么办？"
> 这不是单纯问"有没有分片上传"，而是在考你是否从入口限制、对象存储、临时文件、异步处理、音频切片、AI 成本和生产边界整体思考过视频系统。
> 诚实原则：区分当前已实现、当前项目边界和生产化演进。

## 1. 这个问题为什么一定会被问

VidLens 的核心对象是视频。视频和普通图片、文本最大的区别是：

- 文件体积大，弱网上传容易中断。
- 外链下载大小不可控，目标站点也不稳定。
- 后端处理链路长，下载、FFmpeg、ASR、摘要、Embedding 都可能耗时。
- 临时文件、磁盘、CPU、内存和外部 AI 调用都会被大视频放大。
- 如果处理失败，用户不能接受"重头再来"。

所以这个问题不是边角问题，而是视频理解项目必须回答的主线问题。

## 2. 先给总答案

推荐先这样答：

> 大视频我分入口、存储、处理和边界四层处理。入口层，本地上传有最大文件大小限制，并提供分片上传和断点续传，避免单个 HTTP 请求过大；存储层，分片和最终视频都放 MinIO，合并用 MinIO ComposeObject，不在 Go 进程内拼接大文件；处理层，视频后处理通过 Kafka 异步执行，HTTP 只创建任务，Consumer 再下载到临时文件、用 FFmpeg 提取低码率音频，并按 300 秒切片调用 ASR；边界层，外链下载当前做了 Kafka 异步和 720p 限制，但生产上还应该补下载后大小/时长校验、磁盘空间保护、worker 并发控制和临时文件清理。

一句话定调：

> Kafka 里不放视频文件，Kafka 只放任务消息；大文件本体进 MinIO，临时状态进 Redis，业务状态进 PostgreSQL，处理过程由 worker 异步执行。

## 3. 当前项目已经做了什么

### 3.1 本地普通上传：大小限制 + 流式落盘

当前配置里有上传大小限制：

```yaml
upload:
  max_file_size: 2147483648  # 2GB
  chunk_size: 5242880        # 5MB
```

普通上传不是把整个文件一次性读进内存。后端使用 `io.Copy` 和 `io.TeeReader` 边写临时文件边计算 MD5：

```text
internal/service/media.go
copyStreamToTempAndHash
```

这点很适合主动讲：

> 普通上传虽然是一个请求，但后端不是 `ReadAll` 整个视频，而是流式写临时文件并同时算 MD5，所以 2GB 文件不会整体进 Go 堆内存。

### 3.2 分片上传：Redis Set + MinIO 分片对象

大文件更推荐走分片上传。

当前流程：

1. 前端按固定大小切片。
2. 每个分片带 `file_md5` 和 `chunk_number` 上传。
3. 后端把分片写入 MinIO 临时路径：`chunks/<file_md5>/<chunk_number>`。
4. 写入成功后，把分片编号写入 Redis Set：`upload:chunks:<file_md5>`。
5. 前端恢复上传时查 Redis Set，只补传缺失分片。
6. 合并前后端检查 `0..total_chunks-1` 是否全部存在。
7. 合并时调用 MinIO `ComposeObject` 生成最终视频对象。

关键回答：

> Redis Set 适合记录"已上传分片编号集合"，天然去重；MinIO ComposeObject 适合对象存储侧合并，Go 服务不需要把所有分片下载到内存里拼接。

代码证据：

```text
internal/handler/media.go       UploadChunk / CheckUpload / MergeChunks
internal/service/media.go       UploadChunk / MergeChunks
internal/storage/minio.go       ComposeObject
internal/pkg/lock/redis_lock.go 合并阶段 Redis 锁
```

### 3.3 分片合并：Redis 锁防重复合并

大文件合并是临界区。用户可能重复点击合并，或者多个请求同时合并同一个 MD5。

当前做法：

- 合并前查 `video_assets`，如果已有相同 MD5 asset，直接复用。
- 如果没有 asset，按 `vidlens:merge:<md5>` 获取 Redis 锁。
- 拿到锁后校验分片完整性。
- 调用 MinIO ComposeObject。
- 创建 `video_assets` 和 `video_tasks`。

可以这样讲：

> 分片上传解决弱网重传，Redis 锁解决合并并发，MD5 asset 复用解决重复存储。这三个点要一起讲，不能只说"我用了分片上传"。

### 3.4 外链下载：异步任务 + 720p 限制

外链下载和本地上传不同：用户只给一个 URL，文件大小和清晰度不可控。

当前项目已经做了：

- URL 任务先落库，HTTP 立即返回，不同步等待下载完成。
- 下载由 Kafka `video-download` Consumer 执行。
- `yt-dlp` 下载参数限制最高 720p。
- 下载完成后计算 MD5，如果 asset 已存在则复用。
- URL 校验限制 allowed hosts，并阻止内网、本地地址，降低 SSRF 风险。

代码证据：

```text
internal/service/media.go          UploadByURL
internal/mq/consumer.go            handleDownload
internal/pkg/ytdlp/ytdlp.go        buildArgs
internal/service/remote_video_url.go URL 校验和 SSRF 基础防护
```

推荐说法：

> 外链下载我没有放在 HTTP 请求里同步做，而是先创建下载任务，再由 Kafka Consumer 异步下载。下载侧通过 yt-dlp 限制最高 720p，避免默认拉 4K 源视频导致文件和处理成本过大。

### 3.5 视频后处理：Kafka 异步 + FFmpeg 音频压缩 + ASR 切片

真正的大视频压力不只在上传，还在处理。

当前处理链路：

1. Consumer 从 MinIO 下载视频到临时文件。
2. FFmpeg 提取音频。
3. 音频转成 ASR 友好的低码率格式：

```text
-ac 1 -ar 16000 -acodec libmp3lame -b:a 32k
```

4. 音频统一按 300 秒切片。
5. 每段调用 ASR。
6. 合并文本并写入 `video_transcriptions`。
7. 摘要和 RAG 复用已有转写，避免重复 ASR。

代码证据：

```text
internal/pkg/ffmpeg/ffmpeg.go      ExtractAudio / SplitAudio
internal/mq/consumer.go            transcribeAudio / processVideo
internal/ai/mimo.go                ASR 请求体限制和 base64
```

关键说法：

> 大视频不能直接把整段音频一次性发给 ASR。项目里先把音频压缩成更适合语音识别的 16k 单声道低码率音频，再按 300 秒切片。这样既降低请求体大小，也避免长音频单次识别只返回前半段的问题。

## 4. 当前项目边界：不要夸大

这道题很容易被问穿，所以要主动说边界。

当前已做：

- 本地上传最大大小限制。
- 分片上传和断点续传。
- Redis Set 记录分片。
- MinIO 存储和 ComposeObject 合并。
- 合并阶段 Redis 锁。
- URL 下载异步化。
- yt-dlp 限制 720p。
- FFmpeg 音频压缩。
- ASR 300 秒切片。
- 任务失败治理和退避重试。

当前还不应夸大：

- 不要说支持任意大小视频。
- 不要说外链下载已经有完整大小/时长硬限制。
- 不要说已经做了磁盘空间实时保护。
- 不要说已经有 worker 并发上限和资源配额。
- 不要说临时分片/临时下载文件已有完整生命周期清理系统。
- 不要说 Kafka 能处理大视频文件，Kafka 只处理任务消息。

生产化还应补：

- 外链下载完成后做 post-download size check。
- 外链下载前或下载中尽量探测时长、清晰度、大小。
- 任务开始前检查磁盘剩余空间。
- 限制单机同时运行的 FFmpeg/ASR worker 数量。
- 给下载、转码、ASR 分别设置超时。
- 对长期未合并的分片做生命周期清理。
- 对 MinIO 临时对象设置清理策略。
- 将视频处理 worker 和 API 服务拆开部署。
- 给用户设置上传额度、视频时长额度和每日 AI 调用额度。

## 5. 高频追问与回答

### Q1：如果用户上传 5GB 或 10GB 视频怎么办？

答：

> 当前项目不会承诺任意大小。系统配置了最大上传大小，超过限制应该拒绝。对较大但在限制内的视频，推荐走分片上传，断点续传能避免弱网失败后重传整个文件。生产上还要结合对象存储容量、磁盘临时目录、FFmpeg 处理能力和用户额度来定最大值。

### Q2：为什么不能直接整文件上传？

答：

> 整文件上传对小视频简单，但大视频一旦弱网中断，就要从头重传，而且一个 HTTP 连接会长时间占用。分片上传把失败恢复粒度缩小到单个分片，后端也能限制单次请求大小。

### Q3：断点续传怎么知道哪些分片已经传过？

答：

> 后端用 Redis Set 记录某个 `fileMD5` 下已上传成功的分片编号。前端恢复上传时先查这个 Set，只上传缺失编号。Set 天然去重，所以同一个分片重复上传不会造成重复状态。

### Q4：合并大文件会不会吃满 Go 内存？

答：

> 不会在应用层把所有分片读出来拼。分片已经是 MinIO 里的对象，合并时调用 MinIO ComposeObject，由对象存储侧完成合并。Go 服务主要做完整性校验、锁控制、调用合并接口和创建 asset 元数据。

### Q5：外链下载来的视频特别大怎么办？

答：

> 当前已经做了两层基础控制：下载是 Kafka 异步任务，不阻塞 HTTP；yt-dlp 限制最高 720p，避免默认拉 4K 大文件。但我会主动承认当前还应补下载完成后的大小和时长校验。如果超过配置阈值，就删除临时文件，任务标记失败，避免继续进入 FFmpeg 和 ASR。

### Q6：为什么不把视频文件放进 Kafka？

答：

> Kafka 里只放任务消息，比如 taskID、md5、traceID。视频文件本体放 MinIO。把大文件塞进 Kafka 会放大 broker 存储、网络和消费者压力，也不符合 Kafka 作为消息日志的职责。

### Q7：大视频处理会不会拖垮 API 服务？

答：

> 当前已经把 HTTP 和视频处理解耦了，API 只创建任务，真正的下载、FFmpeg、ASR 在 Consumer 里异步做。生产上更进一步应该把 worker 和 API 服务分开部署，并限制单机同时处理的视频数，避免 FFmpeg 抢占 Web 请求资源。

### Q8：3 小时视频怎么 ASR？

答：

> 不会整段发给 ASR。先提取低码率音频，再按 300 秒切片。3 小时大约会切成 36 段，每段独立调用 ASR，最后合并。当前还记录了分片转写状态，已完成片段可以复用，失败时能定位是哪一段失败。

### Q9：某个 ASR 分片失败怎么办？

答：

> 当前任务会进入失败治理，可恢复错误走退避重试，不可恢复错误快速失败。分片级结果已经有状态记录，后续可以进一步演进为只重试失败片段，而不是整个任务重跑。

### Q10：临时文件会不会堆满磁盘？

答：

> 当前代码在正常路径上有 `defer os.Remove` 或 `os.RemoveAll` 清理下载文件、音频文件和切片目录。但生产环境还需要补更强的后台清理机制，处理进程崩溃、任务取消、长期未合并分片这些非正常路径。这个我不会说已经完全生产化。

### Q11：相同大视频重复上传会不会重复存储？

答：

> 不会直接按任务重复存储。项目把真实文件和用户任务拆开：`video_assets` 表示真实视频资产，按 MD5 唯一；`video_tasks` 表示用户的一次处理任务。相同 MD5 的视频可以复用同一个 asset。

### Q12：如果用户删除任务，视频文件能不能直接删？

答：

> 不能直接删对象。因为同一个 asset 可能被多个 task 引用。删除任务时要先清理当前任务的转写、摘要、RAG chunks、聊天和向量，再检查这个 asset 是否还有其他任务引用。只有没有引用时才删除 MinIO 里的真实对象。

## 6. 面试 30 秒话术

> 大视频我分三层处理。入口层有最大大小限制，并支持分片上传和断点续传，Redis Set 记录已上传分片；存储层用 MinIO 保存分片和最终视频，合并用 ComposeObject，不在 Go 里拼大文件；处理层用 Kafka 异步执行，Consumer 再用 FFmpeg 提取低码率音频，并按 300 秒切片调用 ASR。外链下载当前也异步化并限制 720p，但生产上还要补下载后的大小/时长校验、磁盘保护和 worker 并发控制。

## 7. 面试 2 分钟话术

> 这个问题我会拆成本地上传、外链下载和后端处理三块。
>
> 本地上传大文件时，普通上传有最大大小限制，真正的大文件推荐走分片上传。每个分片单独上传到 MinIO，后端用 Redis Set 记录某个 fileMD5 下哪些分片已经成功。网络中断后前端查询已上传分片，只补传缺失部分。所有分片到齐后，后端先做完整性校验，再调用 MinIO ComposeObject 合并，避免把所有分片读回 Go 内存。合并阶段用 Redis 锁防止重复合并。
>
> 外链下载因为文件大小不可控，所以我没有同步下载，而是先创建任务，投递 Kafka download 消息，由 Consumer 异步执行。yt-dlp 侧限制最高 720p，避免默认拉 4K 源视频。
>
> 后端处理也不在 HTTP 请求里做。Consumer 从 MinIO 下载视频到临时文件，用 FFmpeg 提取音频并转成 16k 单声道低码率 mp3，再按 300 秒切片调用 ASR，最后合并文本。这样可以降低单次请求体和长音频识别不完整风险。
>
> 当前边界是：外链下载还应该补下载后大小和时长校验、磁盘空间监控、worker 并发上限和临时对象清理。这些是我认为生产化必须继续做的点。

## 8. 面试 5 分钟深讲版本

我会先把这个问题定性：大视频不是一个单点问题，而是入口、存储、任务、处理和资源治理的组合问题。

第一层是入口。对于本地上传，项目有 `upload.max_file_size` 限制，避免无限大文件进入系统。普通上传后端用流式落盘和 TeeReader 计算 MD5，不会把整个视频读进内存。更适合大文件的是分片上传：前端切片，后端每片写 MinIO，Redis Set 记录已成功编号，恢复时只补传缺失分片。

第二层是存储和合并。分片合并前必须检查完整性，确认每个分片都存在。合并时不在 Go 应用里拼接，而是用 MinIO ComposeObject 在对象存储侧合并，这能降低应用内存和 IO 压力。合并阶段用 Redis 锁保护同一 MD5，避免重复合并。合并完成后创建或复用 `video_assets`，再创建用户维度的 `video_tasks`。

第三层是外链下载。URL 下载不可控，所以后端先创建任务并投递 Kafka，不阻塞 HTTP 请求。Consumer 用 yt-dlp 异步下载，并限制最高 720p。下载完成后计算 MD5，已有 asset 就复用。这里我会主动说边界：当前还应该补 post-download size check 和 duration check，超过限制就删除临时文件并快速失败。

第四层是处理。视频处理不直接处理原视频全文，而是提取音频。FFmpeg 把音频转为 16k 单声道 32kbps，降低 ASR 请求体。长音频统一按 300 秒切片，逐段 ASR，最后合并。这样大视频不会变成一次超大 ASR 请求。

最后是资源治理。当前已有 Kafka 异步、任务状态、重试治理和正常路径临时文件清理。生产上还要加 worker 并发上限、磁盘空间保护、临时对象生命周期清理、用户额度和视频处理 worker/API 服务分离部署。

## 9. 结合八股怎么答

### 大文件上传

核心不是"能传大文件"，而是：

- 单次请求大小可控。
- 失败后可恢复。
- 合并前完整性校验。
- 合并时不占用应用内存。
- 临时对象可清理。

### 对象存储

视频文件和分片属于对象数据，适合放 MinIO。MySQL 只存元数据，Kafka 只传任务消息，Redis 存临时状态。

### 异步任务

大视频处理是分钟级任务，不能绑定 HTTP 请求生命周期。Kafka 让入口请求快速返回，后台 Consumer 按能力处理。

### 资源隔离

FFmpeg、ASR、Embedding 都是高成本任务。生产上应该把 worker 和 API 服务拆开，限制单机并发，避免一个大视频拖垮整个 Web 服务。

### 最终一致性

上传完成不等于处理完成。视频文件、任务状态、转写、摘要、RAG 索引会在异步流程中逐步达到最终状态。

## 10. 可主动发散的生产化优化

如果面试官继续追问"你还能怎么优化"，按优先级答：

1. **外链下载大小/时长校验**
   下载完成后马上检查文件大小和视频时长，超过阈值直接失败并删除临时文件。

2. **worker 并发上限**
   给 FFmpeg、ASR、Embedding 分别加信号量或 worker pool，避免多个大视频同时处理打满 CPU/内存/磁盘。

3. **API 与 worker 分离**
   API 服务只处理鉴权、上传、查询；视频处理 worker 单独部署，方便扩容和资源隔离。

4. **临时对象生命周期清理**
   清理长期未合并分片、崩溃残留临时音频、下载失败残留文件。

5. **用户额度**
   按用户限制单文件大小、每日上传总量、视频时长、ASR 调用次数和 RAG 索引次数。

6. **预签名直传**
   前端用预签名 URL 直传 MinIO，减少 API 服务带宽压力。但要补对象名权限、回调校验和安全策略。

7. **更细粒度进度**
   展示下载进度、转码进度、ASR N/M 分片、RAG chunk 构建进度。

8. **按时长切片并发 ASR**
   当前可讲串行更稳。生产可以在限流和成本可控前提下并发处理多个 ASR 分片。

9. **格式和内容安全校验**
   不只看扩展名，还要用 ffprobe 或 mimetype 判断是否真实视频，防止伪装文件。

10. **监控和告警**
    监控磁盘、临时目录、FFmpeg 耗时、ASR 分片失败率、Kafka lag、MinIO 容量。

## 11. 不要这么说

- 不要说"Kafka 处理大文件"。Kafka 只处理任务消息。
- 不要说"用了 MinIO，所以大文件没问题"。MinIO 解决存储，不解决上传、处理和资源保护。
- 不要说"支持任意大小视频"。必须有最大大小、最大时长和资源限制。
- 不要说"外链下载完全安全"。当前已有 SSRF 基础防护，但生产还要网络隔离。
- 不要说"分片上传就不会失败"。分片上传只是让失败后可恢复。
- 不要说"ASR 切片可以无限并发"。AI 成本和 provider 限流必须考虑。
- 不要说"删除任务就直接删除视频文件"。共享 asset 必须检查引用。

## 12. 代码证据路径

```text
config.yaml
  upload.max_file_size / upload.chunk_size

internal/service/media.go
  UploadFile
  copyStreamToTempAndHash
  UploadChunk
  CheckUploadProgress
  MergeChunks
  UploadByURL
  DeleteTask

internal/storage/minio.go
  UploadFile
  UploadFromPath
  ComposeObject
  DownloadToTemp

internal/pkg/lock/redis_lock.go
  TryLock
  watchdog
  Unlock

internal/pkg/ytdlp/ytdlp.go
  buildArgs
  DownloadVideo

internal/service/remote_video_url.go
  validate
  unsafeIP
  sanitizeRemoteVideoURL

internal/pkg/ffmpeg/ffmpeg.go
  ExtractAudio
  SplitAudio

internal/mq/consumer.go
  handleDownload
  handleTranscribe
  processVideo
  transcribeAudio

internal/mq/retry.go
  isRetryableError
  RetryScheduler
```

## 13. 最后一页背诵版

> 面试官问大视频怎么办时，我不会只说分片上传。我会分四层讲：
>
> 第一，入口层限制大小，较大文件走分片上传，Redis Set 记录已传分片，断点续传只补缺失分片。
>
> 第二，存储层用 MinIO 存分片和最终文件，合并用 ComposeObject，不在 Go 应用里拼接大文件，合并阶段用 Redis 锁防重复。
>
> 第三，处理层走 Kafka 异步，HTTP 不等下载、转码和 ASR；Consumer 用 FFmpeg 提取低码率音频，按 300 秒切片调用 ASR。
>
> 第四，边界层诚实说明：外链下载已异步化并限制 720p，但生产还要补下载后大小/时长校验、磁盘空间保护、worker 并发上限、临时对象清理和用户额度。
>
> 这道题真正考的是大文件全链路治理，不是某一个 API。
