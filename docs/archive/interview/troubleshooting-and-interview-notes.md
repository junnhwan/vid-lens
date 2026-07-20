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

- 本记录描述的是当时的历史状态；后续已增加业务关联 ID、Prometheus 指标和结构化日志，但仍未引入日志聚合平台或 OpenTelemetry。
- 当前已持久化 chunk 级转写结果，可复用成功片段；这不等于跨服务分布式追踪。

### 后续演进

- 为每个任务生成 `traceID`，贯穿 HTTP 请求、Kafka 消息、消费者日志。
- 保存 chunk 级 ASR 结果，支持失败后只重试失败片段。
- 已增加 Prometheus 指标，包括阶段耗时、ASR chunk 结果、AI 调用、RAG 与限流；后续仍可补告警规则和长期存储。
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
- 当前已有 Go 侧 BM25 风格关键词召回 + RRF 融合，但没有 rerank。
- 当前已有 provider 级 token streaming；早期记录里提到的“只做 SSE 分块”已被记录 023 覆盖。
- 当前自动索引已改为独立 RAG Kafka job；索引失败不删除 ASR 转写文本。
- 当前 Milvus 已写入 compose，并已在本地通过 `milvusdb/milvus:v2.4.15` 和 `quay.io/coreos/etcd:v3.5.18` 启动验证。

### 后续演进

- 继续完善 RAG 独立 job 的可视化状态和重试观测。
- 继续维护索引状态，例如 `indexing/indexed/failed`。
- 支持多 embedding 维度时按维度拆 collection。
- AI 调用日志和用户每日用量已在记录 024 补齐第一版，后续可增加 token 级统计。
- 流式问答后端已在记录 023 补齐 provider 级 streaming，后续可完善前端交互。

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

**Q：为什么当时不直接拆成 job 表？**

A：长期更合理的是 `video_tasks` 表示视频资产任务，`task_jobs` 表示一次 download/transcribe/summarize/index 动作。但当时这会引入较大改动，所以先用 `stage` 补齐可解释性，保持接口和前端兼容。后续已在记录 025 中补了第一版 `task_jobs`。

**Q：completed 后为什么 stage=none？**

A：`stage` 表示当前正在执行或最近明确的处理阶段。任务完成后没有正在执行的阶段，所以清为 `none`。如果要展示历史阶段耗时，后续应该加 stage 事件表或 job 表，而不是让当前 stage 承担历史记录。

**Q：AI 总结为什么可能出现 transcribing 阶段？**

A：因为总结需要转写文本。如果当前任务还没有 ASR 结果，analyze consumer 会先转写，再总结。所以总结任务内部可能经历 `transcribing -> indexing -> summarizing`。

### 这次不要夸大的点

- 当时还没有独立 `task_jobs` 表；后续记录 025 已补第一版子任务表。
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

当时（MySQL + Milvus 阶段）的构建流程变为：

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

- 当前已引入 Prometheus，但没有引入 OpenTelemetry；`trace_id` 是业务关联 ID，不能宣称为标准 Trace/Span。
- 阶段开始/结束时间仍落在 MySQL，同时导出低基数 histogram/counter；Prometheus 不使用 task/user/trace 作为 label。
- Kafka 与 AI 关键日志已迁移为结构化日志并做敏感字段脱敏，仍未接入集中式日志平台。

### 后续演进

- 增加 HTTP middleware 自动注入 trace id，并写响应头。
- 已将 stage 耗时导出为 Prometheus 指标；后续补基于真实运行数据的告警阈值。
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

> 当时 RAG 问答第一版是普通 JSON，因为我优先保证检索、引用和消息持久化正确。后续为了支持更好的用户体验，我先在后端加了 SSE stream 接口，事件包括 citations、answer 和 done。
>
> 这一版当时还不是 provider 级 token streaming，因为现有 AI client 抽象是一次性返回完整字符串，不同 provider 的 streaming 协议也不同。所以我先保持 provider 不变，复用现有 Ask 流程得到完整答案后，通过 SSE 分块输出，先稳定接口契约。后续记录 023 已把 ChatClient 抽象升级为可选 StreamChat。

### 面试官可能追问

**Q：这算真正流式输出吗？**

A：这条记录描述的是当时的 SSE 接口契约第一版，严格说当时还不是 token 级流式。后续记录 023 已在 provider 层增加 `StreamChat`，让 OpenAI-compatible 响应边到边转发。

**Q：为什么还要做这个中间版本？**

A：它能先验证后端路由、鉴权、SSE content-type、事件格式和消息持久化，不影响现有 JSON 接口。后续 provider 级 streaming 可以在这个接口上替换实现，不需要前端再换 URL。

### 这次不要夸大的点

- 当时不是 token-by-token streaming；后续记录 023 已补 provider 级 token streaming。
- 当前没有改前端 UI。
- 当前没有处理半截输出落库，消息仍在完整 Ask 成功后保存。

### 后续演进

- 后续已给 `ai.ChatClient` 增加可选 `StreamChat` 能力。
- 继续完善流式过程中断和半截输出处理。
- 前端可继续用 fetch ReadableStream 消费 POST SSE/stream 响应。

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

这在当时还不是完整 BM25 或 rerank 模型，但已经具备混合检索的基础能力：向量召回负责语义，关键词召回补精确命中。后续记录 019 已升级为 Go 侧 BM25 风格召回 + RRF。

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
> 这在当时不是完整 BM25，也没有引入额外 rerank 模型，但它能在不增加外部依赖的情况下补齐“关键词明确但向量没命中”的场景。后续记录 019 已把 LIKE fallback 升级为 Go 侧 BM25 风格召回，并用 RRF 做排名融合。

### 面试官可能追问

**Q：LIKE 算不算真正混合检索？**

A：它是混合检索的基础版，不是最终形态。核心思想是把语义召回和关键词召回合并。当时用 LIKE 是为了低成本验证链路，后续记录 019 已替换为 Go 侧 BM25 风格召回。

**Q：为什么关键词 chunk 分数较低？**

A：为了让向量召回仍作为主排序来源，关键词召回作为补充进入上下文。后续记录 019 已改为用 RRF rank 融合，而不是手工分数直接混排。

**Q：candidate_k 为什么默认 30？**

A：最终 topK 通常是 5，扩大到 30 个候选可以给融合和后续 rerank 留空间，同时不会让 prompt 直接变长，因为最后仍截取 topK。

### 这次不要夸大的点

- 当时不是 BM25；后续记录 019 已补 Go 侧 BM25 风格召回。
- 当前没有引入 rerank 模型。
- 当时融合策略是基础去重 + 分数排序；后续记录 019 已补 RRF。

### 后续演进

- 继续评估是否用 MySQL fulltext/ngram 或专门检索引擎替换 Go 侧 BM25 风格实现。
- 继续基于 RRF 指标做检索评估。
- 对 candidate_k 候选接 rerank 模型后再截取 topK。

## 记录 016：删除任务时不能误删共享视频资产

### 背景

VidLens 已经做了 MD5 内容级去重，同一个 `video_assets` 资产可以被多个 `video_tasks` 复用。路线图 P0-1 要求补齐任务删除和资源生命周期，否则删除单个任务可能破坏其他任务。

### 现象

旧版 `MediaService.DeleteTask` 的逻辑很直接：

```text
校验任务归属
如果 task.FileURL 非空，直接删除 MinIO object
逻辑删除 video_tasks
```

问题是任务 A 和任务 B 如果复用同一个 `asset_id/object_name`，删除任务 A 时会直接删除 MinIO 对象，任务 B 的数据库记录还在，但视频文件已经没了。

### 排查证据

相关代码入口：

```text
internal/service/media.go
internal/model/asset.go
internal/repository/task.go
internal/repository/video_chunk.go
internal/vector/milvus.go
cmd/server/main.go
```

`video_assets` 注释已经说明“同一个资产可以被多个用户任务复用”，但旧删除流程没有检查引用计数，也没有清理：

```text
video_transcriptions
video_transcription_chunks
ai_summaries
video_chunks
video_rag_indexes
chat_sessions
chat_messages
Milvus vectors
```

### 根因

任务删除和对象删除被混在一起，没有区分“用户任务”与“底层视频资产”的生命周期。内容级去重之后，MinIO object 的所有者不再是某一个 task，而是 asset；只有最后一个 task 引用消失时，才能删除 asset 对应的对象。

