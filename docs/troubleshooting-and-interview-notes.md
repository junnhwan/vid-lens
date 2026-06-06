# VidLens 问题排查与面试复盘记录

> 这份文档用于长期维护项目中真实遇到的问题、排查证据、修复方案和面试话术。后续每次发现问题、定位根因、修复并验证后，都可以按本文末尾的模板追加记录。

## 维护原则

- **证据优先**：先记录日志、数据库状态、代码路径或测试结果，再写结论。
- **区分事实和演进**：已经提交并验证的内容写为“已实现”；想继续优化的内容写为“后续演进”。
- **面试可防守**：不要把 demo 项目说成生产级系统，但可以讲清楚自己如何发现问题、定位根因、做工程化改进。
- **日志归位**：运行日志统一放在 `.logs/`，不要放仓库根目录。`.gitignore` 已忽略 `.logs/` 和 `*.log`。

## 运行和日志约定

当前本地后端日志路径：

```text
.logs/backend-run.out.log
.logs/backend-run.err.log
```

如果手动重启后端，建议使用 `.logs/` 路径：

```powershell
$out = "D:\dev\my_proj\go\vid-lens\.logs\backend-run.out.log"
$err = "D:\dev\my_proj\go\vid-lens\.logs\backend-run.err.log"
Start-Process -FilePath "D:\Go\bin\go.exe" `
  -ArgumentList @("run", "./cmd/server") `
  -WorkingDirectory "D:\dev\my_proj\go\vid-lens" `
  -RedirectStandardOutput $out `
  -RedirectStandardError $err `
  -WindowStyle Hidden
```

## 记录 001：长视频提取文字失败或明显过短

### 背景

用户上传或通过 B 站链接创建了一个约 15 分钟的视频任务。第一次点击“提取文字”失败；后续修复后再次重试，任务虽然显示完成，但转写内容只有几百字，明显不符合视频时长。

### 现象

第一次失败时，数据库中任务状态为失败：

```text
video_tasks.id = 2
status = 4
error_msg = MiMo ASR 音频 base64 超过 10MB，请压缩音频或按片段转录
file_size = 27622604
```

第二次修复后重试，任务状态变为完成，但转写过短：

```text
video_tasks.id = 2
status = 3
video_transcriptions.content length = 711
```

一个约 15 分钟的视频只有 711 个字符，说明“完成”不等于“转写质量正确”。

### 排查证据

相关代码路径：

```text
internal/ai/mimo.go
internal/pkg/ffmpeg/ffmpeg.go
internal/mq/consumer.go
internal/ai/strategy.go
internal/ai/siliconflow.go
web/src/App.vue
web/src/taskDetailPolicy.js
```

第一次失败的直接原因在 `internal/ai/mimo.go`：

```go
const mimoMaxAudioDataBytes = 10 * 1024 * 1024
```

当前 MiMo ASR 实现会把音频读入内存，转成 base64，再放进 OpenAI-compatible `/chat/completions` 请求。base64 会让体积膨胀约 33%，所以较长音频很容易超过单次请求限制。

第一次修复后，FFmpeg 已经将音频压缩为低码率，但仍然出现转写过短。日志中没有出现切片转写日志：

```text
音频过大，切片转写
```

这说明当时的策略只按“音频文件大小”判断是否切片。15 分钟视频压缩后可能小于阈值，但 MiMo 对单次长音频请求仍可能只返回前面一小段内容。

### 根因

根因分两层：

1. **单次请求体积限制**
   整段音频转 base64 后超过 MiMo ASR 的单次请求限制，导致请求前被后端拒绝。

2. **单次长音频识别质量限制**
   即使压缩后体积不大，15 分钟音频作为一个请求发给 ASR，也可能只返回部分内容。长音频不能只按文件大小判断，应该按时长切片。

### 修复方案

已提交的两个关键修复：

```text
8518424 fix long audio transcription pipeline
77c60d1 fix long audio asr chunking strategy
```

#### 1. FFmpeg 提取音频改为 ASR 友好参数

旧方案偏高质量 MP3：

```text
-acodec libmp3lame -q:a 2
```

新方案面向语音识别：

```text
-ac 1 -ar 16000 -acodec libmp3lame -b:a 32k
```

含义：

```text
-ac 1      单声道
-ar 16000  16k 采样率
-b:a 32k   32kbps 码率
```

这样能明显降低音频大小，减少 ASR 请求体积，同时保留语音识别所需信息。

#### 2. 音频统一按 300 秒切片

最终策略不是“超过大小才切片”，而是：

```text
提取音频 -> 按 300 秒切片 -> 每段调用 ASR -> 合并文本
```

对 15 分钟视频，大致会变成：

```text
chunk_000.mp3  0-5 分钟
chunk_001.mp3  5-10 分钟
chunk_002.mp3  10-15 分钟
```

相关实现：

```text
internal/pkg/ffmpeg/ffmpeg.go
internal/mq/consumer.go
internal/ai/mimo.go
internal/ai/siliconflow.go
```

#### 3. AI 策略接口支持分片 ASR

`ai.Strategy` 增加：

```go
TranscribeChunks(ctx context.Context, audioPaths []string) (string, error)
```

MiMo 和 SiliconFlow 都实现了逐段转写与文本合并。

#### 4. AI 总结复用已有转写

修复前：

```text
AI 总结 -> 重新下载视频 -> FFmpeg -> ASR -> LLM
```

修复后：

```text
如果已有 video_transcriptions.content：
  直接读取转写 -> LLM 总结
否则：
  先 FFmpeg + 分片 ASR -> 保存转写 -> LLM 总结
