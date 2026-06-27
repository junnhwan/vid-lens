# VidLens AI 策略层 -- 面试题

> 基于 `internal/ai/` 源码，共 10 题。每题含参考答案（源码原文 + 行号引用）和 2-3 个追问。

---

## Q1. VidLens 的 `Strategy` 接口定义了哪些方法？为什么用 `context.Context` 作为第一个参数？

### 参考答案

接口定义在 `internal/ai/strategy.go:8-17`：

```go
// strategy.go:8-17
type Strategy interface {
    // Transcribe 语音转文字（ASR）
    Transcribe(ctx context.Context, audioPath string) (string, error)

    // TranscribeChunks 分片语音转文字，规避单次 ASR 请求体限制。
    TranscribeChunks(ctx context.Context, audioPaths []string) (string, error)

    // Summarize 大模型总结
    Summarize(ctx context.Context, text string) (string, error)
}
```

三个方法覆盖 AI 分析全流程：单文件转录、分片转录、文本总结。`context.Context` 作为第一个参数是 Go 的惯例（`go vet` 会检查），用于传递截止时间、取消信号和请求级元数据（如 trace ID）。当用户取消任务时，`ctx.Done()` 被触发，底层 HTTP 请求立即终止，避免资源浪费。

### 追问链

1. **`Transcribe` 和 `TranscribeChunks` 返回值都是 `(string, error)`，为什么不让 `TranscribeChunks` 返回 `[]string` 以保留分段信息？**
   看 `internal/ai/mimo.go:83-103`，实现上先逐段转录再用 `\n\n` 拼接成单一字符串。返回 `string` 简化了上层调用——`Summarize` 只需接收一个文本块，不需要关心分段数量。如果未来需要定位到具体片段的时间戳，可以扩展接口或引入新的结构体。

2. **如果要支持流式总结（边生成边推送），接口应该怎么改？**
   参考已有的 `StreamingChatClient` 接口（`chat.go:24-26`），可以在 `Strategy` 上新增 `SummarizeStream` 方法，接受一个 `emit func(delta string) error` 回调。或者用 `io.Writer` 风格。当前的装饰器 `observedStrategy` 已经是分层结构，加新方法只需在装饰器里同样包装。

3. **`TranscribeChunks` 的分片策略由谁决定？是 Strategy 内部还是调用方？**
   调用方决定。`audioPaths []string` 参数表明分片在调用前就已完成（由 FFmpeg 等工具切分），Strategy 只负责逐片转录并拼接。这遵循了单一职责原则——音频切分是 FFmpeg 的事，文本转录是 ASR 的事。

---

## Q2. `Factory` 如何根据用户配置动态创建不同的 AI 客户端？请分析 `NewAnalysisStrategy` 的组装过程。

### 参考答案

工厂定义在 `internal/ai/factory.go:25-74`：

```go
// factory.go:25-29
type Factory struct{}

func NewFactory() *Factory {
    return &Factory{}
}
```

核心组装逻辑在 `factory.go:64-74`：

```go
// factory.go:64-74
func (f *Factory) NewAnalysisStrategy(profile Profile) (Strategy, error) {
    asr, err := f.NewASRStrategy(profile)
    if err != nil {
        return nil, err
    }
    chat, err := f.NewChatClient(profile)
    if err != nil {
        return nil, err
    }
    return &CompositeStrategy{asr: asr, chat: chat}, nil
}
```

ASR 和 Chat 通过 `profile` 中的 Provider 字段分别路由到不同实现（`factory.go:31-53`）。最终用 `CompositeStrategy` 组合为统一的 `Strategy` 接口。Provider 名通过 `normalizeProvider`（`factory.go:130-132`）统一转小写去空格，避免 `"Mimo"` vs `"mimo"` 之类的匹配失败。

### 追问链

1. **`Factory` 是无状态的（空 struct），为什么不直接用包级函数？**
   用 struct 可以在未来添加配置字段（如全局超时、日志级别）而不改变调用签名。同时也便于在测试中 mock——可以嵌入接口替换方法。当前的无状态设计体现了 YAGNI（You Aren't Gonna Need It），但保留了扩展空间。