### 修复方案

第一版只解决了共享引用误删，但删除顺序仍有跨系统一致性窗口：如果先删向量后 PostgreSQL 事务失败，或者 task 已隐藏后 MinIO/Redis 收尾失败，就没有持久化入口恢复。当前实现改为“持久化删除意图 + 可重试执行器”，而不是把所有系统伪装成一个分布式事务：

```text
DELETE /tasks/:id
  -> PostgreSQL transaction
       SELECT task FOR UPDATE
       校验 user/status
       INSERT task_cleanup_jobs (pending)
       soft-delete video_tasks
  -> commit 后返回用户可接受的删除结果
  -> 同请求内尝试 ExecuteJob（best-effort）

TaskCleanupScheduler
  -> FindDue(pending/failed/expired-running)
  -> lease token claim
  -> 根据仍保留的 video_chunks/video_rag_indexes 收集 embedding model
  -> 删除当前 pgvector 或回滚 Milvus 后端中的 task projection
  -> 最后一个 task 引用时 reserve asset: active -> deleting
  -> 删除 MinIO object
  -> 删除 Redis 分片上传状态
  -> PostgreSQL transaction 删除 task-owned rows、软删除 owner asset、完成 cleanup job
  -> 任一步失败：记录 failed + next_retry_at，后续重试
```

`task_cleanup_jobs` 是 durable intent。DELETE 的成功边界是 intent 与 task soft-delete 已在同一 PostgreSQL 事务提交，而不是 pgvector 投影、MinIO、Redis 已经同步完成。这样即时清理失败时不会向客户端返回“删除失败”并诱导重复操作，scheduler 仍能从持久化状态继续恢复。

asset 增加了 `active/deleting` 两态和 `delete_owner_job_id`。cleanup 在短事务中锁定 asset、重新统计 active task 引用并 reserve 删除所有权；进入 `deleting` 后，`FindByMD5`、`CreateOrRestore` 和创建 task 前的行锁复查都会拒绝复用，避免上传线程重新挂到即将消失的对象上。多个 cleanup job 共享同一个 asset 时，只有 owner job 删除对象，其他 job 可以独立完成自身数据清理。

queued/running task 当前返回 409。原因不是做不到删除，而是 consumer 还没有 cancellation/tombstone 协议；若直接隐藏活跃 task，正在运行的 worker 仍可能晚到写回孤儿数据。这里选择显式拒绝，而不是假装已经安全取消。

### 测试与验证

关键证据：

```text
internal/service/task_cleanup_test.go
internal/service/task_cleanup_scheduler_test.go
internal/repository/task_cleanup_job_test.go
internal/repository/asset_test.go
internal/handler/media_delete_test.go
cmd/server/wiring_test.go
```

覆盖行为包括：

- intent insert 失败时整个事务回滚，task 继续可见；重复 DELETE 返回同一个 intent；
- queued/running 删除映射为 HTTP 409，404/403/500 不依赖字符串匹配；
- 向量、MinIO、Redis、PostgreSQL finalization 任一步失败后，job 记录失败并可重试；
- MinIO 已删、Redis/DB 失败时允许再次幂等删除对象；
- 共享 asset 还有 active 引用时不删 object；两个 task 都隐藏时只有一个 owner；
- stale lease token 不能覆盖新 owner 的 failed/completed 状态；过期 running lease 可被 scheduler 恢复；
- scheduler 一个 job 失败仍继续同批次后续 job，并在 context 取消后退出。

### 面试可讲版本

> 我一开始做了 MD5 内容去重，同一个视频只保存一份 MinIO object，多个用户任务引用同一个 `video_assets`。后来排查删除流程时发现，删除 task 不能直接删 object，否则会破坏其他引用；只做引用计数也不够，因为 PostgreSQL 关系状态、pgvector 投影、MinIO 和 Redis 没有一个覆盖全部系统的本地事务，任何一步失败都可能留下半清理状态。
>
> 我的处理不是上一个通用 Saga 框架，而是在 PostgreSQL 增加很窄的 `task_cleanup_jobs`。删除请求先在同一个事务里写 cleanup intent 并软删除 task，事务成功后用户就看不到任务；外部资源清理由带 lease token 的执行器完成，失败会记录 `next_retry_at`，后台 scheduler 再扫描恢复。PostgreSQL 中的 relational chunks 和 RAG index 在向量删除成功前保留，作为重试所需的 embedding model 事实。
>
> 共享 asset 还有一个上传删除竞争。我给 asset 加了 `active/deleting` 状态，最后一个引用消失时由一个 cleanup job reserve 删除所有权；deleting 状态不能再被新任务复用。这样多个 job 可以清理各自数据，但只有 owner 会删对象。这个方案提供的是可恢复最终一致性，不是 PostgreSQL、MinIO、Redis 和外部副作用之间的全局强事务。

### 面试官可能追问

**Q：为什么 DELETE 返回成功时 MinIO 可能还没有删完？**

A：API 接受的是持久化删除请求。intent 和 task soft-delete 已经原子提交，用户侧删除语义不会回滚；外部系统清理由 durable job 恢复。如果同步返回失败，客户端会以为什么都没发生，但 task 实际已经隐藏，反而产生假失败。可以进一步把接口改成 202 并返回 cleanup 状态，但当前 handler 沿用成功响应，文档明确了边界。

**Q：为什么先保留 PostgreSQL relational chunks，再删向量投影？**

A：向量删除需要 `user_id/task_id/embedding_model`。如果先删 PostgreSQL source facts，向量后端临时失败后就失去确定的重试参数。当前顺序是外部投影成功后才在最终事务删除 task-owned rows。

**Q：lease 能避免外部调用重复吗？**

A：不能完全避免。worker 超时后新 owner 可以接管，旧 worker 的外部调用可能已经发生。因此 MinIO/Redis/vector delete 必须本身可重复，lease token 主要防止 stale worker 把数据库 job 错误地标记为 failed/completed。面试中不能把它说成 exactly-once。

**Q：这是 Saga 吗？**

A：它有持久化步骤和补偿恢复的思想，但实现是 VidLens 删除场景专用的 cleanup state machine，没有通用步骤编排、逆向补偿 DSL 或跨业务复用，所以不把它包装成通用 Saga 引擎。

### 这次不要夸大的点

- PostgreSQL 关系状态、向量投影与 MinIO/Redis 之间没有全局事务，只是可恢复最终一致性。
- 外部 delete 可能重复执行，不是 exactly-once。
- queued/running task 目前拒绝删除，没有实现 worker cancellation/tombstone。
- cleanup 只处理 task-owned 数据和最后一个 asset 对象；历史临时 chunks 仍应依赖单独的定时清理或 MinIO lifecycle。
- Milvus 仍保留为迁移观察期回滚后端，不能说已经完全删除。

### 后续演进

- 如果产品需要删除进度，DELETE 可返回 202 + cleanup job 状态查询，而不是扩大当前 job 为通用工作流。
- 为 cleanup job 增加积压、失败次数和最长等待时间指标及告警。
- 设计 processing cancellation/tombstone 后，再允许 queued/running task 删除。
- 给历史分片临时对象配置定期清理或 MinIO lifecycle。

## 记录 017：RetryScheduler 投递失败后恢复 next_retry_at

### 背景

VidLens 使用 DB retry scheduler 弥补 Kafka 没有 RocketMQ 那种业务级延迟重试语义的问题。路线图 P0-2 要求修复一个细节：scheduler claim 到任务后，如果重新投递 Kafka 失败，不能让任务失去下一次调度机会。

### 现象

旧流程是：

```text
FindDueRetryTasks
旧单表 claim -> status 改成 queued/running，next_retry_at 清空
enqueueRetry
```

如果 `enqueueRetry` 失败，旧代码只是把任务写回 failed：

```text
UpdateStatusAndStage(task.ID, failed, stage, err)
```

此时 `next_retry_at` 已经被 claim 阶段清空，后续 scheduler 查询条件要求 `next_retry_at IS NOT NULL`，这个任务就可能一直卡在 failed。

### 排查证据

相关代码入口：

```text
internal/mq/retry.go
internal/repository/task_lease_dispatch.go
internal/mq/consumer_test.go
internal/mq/reliability_review_test.go
```

`FindDueRetryTasks` 的查询条件包含：

```text
status = failed
next_retry_at IS NOT NULL
next_retry_at <= now
last_job_type <> ''
```