```

这样避免重复调用 ASR，降低成本和失败率。

#### 5. 前端显示失败原因

失败任务展开后显示 `error_msg`，不再只显示“失败”。这样用户能区分：

```text
FFmpeg 失败
ASR 第 N 段失败
模型返回空结果
消息投递失败
```

### 测试与验证

已添加或更新测试：

```text
internal/pkg/ffmpeg/ffmpeg_test.go
internal/ai/mimo_test.go
internal/mq/consumer_test.go
web/test/taskDetailPolicy.test.mjs
```

验证命令：

```powershell
go test ./...
cd web
npm test
npm run build
```

已验证通过。

### 面试可讲版本

可以这样讲：

> 这个项目里我遇到过一个真实问题：短视频能正常转写，但 15 分钟左右的视频会失败，或者显示完成但只有几百字。最开始我以为只是 MiMo 的请求体大小限制，后来通过数据库里的 `error_msg`、转写文本长度和后端日志确认，问题其实分两层：一是 base64 后超过单次请求体限制，二是长音频即使压缩后体积不大，单次 ASR 也可能只返回部分内容。
>
> 我的修复思路是把问题收敛到后端，而不是让前端限制用户。先用 FFmpeg 把音频转成更适合 ASR 的低码率单声道 16k 音频，然后统一按 300 秒切片，每段分别调用 ASR，最后合并文本。这样既规避了单次请求大小限制，也避免长音频单次识别不完整的问题。
>
> 同时我把 AI 总结链路改成优先复用已有转写。如果用户已经做过“提取文字”，再点“AI 总结”时不应该重新跑 FFmpeg 和 ASR，而是直接读取数据库里的转写内容喂给 LLM。这样减少重复调用、降低成本，也让系统行为更可解释。

### 面试官可能追问

**Q：为什么不是直接把 10MB 限制调大？**

A：调大只是把失败往后推，而且服务端仍可能有请求体或时长限制。base64 会膨胀体积，长音频还有识别完整性问题。分片是更稳的工程方案。

**Q：为什么用 300 秒一段？**

A：这是一个保守的工程折中。片段太长可能仍然撞模型限制或识别不完整，片段太短会增加请求次数和成本。5 分钟一段适合作为当前 demo 项目的默认值，后续可以做成配置项。

**Q：分片会不会丢上下文？**

A：ASR 阶段主要目标是语音转文字，分片边界可能会切断一句话，这是风险。当前实现先解决“可用性”和“完整性”问题。后续可以加入重叠窗口，例如每段多保留 5 到 10 秒，再在文本层做去重。

**Q：为什么 AI 总结要复用已有转写？**

A：因为 ASR 是成本高、耗时长、容易受音频限制影响的步骤。如果转写已经存在，重复 ASR 会浪费资源，还会引入新的失败点。复用转写能让“提取文字”和“AI 总结”两个功能边界更清晰。

**Q：这个修复体现了什么后端能力？**

A：体现了异步任务排查、外部模型限制适配、FFmpeg 工具链处理、任务状态管理、结果持久化、幂等和成本控制。不是简单调接口，而是把不稳定的外部能力包进后端可控流程里。

### 这次不要夸大的点

- 当前系统不是生产级视频平台。
- 当前分片是串行 ASR，不是并行处理。
- 当前没有做切片重叠和文本去重。
- 当前任务状态仍然比较粗，转写和总结共用一个 `video_tasks.status`，后续可以拆成更细的阶段状态。

### 后续演进

- 将 `DefaultAudioSegmentSeconds` 做成配置项。
- 分片时加入重叠窗口，减少断句丢词。
- 保存分片级转写结果，便于失败重试和定位是哪一段失败。
- 将任务状态拆细，例如 `transcribing`、`summarizing`、`failed_transcribe`、`failed_summary`。
- 增加前端进度展示，例如已完成 N/M 个分片。

## 记录 002：长视频任务完成后日志证据不足

### 背景

在修复长视频 ASR 分片后，用户重新运行约 15 分钟视频任务。数据库显示任务完成，转写文本和 AI 总结都已写入，但当前 `.logs/backend-run.*.log` 中没有完整记录这次任务的 Kafka 处理过程。

### 现象

数据库证据：

```text
video_tasks.id = 2
status = 3
error_msg = ""
video_transcriptions.content length = 5371
ai_summaries.content length = 1541
```

转写文本尾部出现自然收束语，例如：

```text
具体的流程等到下节课再讲吧。... 以上。
```

这说明任务结果大概率是完整的，但日志侧无法回答这些问题：

```text
这次到底切了几段？
每段 ASR 是否都返回了文本？
每段分别返回多少字符？
最终合并后的转写长度是多少？
```

### 排查证据

当前后端日志文件启动时间晚于任务完成时间：

```text
任务完成时间：2026-06-06 13:48:49
backend-run 日志启动时间：2026-06-06 13:49:23
```

因此当前日志只能证明后端后来启动成功，不能证明任务 2 的完整处理链路。旧日志中也只保留了上传和请求记录，缺少 chunk 级处理结果。

### 根因

根因不是 ASR 业务失败，而是**可观测性不足**：

1. 任务级日志不完整，服务重启后难以回看上一个任务的完整链路。
2. 原有转写日志只记录“开始切片转写”，没有记录切片数量、每段结果长度和最终合并长度。
3. `TranscribeChunks` 在 AI provider 内部循环，消费者层拿不到每段 ASR 返回长度，也就无法输出带 `taskID` 的 chunk 级日志。

### 修复方案

已将 chunk 级日志放到 Kafka 消费者层，因为消费者层同时知道：

```text
taskID
音频路径
切片数量
每段 chunk 路径
每段 ASR 返回文本长度
最终合并文本长度
```

新的日志形态类似：

```text
[Kafka] 音频切片转写开始: taskID=2, path=..., segmentSeconds=300
[Kafka] 音频切片转写已切片: taskID=2, chunks=3
[Kafka] 音频切片转写片段完成: taskID=2, chunk=1/3, path=..., chars=...
[Kafka] 音频切片转写片段完成: taskID=2, chunk=2/3, path=..., chars=...
[Kafka] 音频切片转写片段完成: taskID=2, chunk=3/3, path=..., chars=...
[Kafka] 音频切片转写完成: taskID=2, chunks=3, transcriptChars=...
```

这次没有改变分片策略和前端展示，只增强日志证据。

### 测试与验证

新增/更新测试：

```text
internal/mq/consumer_test.go
```

关键测试点：

```text
TestTranscribeAudioAlwaysSplitsAudioBeforeASR
TestTranscribeAudioLogsChunkMetrics
```

验证目标：

1. 音频一定先经过 FFmpeg 切片，再按 chunk 调 ASR。
2. 日志中包含 `taskID`、`chunks`、`chunk=N/M`、chunk 路径、每段字符数和最终合并字符数。

### 面试可讲版本

可以这样讲：

> 我后面又发现一个问题：任务虽然成功了，数据库也有转写和总结，但日志不能证明这个 15 分钟视频到底切了几段、每段有没有识别成功。这个问题不是功能 bug，而是可观测性不足。  
>
> 我的处理方式是把 chunk 级日志放在 Kafka 消费者层，而不是 AI provider 层。因为 provider 只知道音频路径，不知道业务任务 ID；消费者层知道 `taskID`、切片数量、每段路径和最终任务上下文。这样每个异步任务都能通过日志串起来，后续排查时不用只看数据库结果猜测。  
>
> 这类改动体现的是后端排障思路：不只是让功能跑通，还要让系统在出问题时能解释自己发生了什么。

### 面试官可能追问

**Q：为什么日志放在消费者层，而不是 MiMoStrategy 里？**

A：MiMoStrategy 是 provider 适配层，只负责“给一个音频，返回文本”。如果在这里打日志，只能记录音频路径，缺少 `taskID` 和业务上下文。消费者层是任务编排者，能把 chunk、任务状态和数据库结果关联起来。

**Q：为什么不直接保存每个 chunk 的结果到数据库？**

A：保存 chunk 级结果是更进一步的演进，适合支持断点重试和进度展示。当前这次先补日志，因为目标是快速解决排查证据不足，改动更小，不改变数据模型。

**Q：日志里记录 path 会不会有风险？**

A：本地 demo 项目里主要用于排障。生产环境需要注意不要记录用户敏感路径、token、URL 签名等信息，可以只记录对象名、chunk 序号、文件大小和 trace id。

### 这次不要夸大的点

- 当前只是日志增强，不是完整可观测性体系。
- 当前没有引入 trace id、指标系统或日志聚合。
- 当前没有保存 chunk 级转写结果，重试仍然是任务级重试。

### 后续演进

- 为每个任务生成 `traceID`，贯穿 HTTP 请求、Kafka 消息、消费者日志。
- 保存 chunk 级 ASR 结果，支持失败后只重试失败片段。
- 增加 Prometheus 指标，例如 ASR 耗时、chunk 数、失败率、转写字符数。
- 前端展示分片处理进度，例如 `2/3`。

## 记录 003：公开部署不能消耗服务端 AI Key，RAG 接入需要拆分用户配置

### 背景

项目准备公开部署后，如果后端继续使用服务端自己的 MiMo 或 LLM Key，陌生用户一调用 ASR、总结或问答，就会消耗服务端 token。与此同时，RAG 问答需要 Embedding 模型和向量数据库，不能把“AI 总结”简单当成知识库。

### 现象

原有实现只有启动时全局 AI strategy：

```text
config.ai.provider
config.ai.mimo_api_key
config.ai.siliconflow_api_key
```

这适合本地开发，但不适合公开部署。用户未配置自己的 Key 时，系统不应该 fallback 到服务端 Key。

### 排查证据

相关代码路径：

```text
internal/model/ai_profile.go
internal/pkg/secret/crypto.go
internal/service/ai_profile.go
internal/ai/chat.go
internal/ai/embedding.go
internal/ai/factory.go
internal/mq/consumer.go
internal/service/rag_index.go
internal/service/chat.go
internal/service/chat_memory.go
internal/vector/milvus.go
cmd/server/main.go
docker-compose.yml
```

实现中还遇到一个 SDK 兼容问题：新版 Milvus Go SDK 会引入较重的 `milvus/pkg/v2` 服务端依赖，在当前 Windows 编译环境下风险较高。因此最终使用：

```text
github.com/milvus-io/milvus-sdk-go/v2 v2.4.2
github.com/milvus-io/milvus-proto/go-api/v2 v2.4.10-0.20240819025435-512e3b98866a
```

向量数据库服务端在 `docker-compose.yml` 中使用：

```text
milvusdb/milvus:v2.4.15
quay.io/coreos/etcd:v3.5.18
```

本机检查过已有镜像，当前只有 MinIO 镜像，没有 Milvus/etcd 镜像，因此没有直接启动或拉取，避免重复占用磁盘。

### 根因

根因是 AI 能力被混在了服务端全局配置里：

1. ASR、LLM、Embedding 实际上可以来自三个不同 provider，不能共享一个 baseURL 和 Key。
2. 公开部署时服务端不应该兜底消耗自己的 Key。
3. RAG 的知识源应该是 ASR 转写全文，而不是 AI 总结。
4. Milvus collection 的向量维度固定，必须校验用户配置的 embedding 维度。

### 修复方案

本次后端改造成三部分：

1. **用户自带 AI 配置（BYOK）**
   新增 `user_ai_profiles`，按用户保存 LLM、ASR、Embedding 三套 provider/baseURL/apiKey/model 配置。API Key 使用 AES-GCM 加密入库，接口返回时只返回脱敏值。

2. **按任务用户动态调用模型**
   Kafka consumer 处理 ASR 和总结时，根据 `task.UserID` 读取用户默认 profile。配置 resolver 后，如果用户没有 profile，任务明确失败为“请先配置 AI 服务”，不再 fallback 到服务端全局 Key。

3. **基于视频转写文本的 RAG**
   后端将 `video_transcriptions.content` 切成 chunk，调用用户配置的 Embedding 生成向量，写入 Milvus；聊天时将用户问题向量化，在 Milvus 中按 `user_id + task_id + embedding_model` 过滤检索 Top-K，再结合 Redis 最近会话上下文调用用户配置的 LLM。

同时新增 Redis 最近记忆：

```text
vidlens:chat:session:{sessionID}:recent
```

MySQL 保存完整会话消息，Redis 只缓存最近 N 轮，用于减少每次问答读取和 prompt 膨胀。

### 测试与验证

已新增或更新测试：

```text
internal/pkg/secret/crypto_test.go
internal/repository/ai_profile_test.go
internal/service/ai_profile_test.go
internal/ai/chat_embedding_test.go
internal/mq/consumer_test.go
internal/service/rag_index_test.go
internal/service/chat_test.go
internal/repository/chat_test.go
internal/repository/video_chunk_test.go
```

验证命令：

```powershell
go test ./...
docker compose config
go run ./cmd/server
Invoke-RestMethod http://localhost:8080/health
```

当前验证结果：

```text
go test ./...          通过
docker compose config  通过
Milvus 未启动时后端 5 秒超时降级，/health 返回 {"service":"VidLens","status":"ok"}
```

### 面试可讲版本

可以这样讲：

> 项目公开部署前，我发现不能继续用服务端自己的模型 Key。否则任何注册用户都能消耗我的 ASR、LLM 和 Embedding token。所以我把 AI 调用从“启动时全局配置”改成“用户级 BYOK 配置”：用户自己的 ASR、LLM、Embedding endpoint 和 key 加密保存在数据库里，后端只在调用模型前解密使用，响应和日志都不暴露明文 Key。
>
> RAG 部分我没有把 AI 总结当知识库，而是把 ASR 转写全文作为知识源。后端先把转写文本切 chunk，调用用户配置的 embedding 模型生成向量，写入 Milvus。用户对某个视频提问时，系统按 `user_id + task_id + embedding_model` 做向量检索，拿到相关片段后再结合 Redis 缓存的最近对话调用 LLM，最后返回答案和引用片段。
>
> 这里也处理了一个真实工程问题：Milvus 新版 Go SDK 在我的 Windows 编译环境下会带入较重的服务端依赖，所以我没有硬上新版，而是退到和 Milvus 2.4 服务端兼容的旧 SDK，并固定 proto 版本，保证项目能稳定编译和运行。
>
> 另外我在启动验证时发现，如果 Milvus 本地没有启动，后端会卡在向量库连接阶段。这个问题不能靠“启动 Milvus”掩盖，因为公开部署或本地开发中中间件缺失是常见情况。所以我给 Milvus 初始化加了 5 秒超时：连上就启用 RAG，连不上则后端继续提供登录、上传、转写等基础功能，并明确提示 RAG 暂不可用。

### 面试官可能追问

**Q：为什么 ASR、LLM、Embedding 要分开配置？**

A：因为它们不一定来自同一个服务。比如用户自己的 ASR 可以用 MiMo，LLM 可以用另一个 OpenAI-compatible 服务，Embedding 又可以走单独的 `/v1/embeddings` endpoint。强行共用 baseURL 和 Key 会限制扩展，也容易请求到错误接口。

**Q：用户 API Key 为什么不直接明文保存？**

A：API Key 属于敏感信息。当前用 AES-GCM 加密入库，密钥来自 `security.api_key_secret`。后端只在调用模型前解密，接口响应只返回脱敏值，日志也不能打印 Authorization 或明文 Key。

**Q：为什么 RAG 用转写文本，不用 AI 总结？**

A：总结是压缩后的二次生成结果，可能丢细节，也可能带模型表达。RAG 的检索源应该尽量贴近原始知识，所以用 ASR 转写全文切 chunk。总结可以作为展示结果，但不适合作为唯一知识库。

**Q：为什么 Milvus 过滤要带 user_id 和 task_id？**

A：这是数据隔离。公开部署后不同用户的视频 chunk 都在同一个 collection 中，如果检索时不加 `user_id` 和 `task_id`，就可能召回别人的视频内容。

### 这次不要夸大的点

- 当前是单视频 RAG，不是跨视频知识库。
- 当前是向量召回，没有 BM25 混合检索，也没有 rerank。
- 当前没有 SSE 流式输出。
- 当前自动索引失败不会阻断 ASR 主流程，用户仍可通过手动索引接口重试。
- 当前 Milvus 已写入 compose，并已在本地通过 `milvusdb/milvus:v2.4.15` 和 `quay.io/coreos/etcd:v3.5.18` 启动验证。

### 后续演进

- 将 RAG 索引构建改成独立 Kafka topic，避免长文本同步索引阻塞请求。
- 保存索引状态，例如 `not_indexed/indexing/indexed/failed`。
- 支持多 embedding 维度时按维度拆 collection。
- 增加 AI 调用日志，记录用户维度的请求次数和失败原因，但不记录明文 Key。
- 增加流式问答和引用片段前端交互。

## 记录 004：RAG 问答重复要求构建索引，且提问时报 MySQL JSON 错误

### 背景

接入视频 RAG 问答后，前端在“问问视频”页签进入时会先调用：

```text
GET /api/v1/media/task/:id/rag-index
```

用于判断当前视频是否已经完成索引。如果未索引，前端提示用户点击“构建视频索引”；如果已索引，则直接进入聊天界面。

### 现象

本地对 task 2 构建索引后，日志里 `POST /api/v1/media/task/2/rag-index` 返回 200，数据库 `video_chunks` 里也已经有 8 个 chunk。但前端从详情页切回“问问视频”后，仍然提示需要构建索引。

继续提问时，浏览器弹窗报错：

```text
Error 3140 (22032): Invalid JSON text: "The document is empty."
at position 0 in value for column 'chat_messages.retrieval_snapshot'.
```

### 排查证据

日志中能看到索引构建接口成功：

```text
POST /api/v1/media/task/2/rag-index 200
```

数据库中也能查到索引 chunk：

```sql
SELECT task_id, embedding_model, COUNT(*) AS chunks
FROM video_chunks
GROUP BY task_id, embedding_model;
```

结果显示：

```text
task_id=2, embedding_model=text-embedding-3-small, chunks=8
```

但是手动请求状态接口时发现返回的是 HTML，而不是 JSON：

```text
GET /api/v1/media/task/2/rag-index
Content-Type: text/html; charset=utf-8
```

说明后端没有注册 GET 状态路由，请求被 Gin 的 `NoRoute` 静态资源兜底返回了 `index.html`。前端拿不到 `indexed/chunks`，所以每次都认为没有构建。

聊天报错的根因则在数据库字段：`chat_messages.retrieval_snapshot` 是 MySQL JSON 列，原模型用 `string` 表示。保存用户消息时没有检索快照，GORM 会把空字符串写入 JSON 列，而 MySQL JSON 不接受空字符串，只接受合法 JSON 或 NULL。

### 根因

1. RAG 只有构建接口，没有状态查询接口：

```text
POST /api/v1/media/task/:id/rag-index
```

但前端已经调用了：

```text
GET /api/v1/media/task/:id/rag-index
```

2. `ChatMessage.RetrievalSnapshot` 用 `string` 表达 JSON 可空字段，无法区分“没有快照”和“空字符串”。用户消息应该写 NULL，助手消息才写 citations JSON。

### 修复方案

后端新增 RAG 状态查询：

```text
GET /api/v1/media/task/:id/rag-index
```

实现逻辑是按当前登录用户、任务 ID、用户默认 embedding 模型查询 `video_chunks`：

```text
user_id + task_id + embedding_model
```

如果 chunk 数大于 0，则返回：

```json
{
  "indexed": true,
  "chunks": 8,
  "embedding_model": "text-embedding-3-small"
}
```

同时将 `ChatMessage.RetrievalSnapshot` 从 `string` 改为 `*string`。用户消息不设置该字段，数据库写 NULL；助手消息将 citations `json.Marshal` 后写入该字段。

### 测试与验证

新增回归测试：

```text
TestRAGIndexServiceGetTaskIndexStatusUsesStoredChunks
TestChatServiceAskRetrievesChunksAndStoresMessages
```

验证命令：

```powershell
go test ./...
npm test --prefix web
npm run build --prefix web
```

实际接口验证：

```text
GET http://127.0.0.1:5173/api/v1/media/task/2/rag-index
```

返回：

```json
{"indexed":true,"chunks":8,"embedding_model":"text-embedding-3-small"}
```

再调用视频问答接口，返回 200，答案约 505 字，引用片段数为 5。

数据库验证：

```sql
SELECT role, JSON_TYPE(retrieval_snapshot), JSON_LENGTH(retrieval_snapshot)
FROM chat_messages
ORDER BY id DESC;
```

结果显示用户消息为 NULL，助手消息为 JSON ARRAY，长度为 5。

### 面试可讲版本

可以这样讲：

> 我在联调 RAG 问答时遇到一个典型的前后端契约问题：索引构建已经成功，数据库也有 chunk，但前端每次进入问答页仍提示重新构建。最后发现不是索引失败，而是前端调用了 GET 状态接口，后端没有实现这个路由，请求被 SPA 静态资源兜底返回 HTML。这个问题如果只看 HTTP 200 很容易误判，所以我进一步检查了 Content-Type，发现返回的是 text/html 而不是 application/json。
>
> 修复时我没有让前端硬记本地状态，而是在后端补了真正的索引状态接口，从 `video_chunks` 按 `user_id + task_id + embedding_model` 查询，这样刷新页面或重新进入详情都能得到真实状态。
>
> 另一个问题是 MySQL JSON 列不能写空字符串。用户消息没有检索快照，应该是 NULL；助手消息才保存本轮 citations。我把模型字段从 string 改成可空指针，并补测试确认用户消息写 NULL、助手消息写合法 JSON ARRAY。

### 面试官可能追问

**Q：为什么不让前端自己记“已经构建过”？**

A：前端状态不可靠，刷新页面、换浏览器、重新登录都会丢失。索引是否存在属于服务端事实，应该以后端持久化数据为准。

**Q：为什么状态查询要带 embedding_model？**

A：同一个视频可能用不同 embedding 模型或不同维度重建索引。问答时检索使用的是当前用户默认 embedding 模型，所以状态也应该按当前模型判断。

**Q：为什么 MySQL JSON 列不能写空字符串？**

A：JSON 列要求值是合法 JSON 文档。空字符串不是合法 JSON；NULL 表示没有值，`[]` 或 `{}` 才是合法 JSON。用户消息没有检索快照，应写 NULL。

### 这次不要夸大的点

- 当前状态接口只判断 MySQL chunk 是否存在，没有检查 Milvus 中对应向量是否仍然存在。
- 当前索引状态还不是完整状态机，没有 `indexing/failed` 状态。
- 当前聊天不是流式输出。

### 后续演进

- 给 RAG 索引增加状态表，记录 `not_indexed/indexing/indexed/failed` 和失败原因。
- 状态接口同时校验 Milvus collection 是否可用，必要时提示重新构建。
- 将同步构建索引改为 Kafka 异步任务，避免前端长时间等待。

## 记录 005：服务器部署后 Milvus 端口已监听但 RAG 初始化失败

### 背景

新版后端接入了 Milvus，用于视频转写文本的 RAG 索引和问答。服务器部署时，MySQL、Redis、MinIO、Redpanda、后端服务都已启动，`/health` 也返回正常，但后端启动日志提示 Milvus 连接失败，RAG 暂不可用。

### 现象

服务器上能看到 Milvus 容器在运行，`127.0.0.1:19530` 端口也已经监听。但后端启动时先后出现：

```text
Milvus 连接失败，RAG 索引和视频问答暂不可用: context deadline exceeded
Milvus 连接失败，RAG 索引和视频问答暂不可用: service unavailable: internal: Milvus Proxy is not ready yet
```

这说明“端口通”不等于“Milvus 内部组件已经可用”。

### 排查证据

继续查看 Milvus 容器日志，发现反复出现 MinIO 认证失败：

```text
failed to check blob bucket exist
The Access Key Id you provided does not exist in our records.
```

再进入 Milvus 容器检查配置文件，发现 Milvus 2.4.15 的 `milvus.yaml` 中写明读取的环境变量是：

```text
MINIO_ACCESS_KEY_ID
MINIO_SECRET_ACCESS_KEY
```

而 compose 中原来写的是：

```text
MINIO_ACCESS_KEY
MINIO_SECRET_KEY
```

因此 Milvus 没有读到我们给它配置的 MinIO 凭证，而是继续使用默认的 `minioadmin/minioadmin` 去访问服务器已有 MinIO，导致对象存储认证失败。Milvus Standalone 虽然开了 19530 端口，但内部 DataCoord、QueryCoord、Proxy 等组件无法完整就绪。

### 根因

Milvus 2.4.15 的 MinIO 环境变量名称和当前 compose 中使用的变量名称不一致。错误变量名不会报配置解析错误，而是被忽略，最终表现为 Milvus 运行中访问 MinIO 失败。

### 修复方案

将 Milvus 服务的 MinIO 环境变量改为 Milvus 实际读取的名称：

```yaml
environment:
  ETCD_ENDPOINTS: milvus-etcd:2379
  MINIO_ADDRESS: minio:9000
  MINIO_ACCESS_KEY_ID: minioadmin
  MINIO_SECRET_ACCESS_KEY: minioadmin
