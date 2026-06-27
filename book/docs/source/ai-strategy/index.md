# VidLens AI 策略层 -- 源码走读

> 源码目录：`internal/ai/`，共 7 个实现文件 + 3 个测试文件。

---

## 1. 文件表

| 文件 | 行数 | 职责 |
|------|------|------|
| `strategy.go` | 17 | `Strategy` 接口定义：转录 + 分片转录 + 总结 |
| `factory.go` | 133 | `Factory` 工厂 + `CompositeStrategy` 组合 + `ProfileTester` 健康检查 |
| `chat.go` | 183 | `ChatClient` / `StreamingChatClient` 接口 + `OpenAIChatClient` 实现（含 SSE 流式） |
| `embedding.go` | 77 | `EmbeddingClient` 接口 + `OpenAIEmbeddingClient` 实现 |
| `mimo.go` | 186 | 小米 MiMo 策略：base64 音频 ASR + 标准 LLM |
| `siliconflow.go` | 252 | 硅基流动策略：multipart ASR（含指数退避）+ 结构化 Prompt LLM |
| `observed.go` | 212 | 装饰器模式：为 Strategy / ChatClient / EmbeddingClient 添加调用记录 |
| `observed_test.go` | 80 | 装饰器测试：fake 实现 + recording recorder |
| `mimo_test.go` | 138 | MiMo 测试：httptest 模拟 API，验证 base64 编码和 header |
| `chat_embedding_test.go` | 171 | Chat/Embedding 测试：httptest 验证路径、认证、流式解析 |

---

## 2. 核心结构体

### 2.1 接口层

```
Strategy                     strategy.go:8-17
  +-- Transcribe(ctx, audioPath) (string, error)
  +-- TranscribeChunks(ctx, audioPaths) (string, error)
  +-- Summarize(ctx, text) (string, error)

ChatClient                   chat.go:20-22
  +-- Chat(ctx, messages) (string, error)

StreamingChatClient           chat.go:24-26
  +-- StreamChat(ctx, messages, emit) error

EmbeddingClient              embedding.go:14-16
  +-- Embed(ctx, input) ([]float32, error)

CallRecorder                 observed.go:41-43
  +-- RecordAICall(ctx, record) error

AIProfileTester              service/ai_profile.go:20-22
  +-- TestProfile(ctx, profile) error
```

### 2.2 数据结构

```
Profile                      factory.go:9-23
  LLMProvider / LLMBaseURL / LLMAPIKey / LLMModel
  ASRProvider / ASRBaseURL / ASRAPIKey / ASRModel
  EmbeddingProvider / EmbeddingEndpoint / EmbeddingAPIKey / EmbeddingModel / EmbeddingDim

ChatMessage                  chat.go:15-18
  Role string, Content string

CallContext                  observed.go:11-23
  UserID / TaskID / SessionID / Provider / Model / Kind / ASRProvider / ASRModel / LLMProvider / LLMModel / ASRSeconds

CallRecord                   observed.go:25-39
  UserID / TaskID / SessionID / Kind / Provider / Model / Status / DurationMs
  InputChars / OutputChars / ASRSeconds / ErrorCode / ErrorMsg
```

### 2.3 实现层

```
OpenAIChatClient             chat.go:28-35
  baseURL / apiKey / model / authHeader / authPrefix / *http.Client

OpenAIEmbeddingClient        embedding.go:18-23
  endpoint / apiKey / model / *http.Client

MimoStrategy                 mimo.go:21-27
  apiKey / baseURL / asrModel / llmModel / *http.Client

SiliconFlowStrategy          siliconflow.go:18-24
  apiKey / baseURL / asrModel / llmModel / *http.Client

CompositeStrategy            factory.go:76-79
  asr Strategy, chat ChatClient

observedStrategy             observed.go:114-118
  base Strategy, recorder CallRecorder, callCtx CallContext

observedChatClient           observed.go:45-49
  base ChatClient, recorder CallRecorder, callCtx CallContext

observedStreamingChatClient  observed.go:71-74
  embedded observedChatClient, streaming StreamingChatClient

observedEmbeddingClient      observed.go:92-96
  base EmbeddingClient, recorder CallRecorder, callCtx CallContext
```

---

## 3. 关键函数实现

### 3.1 Strategy 接口 (`strategy.go:8-17`)

```go
type Strategy interface {
    Transcribe(ctx context.Context, audioPath string) (string, error)
    TranscribeChunks(ctx context.Context, audioPaths []string) (string, error)
    Summarize(ctx context.Context, text string) (string, error)
}
```