所以 claim 后投递失败必须恢复 `next_retry_at`。

### 根因

重试链路只考虑了“业务处理失败后的下一次重试”，没有覆盖“重试调度自身投递失败”的恢复。Kafka 写入失败发生在业务处理之前，不应该额外消耗一次业务 retry_count，但必须重新设置短退避时间。

### 修复方案

当前实现不再使用两个相互独立的单表 helper，而是通过 aggregate repository 的 dispatch lease 状态机处理：

```go
ClaimRetryDispatch(TaskDispatchClaimRequest{Token, ExpectedVersion, LeaseUntil, ...})
RestoreRetryDispatch(TaskDispatchRestoreRequest{Token, NextRetryAt, ...})
```

claim 在同一个 PostgreSQL transaction 内为 `video_tasks` 和对应 `task_jobs` 写入相同的 dispatch token、lease kind、过期时间和 lease version。Kafka 投递失败时，restore 只允许仍持有该 token 的调度者通过 CAS 恢复，并在同一事务更新两张表：

```text
status = failed
stage = 原重试目标 stage
last_error_code = retry_enqueue_failed
last_error_msg = Kafka 投递错误
next_retry_at = now + dispatch failure backoff
processing_token / lease_kind / lease_expires_at = cleared
lease_version = previous + 1
retry_count 保持不变
```

如果进程在 claim 后直接崩溃，`FindDueRetryTasks` 还能扫描过期 dispatch lease，下一实例可重新接管。`RetryScheduler.RunOnce` 在 producer 失败时恢复状态，同时仍把 enqueue/restore 错误返回日志层。

### 测试与验证

新增测试：

```text
TestRetrySchedulerRestoresNextRetryWhenEnqueueFailsAfterClaim
```

关键断言：

```text
enqueue 失败后任务仍为 failed
next_retry_at 被恢复为 now + 1 minute
retry_count 不增加
last_error_code = retry_enqueue_failed
last_error_msg 包含 Kafka 错误
```

验证命令：

```powershell
go test ./internal/mq -run RetryScheduler
go test ./internal/repository -run Task
go test ./...
```

本阶段验证结果：

```text
go test ./internal/mq -run RetryScheduler 通过
go test ./internal/repository -run Task 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 我没有把 Kafka 消费失败简单地无限不提交 offset，因为那会卡住分区，所以我在业务层做了 DB retry scheduler。后面我补了一个容易被忽略的失败点：scheduler 把任务 claim 出来后，会先清空 `next_retry_at`，再重新投递 Kafka。如果这一步 Kafka 投递失败，任务就会卡在 failed，而且后续 scheduler 再也捞不到。
>
> 我的修复是把这个场景单独标记成 `retry_enqueue_failed`，恢复 failed 状态和下一次短退避调度时间，但不增加业务 retry_count，因为业务逻辑还没有真正重跑。这样重试链路本身也具备失败恢复能力。

### 面试官可能追问

**Q：为什么投递失败不增加 retry_count？**

A：因为 retry_count 记录的是业务处理尝试次数。Kafka 重投递失败发生在业务处理之前，如果也增加 retry_count，会让任务因为 MQ 短暂不可用而更快进入 dead，不符合语义。

**Q：为什么恢复为 1 分钟？**

A：这是当前第一版短退避，避免 scheduler 立刻忙等重投。后续可以把 dispatch failure backoff 配到 `task_retry` 里。

### 这次不要夸大的点

- 当前仍不是 Kafka 原生死信队列。
- 当前没有单独统计 retry dispatch failure 次数。
- 当前调度器是 DB 轮询，不是分布式调度器。

### 后续演进

- 给 retry dispatch failure 单独加计数字段或审计日志。
- 多实例部署时给 scheduler 增加分布式锁或基于 DB 的更严格 claim 条件。
- 将 dispatch failure backoff 配置化。

## 记录 018：URL 下载增加白名单、DNS 安全校验和脱敏

### 背景

VidLens 支持用户提交 B 站或 YouTube 链接，由后端调用 yt-dlp 下载视频。公开部署后，后端会代表用户访问外部网络，因此 URL 下载不能只做简单格式校验。

### 现象

旧校验已经拒绝了：

```text
非 http/https
host 为空
localhost
直接 IP 且为 loopback/private/link-local
```

但仍有风险：

```text
域名 DNS 解析后可能指向内网地址
evilbilibili.com 这类相似域名可能被误认为平台链接
source_url 会保存完整 query token 和 fragment
yt-dlp 参数默认带 --no-check-certificate
```

### 排查证据

相关代码入口：

```text
internal/service/remote_video_url.go
internal/service/media.go
internal/config/config.go
config.yaml
internal/pkg/ytdlp/ytdlp.go
internal/service/media_test.go
internal/pkg/ytdlp/ytdlp_test.go
```

### 根因

URL 下载是服务端代访问外部资源，本质上有 SSRF 风险。只检查 URL 字符串不够，因为域名最终访问的 IP 由 DNS 决定；同时 query/fragment 里可能带用户 token，不应该直接入库或出现在日志里。

### 修复方案

新增配置：

```yaml
tools:
  allowed_video_hosts:
    - bilibili.com
    - b23.tv
    - youtube.com
    - youtu.be
```

新增 `remoteVideoURLValidator`，校验顺序：

```text
1. trim 和 URL parse
2. scheme 必须为 http/https
3. host 不能为空，localhost 拒绝
4. domain whitelist，支持 www.bilibili.com 这类子域名，但拒绝 evilbilibili.com
5. DNS resolver 解析 host
6. 任意解析结果为 private/loopback/link-local/unspecified/multicast 就拒绝
7. 入库前清除 userinfo、query 和 fragment
```

`UploadByURL` 现在使用脱敏 URL 生成下载 key、filename 和 `SourceURL`。yt-dlp 参数移除了 `--no-check-certificate`。

### 测试与验证

新增/更新测试：

```text
internal/config/config_test.go
internal/service/media_test.go
internal/pkg/ytdlp/ytdlp_test.go
```

关键测试点：

```text
allowed_video_hosts 可从 YAML 解析
localhost、127.0.0.1、::1、file scheme 被拒绝
evilbilibili.com 被拒绝
白名单域名解析到 10.0.0.8 时被拒绝
www.bilibili.com 公网解析可通过
SourceURL 不再保存 token/query/fragment
yt-dlp 参数不再包含 --no-check-certificate
```

验证命令：

```powershell
go test ./internal/config -run AllowedVideoHosts
go test ./internal/service -run "RemoteVideoURL|UploadByURL"
go test ./internal/pkg/ytdlp -run Certificate
go test ./...
docker compose config
```

本阶段验证结果：

```text
上述定向测试通过
go test ./... 通过
docker compose config 通过
```

### 面试可讲版本

可以这样讲：

> URL 下载不是把用户传来的链接直接交给 yt-dlp。公开部署后，服务端会代表用户访问网络，如果不限制，就可能出现 SSRF 风险。所以我把校验拆成了几层：只允许 http/https，只允许明确支持的平台域名，对域名做白名单后缀匹配，再解析 DNS，拒绝解析到内网、loopback、link-local、multicast 这类地址。
>
> 同时我把入库的 `source_url` 改成脱敏 URL，不保存 query token 和 fragment，日志里也不会出现这类参数。yt-dlp 侧也去掉了默认跳过证书校验的参数。这个方案不能说是完全生产级 SSRF 防护，但已经比单纯字符串校验更可防守。

### 面试官可能追问

**Q：为什么不能说生产级 SSRF 防护？**

A：因为 yt-dlp 自己可能跟随平台内部重定向，Go 层的 DNS 校验不一定覆盖它实际访问的每一次请求。更严格的生产方案应该把下载放进网络受限的沙箱或独立下载服务，并限制 egress 网络。

**Q：去掉 query 会不会影响某些平台链接？**

A：可能会。所以当前白名单平台主要面向 B 站和 YouTube 常规公开链接。需要 query 才能访问的私有链接不适合直接作为公开部署的下载入口，除非后续专门做加密保存和更严格的访问策略。

### 这次不要夸大的点

- 当前不是完整生产级 SSRF 沙箱。
- 当前没有控制 yt-dlp 跟随重定向后的每一次网络访问。
- 当前没有保存 raw_url_hash 或加密 raw_url。

### 后续演进

- 将 yt-dlp 放到 egress 受限的容器或独立下载服务。
- 如确实需要 query 参数，新增加密 raw URL 字段，只在下载 worker 内部解密使用。
- 记录 raw URL hash，便于排障和去重，但不暴露敏感 query。

## 记录 019：RAG 检索从 LIKE fallback 升级为 Go 侧 BM25 风格召回 + RRF 融合

### 背景

VidLens 的视频问答使用 ASR 转写文本作为 RAG 知识源。旧版本的检索流程是：

```text
query embedding -> Milvus vector candidates -> MySQL LIKE fallback -> 简单按 score 排序
```

这能覆盖一部分语义召回和完整字符串命中，但不能防守成“BM25/RRF 混合检索”。

### 现象

旧 LIKE fallback 的问题：

```text
LIKE "%完整问题%" 很难命中 chunk
关键词召回没有真实排名
vector score 和 keyword score 不是同一尺度
citation 看不出来自 vector、keyword 还是两者同时命中
```

### 排查证据

相关代码入口：

```text
internal/service/chat.go
internal/service/retrieval_fusion.go
internal/repository/video_chunk.go
internal/service/retrieval_fusion_test.go
internal/repository/video_chunk_test.go
```

### 根因

RAG 检索不能把语义向量分数和关键词命中分数直接相加。向量检索更适合语义相似问题，关键词检索更适合精确术语、英文缩写、数字和专有名词；两路结果的分数尺度不同，直接混排不稳定。

### 修复方案

本阶段做了两层改动：

```text
1. query terms 提取：
   - 保留英文、数字和中文 n-gram
   - 避免只拿完整问题做 LIKE

