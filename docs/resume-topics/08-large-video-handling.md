# 专题 8：大视频全链路处理

> 面试高频问题：如果用户上传或处理的视频很大，系统怎么避免超时、内存暴涨和失败后从头开始？
>
> 当前事实以[专题 3：耐久化分片上传与断点续传](03-chunk-upload-resume.md)为准。本篇只解释大视频全链路，不再维护第二份上传协议细节。

## 1. 先给结论

VidLens 把大视频问题拆成四层：

1. **入口层**：普通上传限制最大体积并流式落临时文件；大文件使用用户级 upload session 分片续传。
2. **存储层**：MinIO 保存分片和最终视频，PostgreSQL 保存不可变 manifest、分片台账和完成状态。
3. **处理层**：Kafka 只传任务消息；Consumer 从 MinIO 获取视频，用 FFmpeg 提取音频并按固定时长切片调用 ASR。
4. **资源边界**：承认当前仍缺少 abandoned session 后台回收、统一 worker 并发配额和完整的大视频容量压测。

推荐口语回答：

> 大视频不能只靠“用了 MinIO”解决。我在入口层限制文件大小，普通上传边写临时文件边算 MD5，不把完整视频读进 Go 堆；需要续传时创建绑定用户的 durable upload session，由 PostgreSQL 保存 manifest 和分片台账，MinIO 保存字节。完成时服务端按顺序流式读取分片、计算完整 MD5，再流式写最终对象。上传后 Kafka 只投递 task ID，真正的 FFmpeg 和 ASR 在 Consumer 中异步执行，音频按 300 秒切片。当前还需要继续补 abandoned session 回收、worker 并发控制和容量压测，我不会声称支持任意大小的视频。

## 2. 当前数据边界

```text
PostgreSQL
├── upload_sessions             manifest、owner、lease、终态、task identity
├── upload_session_chunks       exact size、SHA-256、MinIO object name
├── video_assets                按完整文件 MD5 复用真实资产
├── video_tasks / task_jobs     业务任务与阶段状态
└── transcription_chunks        ASR 分片进度和结果

MinIO
├── upload-sessions/<session>/chunks/...
├── upload-sessions/<session>/final...
└── 可被多个 task 引用的视频对象

Kafka
└── task ID、动作和追踪信息，不承载视频字节

Redis
├── 分布式锁
├── 令牌桶限流
└── 最近聊天记忆
    （不参与上传会话正确性）
```

## 3. 普通上传：流式复制而不是整文件读入内存

普通上传仍适合较小文件或网络稳定场景：

1. Handler 交给 `MediaService.UploadFile`；
2. Service 使用 `copyStreamToTempAndHash` 边复制到临时文件边计算 MD5；
3. 校验大小后上传 MinIO；
4. 复用或创建 `video_assets`，再创建用户任务。

这里的关键事实是：**流式复制降低 Go 堆内存占用，但仍会占用本机临时磁盘和 API 网络带宽。** `upload.max_file_size` 是显式上限，不应该描述为支持无限大文件。

## 4. 分片上传：PostgreSQL durable session

当前 HTTP 协议是：

```text
POST /api/v1/media/upload-sessions
GET  /api/v1/media/upload-sessions/:session_id
PUT  /api/v1/media/upload-sessions/:session_id/chunks/:index
POST /api/v1/media/upload-sessions/:session_id/complete
```

关键设计：

- session 绑定 `user_id`，不能只凭客户端 MD5 访问；
- manifest 创建后不可变，包含 filename、file size、chunk size/count 和 expected MD5；
- chunk body 使用 `application/octet-stream`；
- 服务端按 manifest 计算每一片的精确大小，并用请求体上限拒绝多传；
- 接收时计算单片 SHA-256；同 index 同内容幂等，不同内容返回冲突；
- `GET session` 从 PostgreSQL ledger 恢复 uploaded indexes，Redis 不在正确性链路上；
- complete 使用 token + expiry lease，活跃完成者互斥，过期 lease 可回收；
- asset、task 和 session completed 状态在同一个 PostgreSQL transaction 中落库；
- 重复 complete 返回 session 保存的稳定 task identity。

