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