2. retrieval fusion：
   - vector results 带 vector_rank
   - keyword results 带 keyword_rank
   - 用 RRF 做排名融合
   - citation 增加 source/vector_rank/keyword_rank/rrf_score
```

Repository 层新增 `SearchByBM25`。当前实现是从单个 task 的 `video_chunks` 中加载候选 chunk，在 Go 侧计算 BM25 风格分数，主要考虑本项目“单视频 chunk 数量有限、测试可移植”的约束。这里没有引入 Elasticsearch，也没有依赖 MySQL FULLTEXT/ngram parser。

### 测试与验证

新增/更新测试：

```text
internal/service/retrieval_fusion_test.go
internal/repository/video_chunk_test.go
```

关键测试点：

```text
vector 命中 1/2，keyword 命中 2/3 时，chunk 2 通过 RRF 排到前面
缺失一路 rank 时仍可融合
source 正确标记为 vector / keyword / hybrid
中文、英文、数字 query terms 可提取
SearchByBM25 能对相关 chunk 排名
```

验证命令：

```powershell
go test ./internal/service -run Retrieval
go test ./internal/repository -run VideoChunk
go test ./...
```

本阶段验证结果：

```text
P1-1 实现后 go test ./... 通过
当前最新 go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> RAG 第一版只做向量检索，后来我加过 LIKE fallback，但完整问题字符串很难直接命中 chunk，所以这只能算轻量补充。后面我把检索升级成了向量召回加关键词召回，再用 RRF 做排名融合。
>
> 这里我没有直接把向量分数和关键词分数相加，因为两种分数不是同一个尺度。RRF 用的是排名，不依赖分数绝对值，所以对第一版混合检索更稳。当前关键词侧是针对单视频 chunk 的 Go 侧 BM25 风格实现，适合先验证链路；如果后续数据量变大，再替换成 MySQL FULLTEXT/ngram 或专门检索引擎。

### 面试官可能追问

**Q：能不能说已经用了 Elasticsearch？**

A：不能。当前没有引入 Elasticsearch。关键词召回是在 MySQL chunk 数据基础上做 Go 侧 BM25 风格打分。

**Q：能不能说已经做了 rerank？**

A：不能。当前做的是召回和 RRF 融合，还没有接 rerank 模型。rerank 应该放在混合召回稳定、有评估集之后。

### 这次不要夸大的点

- 当前不是 Elasticsearch/OpenSearch。
- 当前不是 MySQL FULLTEXT/ngram parser 实现。
- 当前没有 rerank。
- 当前没有 query rewrite 模型。

### 后续演进

- 增加 RAG 评估集，用 Recall@K 和 MRR 对比纯 vector、keyword、hybrid。
- 数据量增加后，把 Go 侧 BM25 替换为 MySQL FULLTEXT/ngram 或专用检索引擎。
- 在稳定召回基础上增加 rerank。

## 记录 020：RAG 重建索引前清理 Milvus 旧向量

### 背景

RAG 索引重建会替换 MySQL 里的 `video_chunks`，并向 Milvus upsert 新向量。旧版本没有在重建前删除同一 task/model 下的旧向量。

### 现象

如果同一个 task 的转写内容变化，新的 vector_id 会因为内容 hash 变化而改变。此时：

```text
MySQL video_chunks 已是新内容
Milvus 里仍可能保留旧 vector
检索 filter 只按 user_id/task_id/embedding_model
用户问答可能召回旧片段
```

### 排查证据

相关代码入口：

```text
internal/service/rag_index.go
internal/vector/milvus.go
internal/vector/noop.go
internal/service/rag_index_test.go
```

### 根因

MySQL chunk 表和 Milvus 向量库是两套存储。只替换 MySQL chunk 不会自动删除 Milvus 中已经写入的旧 vector。因为当前 Milvus filter 没有 build_version 隔离，旧 vector 仍满足 `user_id/task_id/embedding_model` 条件。

### 修复方案

扩展 `RAGVectorStore`：

```go
UpsertChunks(ctx context.Context, vectors []RAGVector) error
DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error
```

`BuildTaskIndex` 现在的顺序是：

```text
1. 写 video_rag_indexes = indexing
2. DeleteTaskChunks(user_id, task_id, embedding_model)
3. 生成 embedding
4. ReplaceTaskChunks 写 MySQL 新 chunks
5. UpsertChunks 写 Milvus 新 vectors
6. 写 video_rag_indexes = indexed
```

如果删除旧 Milvus vectors 失败，流程会写 `video_rag_indexes = failed`，并且不会继续替换 MySQL chunks，避免 MySQL 和向量库进一步分叉。

### 测试与验证

新增/更新测试：

```text
internal/service/rag_index_test.go
```

关键测试点：

```text
BuildTaskIndex 会先 DeleteTaskChunks 再 UpsertChunks
DeleteTaskChunks 失败时不会 ReplaceTaskChunks
DeleteTaskChunks 失败会记录 RAG index failed 和 last_error
```

验证命令：

```powershell
go test ./internal/service -run "RAGIndexServiceDeletesOldVectors|RAGIndexServiceStopsBefore"
go test ./internal/service
go test ./...
```

本阶段验证结果：