这套协议不再使用已经退役的 `/upload-chunk`、`/check-upload`、`/merge-chunks`，也不再用 Redis Set 作为上传事实源。

## 5. 为什么当前使用应用层流式合并

完成 session 时，服务端按 chunk index 打开 MinIO 对象，通过 `io.Pipe`：

```text
MinIO chunks
    ↓ 顺序 io.Copy
Go streaming pipe ── 同时计算完整 MD5 和 size
    ↓
MinIO final object
```

选择这个路径是为了得到服务端端到端完整性证据：

- 不信任客户端只提交的 MD5；
- 能校验最终字节数；
- 不需要把完整视频一次载入内存；
- 不依赖对象存储 multipart part 的最小尺寸语义，chunk size 只受当前业务配置上限和 manifest 约束。

需要诚实说明代价：

- 字节会经过 `MinIO → Go → MinIO`，消耗应用实例网络带宽；
- Go 进程仍参与整个合并时长；
- 这不是对象存储侧零拷贝合并。

如果未来上传规模显著增大，可以评估预签名直传、S3 multipart upload 和对象存储 checksum，但必须重新设计 owner、完成回执与服务端完整性验证，不能只替换一个 SDK 调用。

## 6. 上传失败窗口与恢复语义

| 失败位置 | 当前结果 | 恢复方式 |
|---|---|---|
| 分片写 MinIO 前失败 | ledger 不记录该片 | 客户端重传 |
| 对象写成功、ledger 写失败 | 候选对象 best-effort 删除 | 客户端重传；content-addressed 名称避免覆盖 winner |
| complete 发现缺片 | 释放 completion claim | 补片后重试 |
| 合并字节或 MD5 校验失败 | final object 删除，session 记录失败原因 | 按错误语义修正或新建 session |
| DB finalization 失败 | 释放 claim，final object best-effort 删除 | 重试 complete |
| 成功响应丢失 | session 已保存 task ID | 重复 complete 返回同一 task |

当前限制：

- 尚未实现 expired/abandoned session 与孤儿 chunk 的后台扫描清理；
- completed session 在关联 task 被删除后的长期返回语义仍需明确；
- 成功后的 chunk 清理是 best-effort；
- 还没有真实 GB 级并发上传容量数据。

这些是后续工作，不能包装成已完成能力。

## 7. Kafka 为什么不传视频

Kafka 消息只携带任务身份、动作、trace 等小型元数据。视频字节放 MinIO，原因是：

- 大消息会放大 broker 网络、磁盘和副本成本；
- 消费重放会重复搬运大对象；
- Kafka 适合持久任务事件，不是视频对象存储；
- Consumer 可以凭稳定 object name 按需读取视频。

HTTP 创建任务后快速返回，下载、转写、摘要和 RAG 索引由 Consumer 异步执行。但必须区分：**异步化避免 HTTP 长时间阻塞，不等于自动获得任务入库与首次 Kafka 投递的原子性。** 该失败窗口需要单独治理。

## 8. FFmpeg 与长音频 ASR

转写阶段不会把数小时视频直接作为一个 ASR 请求：

1. Consumer 从 MinIO 获取视频；
2. FFmpeg 提取适合语音识别的音频；
3. `SplitAudio` 按 300 秒切片；
4. `transcription_chunks` 保存每片状态和文本；
5. 重试时复用已完成分片，失败片可以定位；
6. 最终合并转写文本，再进入摘要和 RAG 索引。

收益是缩小单次 AI 请求和故障范围。代价是切片边界可能损失上下文，串行处理时总耗时仍较长；并发 ASR 还需要受 provider 限流和用户成本约束。

