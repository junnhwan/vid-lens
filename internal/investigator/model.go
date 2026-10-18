package investigator

import (
	"context"
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
		return nil, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型连接暂不可用。", Transient: true}
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		if observer != nil {
			observer.finish(false)
		}
		slog.Warn("model service HTTP failure", "http_status", r.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4096))
		r.Body.Close()
		retry := r.StatusCode == 429 || r.StatusCode == 408 || r.StatusCode >= 500
		code := "PROVIDER_UNAVAILABLE"
		if r.StatusCode == 429 {
			code = "PROVIDER_RATE_LIMITED"
		}
		var delay time.Duration
		if v, e := strconv.Atoi(r.Header.Get("Retry-After")); e == nil {
			delay = time.Duration(v) * time.Second
		} else if t, e := http.ParseTime(r.Header.Get("Retry-After")); e == nil {
			delay = time.Until(t)
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
	client, err := outbound.CredentialClient(ctx, p.ModelBaseURL, proxy, p.ModelProxyPolicy, allowed, 300*time.Second)
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

func completeModelResponse(ctx context.Context, base model.ToolCallingChatModel, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return receiveModelResponse(ctx, base, in, modelResponseLimits{Total: 300 * time.Second, First: 90 * time.Second, Idle: 60 * time.Second}, opts...)
}

type modelResponseLimits struct{ Total, First, Idle time.Duration }

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

		if activity, ok := call.Value(modelActivityKey{}).(func(string) error); ok {
			for _, tc := range chunk.ToolCalls {
				if err := activity(tc.Function.Name); err != nil {
					return nil, err
				}
			}
		}
		meaningful := chunk.Content != "" || chunk.ReasoningContent != "" || len(chunk.ToolCalls) > 0
		if v, ok := chunk.Extra["reasoning-content"].(string); ok && v != "" {
			meaningful = true
		}
		if meaningful {
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