```text
P1-2 定向 RAG 测试通过
P1-2 后 go test ./internal/service 通过
P1-2 后 go test ./... 通过
当前最新 go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> RAG 索引重建不是只替换 MySQL chunks 就够了，因为真正参与向量召回的是 Milvus。旧向量如果不删，即使 MySQL 里已经是新 chunk，Milvus filter 仍然可能召回旧内容。
>
> 所以我把向量存储接口扩展了删除能力，重建前先按 `user_id/task_id/embedding_model` 删除旧向量。如果删除失败，就把 RAG index 状态写成 failed，不继续替换 MySQL chunks。这个问题本质上是关系型元数据和向量库数据的一致性问题。

### 面试官可能追问

**Q：删除旧向量后 upsert 新向量失败怎么办？**

A：当前会把 RAG index 状态标记为 failed，用户可以重试构建索引。第一版选择简单一致性，没有做在线双版本切换。

**Q：为什么没有直接做 build_version？**

A：build_version 更适合在线重建和无缝切换，但要改 Milvus schema 和检索 filter。当前项目先按 task/model 删除旧向量，覆盖重建污染问题；后续如果要在线重建，再引入 build_version。

### 这次不要夸大的点

- 当前不是无缝在线索引切换。
- 当前没有 build_version filter 生效。
- 当前没有后台异步清理旧版本。

### 后续演进

- 增加 build_version 字段并让检索 filter 只查当前 indexed 版本。
- 删除旧版本向量改为后台清理，减少重建窗口。
- 记录每次构建版本、耗时和失败原因。

### 记录 020 后续：pgvector 单库投影替换边界（2026-07-17）

上面的记录描述的是第一版 delete-before-replace 行为。切换 pgvector 后，索引服务增加了可选的 `RAGVectorReplacer` 能力：

> BuildTaskIndex 完成 embedding 后，先在一个 PostgreSQL transaction 中替换 `video_chunks` 事实源，再调用 pgvector 的 `ReplaceTaskChunks`，由另一个 PostgreSQL transaction 原子完成 scope 内旧向量删除和新向量 upsert；任一向量插入失败都会 rollback，旧投影仍保留。Milvus 没有同等事务能力，所以回滚适配仍使用显式 delete + upsert 兼容路径。

这里提升的是向量投影自身的原子性，不是把事实源事务和向量投影事务合成一个事务。PostgreSQL `video_chunks` 仍是事实源，pgvector 表是可重建投影；如果 source 已更新但投影发布失败，RAG index 会记录 `failed`，通过重试或 `cmd/rag-reindex` 恢复。面试中不要把它说成强一致、双阶段发布或零停机切换。

## 记录 021：RAG 索引从 ASR 完成路径拆成独立 Kafka job

### 背景

旧版本在 ASR 转写保存后，直接同步调用 RAG indexer：

```text
transcribe consumer -> save transcription -> BuildTaskIndex -> completed
```

这会让 embedding 和 Milvus 写入占用转写 consumer 的处理时间。

### 现象

RAG 索引和 ASR 的失败边界不同：

```text
ASR 成功后，用户应该能查看转写文本
Embedding 或 Milvus 失败，只应该影响问答索引
旧同步调用会让转写 consumer 被 RAG 索引耗时拖住
```

### 排查证据

相关代码入口：

```text
internal/mq/producer.go
internal/mq/consumer.go
internal/mq/retry.go
internal/config/config.go
config.yaml
cmd/server/main.go
internal/mq/consumer_test.go
internal/mq/producer_test.go
internal/config/config_test.go
```

### 根因

ASR、总结、RAG 索引属于不同处理动作。旧流程把 RAG 索引塞在 ASR 完成后的同步路径里，导致索引慢或向量库失败时，会影响文字提取 worker 的吞吐和语义边界。

### 修复方案

新增 Kafka topic 配置：

```yaml
kafka:
  rag_index_topic: "video-rag-index"
```

新增 payload：

```go
type RAGIndexPayload struct {
    TaskID  int64  `json:"task_id"`
    TraceID string `json:"trace_id"`
}
```

Producer 新增：

```text
EnqueueRAGIndex(ctx, taskID)
```

Consumer 新增：

```text
SetRAGIndexProducer
StartRAGIndexConsumer
handleRAGIndex
```

现在 ASR 完成后：

```text
save transcription
enqueue rag_index job
transcribe task completed
```

RAG consumer 独立执行：

```text
load task
确认 transcription 存在
调用 BuildTaskIndex
成功提交 offset
失败 recordTaskFailure(task, rag_index, indexing, err)
```

RetryScheduler 新增 `TaskJobRAGIndex = "rag_index"`，失败后可以按现有 DB retry scheduler 重新投递到 `video-rag-index` topic。

如果 ASR 后投递 RAG job 失败，ASR 结果仍保留；如果能解析到默认 AI profile，则写 `video_rag_indexes = failed` 记录投递失败原因，方便用户或后续接口看到索引没有构建成功。

### 测试与验证

新增/更新测试：

```text
internal/config/config_test.go
internal/mq/producer_test.go
internal/mq/consumer_test.go
```

关键测试点：

```text
rag_index_topic 可从 YAML 解析
RAGIndexPayload 包含 task_id 和 trace_id
indexAfterTranscription 只 enqueue，不同步调用 indexer
enqueue 失败时写 video_rag_indexes failed
handleRAGIndex 会调用 indexer
RAG indexer 失败会调度 rag_index retry，且转写文本仍保留
RetryScheduler 能重投 rag_index job
```

验证命令：

```powershell
go test ./internal/mq ./internal/config
go test ./...
docker compose config
```

本阶段验证结果：

```text
go test ./internal/mq ./internal/config 通过
go test ./... 通过
docker compose config 通过
```

### 面试可讲版本

可以这样讲：

> 我把 ASR 和 RAG 索引拆成两个异步 job，是因为它们的失败边界不同。ASR 成功后，用户应该能看到转写文本；Embedding 或 Milvus 失败只影响问答索引，不应该让文字提取这个动作也被认为失败。
>
> 现在转写 consumer 在保存 transcription 后只投递 `rag_index` Kafka 消息，然后完成转写任务。RAG consumer 独立读取 `video-rag-index` topic，调用 `BuildTaskIndex` 写 chunks 和 Milvus vectors。失败时通过 `last_job_type=rag_index` 进入现有 DB retry scheduler，同时 `video_rag_indexes` 会记录索引失败状态。

### 面试官可能追问

**Q：RAG 失败后为什么还把主任务标成 failed？**

A：这是当时 `video_tasks.status` 仍混合多个动作语义的历史限制。为了复用现有 retry scheduler，RAG job 失败会写 `last_job_type=rag_index` 和 `stage=indexing`。但 transcription 数据不会删除，用户仍可查看转写文本。后续记录 025 已补第一版 `task_jobs`，用于把转写、总结、RAG 索引的子任务状态拆开记录。

**Q：Kafka 消息失败会不会无限不提交 offset？**

A：业务失败会记录到 DB retry 状态后返回 nil，让 offset 提交，避免卡住分区。真正的重试由 DB retry scheduler 按 `next_retry_at` 重新投递。

### 这次不要夸大的点

- 当时还没有 `task_jobs` 子任务表；后续记录 025 已补第一版子任务状态表。
- 当前没有独立 RAG 微服务，仍是 Go 单体内的独立 Kafka consumer。
- 当前没有改变 RocketMQ，也没有拆微服务。

### 后续演进

- 继续完善 `task_jobs` 的前端展示和更细粒度耗时统计。
- RAG consumer 增加更细的 AI 调用审计和耗时记录。
- 根据实际吞吐给 `video-rag-index` topic 单独配置 consumer 并发。

## 记录 022：增加 RAG 离线评估指标，避免只凭主观感觉判断检索质量

### 背景

RAG 检索从向量 + LIKE fallback 升级到 Go 侧 BM25 风格召回 + RRF 后，需要一个小规模评估办法证明改动是否真的改善召回，而不是只看单次人工问答效果。

### 现象

没有评估体系时，RAG 优化容易变成：

```text
挑几个问题肉眼试一下
只看最终回答是否顺眼
不知道 topK 有没有命中期望片段
不知道无结果率、MRR、source 分布是否变化
```

### 排查证据

相关代码和样例：

```text
internal/service/rag_eval.go
internal/service/rag_eval_test.go
docs/eval/rag-cases.example.md
```

### 根因

RAG 的关键不是只生成答案，而是先检索到正确上下文。没有检索评估时，模型可能靠泛化知识或编造回答掩盖检索失败，后端也无法量化 BM25/RRF 是否真的提升了召回。

### 修复方案

新增离线评估核心：

```text
RAGEvalCase:
  task_hint
  question
  expected_chunk_keywords
  expected_answer_points

RunRAGEval:
  对每个 case 调用传入的检索函数
  记录 citations 和耗时

EvaluateRAGRetrieval:
  计算 Recall@K
  计算 MRR
  计算无结果率
  计算平均检索耗时
  统计 vector / keyword / hybrid source 分布
```

当前实现是纯 service 层指标计算和 runner，不依赖真实 Milvus、LLM 或具体视频数据。`docs/eval/rag-cases.example.md` 只放格式样例，不放真实用户视频内容。

### 测试与验证

新增测试：

```text
internal/service/rag_eval_test.go
```

关键测试点：

```text
命中 rank=2 时 Recall@K 和 MRR 计算正确
无结果 case 会计入 no_result_rate
source_counts 会统计 vector / keyword / hybrid
没有 expected_chunk_keywords 的 case 会被跳过，不污染 Recall/MRR
RunRAGEval 会调用传入 retriever，并保留无结果 case
```

验证命令：

```powershell
go test ./internal/service -run RAGEval -v
go test ./internal/service
go test ./...
```

本阶段验证结果：

```text
go test ./internal/service -run RAGEval -v 通过，3 个 RAGEval 测试执行
go test ./internal/service 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> RAG 优化不能只靠肉眼感觉，所以我补了一个离线评估核心。每个 case 有问题和期望命中的 chunk 关键词，评估时看 topK citation 里是否命中这些关键词，并计算 Recall@K、MRR、无结果率、检索耗时和 vector/keyword/hybrid 来源占比。
>
> 这个设计先不依赖真实 LLM，也不让模型回答质量干扰检索评估。它评估的是“有没有把正确上下文捞出来”。后续接真实视频任务时，只需要把检索函数接到现有 ChatService 的 retrieval 流程上，就能比较纯向量、关键词和 RRF 混合检索。