```

服务器部署版本也同步修正为同样的变量名，然后只重建 Milvus 容器：

```bash
docker compose up -d --no-deps --force-recreate milvus
```

等待 Milvus 日志稳定后，再重启后端，让后端重新初始化 Milvus 客户端。

### 测试与验证

修复后验证了以下几项：

```text
Milvus 容器 restart=0 status=running
127.0.0.1:19530 正常监听
Milvus 日志不再出现 Access Key 认证错误
后端 /health 返回 {"service":"VidLens","status":"ok"}
后端启动日志出现：Milvus 向量库连接成功
```

公开访问也验证了：

```text
http://vidlens.wanjune.qzz.io/       -> 200 text/html
http://vidlens.wanjune.qzz.io/health -> 200 application/json
```

### 面试可讲版本

可以这样讲：

> 我在服务器部署 RAG 功能时遇到过一个典型的中间件“假启动”问题：Milvus 容器是 running，19530 端口也监听了，但后端仍然提示 Milvus 不可用。  
> 我没有只看容器状态，而是继续看 Milvus 内部日志，发现它访问 MinIO 时认证失败。再进容器看 Milvus 2.4.15 的配置文件，确认它读取的是 `MINIO_ACCESS_KEY_ID` 和 `MINIO_SECRET_ACCESS_KEY`，而我的 compose 写成了另一组变量名，导致配置被忽略。  
> 修复后我只重建 Milvus 容器，没有动 MySQL、Redis、MinIO、Redpanda 的数据容器。等 Milvus 日志稳定后重启后端，最终启动日志明确显示 Milvus 连接成功。这次排查让我意识到部署验证不能只看容器 running 和端口监听，还要验证依赖组件是否真正 ready。

### 面试官可能追问

**Q：为什么端口监听了，服务还可能不可用？**

A：端口监听只能说明进程对外提供了 socket，不代表内部依赖和组件状态都 ready。Milvus Standalone 内部还包含 Proxy、DataCoord、QueryCoord、IndexNode 等组件，并依赖 etcd 和对象存储。任何一个关键依赖异常，都可能让客户端请求失败。

**Q：为什么没有直接清空 Milvus 数据目录重来？**

A：清数据是最后手段。这里日志已经明确指向 MinIO 认证失败，根因是配置变量名错误。直接清数据会掩盖问题，而且可能误删已有索引数据。正确做法是先修配置，再重建受影响容器。

**Q：为什么只重建 Milvus，不重建全部中间件？**

A：故障边界已经定位在 Milvus 读取 MinIO 凭证这层。MySQL、Redis、MinIO、Redpanda 都是正常运行的，重建全部容器会扩大风险，也可能影响已有数据。

### 这次不要夸大的点

- 这次只是单机 Milvus Standalone 部署，不是生产级 Milvus 集群运维。
- 当前没有给 Milvus 增加独立 healthcheck，只是通过容器状态、日志和后端连接结果验证。
- 当前公开部署仍是演示性质，服务器磁盘空间有限，不适合大量用户上传大视频。

### 后续演进

- 给 Milvus 加 `healthcheck`，后端启动或部署脚本等待 healthcheck ready 后再重启应用。
- 部署脚本中增加中间件 readiness 检查，避免只检查端口。
- 给服务器补充磁盘监控和日志轮转，防止视频文件、Docker 镜像、Milvus 数据持续增长。

## 记录 006：服务器上传 B 站链接失败，yt-dlp 返回 412

### 背景

新版部署后，公开站点支持用户粘贴视频 URL，由后端调用 `yt-dlp` 下载到临时文件，再上传到 MinIO 并创建任务。用户 `jhwan` 在服务器环境测试 B 站链接上传时，前端提示下载失败。

### 现象

前端错误信息包含两段关键内容：

```text
WARNING: ffmpeg-location ffmpeg does not exist! Continuing without ffmpeg
ERROR: [BiliBili] ... Unable to download webpage: HTTP Error 412: Precondition Failed
```

后端访问日志只显示：

```text
POST /api/v1/media/upload-url -> 500
```

最初日志里没有 `yt-dlp` stderr，无法直接判断是工具路径、B 站风控、链接格式还是下载格式问题。

### 排查证据

1. 服务器实际存在工具：

```text
/usr/bin/ffmpeg
/usr/local/bin/yt-dlp
```

2. 服务器 `config.yaml` 原来写的是：

```yaml
tools:
  ffmpeg_path: "ffmpeg"
  ytdlp_path: "yt-dlp"