三个方法覆盖完整的 AI 分析流程。所有实现（MimoStrategy、SiliconFlowStrategy、CompositeStrategy、observedStrategy）都满足此接口。

### 3.2 Factory 组装 (`factory.go:64-74`)

```go
func (f *Factory) NewAnalysisStrategy(profile Profile) (Strategy, error) {
    asr, err := f.NewASRStrategy(profile)   // 根据 ASRProvider 路由
    chat, err := f.NewChatClient(profile)    // 根据 LLMProvider 路由
    return &CompositeStrategy{asr: asr, chat: chat}, nil
}
```

Provider 路由表（`factory.go:31-62`）：

| Provider 值 | ASR 创建 | Chat 创建 | Embedding 创建 |
|-------------|----------|-----------|---------------|
| `mimo` | `NewMimoStrategy` | `NewMimoChatClient` | 不支持 |
| `siliconflow` | `NewSiliconFlowStrategy` | `NewOpenAIChatClient` | `NewOpenAIEmbeddingClient` |
| `openai_compatible` | `NewSiliconFlowStrategy` | `NewOpenAIChatClient` | `NewOpenAIEmbeddingClient` |

### 3.3 OpenAI SSE 流式解析 (`chat.go:103-165`)

```go
func (c *OpenAIChatClient) StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error {
    // 1. 构造请求，stream=true
    // 2. 发送 HTTP 请求
    // 3. bufio.Scanner 逐行读取
    // 4. 过滤空行和注释行（: 开头）
    // 5. 提取 data: 后的 JSON
    // 6. [DONE] 终止
    // 7. 解析 delta 并回调 emit
}
```

SSE 协议要点：
- 每行格式：`data: {"choices":[{"delta":{"content":"..."}}]}`
- 空行分隔事件
- `:` 开头是注释（如 `:keep-alive`）
- `data: [DONE]` 表示流结束

### 3.4 MiMo ASR 实现 (`mimo.go:50-81`)

```go
func (s *MimoStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
    dataURL, err := audioDataURL(audioPath)  // 音频 -> base64 data URL
    // 构造 chat/completions 请求，content 类型为 input_audio
    // 调用 s.chatCompletion(ctx, reqBody, "MiMo ASR")
}
```

MiMo ASR 的特殊之处：
- 走 `/chat/completions` 而非 `/audio/transcriptions`
- 音频以 `input_audio` 类型嵌入消息体（多模态格式）
- 认证用 `api-key` header（非 `Authorization: Bearer`）
- base64 体积限制 10MB（`mimo.go:17`）

### 3.5 SiliconFlow 指数退避 (`siliconflow.go:41-67`)

```go
for attempt := 0; attempt < 3; attempt++ {
    if attempt > 0 {
        waitTime := time.Second * time.Duration(1<<(attempt-1)) // 1s, 2s, 4s
        select {
        case <-ctx.Done():
            return "", ctx.Err()
        case <-time.After(waitTime):
        }
    }
    text, err := s.doTranscribe(ctx, audioPath)
    if err == nil { return text, nil }
    // 401/400 不重试
    if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "400") { break }
}
```

重试策略：3 次、指数退避（1/2/4 秒）、支持 context 取消、客户端错误快速失败。

### 3.6 SiliconFlow 结构化 Prompt (`siliconflow.go:147-184`)

```
# Role        -- 角色定义（信息架构师）
# Input Context -- 输入说明（ASR 文本特征）
# Goals       -- 目标（降噪、精炼）
# Constraints -- 约束（格式、语气、边界情况）
# Output Format -- 输出模板（核心摘要 + 深度洞察 + 原始精选 + 标签）
```

对比 MiMo 的简单 prompt（`mimo.go:177-185`），SiliconFlow 使用了更详细的结构化 prompt，引导 LLM 输出格式化报告。

### 3.7 装饰器 — 观测 ChatClient (`observed.go:51-69`)

```go
func NewObservedChatClient(base ChatClient, recorder CallRecorder, callCtx CallContext) ChatClient {
    if base == nil || recorder == nil || callCtx.UserID <= 0 {
        return base  // 防御性短路：无效参数直接返回原始对象
    }
    if streaming, ok := base.(StreamingChatClient); ok {
        return &observedStreamingChatClient{...}  // base 支持流式 -> 包装为流式观测客户端
    }
    return &observedChatClient{...}               // base 不支持流式 -> 普通观测客户端
}
```

关键设计：运行时检测 base 是否实现 `StreamingChatClient`，动态选择包装器类型。这保证了类型断言的正确性——上层代码 `client.(StreamingChatClient)` 在 base 不支持流式时会正确返回 `false`。