### 面试官可能追问

**Q：为什么用 expected_chunk_keywords，而不是人工标 chunk_id？**

A：第一版更轻量，适合本地样例和隐私脱敏。真实评估集更严谨的做法是标注期望 chunk_id 或时间段，再计算更精确的 Recall@K。

**Q：现在能证明回答质量提升了吗？**

A：不能直接证明回答质量。当前评估的是检索质量。回答质量还需要结合 expected_answer_points、人工评审或 LLM-as-judge，但那应该建立在检索评估之后。

### 这次不要夸大的点

- 当前没有接真实生产数据集。
- 当前没有 LLM-as-judge。
- 当前没有自动对比多套检索配置的 CLI。

### 后续演进

- 增加真实脱敏评估集，标注期望 chunk_id 或时间段。
- 增加纯 vector、keyword、hybrid 的对比 runner。
- 加入 expected_answer_points 的答案覆盖率评估。

## 记录 023：SSE 问答从完整回答切块升级为 provider 级 token streaming

### 背景

旧版 `/chat/.../messages/stream` 接口虽然使用 SSE 返回，但后端流程是：

```text
ChatService.Ask -> 等 LLM 完整回答 -> 按 80 rune 切块 -> SSE answer 事件
```

这只能算接口契约上的流式输出，不是模型 provider 级 token streaming。

### 现象

旧实现的问题：

```text
用户要等 LLM 完整回答返回后才开始看到 answer 事件
上游模型中途输出的 delta 没有被实时转发
请求取消时无法尽早停止上游流式读取
面试中不能说“真正 token streaming”
```

### 排查证据

相关代码入口：

```text
internal/ai/chat.go
internal/service/chat.go
internal/service/chat_test.go
internal/ai/chat_embedding_test.go
internal/handler/chat.go
```

### 根因

`ChatClient` 旧接口只有 `Chat(ctx, messages) (string, error)`，它天然只能等完整响应。`AskStream` 复用 `Ask`，导致检索、LLM 调用、落库全部完成后才开始 SSE 输出 answer chunk。

### 修复方案

AI 层新增可选接口：

```go
type StreamingChatClient interface {
    StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error
}
```

`OpenAIChatClient` 实现 `StreamChat`：

```text
POST /chat/completions
stream=true
读取 text/event-stream
解析 data: {...choices[].delta.content...}
遇到 data: [DONE] 结束
每个 delta 立即回调 emit
```

Service 层拆出共用流程：

```text
prepareRAGChat:
  校验 question/session
  embedding
  vector + BM25/RRF 检索
  加载 recent memory
  构造 RAG messages

saveChatExchange:
  保存 user message
  保存 assistant message
  保存 retrieval snapshot
  刷新 Redis recent memory
```

`AskStream` 现在流程：

```text
prepareRAGChat
emit citations
如果 chat 支持 StreamingChatClient:
  StreamChat 每个 delta -> emit answer
  累积完整 answer
否则:
  fallback 到 Chat 完整回答后切块
saveChatExchange
emit done
```

### 测试与验证

新增/更新测试：

```text
internal/ai/chat_embedding_test.go
internal/service/chat_test.go
```

关键测试点：

```text
OpenAIChatClient.StreamChat 会发送 stream=true
能解析 OpenAI-compatible SSE delta
AskStream 在 streaming client 下不调用普通 Chat()
AskStream 会按 delta emit answer 事件
assistant message 保存的是累积后的完整答案
不支持 streaming 的 client 仍 fallback 到原切块逻辑
```

验证命令：

```powershell
go test ./internal/ai -run Stream -v
go test ./internal/service -run AskStream -v
go test ./internal/ai
go test ./internal/service
go test ./...
```

本阶段验证结果：

```text
go test ./internal/ai -run Stream -v 通过
go test ./internal/service -run AskStream -v 通过
go test ./internal/ai 通过
go test ./internal/service 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> 我第一版 SSE 只是把完整回答切块返回，不能算真正 token streaming。后面我在 AI client 层增加了可选的 `StreamChat` 接口，对 OpenAI-compatible `/chat/completions` 使用 `stream=true`，逐行解析 SSE 里的 `delta.content`，后端拿到一个 delta 就通过 SSE 发给前端。
>
> Service 层没有直接丢掉原来的兼容性：如果 provider client 支持 streaming，就走真实流式；如果不支持，就 fallback 到完整回答后切块。无论哪种方式，后端都会累积完整回答，最后保存 user message、assistant message 和 retrieval snapshot。

### 面试官可能追问

**Q：中途失败时会不会保存半截回答？**

A：当前实现是 StreamChat 成功结束后才落库。如果上游中途失败，接口返回错误事件，不保存半截 assistant message。后续如果要保存半截，需要单独定义 partial 状态。

**Q：为什么还保留 fallback？**

A：因为不是所有 provider 或 profile 都保证支持 streaming。保留 fallback 可以不破坏旧 provider，但我会明确标注它是 fallback，不把它说成真正 token streaming。

### 这次不要夸大的点

- 当前没有为 SSE event 增加 `stream_mode` 字段。
- 当前没有保存 partial assistant message。
- 当前没有对每个 token 做审计计费，只记录完整 answer 落库。

### 后续演进

- 给 stream event 增加 `stream_mode=provider/fallback`。
- 支持中途失败的 partial message 状态。
- 将 LLM streaming 调用纳入 AI 调用审计和用户额度。

## 记录 024：增加 AI 调用审计和用户每日用量聚合

### 背景

VidLens 支持用户级 BYOK profile，这能避免公开部署时默认消耗服务端 Key，但还不能回答：

```text
某次问答失败到底是 embedding、LLM、ASR 还是配置问题
某个用户今天发起了多少次 LLM/Embedding/ASR 调用
调用耗时和失败状态如何
```

### 现象

旧版本 AI 调用只在业务流程中返回错误或写任务失败状态，没有统一审计表。排障时需要从不同日志和任务状态里拼线索。

### 排查证据

相关代码入口：

```text
internal/model/ai_call_log.go
internal/repository/ai_call_log.go
internal/service/ai_observer.go
internal/ai/observed.go
internal/service/chat.go
internal/service/rag_index.go
internal/mq/consumer.go
cmd/server/main.go
internal/repository/ai_call_log_test.go
internal/service/ai_observer_test.go
internal/ai/observed_test.go
```

### 根因

BYOK 解决的是 Key 来源和成本归属，但没有统一记录 AI 调用元数据。Embedding、LLM、ASR 分散在 ChatService、RAGIndexService 和 Kafka consumer 中，如果不做统一 observer，后续排障和额度控制都会缺证据。

### 修复方案

新增模型：

```text
ai_call_logs:
  user_id/task_id/session_id
  kind: asr / llm / embedding
  provider/model
  status: success / failed
  duration_ms
  input_chars/output_chars
  error_code/error_msg
  created_at

user_usage_daily:
  user_id/date
  asr_seconds/asr_requests
  llm_requests
  embedding_requests
  failed_requests
  input_chars/output_chars
```

新增 `AIObserver`：

```text
RecordAICall -> 写 ai_call_logs -> upsert user_usage_daily
```

新增 AI wrapper：

```text
ObservedChatClient
ObservedEmbeddingClient
ObservedStrategy
```

接入点：

```text
ChatService:
  question embedding
  LLM Chat / StreamChat

RAGIndexService:
  每个 chunk embedding

MQ Consumer:
  ASR Transcribe
  LLM Summarize