```

在 systemd 服务环境里，进程 `PATH` 不一定包含这些路径，所以 `yt-dlp` 收到 `--ffmpeg-location ffmpeg` 后提示找不到 ffmpeg。

3. 改成绝对路径并重启后，重新直接在服务器执行：

```bash
/usr/local/bin/yt-dlp --simulate \
  --ffmpeg-location /usr/bin/ffmpeg \
  --referer https://www.bilibili.com/ \
  https://www.bilibili.com/video/...
```

ffmpeg 警告消失，但 B 站仍返回：

```text
HTTP Error 412: Precondition Failed
```

4. 尝试补充 UA、Referer、Accept-Language、IPv4 后，服务器请求仍然被 B 站 412 拦截。`--impersonate chrome` 在当前服务器的 yt-dlp 环境不可用。

### 根因

这是两个问题叠在一起：

1. **服务器工具路径配置不稳**：systemd 环境下使用 `ffmpeg` / `yt-dlp` 相对命令不可靠，应使用绝对路径。
2. **B 站反爬 / 风控**：服务器公网 IP 直接请求 B 站网页时，被 B 站返回 412。这个不是 AI 配置问题，也不是 MinIO 或 Kafka 问题。

### 修复方案

1. 服务器部署配置改为绝对路径：

```yaml
tools:
  ffmpeg_path: "/usr/bin/ffmpeg"
  ytdlp_path: "/usr/local/bin/yt-dlp"
```

2. 后端 URL 上传失败时记录可排查日志：

```text
[Media] URL upload download failed: userID=... url=... err=...
```

日志里的 URL 会去掉 query 和 fragment，避免把分享链接里的敏感参数完整写入日志。

3. 新增 `tools.cookies_path` 可选配置：

```yaml
tools:
  cookies_path: "/opt/vidlens/secrets/bilibili-cookies.txt"
```

如果配置了 cookies 文件，后端会把 `--cookies <path>` 传给 `yt-dlp`。未配置时行为保持不变。

4. 对 B 站 412 做友好错误包装，明确提示：

```text
B 站返回 412，服务器请求被 B 站风控拦截。请改用本地视频上传，或在服务器配置 B 站 cookies 后重试
```

### 测试与验证

本地新增并通过了测试：

```text
internal/pkg/ytdlp: cookies_path 配置存在时会追加 --cookies 参数
internal/pkg/ytdlp: BiliBili HTTP 412 会包装成可理解错误
internal/service: URL 下载失败会记录 userID、脱敏 URL 和 yt-dlp 错误
```

服务器验证：

```text
ffmpeg 警告已消失
B 站 412 仍可复现，证明剩余问题是 B 站访问风控
```

### 面试可讲版本

可以这样讲：

> 我在公开部署后测试 B 站链接上传，发现前端只提示下载失败。第一步我先把 `yt-dlp` 的 stderr 打进后端日志，补齐可观测性。日志显示两个问题叠在一起：一是 systemd 进程里 `ffmpeg` 相对路径不稳定，`yt-dlp` 找不到 ffmpeg；二是修正为 `/usr/bin/ffmpeg` 后，B 站仍返回 412。
> 我没有把 412 误判成后端上传失败，而是直接在服务器用同样参数执行 `yt-dlp --simulate` 复现，确认这是服务器 IP 访问 B 站网页被风控。最后我做了三件事：部署配置改绝对路径；下载失败日志脱敏记录；后端支持可选 cookies 文件，并对 B 站 412 返回更明确的用户提示。

### 面试官可能追问

**Q：为什么不用相对命令 `ffmpeg`？**

A：在交互式 shell 里能找到命令，不代表 systemd 服务进程的 PATH 也一样。部署服务应该尽量使用绝对路径，减少环境差异。

**Q：为什么 B 站需要 cookies？**

A：B 站对服务端 IP、请求头、访问频率、登录状态等都有风控。某些视频网页未登录或从服务器 IP 直接访问会返回 412。cookies 可以让 yt-dlp 带上登录态，但也引入账号安全风险，所以应该作为用户/部署者显式配置，而不是项目内置。

**Q：为什么不把 cookies 写进数据库或配置到公开站点默认使用？**

A：公开部署不能默认使用服务拥有者的个人 B 站 cookies，否则陌生用户会消耗或滥用账号能力，也有隐私风险。更合理的是 BYOC，用户自己提供，或者提示改用本地视频上传。

### 这次不要夸大的点

- 加 cookies path 只是提供能力，不保证所有 B 站视频都能下载。
- 当前没有实现用户级 B 站 cookies 管理，只是部署级可选配置。
- 当前没有解决所有平台反爬，只是对 B 站 412 做了明确识别和提示。

### 后续演进

- 前端对 B 站 412 展示更友好的提示，建议用户改用本地上传。
- 后续如果要做公开视频下载能力，可以设计用户级 cookies 配置，但必须考虑加密存储、脱敏展示、删除能力和风险提示。
- 给 URL 下载增加任务化处理，避免 HTTP 请求长时间阻塞。

## 记录 007：服务器上传 YouTube 链接卡住

### 背景

公开部署后，用户在前端提交 YouTube 视频链接，页面长时间显示处理中或卡住。此前 B 站问题已经修过 ffmpeg / yt-dlp 绝对路径和 B 站 412 提示，但 YouTube 属于新的部署环境问题。

### 现象

后端日志显示：

```text
POST /api/v1/media/upload-url -> 500，耗时约 2 分钟
yt-dlp: [youtube] [Errno 101] Network is unreachable
```

任务表里没有新增视频任务，因为当前 URL 上传流程是同步的：后端先调用 `yt-dlp` 下载视频，下载成功后才上传 MinIO 并创建 `video_tasks`。下载失败时不会创建任务。

### 排查证据

1. 查看进程，确认没有长期残留的 `yt-dlp` / `ffmpeg` 子进程。
2. 查后端日志，发现错误发生在 URL 下载阶段，而不是转写、AI 总结或 RAG 阶段。
3. 在服务器上直接测试网络：

```text
curl https://www.youtube.com -> timeout
curl -x http://127.0.0.1:7890 https://www.youtube.com -> 200
```

4. 查看 systemd 环境，`vidlens.service` 只有 `TZ=Asia/Shanghai`，没有代理环境变量。
5. 用 `yt-dlp --proxy http://127.0.0.1:7890 --simulate <youtube-url>` 可以解析视频，证明服务器代理可用。

### 根因

有两个问题叠加：

1. 部署机访问 YouTube 需要走代理，但后端调用 `yt-dlp` 时没有传 `--proxy`，所以 yt-dlp 仍然直连，导致 `Network is unreachable`。
2. 即使代理打通，yt-dlp 默认可能选择 YouTube 的高码率音视频分离格式，例如 `401+251`。项目只是为了后续 ASR 和内容理解，不需要下载 4K 源视频；高码率下载和转码会让同步上传接口长时间阻塞。

### 修复方案

1. 在 `tools` 配置中新增 `proxy_url`：

```yaml
tools:
  ffmpeg_path: "/usr/bin/ffmpeg"
  ytdlp_path: "/usr/local/bin/yt-dlp"
  cookies_path: ""
  proxy_url: "http://127.0.0.1:7890"
```

2. 后端调用 yt-dlp 时，如果 `proxy_url` 非空，追加：

```text
--proxy <proxy_url>
```

3. 对 YouTube 直连失败增加明确错误提示：服务器直连 YouTube 失败，请配置 `tools.proxy_url`。
4. URL 下载格式限制为最高 720p，并优先 mp4/m4a，避免无意义的大文件下载：

```text
bv*[height<=720][ext=mp4]+ba[ext=m4a]/bv*[height<=720]+ba/best[height<=720]/best
```

### 测试与验证

新增并通过测试：

```text
internal/pkg/ytdlp: proxy_url 配置存在时会追加 --proxy 参数
internal/pkg/ytdlp: YouTube Network is unreachable 会包装成可理解错误
internal/pkg/ytdlp: URL 下载格式限制最高 720p
```

部署后验证：