2. **`NewASRStrategy` 对 `openai_compatible` 和 `siliconflow` 返回同一个 `SiliconFlowStrategy`（`factory.go:37-38`），这合理吗？**
   合理。硅基流动的 ASR 接口兼容 OpenAI 的 `/audio/transcriptions` 规范，所以 `SiliconFlowStrategy` 本身就是 OpenAI 兼容实现。用同一个结构体、不同的 `baseURL` 就能覆盖两种 provider。这是一种务实的复用，避免为名义不同的 provider 写重复代码。

3. **如果要新增一个 ASR provider（比如 Whisper 自部署），需要改哪些地方？**
   三步：(1) 新建 `whisper.go` 实现 `Strategy` 接口的 `Transcribe` 和 `TranscribeChunks`；(2) 在 `NewASRStrategy` 的 switch 里加一个 `case "whisper"` 分支；(3) 在 `Profile` 结构体中确保有对应的配置字段（已有 `ASRProvider`/`ASRBaseURL`/`ASRAPIKey`/`ASRModel`，足够）。上层代码无需改动。

---

## Q3. `CompositeStrategy` 如何将 ASR 和 Chat 组合为统一的 `Strategy` 接口？

### 参考答案

定义在 `internal/ai/factory.go:76-94`：

```go
// factory.go:76-79
type CompositeStrategy struct {
    asr  Strategy
    chat ChatClient
}

// factory.go:81-83
func (s *CompositeStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
    return s.asr.Transcribe(ctx, audioPath)
}

// factory.go:85-87
func (s *CompositeStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
    return s.asr.TranscribeChunks(ctx, audioPaths)
}

// factory.go:89-94
func (s *CompositeStrategy) Summarize(ctx context.Context, text string) (string, error) {
    return s.chat.Chat(ctx, []ChatMessage{
        {Role: "system", Content: defaultSummarySystemPrompt()},
        {Role: "user", Content: text},
    })
}
```

这是一个典型的 **组合模式（Composite）**。`Transcribe` 委托给 ASR 实现，`Summarize` 委托给 ChatClient，并注入默认的 system prompt。`CompositeStrategy` 本身也实现了 `Strategy` 接口，所以可以被 `ObservedStrategy` 再次包装。

### 追问链

1. **`Summarize` 直接硬编码了 system prompt，如果用户想自定义 prompt 怎么办？**
   当前设计确实将 prompt 固定在 `defaultSummarySystemPrompt()`（`mimo.go:177-185`）里。扩展方案：在 `CompositeStrategy` 中增加 `systemPrompt string` 字段，由 `Factory` 从 `Profile` 或额外配置中读取。或者引入 `SummarizeOption` 函数选项模式。

2. **`asr` 字段的类型是 `Strategy` 而不是 `ASRStrategy`，这意味着什么？**
   注意 `factory.go:77` 中 `asr` 的类型是 `Strategy`（整个接口），而不是一个更窄的 `ASRStrategy`。这意味着 ASR 实现也带有 `Summarize` 方法（虽然不会被调用）。这是一种接口复用——项目没有为 ASR 单独定义接口，因为所有 ASR 实现（MiMo、SiliconFlow）都是完整的 `Strategy`。代价是 `asr` 字段可以调用 `Summarize`，但这在组合上下文中不会被误用。

3. **如果 ChatClient 也实现了 `Strategy`，会不会产生无限递归？**
   不会。`CompositeStrategy.Summarize` 调用的是 `s.chat.Chat()`，不是 `s.chat.Summarize()`。`ChatClient` 接口（`chat.go:20-22`）只有 `Chat` 方法，与 `Strategy` 接口不重叠。组合时两个接口各司其职。

---

## Q4. 装饰器模式在 VidLens 中如何实现 AI 调用的可观测性？分析 `ObservedStrategy` 的结构。

### 参考答案

装饰器定义在 `internal/ai/observed.go:114-152`：