## 9. URL 下载只保留基础能力

URL 下载不是简历重点。当前保留：

- HTTP 入口快速创建下载任务；
- Kafka Consumer 异步调用 yt-dlp；
- 入口和执行前共享 allowlist/DNS 基础校验；
- 下载规格限制，避免默认获取最高分辨率。

它仍不是生产级下载沙箱。yt-dlp 后续重定向、DNS 变化、下载体积、时长和临时磁盘保护仍需要更强的网络与资源隔离。本项目不继续把这条链路作为优先重构对象。

## 10. 资源瓶颈怎么回答

| 资源 | 大视频下的具体压力 | 当前措施 | 仍需补充 |
|---|---|---|---|
| API 网络 | 普通上传和 chunk 中转 | 最大体积限制、分片重试 | 预签名直传评估 |
| Go 内存 | 错误地 `ReadAll` 完整视频 | 普通上传、分片合并都流式处理 | heap/profile 容量验证 |
| Go/MinIO 网络 | 应用层流式合并搬运两次 | 有界流式管道 | 压测后评估 multipart/checksum |
| 临时磁盘 | 普通上传、下载、FFmpeg 中间文件 | 正常路径清理 | 崩溃残留扫描、磁盘水位保护 |
| CPU | FFmpeg 转码/切片 | Kafka 异步 | API/worker 分离、并发上限 |
| 外部 AI | ASR/LLM/Embedding 成本与限流 | 切片、错误分类、重试预算 | 用户额度和 provider 并发治理 |
| PostgreSQL | session ledger、轮询和任务状态 | 索引与短事务 | 真实并发压测、连接池调优 |

## 11. 推荐演进顺序

1. 先实现 abandoned/expired upload session 和孤儿对象回收；
2. 明确 task 删除后的 completed session 生命周期；
3. 给 FFmpeg、ASR、Embedding 增加可配置并发上限；
4. 用固定大小与并发矩阵压测 PostgreSQL、MinIO、Go 网络和临时磁盘；
5. 有证据表明 API 中转成为瓶颈后，再评估预签名 multipart 直传；
6. 最后补用户级存储、时长和 AI 费用额度。

不要在没有压测数据时直接引入复杂 multipart 协调器或单独的上传中间件。

## 12. 代码证据

```text
internal/model/upload_session.go
internal/model/upload_session_chunk.go
internal/repository/upload_session.go
internal/service/upload_session.go
internal/service/upload_session_chunk.go
internal/service/upload_session_complete.go
internal/handler/upload_session.go
web/src/chunkedUpload.js

internal/service/media_file_upload.go
internal/storage/minio.go

internal/mq/consumer_transcribe.go
internal/pkg/ffmpeg/ffmpeg.go
internal/model/transcription_chunk.go

internal/mq/consumer_download.go
internal/pkg/remoteurl/
internal/pkg/ytdlp/ytdlp.go
```

详细上传不变量、测试与面试防守统一维护在：

- [专题 3：耐久化分片上传与断点续传](03-chunk-upload-resume.md)
- [后端维护地图](../backend-maintenance-map.md)

## 13. 最后背诵版

> 我不会把“大视频”只回答成分片上传。入口层普通上传是有大小上限的流式复制，大文件可以创建绑定用户的 durable upload session；PostgreSQL 保存不可变 manifest、分片 SHA-256 台账和完成 lease，MinIO 保存字节，Redis 不参与上传正确性。完成时服务端通过 io.Pipe 顺序读取分片、流式写最终对象，并重新计算完整 MD5，因此不会把整段视频读入内存，但会付出 MinIO 到 Go 再到 MinIO 的网络代价。上传完成后 Kafka 只传任务消息，Consumer 用 FFmpeg 提取音频并按 300 秒切片调用 ASR。当前还缺 abandoned session 回收、统一 worker 并发限制和大规模容量数据，这些我会明确作为后续工作。
