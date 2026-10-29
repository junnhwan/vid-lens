package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/outbound"
)

type remoteSubmitKey struct{}
type modelActivityKey struct{}
type serialArticleToolsKey struct{}
type safeTransport struct{ base http.RoundTripper }

func (t safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if submit, ok := req.Context().Value(remoteSubmitKey{}).(func() error); ok {
		if e := submit(); e != nil {
			return nil, e
		}
	}
	observer, _ := req.Context().Value(usageKey{}).(*usageObserver)
	if get, ok := req.Context().Value(probeObserverKey{}).(func() *usageObserver); ok {
		observer = get()
	}
	if observer != nil {
		observer.wire(req)
	}
	r, e := t.base.RoundTrip(req)
	if e != nil {
		if observer != nil {
			observer.finish(false)
		}
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型连接暂不可用。", Transient: true}
	}
	if observer != nil {
		observer.report.HTTPStatus = domain.Ptr(r.StatusCode)
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		if observer != nil {
			// Record bounded, redacted diagnostics for business calls as well as
			// capability checks. Never persist a credential echoed by a gateway.
			redacted := string(body)
			for _, value := range []string{req.Header.Get("Authorization"), strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")} {
				if len(value) > 6 {
					redacted = strings.ReplaceAll(redacted, value, "[redacted]")
				}
			}
			observer.providerFailure([]byte(redacted))
			observer.finish(false)
		}
		slog.Warn("model service HTTP failure", "http_status", r.StatusCode)
		r.Body.Close()
		retry := r.StatusCode == 429 || r.StatusCode == 408 || r.StatusCode >= 500
		code := "PROVIDER_UNAVAILABLE"
		if r.StatusCode == 429 {
			code = "PROVIDER_RATE_LIMITED"
		}
		if (r.StatusCode == 400 || r.StatusCode == 413 || r.StatusCode == 422) && providerContextExceeded(body) {
			return nil, &domain.WorkError{Code: "MODEL_CONTEXT_EXCEEDED", Message: "模型线路拒绝了本次上下文容量；来源和草稿已保留，未原样自动重试。"}
		}
		var delay time.Duration
		if v, e := strconv.Atoi(r.Header.Get("Retry-After")); e == nil {
			delay = time.Duration(v) * time.Second
		} else if t, e := http.ParseTime(r.Header.Get("Retry-After")); e == nil {
			delay = time.Until(t)
		}
		if retry {
			delay = max(delay, providerRetryAfter(body, 0))
		}
		return nil, &domain.WorkError{Code: code, Message: "模型服务未能完成请求。", Transient: retry, RetryAfter: max(time.Duration(0), delay)}
	}
	if observer != nil {
		observer.report.RequestID = bounded(r.Header.Get("X-Request-ID"))
		observer.jsonBody = !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream")
		r.Body = &observedBody{ReadCloser: r.Body, o: observer}
	}
	return r, nil
}