```go
// observed.go:114-125
type observedStrategy struct {
    base     Strategy
    recorder CallRecorder
    callCtx  CallContext
}

func NewObservedStrategy(base Strategy, recorder CallRecorder, callCtx CallContext) Strategy {
    if base == nil || recorder == nil || callCtx.UserID <= 0 {
        return base
    }
    return &observedStrategy{base: base, recorder: recorder, callCtx: callCtx}
}
```

每个方法都在调用前后记录指标（`observed.go:127-152`）：

```go
// observed.go:127-134
func (s *observedStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
    startedAt := time.Now()
    text, err := s.base.Transcribe(ctx, audioPath)
    callCtx := asrCallContext(s.callCtx)
    record := baseRecord(callCtx, startedAt, 0, utf8.RuneCountInString(text), err)
    _ = s.recorder.RecordAICall(ctx, record)
    return text, err
}
```

记录内容包括：耗时（`DurationMs`）、输入/输出字符数、成功/失败状态、错误信息。ASR 和 LLM 调用通过 `asrCallContext`（`observed.go:154-163`）和 `llmCallContext`（`observed.go:165-174`）分别设置 `Kind`、`Provider`、`Model`。

### 追问链

1. **`NewObservedStrategy` 中 `callCtx.UserID <= 0` 时直接返回原始 `base`，这是什么设计考量？**
   这是一个防御性的短路——没有有效 UserID 就无法记录有意义的调用日志（日志需要关联到用户）。直接返回原始对象避免了装饰器的开销，也避免写入无效数据。这是一种"零开销降级"策略。

2. **`baseRecord` 中错误信息被截断到 500 字符（`observed.go:184-186`），为什么？**
   ```go
   // observed.go:183-186
   if len(errMsg) > 500 {
       errMsg = errMsg[:500]
   }
   ```
   防止异常堆栈或大段 HTML 错误页面写入数据库，避免存储膨胀和日志污染。500 字符足以定位大多数 API 错误，又不会撑爆字段。

3. **为什么 `record` 方法忽略了 `RecordAICall` 的错误（`_ = c.recorder.RecordAICall`）？**
   观测性是"尽力而为"的——记录日志失败不应该影响核心业务流程（转录或总结）。如果记录失败就返回错误，用户会看到"总结成功但报了个日志错误"的奇怪状态。这是一种典型的"fire-and-forget"观测模式。

---

## Q5. `OpenAIChatClient` 如何实现 SSE 流式解析？分析 `StreamChat` 方法的关键步骤。

### 参考答案

流式实现在 `internal/ai/chat.go:103-165`：

```go
// chat.go:103-111
func (c *OpenAIChatClient) StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error {
    if emit == nil {
        return fmt.Errorf("stream emit 不能为空")
    }
    reqBody := map[string]interface{}{
        "model":    c.model,
        "stream":   true,
        "messages": messages,
    }
```

SSE 解析核心（`chat.go:136-164`）：

```go
// chat.go:136-164
    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, ":") {
            continue
        }
        if !strings.HasPrefix(line, "data:") {
            continue
        }
        data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
        if data == "[DONE]" {
            return nil
        }
        delta, err := parseChatCompletionStreamDelta(data)
        if err != nil {
            return err
        }
        if delta == "" {
            continue
        }
        if err := emit(delta); err != nil {
            return err
        }
    }
```

关键步骤：(1) 设置 `"stream": true`；(2) 用 `bufio.Scanner` 逐行读取；(3) 过滤空行和注释行（`:` 开头）；(4) 提取 `data:` 后的 JSON；(5) 遇到 `[DONE]` 终止；(6) 解析 delta 并回调 `emit`。

### 追问链

1. **`scanner.Buffer(make([]byte, 0, 4096), 1024*1024)` 中两个参数分别是什么作用？**
   第一个参数是初始缓冲区（4096 字节），第二个是最大缓冲区（1MB）。默认 `bufio.Scanner` 的最大 token 大小是 64KB，如果某个 SSE 行（如含大段代码的 delta）超过这个限制，Scanner 会报 `ErrTooLong`。显式设置 1MB 上限避免这种边缘情况。