```text
systemctl is-active vidlens.service -> active
GET /health -> ok
服务器 config.yaml tools.proxy_url -> http://127.0.0.1:7890
yt-dlp --proxy ... --simulate YouTube 链接 -> 可解析
POST /api/v1/media/upload-url YouTube 链接 -> 200，链接资源已入库
```

### 面试可讲版本

可以这样讲：

> 我在服务器上测试 YouTube 链接上传时，前端表现是卡住。第一步我没有先猜前端问题，而是看后端日志、进程和数据库任务表。日志显示 `yt-dlp` 在下载阶段报 `Network is unreachable`，任务表没有新任务，说明问题发生在“下载成功前”，不是后续队列或 AI 处理。
> 接着我在服务器上分别测试直连和代理访问 YouTube，发现直连超时，但 `curl -x http://127.0.0.1:7890` 可以访问。再看 systemd 环境，服务没有代理变量，而且后端调用 yt-dlp 也没有传 `--proxy`。所以我把代理做成部署配置 `tools.proxy_url`，只传给 yt-dlp，不影响数据库、MinIO、Milvus 和 AI API。
> 后来代理打通后又发现默认格式可能拉 4K 视频，上传接口依然容易等待很久。因为项目后续主要做 ASR 和内容理解，不需要高清视频源，所以我把 yt-dlp 格式限制到最高 720p，降低下载和转码成本。最后用接口级测试验证 YouTube 链接可以成功入库。

### 面试官可能追问

**Q：为什么不直接给 systemd 配 HTTP_PROXY / HTTPS_PROXY？**

A：全局代理会影响所有出站请求，包括 AI API、Embedding API、甚至某些内部服务调用。这里真正需要代理的是 `yt-dlp` 下载外部视频，所以把代理作为 `tools.proxy_url` 显式传给 yt-dlp，影响面更小，也更容易排查。

**Q：为什么要限制到 720p？**

A：项目的核心是提取音频、ASR、总结和 RAG 问答，不是视频画质处理。下载 4K 视频只会增加带宽、磁盘、转码时间和接口等待时间，对业务收益很小。720p 对保留基本视频文件已经足够，音频质量也能满足转写。

**Q：这样是不是彻底解决了 YouTube 链接？**

A：不是。它解决的是部署机需要代理但 yt-dlp 未走代理，以及默认下载过大格式的问题。YouTube 仍可能因为视频不可用、地区限制、登录限制、版权限制或 yt-dlp 解析策略变化而失败。

### 这次不要夸大的点

- 当前 URL 上传仍然是同步接口，长视频仍可能等待较久。
- `proxy_url` 是部署级配置，不是用户级代理配置。
- 720p 限制降低卡顿概率，但不是完整的 URL 下载任务化方案。

### 后续演进

- 把 URL 下载改成异步任务：先创建“下载中”任务，再由 worker 下载、入库、更新状态。
- 增加 URL 下载超时、进度日志和失败原因分类。
- 支持按平台设置下载策略，例如 YouTube 走代理、B 站走 cookies、本地直链不走代理。

## 记录 008：URL 下载从同步 HTTP 链路改为 Kafka 异步任务

### 背景

路线图阶段一要求先解决 URL 上传同步等待问题。原来的 `POST /api/v1/media/upload-url` 会在 HTTP 请求内直接调用 `yt-dlp`，下载成功后才计算 MD5、上传 MinIO、创建任务并返回。

### 现象

同步链路的问题是：

```text
校验 URL -> yt-dlp 下载 -> 计算 MD5 -> 上传 MinIO -> 创建任务 -> HTTP 返回
```

当公网下载慢、YouTube 需要代理、B 站返回 412 或视频较大时，前端会长时间等待。更关键的是，下载完成前没有 `task_id`，用户无法轮询任务详情，也看不到“正在下载”或失败原因。

### 排查证据

相关代码入口：

```text
internal/service/media.go
internal/mq/producer.go
internal/mq/consumer.go
internal/model/task.go
internal/repository/task.go
cmd/server/main.go
config.yaml
```

阶段一改造前，`UploadByURL` 在 service 层直接调用：

```text
ytdlp.DownloadVideo(...)
hashLocalFile(...)
createAssetFromLocalFile(...)
createTaskFromAsset(...)
```

这说明 URL 下载没有进入已有 Kafka 异步任务体系。

### 根因

根因是 URL 下载仍被当作普通上传接口的一部分，而不是长耗时任务。ASR 和总结已经异步化，但下载本身还在请求线程里执行，导致公网下载的不稳定性直接暴露给 HTTP 请求。

### 修复方案

本阶段保留 Kafka，不迁移 RocketMQ，采用“保留旧 `status` + 新增 `stage`”的低冲击方案。

新增任务字段：

```text
source_type
source_url
stage
retry_count
max_retries
next_retry_at
last_error_code
last_error_msg
started_at
finished_at
```

新增阶段常量：

```text
downloading
uploaded
transcribing
summarizing
indexing
```

新的 URL 上传链路：

```text
POST /api/v1/media/upload-url
  -> 校验 URL
  -> 创建 video_tasks：status=running, stage=downloading, source_type=url
  -> 投递 Kafka download_topic
  -> 立即返回 task_id/status/stage
```

新增 `video-download` Kafka topic 和 download consumer：

```text
download consumer
  -> 读取 task.source_url
  -> 调用 yt-dlp 下载
  -> 计算真实文件 MD5
  -> 按 MD5 复用 video_assets，或上传 MinIO 后创建 asset
  -> 回写 task.asset_id/file_md5/file_url/file_size
  -> status=pending, stage=uploaded
```

下载失败时，consumer 会把任务更新为：

```text
status=failed
stage=downloading
error_msg=...
last_error_msg=...
```

日志记录会使用脱敏 URL，只保留 scheme/host/path，不输出 query 和 fragment，避免泄露分享链接 token。

### 测试与验证

新增/更新测试：

```text
internal/service/media_test.go
internal/mq/consumer_test.go
```

关键测试点：

```text
URL 格式非法时仍拒绝创建任务
UploadByURL 创建 downloading 任务并投递 download 消息
UploadByURL 不再同步调用 yt-dlp，即使 yt-dlp 路径不存在也不会因此失败
download consumer 下载成功后创建 asset，并把任务更新为 pending/uploaded
同 MD5 下载结果会复用已有 asset，不重复上传
下载失败会更新 failed/downloading，并在日志中脱敏 URL
```

验证命令：

```powershell
go test ./internal/service ./internal/mq
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service ./internal/mq 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 我发现项目虽然已经把 ASR 和 AI 总结放进 Kafka，但 URL 上传仍然在 HTTP 请求里同步跑 `yt-dlp`。这会让 YouTube 或 B 站下载的不稳定性直接影响接口响应，而且下载失败前没有任务 ID，前端只能看到请求卡住或失败。
>
> 所以后来我把 URL 下载也任务化：上传链接接口只校验 URL、创建 `status=running/stage=downloading` 的任务，然后投递 Kafka `video-download` topic 并立即返回 `task_id`。真正的下载、MD5 计算、MinIO 入库和 asset 复用都由 download consumer 完成。下载成功后任务回到 `pending/uploaded`，用户可以继续点“提取文字”或“AI 总结”；下载失败则记录明确错误，前端轮询任务详情就能看到失败原因。
>
> 这个改动没有迁移 RocketMQ，因为当前 Go 项目已经有 Kafka 生产消费链路。对这个阶段来说，更有价值的是补齐业务任务边界和状态，而不是为了换技术栈重写 MQ。

### 面试官可能追问

**Q：为什么 URL 下载完成后状态不是 completed？**

A：因为 URL 下载只是“视频文件入库完成”，不是 ASR、总结或 RAG 处理完成。为了兼容现有前端和任务操作，下载成功后整体 `status` 回到 `pending`，同时 `stage=uploaded` 表示文件已就绪，可以继续提交转写或总结。

**Q：下载消息失败为什么要把任务标失败，而不是删除任务？**

A：任务已经返回给用户或即将被查询，删除会让前端无法解释发生了什么。标记失败并记录错误更利于用户重试和排查。后续阶段会继续补 `retry_count`、`next_retry_at` 和 dead 状态。

**Q：为什么先用 `stage`，而不是把 status 扩展成十几个枚举？**

A：旧代码和前端已经依赖 `pending/queued/running/completed/failed`。第一版用 `stage` 表示当前阶段，可以减少兼容成本。长期如果任务动作越来越复杂，可以进一步拆 `video_tasks` 和 `task_jobs`。

**Q：source_url 保存完整 URL 会不会有风险？**

A：worker 需要原始 URL 才能下载，所以第一版保存完整 URL。但日志里只记录脱敏后的 URL，不输出 query 和 fragment。公开部署如果要进一步收敛风险，可以保存净化 URL，并把需要的授权信息改成用户级加密配置。

### 这次不要夸大的点

- 当前只是 URL 下载任务化，不是完整下载进度百分比。
- 当前失败后先进入 failed，业务级退避重试和 dead 状态会在阶段二继续补。
- 当前没有迁移 RocketMQ，也不应该把 Kafka 说成天然具备完整业务死信机制。
- 当前没有大改前端，只保证任务详情和上传响应里有 `status/stage` 可用。

### 后续演进

- 阶段二补 Kafka 业务级重试、`retry_count`、`next_retry_at` 和 `dead` 状态。
- 阶段三把转写、总结、索引阶段统一写入 `stage`，让前端能展示更细处理阶段。
- 后续可增加 URL 下载进度日志或进度字段，但不作为第一阶段阻塞项。

## 记录 009：Kafka 消费失败补业务级重试和 dead 状态

### 背景

路线图阶段二要求在保留 Kafka 的前提下补齐业务级失败治理。Kafka 能提供 at-least-once 消费语义，但它不像 RocketMQ 那样直接开箱提供业务任务的延迟重试、最大次数和死信状态。

### 现象

旧消费逻辑主要有两类问题：

```text
业务失败 -> 更新 task failed -> commit offset
```

或早期文档中提到的：

```text
业务失败 -> 不 commit offset -> 等待 Kafka 下次重放
```

第一种不会自动重试，第二种如果长期失败会卡住同一分区后续消息。两者都不能回答：

```text
这个错误能不能重试？
已经重试了几次？
下一次什么时候重试？
超过上限后任务处于什么状态？
应该重投递到 analyze、transcribe 还是 download topic？
```

### 排查证据

相关代码入口：

```text
internal/mq/consumer.go
internal/mq/retry.go
internal/repository/task.go
internal/model/task.go
cmd/server/main.go
config.yaml
```

阶段一已经给 `video_tasks` 增加了 `retry_count/max_retries/next_retry_at` 等字段；阶段二继续补了 `last_job_type`，用于调度器知道失败任务应该重新投递到哪个 Kafka topic。

### 根因

根因是把 Kafka offset 重放和业务任务重试混在了一起。Kafka 的 offset 控制的是消息消费进度，不应该承担全部业务失败治理。外部 AI、Milvus、MinIO、yt-dlp 的错误有些可重试，有些不可重试，需要在业务层分类并持久化。

### 修复方案

新增统一任务类型：

```text
analyze
transcribe
download
```

新增重试配置：

```yaml
task_retry:
  max_retries: 3
  backoff_seconds: [60, 300, 900]
  scan_interval_seconds: 30
  batch_size: 20