### 3.8 stripThinkTags 工具函数 (`siliconflow.go:236-251`)

```go
func stripThinkTags(s string) string {
    for {
        start := strings.Index(s, "<think")
        if start == -1 { break }
        end := strings.Index(s, "</think")
        if end == -1 { break }
        s = s[:start] + s[end+len("</think"):]
        s = strings.TrimPrefix(s, ">")
        s = strings.TrimPrefix(s, "\n")
    }
    return strings.TrimSpace(s)
}
```

清理 DeepSeek R1 等推理模型输出的 `<think>...</think>` 标签。使用循环处理多个 think 块，逐个移除。

---

## 4. 调用链图

### 4.1 视频分析主流程

```
Handler 层
  |
  v
Service.AnalyzeVideo(userID, videoPath)
  |
  v
Factory.NewAnalysisStrategy(profile)         factory.go:64
  |-- NewASRStrategy(profile)                factory.go:31
  |     |-- "mimo"           -> NewMimoStrategy(...)
  |     |-- "siliconflow"    -> NewSiliconFlowStrategy(...)
  |     |-- "openai_compatible" -> NewSiliconFlowStrategy(...)
  |
  |-- NewChatClient(profile)                 factory.go:44
  |     |-- "mimo"           -> NewMimoChatClient(...)
  |     |-- "openai_compatible" -> NewOpenAIChatClient(...)
  |
  +-- return CompositeStrategy{asr, chat}    factory.go:73
  |
  v
NewObservedStrategy(strategy, recorder, ctx) observed.go:120
  |
  v
observedStrategy.Transcribe(audioPath)       observed.go:127
  |-- base.Transcribe(audioPath)             -> MimoStrategy / SiliconFlowStrategy
  |-- recorder.RecordAICall(record)          -> DB 写入调用日志
  |
  v
observedStrategy.TranscribeChunks(paths)     observed.go:136
  |-- base.TranscribeChunks(paths)
  |     |-- for each path: Transcribe(path)
  |     |-- join with "\n\n"
  |-- recorder.RecordAICall(record)
  |
  v
observedStrategy.Summarize(text)             observed.go:145
  |-- base.Summarize(text)                   -> CompositeStrategy.Summarize
  |     |-- chat.Chat(messages)
  |     |     |-- system: defaultSummarySystemPrompt()
  |     |     |-- user: text
  |     |     |-- HTTP POST /chat/completions
  |-- recorder.RecordAICall(record)
  |
  v
返回 markdown 分析报告
```

### 4.2 流式总结流程

```
Service.SummarizeStream(userID, text, emit)
  |
  v
Factory.NewChatClient(profile) -> OpenAIChatClient
  |
  v
NewObservedChatClient(base, recorder, ctx)   observed.go:51
  |-- base.(StreamingChatClient) == true?
  |     yes -> observedStreamingChatClient
  |     no  -> observedChatClient
  |
  v
observedStreamingChatClient.StreamChat(messages, emit)  observed.go:76
  |-- base.StreamChat(messages, wrappedEmit)
  |     |-- HTTP POST with stream=true
  |     |-- SSE: bufio.Scanner 逐行读取
  |     |-- parseChatCompletionStreamDelta(data)
  |     |-- emit(delta) -> wrappedEmit(delta) -> 累计 outputChars
  |-- recorder.RecordAICall(record)
```

### 4.3 AI Profile 测试流程

```
Handler.TestAIProfile(req)
  |
  v
AIProfileService.Test(ctx, req)              service/ai_profile.go:152
  |-- validateAIProfileRequest(req)
  |-- build DecryptedAIProfile
  |
  v
ProfileTester.TestProfile(ctx, profile)      factory.go:104
  |-- NewChatClient(profile) -> chatClient
  |-- chatClient.Chat("ping")               -> 验证 LLM 连通性
  |-- NewEmbeddingClient(profile) -> embeddingClient
  |-- embeddingClient.Embed("health check") -> 验证 Embedding 连通性
  |-- check len(vector) == profile.EmbeddingDim  -> 验证维度一致性
  |
  v
return nil (success) or error
```

---

## 5. 设计决策表