2. **如果 `emit` 回调返回错误，`StreamChat` 立即终止。这在实际使用中会有什么影响？**
   `emit` 返回错误意味着消费者（如 WebSocket 推送）出了问题（连接断开、写缓冲满）。立即终止是正确的——继续解析并丢弃 delta 没有意义，而且 HTTP response body 会在 `StreamChat` 返回后被 `defer resp.Body.Close()` 关闭，释放连接。

3. **`parseChatCompletionStreamDelta` 解析失败时直接返回错误（`chat.go:152`），会不会因为一个坏掉的 delta 就中断整个流？**
   是的，这是有意的。OpenAI 兼容的 SSE 格式是严格的 JSON，如果某行解析失败，说明服务端返回了非预期格式，继续解析后续行可能得到更多错误。快速失败比默默吞掉错误更安全。如果需要容错，可以在调用层做降级。

---

## Q6. MiMo 策略和 OpenAI 客户端在认证方式上有什么关键差异？如何通过代码统一处理？

### 参考答案

MiMo 使用 `api-key` header（`internal/ai/mimo.go:133`）：

```go
// mimo.go:132-133
req.Header.Set("Content-Type", "application/json")
req.Header.Set("api-key", s.apiKey)
```

OpenAI 标准客户端使用 `Authorization: Bearer` 前缀（`chat.go:43-44`）：

```go
// chat.go:43-44
authHeader: "Authorization",
authPrefix: "Bearer ",
```

统一处理的关键在 `NewMimoChatClient`（`chat.go:50-55`）：

```go
// chat.go:50-55
func NewMimoChatClient(baseURL, apiKey, model string) *OpenAIChatClient {
    client := NewOpenAIChatClient(baseURL, apiKey, model)
    client.authHeader = "api-key"
    client.authPrefix = ""
    return client
}
```

通过将 `authHeader` 和 `authPrefix` 作为可配置字段，`OpenAIChatClient` 同时适配两种认证方式。发送请求时统一使用 `req.Header.Set(c.authHeader, c.authPrefix+c.apiKey)`（`chat.go:73`）。

### 追问链

1. **为什么不为 MiMo 单独实现一个 ChatClient？**
   MiMo 的 chat/completions API 与 OpenAI 兼容，唯一差异就是认证 header。用字段注入（`authHeader`/`authPrefix`）比写一个新结构体更简洁，也避免了重复 95% 的 HTTP 请求代码。这体现了"组合优于继承"的思想。

2. **`OpenAIChatClient` 的 `authHeader` 和 `authPrefix` 是私有字段，外部包无法修改。这是不是限制了扩展性？**
   确实如此。但 `NewMimoChatClient` 工厂函数在同一个 `ai` 包内，可以直接访问私有字段。如果外部包需要自定义认证，应该通过 `Factory.NewChatClient` 传入 `Profile`，由工厂内部处理。这是一种"受控扩展点"设计。

3. **如果未来需要支持 OAuth2 token 刷新，应该在哪个层面做？**
   在 `http.Client` 的 `Transport` 层。Go 的 `http.Transport` 支持自定义 `RoundTripper`，可以在请求发出前自动刷新过期 token 并注入 header。这样 `OpenAIChatClient` 的业务逻辑完全不需要改动。

---

## Q7. SiliconFlow 的 ASR 重试策略是如何实现的？分析指数退避的代码。

### 参考答案

重试逻辑在 `internal/ai/siliconflow.go:41-67`：

```go
// siliconflow.go:41-67
func (s *SiliconFlowStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
    var lastErr error

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
        if err == nil {
            return text, nil
        }
        lastErr = err

        // 客户端错误（如 401/400）不重试
        if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "400") {
            break
        }
    }

    return "", fmt.Errorf("ASR 重试 3 次后仍失败: %w", lastErr)
}
```