// Compatible relays sometimes preserve a structured retry policy inside the
// error JSON (including a JSON-encoded message) instead of an HTTP header.
// Read bounded numeric guidance only; body text is never an instruction.
func providerRetryAfter(body []byte, depth int) time.Duration {
	if depth > 4 {
		return 0
	}
	var encoded string
	if json.Unmarshal(body, &encoded) == nil {
		return providerRetryAfter([]byte(encoded), depth+1)
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(body, &value) != nil {
		return 0
	}
	var policy struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(value["retry_policy"], &policy) == nil && policy.RetryAfter > 0 && policy.RetryAfter <= 86400 {
		return time.Duration(policy.RetryAfter * float64(time.Second))
	}
	for _, key := range []string{"error", "message"} {
		if delay := providerRetryAfter(value[key], depth+1); delay > 0 {
			return delay
		}
	}
	return 0
}

func providerContextExceeded(body []byte) bool {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	e := envelope.Error
	text := strings.ToLower(e.Code + " " + e.Type + " " + e.Message)
	for _, marker := range []string{"context_length_exceeded", "context_window_exceeded", "maximum context length", "longer than the model's context window", "prompt is too long", "too many input tokens"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
func (e *Engine) baseModel(ctx context.Context, p config.Profile) (model.ToolCallingChatModel, error) {
	key, err := e.Store.Credential(ctx, p.ModelKeyRef)
	if err != nil {
		return nil, err
	}
	allow := append([]string{}, e.Config.ModelAllowedHosts...)
	if e.Config.DevMediaDir != "" {
		if u, err := url.Parse(p.ModelBaseURL); err == nil {
			allow = append(allow, u.Host)
		}
	}
	return NewModel(ctx, p, key, e.Config.ProxyURL, allow...)
}
func NewModel(ctx context.Context, p config.Profile, key, proxy string, allowed ...string) (model.ToolCallingChatModel, error) {
	var options []outbound.CredentialOptions
	if capability, _ := ctx.Value(capabilityCheckKey{}).(bool); capability && reasoningGPT(p.ModelName) {
		// Non-streaming reasoning checks return headers only after generation.
		// Their wait must use the probe deadline rather than the streaming default.
		options = append(options, outbound.CredentialOptions{ResponseHeaderTimeout: 5 * time.Minute})
	}
	client, err := outbound.CredentialClient(ctx, p.ModelBaseURL, proxy, p.ModelProxyPolicy, allowed, modelHTTPTimeout(ctx), options...)
	if err != nil {
		return nil, domain.Err(503, "MODEL_UNAVAILABLE", "模型端点或代理不符合服务器出站策略。")
	}
	strict, _ := ctx.Value(nativeStrictKey{}).(bool)
	client.Transport = safeTransport{client.Transport}
	if strict && nativeStrictEndpoint(p.ModelBaseURL) {
		client.Transport = strictTransport{client.Transport}
	}
	return newModelWithClient(ctx, p, key, client)
}

func newModelWithClient(ctx context.Context, p config.Profile, key string, client *http.Client) (model.ToolCallingChatModel, error) {
	extra := map[string]any{}
	if serial, _ := ctx.Value(serialArticleToolsKey{}).(bool); serial {
		// The article Interface accepts one action per durable turn. Declare the
		// same constraint to the provider instead of inviting parallel tool calls
		// that cannot be applied atomically as a single authoring action.
		extra["parallel_tool_calls"] = false
	}
	if p.ModelDisableThinking {
		if strings.Contains(strings.ToLower(p.ModelName), "deepseek") {
			extra["thinking"] = map[string]string{"type": "disabled"}
		} else {
			extra["enable_thinking"] = false
		}
	}
	cfg := &openai.ChatModelConfig{ExtraFields: extra, BaseURL: strings.TrimRight(p.ModelBaseURL, "/"), Model: p.ModelName, APIKey: key, HTTPClient: client, MaxTokens: domain.Ptr(p.MaxOutputTokens), Temperature: domain.Ptr(float32(0.2))}
	if reasoningGPT(p.ModelName) {
		// Reasoning GPTs use a completion budget that includes reasoning tokens.
		// Omit sampling settings rather than forcing a value rejected by the SDK
		// or by upstream reasoning modes.
		cfg.MaxTokens = nil
		cfg.MaxCompletionTokens = domain.Ptr(p.MaxOutputTokens)
		cfg.Temperature = nil
	}
	return openai.NewChatModel(ctx, cfg)
}

func reasoningGPT(name string) bool {
	name = strings.TrimPrefix(name, "openai/")
	return name == "gpt-5" || strings.HasPrefix(name, "gpt-5.") || strings.HasPrefix(name, "gpt-5-") || name == "gpt-6" || strings.HasPrefix(name, "gpt-6.") || strings.HasPrefix(name, "gpt-6-")
}

// Some Chat Completions providers accept images only in user messages. Keep the
// complete tool-call/result exchange and append its real pixels at the transport boundary.
func NormalizeToolImages(input []*schema.Message) []*schema.Message {
	out := []*schema.Message{}
	pending := []schema.MessageInputPart{}
	flush := func() {
		if len(pending) > 0 {
			out = append(out, &schema.Message{Role: schema.User, UserInputMultiContent: pending})
			pending = nil
		}
	}
	for _, m := range input {
		if m.Role != schema.Tool {
			flush()
			copy := *m
			if len(copy.UserInputMultiContent) > 0 && copy.Content != "" {
				copy.UserInputMultiContent = append([]schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: copy.Content}}, copy.UserInputMultiContent...)
				copy.Content = ""
			}
			out = append(out, &copy)
			continue
		}
		copy := *m
		parts := []schema.MessageInputPart{}
		hasImage := false
		for _, p := range m.UserInputMultiContent {
			if p.Type == schema.ChatMessagePartTypeImageURL {
				if !hasImage {
					pending = append(pending, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: "实际工具图片，call_id=" + m.ToolCallID + "；帧及时间以工具元信息为准。"})
					hasImage = true
				}
				pending = append(pending, p)
			} else {
				parts = append(parts, p)
			}
		}
		if hasImage {
			copy.UserInputMultiContent = nil
			copy.Content = ""
			for _, p := range parts {
				copy.Content += p.Text + "\n"
			}
		}
		out = append(out, &copy)
	}
	flush()
	return out
}