| 决策 | 方案 | 理由 | 位置 |
|------|------|------|------|
| ASR/LLM 解耦 | Strategy 接口包含 Transcribe + Summarize | 一个接口覆盖完整流程，CompositeStrategy 内部组合不同实现 | `strategy.go:8-17` |
| Provider 路由 | Factory + switch-case | 用户配置 Profile 中的 Provider 字段决定具体实现，运行时动态绑定 | `factory.go:31-62` |
| 认证差异处理 | `authHeader`/`authPrefix` 字段注入 | MiMo 用 `api-key`，OpenAI 用 `Authorization: Bearer`，通过字段配置统一一个结构体 | `chat.go:43-44, 50-55` |
| ASR 分片 | 调用方负责切分，Strategy 负责逐片转录 | 职责分离——FFmpeg 切分，ASR 转录；Strategy 只接收 `[]string` | `mimo.go:83-103` |
| 重试策略 | 指数退避 3 次 + 客户端错误快速失败 | 500/429 瞬时错误值得重试，401/400 确定性错误不浪费时间 | `siliconflow.go:41-67` |
| 流式输出 | StreamingChatClient 独立接口 | 渐进式展示，避免长等待；接口分离满足 ISP | `chat.go:24-26` |
| 可观测性 | 装饰器模式包装 Strategy/ChatClient/EmbeddingClient | 零侵入，运行时按需包装；支持 ASR/LLM/Embedding 分别记录 | `observed.go:114-125, 51-62, 98-104` |
| 防御性短路 | `UserID <= 0` 时跳过装饰器 | 无用户上下文时不做无效记录，零开销降级 | `observed.go:121-123` |
| Think 标签清理 | `stripThinkTags` 循环移除 | DeepSeek R1 等推理模型输出 `<think>` 块，用户不需要看到推理过程 | `siliconflow.go:236-251` |
| Prompt 策略 | MiMo 简短 vs SiliconFlow 结构化 | MiMo 用通用 prompt；SiliconFlow 用 Role/Input/Goals/Constraints/Output 五段式 | `mimo.go:177-185`, `siliconflow.go:147-184` |
| 测试隔离 | httptest.NewServer 模拟 API | 不依赖外部服务，验证请求构造（路径/header/body）和响应解析 | `mimo_test.go`, `chat_embedding_test.go` |
| 错误信息截断 | errMsg 限 500 字符 | 防止堆栈或 HTML 错误页面撑爆数据库字段 | `observed.go:184-186` |
| Embedding 维度校验 | 健康检查时比对实际 vs 配置 | 运行时维度不匹配会导致向量检索失败，fail fast | `factory.go:124-126` |
| MiMo base64 限制 | 10MB 上限 (`mimoMaxAudioDataBytes`) | base64 膨胀 33%，10MB 约对应 7.5MB 原始音频；超出则要求压缩或分片 | `mimo.go:17, 55-57` |

---

## 6. 依赖关系

```
internal/ai/
  strategy.go          <- 定义 Strategy 接口
  chat.go              <- 定义 ChatClient / StreamingChatClient + OpenAIChatClient
  embedding.go         <- 定义 EmbeddingClient + OpenAIEmbeddingClient
  factory.go           <- 依赖 strategy.go, chat.go, embedding.go
  mimo.go              <- 实现 Strategy (MimoStrategy)
  siliconflow.go       <- 实现 Strategy (SiliconFlowStrategy)
  observed.go          <- 依赖 strategy.go, chat.go, embedding.go, model 包

internal/service/
  ai_profile.go        <- 依赖 ai.Profile, ai.Factory, ai.ProfileTester

internal/model/
  ai_call_log.go       <- 定义 AICallKind / AICallStatus 常量
  ai_profile.go        <- 定义 UserAIProfile GORM 模型
```

---

## 7. 扩展点

| 扩展方向 | 改动位置 | 工作量 |
|----------|----------|--------|
| 新增 ASR Provider | 新建 `xxx.go` 实现 Strategy + `factory.go` switch 加分支 | 小 |
| 新增 LLM Provider | 新建 `xxx.go` 实现 ChatClient + `factory.go` switch 加分支 | 小 |
| 新增 Embedding Provider | 新建 `xxx.go` 实现 EmbeddingClient + `factory.go` switch 加分支 | 小 |
| 自定义 System Prompt | `CompositeStrategy` 加字段 + Factory 从 Profile 读取 | 小 |
| 流式 Strategy | Strategy 接口加 `SummarizeStream` + observed 装饰器加对应方法 | 中 |
| ASR 健康检查 | `ProfileTester.TestProfile` 加 ASR 调用（需要测试音频） | 中 |
| 多模态输入（图片/视频帧） | Strategy 接口加方法或扩展 `Summarize` 参数 | 大 |
| 并发分片转录 | `TranscribeChunks` 内部用 `errgroup` 并发调用 `Transcribe` | 小 |