三个关键设计：(1) 最多重试 3 次；(2) 等待时间为 `1s, 2s, 4s`（`1<<(attempt-1)` 位移实现指数退避）；(3) 等待期间通过 `select` 监听 `ctx.Done()`，支持取消；(4) 401/400 客户端错误立即停止重试。

### 追问链

1. **为什么 401/400 不重试而其他错误（如 500/429）要重试？**
   401（认证失败）和 400（请求格式错误）是确定性的客户端错误，重试不会改变结果。而 500（服务端内部错误）和 429（限流）是暂时性的，稍后重试可能成功。这是 HTTP 重试的通用原则。

2. **`time.After(waitTime)` 在 context 取消时会泄漏 timer 吗？**
   会。`time.After` 返回的 channel 在 timer 到期前不会被 GC。对于 3 次重试、最长 4 秒等待的场景，泄漏量可以忽略。如果要严格避免，可以用 `time.NewTimer` + `defer timer.Stop()`。但在这种低频调用场景下，代码简洁性更重要。

3. **重试间隔是 1s/2s/4s，没有加随机抖动（jitter）。在高并发场景下会有什么问题？**
   如果大量请求同时失败并以相同的间隔重试，会在同一时刻产生请求风暴（thundering herd）。标准做法是加随机抖动，如 `waitTime * (0.5 + rand.Float64())`。当前 VidLens 是单用户场景，ASR 请求是串行的，所以没有这个问题。

---

## Q8. MiMo 的 ASR 实现为什么将音频编码为 base64 通过 chat/completions 发送？这与 SiliconFlow 的 multipart 上传有什么区别？

### 参考答案

MiMo ASR 实现在 `internal/ai/mimo.go:50-81`：

```go
// mimo.go:50-57
func (s *MimoStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
    dataURL, err := audioDataURL(audioPath)
    if err != nil {
        return "", err
    }
    if len(dataURL) > mimoMaxAudioDataBytes {
        return "", fmt.Errorf("MiMo ASR 音频 base64 超过 10MB，请压缩音频或按片段转录")
    }

// mimo.go:59-78
    reqBody := map[string]interface{}{
        "model":  s.asrModel,
        "stream": false,
        "messages": []map[string]interface{}{
            {
                "role": "user",
                "content": []map[string]interface{}{
                    {
                        "type": "input_audio",
                        "input_audio": map[string]string{
                            "data": dataURL,
                        },
                    },
                },
            },
        },
        "asr_options": map[string]string{
            "language": "auto",
        },
    }
```

base64 编码辅助函数在 `mimo.go:163-175`：

```go
// mimo.go:163-175
func audioDataURL(audioPath string) (string, error) {
    fileBytes, err := os.ReadFile(audioPath)
    if err != nil {
        return "", fmt.Errorf("读取音频文件失败: %w", err)
    }

    mimeType := "audio/mpeg"
    if strings.EqualFold(filepath.Ext(audioPath), ".wav") {
        mimeType = "audio/wav"
    }

    return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(fileBytes)), nil
}
```

MiMo 将音频作为 `input_audio` 类型的消息内容嵌入 chat/completions 请求，本质上是把 ASR 当作多模态对话。SiliconFlow 则用标准的 multipart form 上传到 `/audio/transcriptions`（`siliconflow.go:100-142`）。区别在于 API 设计哲学：MiMo 走多模态统一接口，SiliconFlow 走专用 ASR 接口。

### 追问链

1. **base64 编码会使文件体积膨胀约 33%，MiMo 为什么还选择这种方式？**
   因为 MiMo 的 ASR 是 chat/completions 的扩展，不是独立端点。chat/completions 的请求体是 JSON，JSON 不支持二进制数据，只能用 base64。这是 API 设计约束决定的，不是技术偏好。`mimoMaxAudioDataBytes = 10 * 1024 * 1024`（`mimo.go:17`）的限制也与此相关——10MB 的 base64 约对应 7.5MB 原始音频。