func (s *session) modelImageInput(input []*schema.Message) []*schema.Message {
	out := NormalizeToolImages(input)
	c := s.cp.ExecutionContract
	if c == nil || (c.Version != domain.ArticleLoopV2 && !domain.QuestionSemanticReadingContract(c.InputRevision)) {
		return out
	}
	move := false
	for i, m := range out {
		for _, part := range m.UserInputMultiContent {
			move = move || (part.Type == schema.ChatMessagePartTypeImageURL && m.Role == schema.User && i != len(out)-1)
		}
	}
	if !move {
		return out
	}
	// Some compatible relays forward pixels only from the final user message.
	// Preserve the frozen text and complete tool exchanges; relocate the exact
	// hydrated pixels, never a caption or a replacement image, at this boundary.
	var pixels []schema.MessageInputPart
	result := make([]*schema.Message, 0, len(out)+1)
	for _, m := range out {
		copy := *m
		copy.UserInputMultiContent = nil
		for _, part := range m.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL && m.Role == schema.User {
				if id, ok := part.Extra["frame_id"].(string); ok {
					pixels = append(pixels, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: "frame_id=" + id})
				}
				pixels = append(pixels, part)
			} else {
				copy.UserInputMultiContent = append(copy.UserInputMultiContent, part)
			}
		}
		if copy.Role == schema.User && copy.Content == "" && len(copy.UserInputMultiContent) == 0 {
			continue
		}
		result = append(result, &copy)
	}
	if len(pixels) == 0 {
		return out
	}
	result = append(result, &schema.Message{Role: schema.User, UserInputMultiContent: pixels})
	return result
}

func completeModelResponse(ctx context.Context, base model.ToolCallingChatModel, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return receiveModelResponse(ctx, base, in, modelResponseLimits{Total: 300 * time.Second, First: 90 * time.Second, Idle: 60 * time.Second}, opts...)
}

type modelResponseLimits struct {
	Total, First, Idle time.Duration
	NarrowOnDeadline   bool
	BudgetDeadline     bool
}

var errEmptyModelResponse = &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型响应已结束，但没有正文或工具动作；空响应未作为有效结果。", Transient: true}