```

consumer 处理失败后不再简单写 failed，而是走统一失败记录：

```text
不可重试错误
  -> status=failed
  -> next_retry_at=NULL
  -> last_error_code=non_retryable_error
  -> commit offset

可重试错误且未超过上限
  -> retry_count + 1
  -> next_retry_at = now + backoff
  -> status=failed
  -> last_job_type=download/transcribe/analyze
  -> commit offset

可重试错误但超过上限
  -> status=dead
  -> last_error_code=retry_exhausted
  -> next_retry_at=NULL
  -> commit offset
```

新增数据库调度器：

```text
每 30 秒扫描：
  status=failed
  next_retry_at <= now
  retry_count <= max_retries
  last_job_type 不为空

claim 成功后按 last_job_type 重新投递：
  download    -> video-download
  transcribe  -> video-transcribe
  analyze     -> video-analyze
```

重试调度器会先用条件更新 claim 任务，降低多实例重复扫描风险。download 任务重新进入 `running/downloading`，transcribe/analyze 任务重新进入 `queued`，后续由原 consumer 继续处理。

错误分类第一版采用字符串规则：

```text
不可重试：
  请先配置 AI 服务
  API Key 解密失败
  无权
  文件不存在
  Embedding 维度
  ASR 返回空结果
  video unavailable
  B 站 HTTP 412

可重试：
  timeout
  context deadline exceeded
  network
  connection refused/reset
  service unavailable
  HTTP 429/500/502/503/504
  MinIO/Milvus 临时错误
```

### 测试与验证

新增/更新测试：

```text
internal/mq/consumer_test.go
```

关键测试点：

```text
可重试错误会递增 retry_count，并写入 next_retry_at
不可重试错误直接 failed，不写 next_retry_at
超过 max_retries 后进入 dead
retry scheduler 只重投递到期 failed 任务
未到期任务不会被调度器改动
到期 transcribe 任务会重新投递到 transcribe topic，并清空 next_retry_at
```

验证命令：

```powershell
go test ./internal/mq
go test ./...
```

本阶段验证结果：

```text
go test ./internal/mq 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> Kafka 给我的是 at-least-once 消费能力，但业务任务不能只靠“不提交 offset”来重试。因为如果一个任务因为用户没配置 API Key 或 B 站 412 这种不可重试问题一直失败，不提交 offset 会卡住同一分区后面的消息。
>
> 所以我在业务层补了重试状态：consumer 失败后先分类错误。网络 timeout、AI 5xx、MinIO 或 Milvus 临时不可用这类错误会递增 `retry_count`，按 `[1分钟、5分钟、15分钟]` 写 `next_retry_at`；用户未配置 AI 服务、权限问题、Embedding 维度不匹配、B 站 412 这类问题直接 failed。超过最大重试次数后进入 `dead`，表示需要人工或用户重新触发。
>
> 重试不是让 Kafka consumer sleep，而是由数据库调度器扫描到期任务，再按 `last_job_type` 重新投递到对应 Kafka topic。这样失败消息会被 commit，不会无限卡住 Kafka 分区，同时前端也能从任务详情看到重试次数、下次重试时间和最终 dead 状态。

### 面试官可能追问

**Q：为什么不直接用 Kafka retry topic？**

A：可以做，但第一版用数据库调度器更容易和任务状态表结合，前端能直接查到 `retry_count/next_retry_at/dead`。Kafka consumer 长时间 sleep 等延迟重投递不合适，会占住分区；retry topic 方案后续可以演进，但不是当前最小可靠方案。

**Q：为什么失败后还 commit offset？**

A：因为失败状态已经落库，业务重试由数据库调度器接管。继续不提交 offset 会让同一条消息反复阻塞消费进度，尤其不可重试错误会拖住同分区后续任务。

**Q：怎么避免多实例重复扫描？**

A：调度器先查到期任务，再用条件更新 claim：只有仍然是 `failed` 且 `next_retry_at <= now` 的任务才能被改成 queued/running。多个实例同时扫描时，只有一个实例能成功 claim。当前第一版没有再叠 Redis 扫描锁，后续可以补全局锁减少无效扫描。

**Q：错误分类为什么先用字符串？**

A：因为现有 provider、yt-dlp、MinIO、Milvus 返回的错误类型并不统一。第一版先用字符串规则覆盖明确场景，并通过测试固定行为。后续可以把 AI client 和工具层错误包装成带 `Retryable()` 和 `ErrorCode()` 的结构化错误。

### 这次不要夸大的点

- 当前是业务层 retry/dead 状态，不是 Kafka 原生死信队列。
- 当前没有新增 dead topic。
- 当前错误分类是第一版规则，不是完整错误类型体系。
- 当前调度器用数据库条件 claim 降低重复重投递风险，还没有加 Redis 全局扫描锁。

### 后续演进

- 把错误分类升级成结构化 `RetryableError`。
- 给 retry scheduler 增加 Redis 分布式锁，减少多实例重复扫描。
- 在前端展示 `retry_count/next_retry_at/dead`，提示用户等待自动重试或手动重新触发。
- 结合阶段三，把失败时的 `stage` 写得更精确。

## 记录 010：任务状态机从单一 status 拆成 status + stage

### 背景

路线图阶段三要求细化任务状态。旧版本只有：

```text
pending -> queued -> running -> completed/failed
```

这能表达任务是否完成，但不能表达任务正在下载、转写、总结还是索引。

### 现象

前端和日志看到的经常只是：

```text
status=running
```

但实际可能处于：

```text
yt-dlp 下载
FFmpeg 提取音频
ASR 转写
RAG 索引
LLM 总结
```

如果失败，也只能看到 `failed`，很难判断应该排查 yt-dlp、FFmpeg、ASR、Milvus 还是 LLM。

### 排查证据

相关代码入口：

```text
internal/service/media.go
internal/mq/consumer.go
internal/mq/retry.go
internal/repository/task.go
internal/model/task.go
```

阶段一虽然已经新增 `stage` 字段，但主要用于 URL 下载的 `downloading/uploaded`。阶段三把现有转写、总结和索引链路也接入 `stage`。

### 根因

单一 `status` 字段既承担整体生命周期，又想表达处理阶段，语义过载。视频任务是多阶段流程，应该用：

```text
status = 整体状态
stage  = 当前处理阶段
```

来拆开表达。

### 修复方案

保留旧状态，降低前端和旧逻辑冲击：

```text
pending
queued
running
completed
failed
dead
```

新增和贯穿阶段字段：

```text
none
downloading
uploaded
transcribing
summarizing
indexing
```

本阶段补齐的流转：

```text
本地/分片上传成功：
  pending/uploaded

URL 上传创建：
  running/downloading

URL 下载成功：
  pending/uploaded

提交文字提取：
  queued/transcribing

文字提取消费中：
  running/transcribing
  running/indexing
  completed/none

提交 AI 总结：
  queued/summarizing

AI 总结消费中：
  running/transcribing   # 如果缺少转写，先 ASR
  running/indexing       # ASR 后尝试构建 RAG 索引
  running/summarizing
  completed/none
```

失败时，`recordTaskFailure` 会优先读取任务当前 `stage`，所以 analyze 链路里如果 ASR 失败，会保留 `transcribing`，不会粗暴标成 `summarizing`。

### 测试与验证

新增/更新测试：

```text
internal/service/media_test.go
internal/mq/consumer_test.go
```

关键测试点：

```text
RequestTranscribe 会把任务更新为 queued/transcribing
RequestAnalysis 会把任务更新为 queued/summarizing
consumer 失败记录会保留具体 stage
重试调度时 transcribe/download/analyze 会恢复到对应 stage
```

验证命令：

```powershell
go test ./internal/service ./internal/mq
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service ./internal/mq 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 我后来发现单个 `status` 不够表达视频处理任务。`running` 可能是在下载，也可能是在 ASR、RAG 索引或 LLM 总结；`failed` 也不知道失败在哪一步。所以我没有直接把 status 扩成很多枚举，而是保留原来的整体状态，再增加 `stage` 表示当前阶段。
>
> 这样前端和旧逻辑还能继续依赖 `pending/queued/running/completed/failed`，同时任务详情可以展示 `downloading/transcribing/summarizing/indexing`。失败时也能记录具体阶段，比如 AI 总结任务如果先跑 ASR 时失败，最终会保留 `stage=transcribing`，排查时就不会误以为是 LLM 总结失败。

### 面试官可能追问

**Q：为什么不直接拆成 job 表？**

A：长期更合理的是 `video_tasks` 表示视频资产任务，`task_jobs` 表示一次 download/transcribe/summarize/index 动作。但这会引入较大改动。当前阶段先用 `stage` 补齐可解释性，保持接口和前端兼容。

**Q：completed 后为什么 stage=none？**

A：`stage` 表示当前正在执行或最近明确的处理阶段。任务完成后没有正在执行的阶段，所以清为 `none`。如果要展示历史阶段耗时，后续应该加 stage 事件表或 job 表，而不是让当前 stage 承担历史记录。

**Q：AI 总结为什么可能出现 transcribing 阶段？**

A：因为总结需要转写文本。如果当前任务还没有 ASR 结果，analyze consumer 会先转写，再总结。所以总结任务内部可能经历 `transcribing -> indexing -> summarizing`。

### 这次不要夸大的点

- 当前还没有独立 `task_jobs` 表。
- 当前没有记录每个 stage 的耗时历史，只记录当前阶段。
- 当前 RAG 索引失败仍不阻塞转写/总结主任务，只会记录日志；阶段四会补独立索引状态表。

### 后续演进

- 阶段四增加 RAG 索引状态表，表达 indexing/indexed/failed。
- 阶段五增加 ASR chunk 表，支持分片级进度和失败恢复。
- 后续可以把 stage 流转抽成显式状态机，集中校验非法流转。

## 记录 011：RAG 索引增加独立状态表

### 背景

路线图阶段四要求修复“RAG 是否已索引”只依赖 `video_chunks` 的粗糙判断。旧逻辑是：

```text
有 chunks -> indexed=true
没有 chunks -> indexed=false
```

这能回答“有没有 chunk”，但不能回答“是否正在构建、是否构建失败、失败原因是什么、使用的是哪个 embedding dim”。

### 现象

旧状态接口只能从 chunk 数量推断：

```json
{
  "indexed": true,
  "chunks": 8,
  "embedding_model": "text-embedding-3-small"
}
```

如果 Milvus 写入失败、Embedding 超时或维度不匹配，前端和排障文档只能看到 chunks 或错误响应，缺少持久化的索引状态。

### 排查证据

相关代码入口：

```text
internal/model/rag_index.go
internal/repository/rag_index.go
internal/service/rag_index.go
internal/service/rag_index_test.go
internal/model/model.go
internal/repository/repository.go
```

阶段四前，`GetTaskIndexStatus` 只调用：

```text
repos.VideoChunk.ListByTaskID(userID, taskID, embeddingModel)
```

没有索引状态表。

### 根因

`video_chunks` 是索引内容表，不是索引任务状态表。用内容表是否为空推断状态，会丢失构建中、失败原因、embedding 维度和重建版本等信息。

### 修复方案

新增表：

```text
video_rag_indexes

