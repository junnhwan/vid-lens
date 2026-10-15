package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
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
)

type remoteSubmitKey struct{}
type safeTransport struct{ base http.RoundTripper }

func (t safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if submit, ok := req.Context().Value(remoteSubmitKey{}).(func() error); ok {
		if e := submit(); e != nil {
			return nil, e
		}
	}
	r, e := t.base.RoundTrip(req)
	if e != nil {
		return nil, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型连接暂不可用。", Transient: true}
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
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
	return r, nil
}
func (e *Engine) baseModel(ctx context.Context, p config.Profile) (model.ToolCallingChatModel, error) {
	key, err := e.Store.Credential(ctx, p.ModelKeyRef)
	if err != nil {
		return nil, err
	}
	return NewModel(ctx, p, key, e.Config.ProxyURL)
}
func NewModel(ctx context.Context, p config.Profile, key, proxy string) (model.ToolCallingChatModel, error) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, ResponseHeaderTimeout: 90 * time.Second, MaxIdleConnsPerHost: 2}
	if proxy != "" {
		u, e := url.Parse(proxy)
		if e != nil {
			return nil, e
		}
		tr.Proxy = http.ProxyURL(u)
	}
	client := &http.Client{Transport: safeTransport{tr}, Timeout: 300 * time.Second}
	extra := map[string]any{}
	if p.ModelDisableThinking {
		extra["enable_thinking"] = false
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{ExtraFields: extra, BaseURL: strings.TrimRight(p.ModelBaseURL, "/"), Model: p.ModelName, APIKey: key, HTTPClient: client, MaxTokens: domain.Ptr(p.MaxOutputTokens), Temperature: domain.Ptr(float32(0.2))})
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

type meteredModel struct {
	session *session
	base    model.ToolCallingChatModel
}

func (m *meteredModel) WithTools(t []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	b, e := m.base.WithTools(t)
	if e != nil {
		return nil, e
	}
	return &meteredModel{session: m.session, base: b}, nil
}
func (m *meteredModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	s := m.session
	key := "model:" + strconv.Itoa(s.cp.Calls)
	args := stripPixels(in)
	for local := 0; local < 2; local++ {
		op, e := s.engine.Store.BeginOperation(ctx, s.claim, key, "model", args, false)
		if e != nil {
			return nil, e
		}
		if op.Status == "completed" {
			var saved schema.Message
			if e = json.Unmarshal(op.Result, &saved); e != nil {
				return nil, e
			}
			return &saved, nil
		}
		actx, release, e := s.engine.Resources.Acquire(ctx, s.claim, "ai")
		if e != nil {
			return nil, e
		}
		sub, cancel := context.WithTimeout(actx, 300*time.Second)
		sub = context.WithValue(sub, remoteSubmitKey{}, func() error { _, e := s.engine.Store.BeginOperation(sub, s.claim, key, "model", args, true); return e })
		result, e := m.base.Generate(sub, NormalizeToolImages(in), opts...)
		cancel()
		release()
		if e != nil {
			var work *domain.WorkError
			if errors.As(e, &work) {
				if work.Transient && local == 0 && op.Submissions < 3 && work.RetryAfter <= time.Second {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(time.Second):
					}
					continue
				}
				return nil, work
			}
			secret, _ := s.engine.Store.Credential(ctx, s.profile.ModelKeyRef)
			safe := e.Error()
			if secret != "" {
				safe = strings.ReplaceAll(safe, secret, "[redacted]")
			}
			if len(safe) > 1500 {
				safe = safe[:1500]
			}
			slog.Warn("model invocation rejected", "diagnostic", safe)
			return nil, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "模型请求格式不兼容。", Transient: false}
		}
		result.ReasoningContent = ""
		result.Extra = cleanExtra(result.Extra)
		var usage any
		if result.ResponseMeta != nil {
			usage = result.ResponseMeta.Usage
		}
		if e = s.engine.Store.FinishOperation(ctx, s.claim, key, result, providerID(result), usage); e != nil {
			return nil, e
		}
		return result, nil
	}
	return nil, errors.New("model retry exhausted")
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