```

安全边界：

```text
不保存明文 API Key
不保存 Authorization header
不保存完整 prompt
不保存完整模型响应
只保存字符数、状态、耗时、provider/model 和错误摘要
```

### 测试与验证

新增/更新测试：

```text
internal/repository/ai_call_log_test.go
internal/service/ai_observer_test.go
internal/ai/observed_test.go
internal/service/chat_test.go
internal/service/rag_index_test.go
```

关键测试点：

```text
AI call log 可落库
user_usage_daily 可按日聚合 LLM/Embedding/失败次数和字符数
observer 不保存完整 prompt/response
ObservedChatClient 失败时记录 failed llm log
非 streaming base client 不会被 wrapper 伪装成 streaming client
ObservedStrategy 区分 ASR provider/model 和 LLM provider/model
ChatService Ask 记录 embedding + llm
RAGIndexService BuildTaskIndex 记录 embedding
```

验证命令：

```powershell
go test ./internal/repository -run AICall -v
go test ./internal/service -run AIObserver -v
go test ./internal/ai -run Observed -v
go test ./internal/service -run "RecordsEmbeddingAndLLM|RecordsEmbeddingCalls" -v
go test ./internal/repository ./internal/service ./internal/ai ./internal/mq
go test ./...
```

本阶段验证结果：

```text
上述定向测试通过
go test ./internal/repository ./internal/service ./internal/ai ./internal/mq 通过
go test ./... 通过
```

### 面试可讲版本

可以这样讲：

> BYOK 只能说明不默认消耗服务端 Key，但排障和成本可观测还需要审计。所以我加了 `ai_call_logs` 和 `user_usage_daily`。每次 ASR、Embedding、LLM 调用都会记录 provider、model、状态、耗时、输入输出字符数和错误摘要，并按用户日期聚合调用次数。
>
> 这里我刻意不保存明文 API Key、Authorization header、完整 prompt 和完整模型响应。因为这些内容可能包含用户隐私或密钥。当前记录的是排障和额度控制需要的元数据，而不是把模型上下文全文落库。

### 面试官可能追问

**Q：这算完整计费系统吗？**

A：不能算完整计费系统。当前是审计和每日用量聚合，主要用于排障和基础额度控制。真正计费还需要 token 级统计、套餐规则、扣减事务和账单状态。

**Q：为什么只记录字符数，不记录 token？**

A：不同 provider 的 token 计算规则不完全一致，第一版先用字符数做近似可观测指标。后续可以根据 provider 返回的 usage 字段补 token 统计。

### 这次不要夸大的点

- 当前没有真实扣费。
- 当前没有 token 级精确计量。
- 当前没有套餐/账单系统。
- 当前没有记录完整 prompt/response。

### 后续演进

- 读取 provider usage 字段，补 prompt_tokens/completion_tokens。
- 增加用户每日额度限制和超额拒绝。
- 在管理端展示 AI 调用审计和失败分布。

## 记录 025：新增 task_jobs 子任务状态表，拆开长任务动作语义

### 背景

`video_tasks.status` 早期同时承载了多个动作的状态：

```text
URL 下载
ASR 转写
AI 总结
RAG 索引
```

虽然前面已经通过 `stage`、`last_job_type`、`retry_count`、`next_retry_at` 缓解了排障问题，但单表状态仍然容易让“视频任务状态”和“某个处理动作状态”混在一起。

### 现象

典型问题是：

```text
RAG 索引失败会把 video_tasks 写成 failed/indexing
但转写文本其实已经存在，用户仍能查看 transcription
download/transcribe/analyze/rag_index 的失败、重试和完成状态缺少独立行
任务详情无法直接展示每个处理动作的状态
```

### 排查证据

相关实现路径：

```text
internal/model/task_job.go
internal/model/task.go
internal/model/model.go
internal/repository/task_job.go
internal/repository/repository.go
internal/service/media.go
internal/mq/consumer.go
internal/mq/retry.go
internal/repository/task_job_test.go
internal/service/media_test.go
internal/mq/consumer_test.go
```

### 根因

`video_tasks` 本质上更适合表示用户的视频入口和资产引用，但实际处理流程里存在多个动作，每个动作都有自己的 `queued/running/completed/failed/dead`、阶段、重试次数、下次重试时间和错误原因。只靠主任务表会让这些动作互相覆盖状态。

### 修复方案

新增 `task_jobs` 表：

```text
task_id
user_id
job_type: download / transcribe / analyze / rag_index
status
stage
trace_id
retry_count
max_retries
next_retry_at
last_error_code
last_error_msg
started_at
finished_at
```

第一版使用 `(task_id, job_type)` 唯一索引，表示同一个视频任务下每类动作保留当前状态行，不做完整历史事件表。

接入点：

```text
UploadByURL:
  创建 download queued job
  投递失败时写 download failed job

RequestTranscribe:
  创建 transcribe queued job
  投递失败时写 transcribe failed job

RequestAnalysis:
  创建 analyze queued job
  投递失败时写 analyze failed job

Consumer:
  handleDownload/handleTranscribe/handleAnalyze/handleRAGIndex 开始时写 running
  成功时写 completed
  recordTaskFailure 同步写 failed/dead 和 retry 元数据

RetryScheduler:
  claim 后写 dispatching 状态
  Kafka 重新投递失败时同步恢复 failed + next_retry_at

DeleteTask:
  删除任务时清理 task_jobs

GetTaskDetail:
  预加载 Jobs，后端详情响应可带出子任务状态