id
user_id
task_id
embedding_model
embedding_dim
status           # not_indexed / indexing / indexed / failed
chunk_count
last_error
build_version
started_at
finished_at
created_at
updated_at
```

唯一索引：

```text
unique(user_id, task_id, embedding_model)
```

构建流程变为：

```text
BuildTaskIndex
  -> upsert status=indexing
  -> split transcription
  -> embedding chunks
  -> replace MySQL chunks
  -> upsert Milvus vectors
  -> upsert status=indexed, chunk_count=N
```

失败流程：

```text
构建中任一步失败
  -> upsert status=failed
  -> last_error=...
  -> chunk_count=0
```

状态接口优先读取 `video_rag_indexes`。只有没有状态行时，才 fallback 到旧的 `video_chunks` 判断，兼容历史数据。

响应现在包含：

```json
{
  "task_id": 2,
  "status": "indexed",
  "indexed": true,
  "chunks": 8,
  "embedding_model": "text-embedding-3-small",
  "last_error": ""
}
```

### 测试与验证

新增/更新测试：

```text
internal/service/rag_index_test.go
```

关键测试点：

```text
BuildTaskIndex 成功后创建 indexed 状态行
状态行记录 chunk_count 和 embedding_dim
Milvus/vector store 写入失败时记录 failed 和 last_error
GetTaskIndexStatus 优先读取 video_rag_indexes
即使存在旧 chunks，failed 状态也不会被误判为 indexed
用户 A 的索引状态不会泄露给用户 B
```

验证命令：

```powershell
go test ./internal/service -run RAGIndex
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service -run RAGIndex 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 我第一版 RAG 状态是通过 `video_chunks` 是否存在来判断 indexed，这只能说明 MySQL 里有没有 chunk，不能说明 Milvus 向量是否写入成功，也不能表达 indexing、failed 和失败原因。
>
> 所以后来我加了 `video_rag_indexes` 状态表，按 `user_id + task_id + embedding_model` 唯一记录一次索引状态。构建开始写 `indexing`，成功写 `indexed/chunk_count`，任何一步失败都写 `failed/last_error`。状态接口优先查这个表，没有状态行时才兼容旧 chunks 判断。
>
> 这样刷新页面或换浏览器后，前端不需要自己记“是否构建过索引”，而是从服务端拿真实状态；排障时也能知道索引失败是 embedding、MySQL chunk、Milvus 写入还是维度配置问题。

### 面试官可能追问

**Q：为什么唯一索引要带 embedding_model？**

A：同一个视频可能用不同 embedding 模型重建索引，不同模型的向量空间不能混用。状态也要按当前用户默认 embedding model 区分。

**Q：为什么状态表不直接替代 chunks 表？**

A：两者职责不同。`video_rag_indexes` 记录索引构建状态和元信息；`video_chunks` 保存具体文本片段、vector_id 和内容 hash。状态表不能替代内容表。

**Q：为什么失败时 chunk_count 设为 0？**

A：失败状态表示本次索引不可用。即使数据库里有旧 chunks，也不能把本次失败误判为可用索引。状态接口优先读状态表，就是为了避免这种误判。

### 这次不要夸大的点

- 当前状态表记录的是构建状态，没有校验 Milvus 中旧向量是否仍然存在。
- 当前重建版本固定为第一版字段，还没有实现版本递增策略。
- 当前 RAG 索引仍是同步构建，不是独立 Kafka job。

### 后续演进

- 重建索引时递增 `build_version`，并清理旧 Milvus 向量。
- 状态接口增加 Milvus readiness 或向量存在性校验。
- 后续可把 RAG index 构建也作为独立 Kafka job，避免手动构建接口长时间等待。

## 记录 012：ASR 分片结果持久化，支持失败片段复用

### 背景

路线图阶段五要求把 ASR chunk 结果落库。此前长视频已经按 300 秒切片转写，但每段结果只在内存中合并，数据库只保存最终全文。

### 现象

旧流程：

```text
split audio -> for each chunk call ASR -> memory merge -> save full transcription
```

问题是：

```text
第 3 段失败时，第 1/2 段已成功结果无法复用
重试只能任务级重跑
无法从数据库看到哪一段失败
无法展示 2/3 这类转写进度
```

### 排查证据

相关代码入口：

```text
internal/model/transcription_chunk.go
internal/repository/transcription_chunk.go
internal/mq/consumer.go
internal/mq/consumer_test.go
internal/model/model.go
internal/repository/repository.go
```

阶段五前，`transcribeAudio` 只把每段文本 append 到内存数组，最后合并成 `video_transcriptions.content`。

### 根因

ASR 分片是处理过程事实，但没有持久化。任务失败后，系统只知道整体失败，不知道哪些片段已经成功，也不能跳过已完成片段。

### 修复方案

新增表：

```text
video_transcription_chunks

id
task_id
chunk_index
audio_object
start_second
end_second
status       # pending / running / completed / failed
content
chars
error_msg
retry_count
created_at
updated_at
```

唯一索引：

```text
unique(task_id, chunk_index)
```

新的分片转写流程：

```text
split audio
for each chunk:
  如果 video_transcription_chunks 中该 chunk 已 completed：
    复用 content，跳过 ASR
  否则：
    upsert status=running
    call ASR
    success -> upsert status=completed, content, chars
    fail    -> upsert status=failed, error_msg, retry_count+1

全部成功：
  merge completed/current chunk content
  save video_transcriptions.content
```

这次第一版不把音频 chunk 上传 MinIO，只记录本次处理的本地 chunk path 到 `audio_object`，后续如果要跨进程长期复用音频片段，可以再把 chunk 对象化。

### 测试与验证

新增/更新测试：

```text
internal/mq/consumer_test.go
```

关键测试点：

```text
已 completed 的 chunk 会被复用，不再调用 ASR
新成功 chunk 会写 status=completed/content/chars
ASR 失败 chunk 会写 status=failed/error_msg/retry_count
最终合并文本包含复用 chunk 和新转写 chunk
```

验证命令：

```powershell
go test ./internal/mq
go test ./...
```

本阶段验证结果：

```text
go test ./internal/mq 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 长视频 ASR 第一版虽然做了 300 秒切片，但每段结果只在内存里合并，数据库只保存最终全文。这样如果第 3 段失败，前 2 段的成功结果也无法复用，用户重试时只能整个视频重新转写。
>
> 后来我加了 `video_transcription_chunks` 表，按 `task_id + chunk_index` 记录每段状态、文本、字符数和错误原因。consumer 转写前会先查该 chunk 是否已经 completed，如果已经完成就直接复用内容；否则才调用 ASR。某段失败时写 `failed/error_msg/retry_count`，下次重试可以跳过已完成片段，只重跑失败或未完成片段。
>
> 这个改动让长视频处理从“任务级重试”开始演进到“分片级恢复”，也为前端展示 `2/3` 这类进度打基础。

### 面试官可能追问

**Q：为什么不直接把音频 chunk 上传到 MinIO？**

A：这是后续更完整的方案。第一版先持久化转写结果和状态，解决“成功文本不能复用”和“失败段不可见”的问题。音频 chunk 对象化会增加存储清理和生命周期管理，适合下一步做。

**Q：复用 completed chunk 会不会复用旧模型结果？**

A：当前表还没有记录 ASR model/version，所以复用粒度是 task + chunk。后续如果用户切换 ASR 模型，应该把 ASR provider/model 也纳入 chunk 记录或触发重建。

**Q：失败 chunk 的 retry_count 和任务 retry_count 有什么区别？**

A：任务 retry_count 表示 Kafka 业务任务整体重试次数；chunk retry_count 表示某个 ASR 片段失败次数。两者粒度不同，后续可以结合起来做更细的进度和告警。

### 这次不要夸大的点

- 当前没有前端进度条，只是后端数据已经可支撑。
- 当前没有把音频 chunk 上传 MinIO，`audio_object` 第一版记录本地处理路径。
- 当前没有按 ASR model 维度隔离 chunk 结果。

### 后续演进

- 保存 chunk 的 start/end 秒数，前端展示分片进度和定位。
- 将音频 chunk 上传 MinIO，支持跨进程/跨实例重试。
- 记录 ASR provider/model，避免切换模型后复用旧 chunk。

## 记录 013：任务链路增加 trace_id 和阶段时间字段

### 背景

路线图阶段六要求增强可观测性，让系统能回答：

```text
哪个 task 出错？
当前在哪个 stage？
每个 stage 大概什么时候开始/结束？
HTTP 创建、Kafka 消费和任务详情能不能串起来？
```

### 现象

阶段三已经有 `stage`，但缺少统一 trace id。日志里能看到 taskID，但 HTTP 创建、Kafka payload、consumer 日志和数据库任务之间没有统一链路 ID。

### 排查证据

相关代码入口：

```text
internal/model/task.go
internal/repository/task.go
internal/mq/trace.go
internal/mq/producer.go
internal/mq/consumer.go
internal/service/media.go
internal/service/media_test.go
```

阶段六前，Kafka payload 只有：

```json
{
  "task_id": 1,
  "md5": "..."
}
```

没有 `trace_id`。

### 根因

异步链路跨越 HTTP 请求、数据库任务、Kafka 消息和 consumer 执行。只靠 taskID 可以定位单个任务，但不利于把创建、投递、消费、重试日志统一串起来，也不利于后续接入结构化日志或指标。

### 修复方案

新增任务字段：

```text
trace_id
stage_started_at
stage_finished_at
```

任务创建时生成 `trace_id`：

```text
本地上传 / 分片合并 / URL 上传任务创建 -> trace_id = uuid
```

Kafka producer 保持原方法签名不变，通过 context 读取 trace id：

```go
mq.ContextWithTraceID(ctx, task.TraceID)
mq.TraceIDFromContext(ctx)
```

Kafka payload 增加：

```json
{
  "task_id": 1,
  "md5": "...",
  "trace_id": "..."
}
```

download payload 同样增加：

```json
{
  "task_id": 1,
  "key": "...",
  "trace_id": "..."
}
```

consumer 日志优先使用 payload trace id，缺失时回退数据库 task trace id：

```text
[Kafka] URL 下载开始: traceID=... taskID=... url=...
[Kafka] URL 下载失败: traceID=... taskID=... userID=... url=... err=...
[Kafka] URL 下载完成: traceID=... taskID=... assetID=... md5=... size=...
```

repository 在 `UpdateStatusAndStage` / `UpdateStatusAndStageIf` / retry claim / failure 记录中写入阶段时间：

```text
stage_started_at
stage_finished_at
```

这不是完整 tracing 系统，但已经让任务详情和日志具备可串联的核心字段。

### 测试与验证

新增/更新测试：

```text
internal/service/media_test.go
```

关键测试点：

```text
UploadByURL 创建任务时生成 trace_id
UploadByURL 响应返回 trace_id
下载消息投递 context 中包含同一个 trace_id
RequestTranscribe 投递消息时沿用 task.trace_id
RequestAnalysis 投递消息时沿用 task.trace_id
```

验证命令：

```powershell
go test ./internal/service ./internal/mq ./internal/repository
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service ./internal/mq ./internal/repository 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 视频处理是异步链路，请求创建任务后，后续下载、转写、总结和索引都在 Kafka consumer 里执行。只看 HTTP 日志或只看 consumer 日志都不完整，所以我给任务加了 `trace_id`，创建任务时生成并保存到数据库，投递 Kafka 时也写入 payload。
>
> 这样排查时可以用同一个 trace id 串起：用户什么时候提交 URL、任务 ID 是多少、投递到了哪个 topic、consumer 下载或转写是否失败、最终任务状态是什么。与此同时，`stage_started_at/stage_finished_at` 能记录阶段时间，为后续统计每个阶段耗时打基础。