2. **`audioDataURL` 只识别 `.mp3` 和 `.wav`，如果传入 `.ogg` 或 `.m4a` 会怎样？**
   默认使用 `audio/mpeg` MIME 类型（`mimo.go:169`），对于 `.ogg`/`.m4a` 等格式可能不正确。但实际影响取决于 MiMo 服务端是否严格校验 MIME type。如果要更健壮，可以用 `mime.TypeByExtension` 或维护一个扩展名到 MIME 的映射表。

3. **MiMo 的 `asr_options` 中 `"language": "auto"` 有什么作用？**
   告诉 ASR 引擎自动检测音频语言，而不是假设为中文或英文。对于 VidLens 这种处理各种视频的工具，自动检测是正确的默认值。如果用户明确知道语言，可以配置为具体值以提高准确率。

---

## Q9. `AIProfileTester` 如何验证用户配置的 AI 服务是否可用？分析其健康检查策略。

### 参考答案

接口定义在 `internal/service/ai_profile.go:20-22`：

```go
// service/ai_profile.go:20-22
type AIProfileTester interface {
    TestProfile(ctx context.Context, profile *DecryptedAIProfile) error
}
```

实现在 `internal/ai/factory.go:96-128`：

```go
// factory.go:96-102
type ProfileTester struct {
    factory *Factory
}

func NewProfileTester(factory *Factory) *ProfileTester {
    return &ProfileTester{factory: factory}
}

// factory.go:104-128
func (t *ProfileTester) TestProfile(ctx context.Context, profile Profile) error {
    chatClient, err := t.factory.NewChatClient(profile)
    if err != nil {
        return err
    }
    if _, err := chatClient.Chat(ctx, []ChatMessage{
        {Role: "system", Content: "Return a short health check response."},
        {Role: "user", Content: "ping"},
    }); err != nil {
        return err
    }

    embeddingClient, err := t.factory.NewEmbeddingClient(profile)
    if err != nil {
        return err
    }
    vector, err := embeddingClient.Embed(ctx, "VidLens embedding health check")
    if err != nil {
        return err
    }
    if profile.EmbeddingDim > 0 && len(vector) != profile.EmbeddingDim {
        return fmt.Errorf("Embedding 维度不匹配: 返回 %d，配置 %d", len(vector), profile.EmbeddingDim)
    }
    return nil
}
```

健康检查策略：(1) 发送一个最小的 chat 请求验证 LLM 连通性；(2) 发送一个 embedding 请求验证向量服务连通性；(3) 校验返回的向量维度是否与配置一致。注意没有测试 ASR——因为 ASR 需要真实音频文件，不适合健康检查。

### 追问链

1. **为什么只检查 Chat 和 Embedding，不检查 ASR？**
   ASR 健康检查需要一个音频样本文件。要么打包一个测试音频到代码库里（增加二进制体积），要么运行时生成（需要 FFmpeg）。相比之下，Chat 和 Embedding 的检查只需要发一段文本，零成本。ASR 的连通性在实际任务执行时自然会被验证。

2. **`EmbeddingDim` 校验（`factory.go:124-126`）的意义是什么？**
   用户配置的 `EmbeddingDim` 影响数据库中向量列的维度。如果实际返回的维度与配置不一致，后续的向量检索会失败。在健康检查时发现并报错，比运行时才发现要好得多。这是一个"fail fast"策略。

3. **`TestProfile` 中的 `chatClient.Chat` 返回值被丢弃了（`factory.go:109` 的 `_`），为什么不验证返回内容？**
   健康检查只关心"能不能通"，不关心"返回什么"。只要不报错，就说明 API key 有效、网络连通、模型可用。验证返回内容属于功能测试，不是健康检查的职责。

---

## Q10. VidLens 的 AI 策略层测试策略有什么特点？分析 `httptest` 在其中的使用。

### 参考答案

**装饰器测试**（`internal/ai/observed_test.go:17-44`）使用 fake 实现：