```

兼容策略：

```text
保留 video_tasks.status/stage/last_job_type 作为旧接口和 retry scheduler 的兼容状态源
task_jobs 作为第一版子任务状态镜像和更清晰的后端排障数据
不改前端样式，不拆微服务，不迁移 MQ
```

### 测试与验证

新增/更新测试：

```text
internal/repository/task_job_test.go
internal/service/media_test.go
internal/mq/consumer_test.go
```

关键测试点：

```text
task_jobs 可记录 queued -> running -> failed/retry -> queued -> completed
queued 子任务新建时不会继承旧 retry_count
UploadByURL 创建 download queued job
RequestTranscribe 创建 transcribe queued job
RequestAnalysis 创建 analyze queued job
DeleteTask 清理 task_jobs
RAG enqueue 成功创建 rag_index queued job
RAG enqueue 失败创建 rag_index failed job
RAG consumer 成功写 rag_index completed job
RAG consumer 失败写 rag_index failed + next_retry_at
download consumer 成功写 download completed job
recordTaskFailure 同步写 task_jobs retry 元数据
RetryScheduler 投递失败恢复 task_jobs next_retry_at
```

验证命令：

```powershell
go test ./internal/repository -run "TaskJob|UpsertQueued" -v
go test ./internal/mq -run "IndexAfterTranscription" -v
go test ./internal/repository ./internal/service ./internal/mq
```

本阶段验证结果：

```text
上述定向测试通过
go test ./internal/repository ./internal/service ./internal/mq 通过
```

### 面试可讲版本

可以这样讲：

> 我最后补了一版 `task_jobs`，解决的是 `video_tasks.status` 混合多个动作语义的问题。比如一个视频任务可能已经完成 ASR，但 RAG 索引失败了。旧设计只能把主任务写成 failed/indexing，用户容易误解成整个视频处理都失败。
>
> 第一版没有推翻现有接口，而是保留 `video_tasks` 作为兼容状态源，在旁边增加 `task_jobs`：download、transcribe、analyze、rag_index 每类动作都有独立的 status、stage、retry_count、next_retry_at 和错误摘要。这样失败重试仍然复用现有 scheduler，但后端排障和任务详情可以看到每个动作自己的状态。

### 面试官可能追问

**Q：这是不是完整工作流引擎？**

A：不是。当前只是把视频任务下的几个处理动作拆成子任务状态表，没有做 DAG 编排、历史事件流、补偿事务或可视化工作流。它解决的是当前项目里最直接的状态语义混杂问题。

**Q：为什么 `(task_id, job_type)` 唯一，而不是每次重试都插一行？**

A：第一版更关注当前可查询状态和兼容现有接口。重试历史如果全部保留，会更接近事件表或 job_attempts 表，改动更大。现在保留当前 job 状态，后续如果要做审计，可以再加 `task_job_attempts`。

**Q：为什么还保留 `video_tasks.status`？**

A：因为现有前端、API 和 retry scheduler 已经依赖它。直接替换会扩大改动面。当前做法是兼容保留主任务状态，同时用 `task_jobs` 提供更细的后端状态边界。

### 这次不要夸大的点

- 当前没有完整工作流引擎。
- 当前没有记录每一次 retry attempt 的历史行。
- 当前没有前端样式重构，只是后端详情可预加载 Jobs。
- 当前 `video_tasks.status` 仍保留兼容语义，没有完全退役。

### 后续演进

- 增加 `task_job_attempts` 或事件表，记录每次尝试的耗时和错误。
- 让前端任务详情明确展示每个子任务状态。
- 将 retry scheduler 逐步从 `video_tasks.last_job_type` 迁移到 `task_jobs` 驱动。

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


## 记录 026：第三方 AI 故障演练与可观测性定位闭环

### 演练目标

验证 ASR 超时、LLM 429 和 Embedding 503 发生后，能否从同一个 `task_id` 关联到处理阶段、子任务、尝试次数、AI 调用审计、失败分类、下一次重试时间和 Prometheus 指标，而不是只看到一条无上下文错误。

### 验证层次与边界

这次明确区分两类证据，不能把组件测试替身包装成真实运行结果：

1. **组件测试**：`internal/mq/observability_fault_drill_test.go` 使用可控 Strategy 替身返回 `context deadline exceeded`，验证 Consumer、租约、失败持久化和审计字段契约。
2. **本地真实链路演练**：后端、Kafka、MySQL、Redis、MinIO、Milvus 和 Prometheus 都实际运行；只有第三方 AI Provider 替换成本机可控故障服务，以稳定注入 TLS handshake timeout、HTTP 429 和 HTTP 503。它能验证本项目基础设施和业务链路，但不等于真实第三方厂商事故或生产压测。

### 首轮真实演练发现的问题

首轮任务结果如下：

- task 17：ASR TLS handshake timeout，正确进入 `retryable_error`，`retry_count=1` 且生成 `next_retry_at`。
- task 18：LLM HTTP 429，被错误记录为 `non_retryable_error`。
- task 19：Embedding HTTP 503，被错误记录为 `non_retryable_error`。

这说明“审计表识别出 `rate_limited/provider_5xx`”不代表“任务层一定会按可重试错误处理”。必须继续检查任务失败分类和重试调度，而不能只看 provider wrapper 的错误码。

### 根因

AI provider 层已经返回 typed `*ai.ProviderError`，其中包含 `Retryable` 和 `RetryAfter`；但任务层 `isRetryableError` 仍主要依赖错误字符串匹配。序列化后的错误文本是 `rate_limited` 或 `provider_5xx`，没有命中原有的 `http 429/http 503` 字符串规则，因此任务被误判为不可重试。

另外，`title_generation` 使用 observed chat wrapper 时只设置了 `Kind`，没有执行统一的 LLM 调用上下文归一化，导致审计记录的 `provider/model` 为空。

### 修复方案

1. `isRetryableError` 优先通过 `errors.As` 读取 `ProviderError.Retryable`，只有非 typed error 才回退到兼容性的字符串分类。
2. 重试时间使用 `max(任务阶梯退避, ProviderError.RetryAfter)`，既保留任务级退避，也不早于 provider 明确要求的等待时间。
3. 普通失败路径和 processing lease 路径使用同一重试延迟计算，避免两条链路行为不一致。
4. `NewObservedChatClient` 统一调用 `llmCallContext`，将 `LLMProvider/LLMModel` 归一化到通用 `Provider/Model` 审计字段。

### 修复后的真实复验

第二轮真实 Kafka/MySQL/Prometheus 链路使用新任务复验：

- task 20：LLM 429，`stage=summarizing`、`retry_count=1`、`last_error_code=retryable_error`，并写入 `next_retry_at`。
- task 21：Embedding 503，`stage=indexing`、`retry_count=1`、`last_error_code=retryable_error`，并写入 `next_retry_at`。
- Prometheus 查询能看到 `analyze/rag_index` 各一次 `retryable_error`，以及 summarizing/indexing 的失败阶段指标。

第三轮使用成功的可控 Provider 复验标题审计：task 22 的 `title_generation` 记录包含 `kind=llm`、`provider=openai_compatible`、`model_name=fault-chat`，并且 `task_id/job_id/trace_id/stage/attempt` 均完整。该轮任务经过真实上传、Kafka 消费、MySQL 持久化和 AI 审计链路完成。

本地复验证据保存在：

```text
.logs/fault-drill/run-meta.json
.logs/fault-drill/mysql-evidence.tsv
.logs/fault-drill/backend-log-evidence.txt
.logs/fault-drill/metrics-after.prom
.logs/fault-drill/promql-task-stage.json
.logs/fault-drill/promql-ai-call.json
.logs/fault-drill/run-meta-v2.json
.logs/fault-drill/mysql-evidence-v2.tsv
.logs/fault-drill/metrics-after-v2.prom
.logs/fault-drill/promql-retry-fix-v2.json
.logs/fault-drill/run-meta-v3.json
.logs/fault-drill/mysql-evidence-v3.tsv
.logs/fault-drill/metrics-after-v3.prom
```

`.logs/` 是本地证据目录，不应提交 API Key、用户真实凭据或第三方响应正文。

### 排查顺序

1. `video_tasks`：确认 `stage/status/retry_count/next_retry_at/last_error_code`。
2. `task_jobs`：确认同一 `task_id + job_type` 的 attempt、阶段和失败状态。
3. `ai_call_logs`：按 `task_id/job_id/trace_id` 检查 provider、model、kind、stage、attempt 和 error code。
4. Prometheus：检查 `vidlens_task_stage_total`、`vidlens_task_retry_total` 和 `vidlens_ai_call_total`，label 中不放 task/user/trace 等高基数值。
5. 结构化日志：按业务 `trace_id` 或 `task_id` 定位，日志不得包含 API Key、转写正文、完整本地媒体路径或未脱敏 provider 响应。

### 本地查询与看板

- Metrics 使用独立管理监听，默认 `127.0.0.1:19090/metrics`，不挂在公共 Gin 业务路由上。
- Task Overview：查看阶段吞吐、失败率、P95 阶段耗时与 retry/dead 计数。
- AI Usage：按受控的 provider/model 标签查看调用结果；用户自定义值统一归一化，避免高基数。
- MySQL 定位顺序：`video_tasks -> task_jobs -> ai_call_logs`。

### 安全边界

Prometheus/Grafana compose 端口只绑定本机，Grafana 关闭匿名访问。后端 metrics 默认只监听 loopback；若容器 Prometheus 需要跨网络抓取，必须显式设置 `VIDLENS_METRICS_ALLOW_REMOTE=true`，并通过主机防火墙或私有网络限制管理端口，不能把该端口加入公网反向代理。

### 面试可讲版本

> 我不是先写一个“可观测性完成”的描述，而是做了可控故障演练。第一次跑真实 Kafka、MySQL 和 Prometheus 链路时，发现 provider 审计已经识别出 429 和 503，但任务却被标成不可重试。根因是 provider 层返回了结构化错误，任务层仍靠字符串判断。我改成优先读取 typed ProviderError，并把 Retry-After 和任务退避取较大值。修复后重新创建任务验证，429 和 503 都进入了 retryable_error，数据库状态、AI 调用审计和 Prometheus 指标能够用 task、job 和业务 trace 串起来。这里的 trace_id 是业务关联 ID，不是 OpenTelemetry Trace。

### 当前限制与不要夸大的点

- 当前是单体应用内的业务关联 ID、结构化日志、MySQL 审计和 Prometheus 指标，不是 OpenTelemetry 分布式追踪。
- 第三方 Provider 是本地可控故障服务；不能声称验证了某个真实厂商的故障恢复能力。
- 本轮证明的是功能链路和字段契约，不是生产级告警有效性，也没有完成长期容量或高并发压测。
- Token/成本无法从 provider 获得时保持 NULL/unknown，不用 0 冒充真实用量。

## URL 下载执行期校验的当前边界

URL 创建任务时会校验协议、host allowlist 和当时的 DNS 结果；Kafka consumer 真正调用 yt-dlp 前会使用同一策略再次校验，并传入清理后的 URL。这覆盖了任务排队期间的再次检查需求。

但 yt-dlp 是外部进程，可能跟随重定向或重新解析域名，当前 Go 层没有提供完整的网络沙箱、固定 IP 连接或 egress 防火墙。因此这里的准确说法是“入口和执行前的 allowlist/DNS 校验”，仍不能称为生产级 SSRF 防护。更强的后续方案是独立下载 worker + 受限出网网络/代理，并在重定向链路上逐跳执行策略。