### 面试官可能追问

**Q：这是不是完整链路追踪？**

A：不是。当前是应用层 trace id 和阶段时间字段，不是 OpenTelemetry 那种 span 体系。它解决的是 demo 项目最直接的排障问题：跨 HTTP、Kafka、DB、consumer 串日志。后续如果上 OTel，可以把这个 trace id 接入真正的 trace/span。

**Q：为什么 producer 方法签名不加 traceID 参数？**

A：为了不让所有业务调用都扩散参数，我用 context 携带 trace id。service 层在投递前 `ContextWithTraceID`，producer 只负责读取并写入 payload。这样保留了原有方法签名，也方便后续从 HTTP middleware 自动注入 trace id。

**Q：stage 时间准不准？**

A：当前是阶段状态更新时记录的应用时间，适合排查和粗粒度耗时估算，不是高精度指标。后续要做指标可以用 Prometheus histogram 或 OpenTelemetry span。

### 这次不要夸大的点

- 当前没有引入 Prometheus 或 OpenTelemetry。
- 当前阶段耗时是 DB 字段，不是完整指标系统。
- 当前只有关键 Kafka 日志带 trace id，后续还可以把 ASR、LLM、Milvus 日志统一结构化。

### 后续演进

- 增加 HTTP middleware 自动注入 trace id，并写响应头。
- 将 stage 耗时导出为 Prometheus 指标。
- 用 OpenTelemetry span 表达 download/transcribe/summarize/index 子阶段。

## 记录 014：新增后端 SSE 问答接口

### 背景

路线图阶段七提到 RAG 问答当前是普通 JSON 响应，LLM 响应较慢时用户需要等待完整答案返回。阶段七先补后端 stream 接口契约，不大改前端 UI。

### 现象

旧接口：

```text
POST /api/v1/chat/sessions/:session_id/messages
  -> 检索
  -> LLM 完整生成
  -> 保存消息
  -> 一次性返回 JSON
```

如果 LLM 慢，前端只能等待。

### 排查证据

相关代码入口：

```text
internal/service/chat.go
internal/handler/chat.go
cmd/server/main.go
internal/service/chat_test.go
```

当前 AI provider 抽象是：

```go
Chat(ctx, messages) (string, error)
```

还不是 token streaming provider。

### 根因

前端体验优化需要后端先提供流式接口。但不同 LLM provider 的 streaming 协议不同，直接改 AI client 抽象会扩大范围。阶段七先补 SSE 后端接口契约，后续再把 provider 层升级成真实 token streaming。

### 修复方案

新增 service 方法：

```go
AskStream(ctx, ..., emit func(ChatStreamEvent) error)
```

新增事件：

```text
citations
answer
done
error
```

新增接口：

```text
POST /api/v1/chat/sessions/:session_id/messages/stream
Content-Type: text/event-stream
```

第一版实现方式：

```text
复用现有 Ask 流程
  -> 检索 citations
  -> 调用非流式 LLM 得到完整 answer
  -> 保存聊天消息
  -> SSE 输出 citations
  -> 按 80 rune 分块输出 answer
  -> 输出 done(message_id/model)
```

这不是 provider 级 token streaming，但已经让后端具备 SSE 接口契约，前端或 API 调用方可以用流式读取方式接入。

### 测试与验证

新增/更新测试：

```text
internal/service/chat_test.go
```

关键测试点：

```text
AskStream 会复用 RAG Ask 结果
事件顺序包含 citations、answer、done
done 事件在最后
原有消息持久化仍由 Ask 流程保证
```

验证命令：

```powershell
go test ./internal/service -run ChatService
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service -run ChatService 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 当前 RAG 问答第一版是普通 JSON，因为我优先保证检索、引用和消息持久化正确。后续为了支持更好的用户体验，我先在后端加了 SSE stream 接口，事件包括 citations、answer 和 done。
>
> 这一版还不是 provider 级 token streaming，因为现有 AI client 抽象是一次性返回完整字符串，不同 provider 的 streaming 协议也不同。所以我先保持 provider 不变，复用现有 Ask 流程得到完整答案后，通过 SSE 分块输出，先稳定接口契约。下一步再把 ChatClient 抽象升级为可选 StreamChat。

### 面试官可能追问

**Q：这算真正流式输出吗？**

A：严格说还不是 token 级流式。它是 SSE 接口契约的第一版，答案是在 LLM 完整返回后分块发给客户端。真正 token streaming 需要改 provider 层，让 OpenAI-compatible 响应边到边转发。

**Q：为什么还要做这个中间版本？**

A：它能先验证后端路由、鉴权、SSE content-type、事件格式和消息持久化，不影响现有 JSON 接口。后续 provider 级 streaming 可以在这个接口上替换实现，不需要前端再换 URL。

### 这次不要夸大的点

- 当前不是 token-by-token streaming。
- 当前没有改前端 UI。
- 当前没有处理半截输出落库，消息仍在完整 Ask 成功后保存。

### 后续演进

- 给 `ai.ChatClient` 增加可选 `StreamChat` 能力。
- 流式过程中支持中途取消和半截输出处理。
- 前端用 fetch ReadableStream 消费 POST SSE/stream 响应。

## 记录 015：RAG 问答增加候选扩展和关键词融合

### 背景

路线图阶段八提到当前 RAG 是：

```text
query embedding -> Milvus Top-K -> prompt -> LLM
```

这适合作为第一版，但如果用户问题里有非常明确的关键词，而向量召回没有命中对应 chunk，答案质量会受影响。

### 现象

旧流程只取最终 `top_k` 个向量结果。比如前端请求 topK=5，Milvus 只返回 5 个候选，后端没有机会从更多候选里融合关键词命中的片段。

### 排查证据

相关代码入口：

```text
internal/service/chat.go
internal/service/chat_test.go
internal/repository/video_chunk.go
internal/config/config.go
cmd/server/main.go
config.yaml
```

阶段八前，`ChatService.Ask` 直接把用户 topK 传给 retriever。

### 根因

单一路径向量召回对语义相似问题很有用，但对精确术语、代码名、专有名词或短关键词不一定稳定。关键词召回可以作为低成本补充。

### 修复方案

新增配置：

```yaml
rag:
  top_k: 5
  candidate_k: 30
```

新的检索流程：

```text
1. 向量召回 candidate_k 个候选
2. MySQL video_chunks 按 content LIKE question 做关键词召回
3. 按 chunk_id / chunk_index 去重
4. 保留向量分数，关键词-only chunk 给较低补充分数
5. 按分数排序后截取最终 top_k
6. 进入 prompt
```

这不是完整 BM25 或 rerank 模型，但已经具备混合检索的基础能力：向量召回负责语义，关键词召回补精确命中。

### 测试与验证

新增/更新测试：

```text
internal/service/chat_test.go
```

关键测试点：

```text
ChatService 会向 retriever 请求 candidate_k 个向量候选
当 MySQL chunk 命中问题关键词时，会融合进 citations
prompt 中包含关键词-only chunk
最终 citations 截取到用户请求 topK
```

验证命令：

```powershell
go test ./internal/service -run ChatServiceAsk
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service -run ChatServiceAsk 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> RAG 第一版只做向量 Top-K，这对语义问题够用，但对明确关键词、专有名词或代码名不一定稳定。所以我加了一个轻量混合检索版本：向量侧先召回 `candidate_k` 个候选，同时从 MySQL 的 `video_chunks` 做关键词 LIKE 召回，然后去重、融合并截取最终 topK 给 LLM。
>
> 这不是完整 BM25，也没有引入额外 rerank 模型，但它能在不增加外部依赖的情况下补齐“关键词明确但向量没命中”的场景。后续如果要继续提升质量，可以把 MySQL LIKE 换成 fulltext/BM25，再对候选做 RRF 或 rerank model。

### 面试官可能追问

**Q：LIKE 算不算真正混合检索？**

A：它是混合检索的基础版，不是最终形态。核心思想是把语义召回和关键词召回合并。当前用 LIKE 是为了低成本验证链路，后续可以替换为 MySQL fulltext、BM25 或专门检索引擎。

**Q：为什么关键词 chunk 分数较低？**

A：为了让向量召回仍作为主排序来源，关键词召回作为补充进入上下文。后续做 RRF 时可以用 rank 而不是手工分数。

**Q：candidate_k 为什么默认 30？**

A：最终 topK 通常是 5，扩大到 30 个候选可以给融合和后续 rerank 留空间，同时不会让 prompt 直接变长，因为最后仍截取 topK。

### 这次不要夸大的点

- 当前不是 BM25。
- 当前没有引入 rerank 模型。
- 当前融合策略是基础去重 + 分数排序，不是完整 RRF。

### 后续演进

- 用 MySQL fulltext/BM25 替换 LIKE。
- 引入 RRF 融合 vector_rank 和 keyword_rank。
- 对 candidate_k 候选接 rerank 模型后再截取 topK。

## 后续问题记录模板

复制下面模板追加：

```markdown
## 记录 XXX：问题标题

### 背景

### 现象

### 排查证据

### 根因

### 修复方案

### 测试与验证

### 面试可讲版本

### 面试官可能追问

### 这次不要夸大的点

### 后续演进
```