```go
// observed_test.go:17-44
func TestObservedStrategyRecordsASRAndLLMProviderModelsSeparately(t *testing.T) {
    recorder := &recordingCallRecorder{}
    strategy := NewObservedStrategy(&recordingStrategy{}, recorder, CallContext{
        UserID:      7,
        TaskID:      42,
        ASRProvider: "mimo",
        ASRModel:    "mimo-v2.5-asr",
        LLMProvider: "openai_compatible",
        LLMModel:    "chat-model",
    })

    if _, err := strategy.Transcribe(context.Background(), "audio.mp3"); err != nil {
        t.Fatalf("Transcribe() error = %v", err)
    }
    if _, err := strategy.Summarize(context.Background(), "转写文本"); err != nil {
        t.Fatalf("Summarize() error = %v", err)
    }

    if len(recorder.records) != 2 {
        t.Fatalf("records = %d, want 2", len(recorder.records))
    }
```

**API 客户端测试**（`internal/ai/chat_embedding_test.go:12-50`）使用 `httptest.NewServer`：

```go
// chat_embedding_test.go:12-50
func TestOpenAIChatClientPostsChatCompletions(t *testing.T) {
    var gotPath string
    var gotAuth string
    var gotModel string
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotPath = r.URL.Path
        gotAuth = r.Header.Get("Authorization")

        var body struct {
            Model    string        `json:"model"`
            Messages []ChatMessage `json:"messages"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            t.Fatalf("decode request: %v", err)
        }
        gotModel = body.Model
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
    }))
    defer server.Close()

    client := NewOpenAIChatClient(server.URL+"/v1", "sk-chat", "chat-model")
```

**MiMo 测试**（`internal/ai/mimo_test.go:14-51`）验证非标准 header：

```go
// mimo_test.go:14-51
func TestMimoTranscribeSendsAudioChatCompletion(t *testing.T) {
    var captured map[string]interface{}
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/chat/completions" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        if got := r.Header.Get("api-key"); got != "tp-test-key" {
            t.Fatalf("unexpected api-key header: %q", got)
        }
```

三层测试策略：(1) 装饰器用 fake 实现测试逻辑正确性；(2) HTTP 客户端用 `httptest` 测试请求构造和响应解析；(3) 验证非标准行为（如 MiMo 的 `api-key` header）。

### 追问链

1. **`httptest.NewServer` 的 handler 在测试结束后会怎样？**
   `defer server.Close()` 关闭监听器，但已建立的连接可能还在处理中。`httptest` 的实现会等待所有活跃请求完成后才真正关闭。这保证了测试的确定性——不会因为 server 提前关闭而导致假失败。

2. **`TestObservedChatClientDoesNotExposeStreamingWhenBaseDoesNotStream`（`observed_test.go:8-15`）测试的是什么场景？**
   ```go
   // observed_test.go:8-15
   func TestObservedChatClientDoesNotExposeStreamingWhenBaseDoesNotStream(t *testing.T) {
       base := &nonStreamingChatClient{}
       wrapped := NewObservedChatClient(base, discardCallRecorder{}, CallContext{UserID: 7})

       if _, ok := wrapped.(StreamingChatClient); ok {
           t.Fatal("observed wrapper exposed StreamChat for a non-streaming base client")
       }
   }
   ```
   确保当底层 ChatClient 不支持流式时，observed 包装器不会暴露 `StreamingChatClient` 接口。这是类型安全的关键——如果上层代码通过类型断言检查 `StreamingChatClient`，一个不支持流式的 observed 包装器不应该骗过这个检查。看 `observed.go:58-61`，实现上确实用 `ok` 检测了 base 是否支持流式。

3. **为什么 `mimo_test.go` 用 `mustFindInputAudioData` 辅助函数（`mimo_test.go:109-137`）来提取 base64 数据，而不是直接断言整个请求体？**
   因为请求体中嵌套了 base64 编码的音频数据（可能很长），直接断言整个 JSON 不现实。辅助函数逐层解嵌套（messages -> content -> input_audio -> data）精准定位到需要验证的字段。这也使测试意图更清晰——只关心"是否正确编码了音频"，不关心其他字段的排列顺序。