// Consume provider streaming internally. Public events still contain only durable
// activities and finalized results; a long completion must not wait for its entire
// body before satisfying the response-header deadline.
func receiveModelResponse(ctx context.Context, base model.ToolCallingChatModel, in []*schema.Message, limits modelResponseLimits, opts ...model.Option) (*schema.Message, error) {
	deadline, stop := context.WithTimeout(ctx, limits.Total)
	defer stop()
	call, cancel := context.WithCancelCause(deadline)
	defer cancel(nil)
	timeout := func(message string) error {
		return &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: message, Transient: true}
	}
	first := time.AfterFunc(limits.First, func() { cancel(timeout("模型未在期限内返回有效响应。")) })
	defer first.Stop()
	var idle *time.Timer
	hadActivity := false
	defer func() {
		if idle != nil {
			idle.Stop()
		}
	}()
	failed := func(err error) error {
		if cause := context.Cause(call); cause != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(cause, context.DeadlineExceeded) {
				if limits.BudgetDeadline {
					return &domain.WorkError{Code: "MODEL_CALL_BUDGET_EXHAUSTED", Message: "模型调用达到冻结总时长预算；这不代表响应流无进展，未完成结果保留为未知，不重发原输入。"}
				}
				if limits.NarrowOnDeadline && hadActivity {
					return &domain.WorkError{Code: "MODEL_RESPONSE_BUDGET_EXHAUSTED", Message: "响应已开始但未在本次冻结时间内完整结束；发送结果仍为未知，下一调用须缩小工作范围。"}
				}
				return timeout("模型单次调用超过期限。")
			}
			return cause
		}
		return err
	}
	stream, err := base.Stream(call, in, opts...)
	if err != nil {
		return nil, failed(err)
	}
	defer stream.Close()
	chunks := []*schema.Message{}
	complete := false
	bytes := 0
	activityName, activityPrefix, activityIntent := "", "", ""
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, failed(timeout("模型响应流意外中断。"))
		}
		if chunk == nil {
			continue
		}

		if activity, ok := call.Value(modelActivityKey{}).(func(string, string) error); ok {
			for _, tc := range chunk.ToolCalls {
				if tc.Index != nil && *tc.Index != 0 {
					continue
				}
				if tc.Function.Name != "" {
					activityName = tc.Function.Name
				}
				if activityIntent == "" && len(activityPrefix) < 4096 {
					remaining := 4096 - len(activityPrefix)
					activityPrefix += tc.Function.Arguments[:min(len(tc.Function.Arguments), remaining)]
					activityIntent = streamedActivityTitle(activityPrefix)
				}
				if err := activity(activityName, activityIntent); err != nil {
					return nil, err
				}
			}
		}
		meaningful := chunk.Content != "" || chunk.ReasoningContent != ""
		for _, tc := range chunk.ToolCalls {
			meaningful = meaningful || tc.Function.Name != "" || tc.Function.Arguments != ""
		}
		if v, ok := chunk.Extra["reasoning-content"].(string); ok && v != "" {
			meaningful = true
		}
		if meaningful {
			hadActivity = true
			first.Stop()
			if idle == nil {
				idle = time.AfterFunc(limits.Idle, func() { cancel(timeout("模型响应流长时间没有新内容。")) })
			} else {
				idle.Reset(limits.Idle)
			}
		}
		if chunk.ResponseMeta != nil && chunk.ResponseMeta.FinishReason != "" {
			complete = true
		}
		bytes += len(chunk.Content) + len(chunk.ReasoningContent)
		for _, tc := range chunk.ToolCalls {
			bytes += len(tc.Function.Arguments)
		}
		if bytes > 8<<20 || len(chunks) >= 100000 {
			return nil, timeout("模型响应超过接收上限。")
		}
		clean := *chunk
		clean.ReasoningContent = ""
		clean.Extra = cleanExtra(chunk.Extra)
		chunks = append(chunks, &clean)
	}
	if err := context.Cause(call); err != nil {
		return nil, failed(err)
	}
	if !complete {
		return nil, timeout("模型响应未完整结束，尚未保存为结果。")
	}
	result, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, timeout("模型响应片段无法组合。")
	}
	if result.ResponseMeta.FinishReason == "length" {
		return nil, &domain.WorkError{Code: "MODEL_OUTPUT_TRUNCATED", Message: "模型输出达到长度上限，未产生完整结果。"}
	}
	// A stop marker and billed reasoning tokens do not make an empty response
	// a usable agent turn. Keep it in bounded transport recovery, so it cannot
	// terminate an unfinished draft or consume its content-correction budget.
	if len(result.ToolCalls) == 0 && strings.TrimSpace(result.Content) == "" {
		return nil, errEmptyModelResponse
	}
	return result, nil
}
func (m *meteredModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, in, opts...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{v}), nil
}

func providerID(m *schema.Message) string {
	if v, ok := m.Extra["openai-request-id"].(string); ok {
		return v
	}
	return ""
}
