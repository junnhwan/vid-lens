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
