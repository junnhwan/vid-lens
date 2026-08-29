package ai

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"
)

type CallContext struct {
	UserID            int64
	TaskID            int64
	JobID             int64
	SessionID         int64
	TraceID           string
	JobType           string
	Stage             string
	Attempt           int
	Provider          string
	Model             string
	ASRProvider       string
	ASRModel          string
	LLMProvider       string
	LLMModel          string
	Kind              string
	ASRSeconds        int
	PromptTokens      *int64
	CompletionTokens  *int64
	TotalTokens       *int64
	EstimatedCost     *float64
	TokenEstimated    bool
	Currency          string
	PriceVersion      string
	ProviderRequestID string
}

type CallRecord struct {
	UserID            int64
	TaskID            int64
	JobID             int64
	SessionID         int64
	TraceID           string
	JobType           string
	Stage             string
	Attempt           int
	Kind              string
	Provider          string
	Model             string
	Status            string
	DurationMs        int64
	InputChars        int
	OutputChars       int
	ASRSeconds        int
	PromptTokens      *int64
	CompletionTokens  *int64
	TotalTokens       *int64
	EstimatedCost     *float64
	TokenEstimated    bool
	Currency          string
	PriceVersion      string
	ProviderRequestID string
	ErrorCode         string
	ErrorMsg          string
}

type CallRecorder interface {
	RecordAICall(ctx context.Context, record CallRecord) error
}

type observedChatClient struct {
	base     ChatClient
	recorder CallRecorder
	callCtx  CallContext
}

func NewObservedChatClient(base ChatClient, recorder CallRecorder, callCtx CallContext) ChatClient {
	if base == nil || recorder == nil {
		return base
	}
	callCtx = llmCallContext(callCtx)
	if streaming, ok := base.(StreamingChatClient); ok {
		return &observedStreamingChatClient{observedChatClient: observedChatClient{base: base, recorder: recorder, callCtx: callCtx}, streaming: streaming}
	}
	return &observedChatClient{base: base, recorder: recorder, callCtx: callCtx}
}

func (c *observedChatClient) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	startedAt := time.Now()
	answer, err := c.base.Chat(ctx, messages)
	c.record(ctx, startedAt, countChatMessageChars(messages), utf8.RuneCountInString(answer), err)
	return answer, err
}

type observedStreamingChatClient struct {
	observedChatClient
	streaming StreamingChatClient
}

func (c *observedStreamingChatClient) StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error {
	startedAt := time.Now()
	var outputChars int
	err := c.streaming.StreamChat(ctx, messages, func(delta string) error { outputChars += utf8.RuneCountInString(delta); return emit(delta) })
	c.record(ctx, startedAt, countChatMessageChars(messages), outputChars, err)
	return err
}
func (c *observedChatClient) record(ctx context.Context, startedAt time.Time, inputChars, outputChars int, err error) {
	recordCall(ctx, c.recorder, baseRecord(ctx, c.callCtx, startedAt, inputChars, outputChars, err))
}

type observedEmbeddingClient struct {
	base     EmbeddingClient
	recorder CallRecorder
	callCtx  CallContext
}

func NewObservedEmbeddingClient(base EmbeddingClient, recorder CallRecorder, callCtx CallContext) EmbeddingClient {
	if base == nil || recorder == nil {
		return base
	}
	callCtx.Kind = model.AICallKindEmbedding
	return &observedEmbeddingClient{base: base, recorder: recorder, callCtx: callCtx}
}
func (c *observedEmbeddingClient) Embed(ctx context.Context, input string) ([]float32, error) {
	startedAt := time.Now()
	vector, err := c.base.Embed(ctx, input)
	recordCall(ctx, c.recorder, baseRecord(ctx, c.callCtx, startedAt, utf8.RuneCountInString(input), 0, err))
	return vector, err
}

type observedStrategy struct {
	base     Strategy
	recorder CallRecorder
	callCtx  CallContext
}

func NewObservedStrategy(base Strategy, recorder CallRecorder, callCtx CallContext) Strategy {
	if base == nil || recorder == nil {
		return base
	}
	return &observedStrategy{base: base, recorder: recorder, callCtx: callCtx}
}
func (s *observedStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	startedAt := time.Now()
	text, err := s.base.Transcribe(ctx, audioPath)
	recordCall(ctx, s.recorder, baseRecord(ctx, asrCallContext(s.callCtx), startedAt, 0, utf8.RuneCountInString(text), err))
	return text, err
}
func (s *observedStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	startedAt := time.Now()
	text, err := s.base.TranscribeChunks(ctx, audioPaths)
	recordCall(ctx, s.recorder, baseRecord(ctx, asrCallContext(s.callCtx), startedAt, 0, utf8.RuneCountInString(text), err))
	return text, err
}
func (s *observedStrategy) Summarize(ctx context.Context, text string) (string, error) {
	startedAt := time.Now()
	summary, err := s.base.Summarize(ctx, text)
	recordCall(ctx, s.recorder, baseRecord(ctx, llmCallContext(s.callCtx), startedAt, utf8.RuneCountInString(text), utf8.RuneCountInString(summary), err))
	return summary, err
}

func recordCall(ctx context.Context, recorder CallRecorder, record CallRecord) {
	observability.RecordAICall(observability.AICallObservation{
		Kind: record.Kind, Provider: record.Provider, Model: record.Model, Status: record.Status,
		Duration: time.Duration(record.DurationMs) * time.Millisecond, PromptTokens: record.PromptTokens,
		CompletionTokens: record.CompletionTokens, TotalTokens: record.TotalTokens, EstimatedCost: record.EstimatedCost,
	})
	if err := recorder.RecordAICall(ctx, record); err != nil {
		observability.Log(ctx, slog.Default(), slog.LevelError, "record ai audit failed", slog.String("error", observability.SafeError(err)))
	}
}

func asrCallContext(callCtx CallContext) CallContext {
	callCtx.Kind = model.AICallKindASR
	if callCtx.ASRProvider != "" {
		callCtx.Provider = callCtx.ASRProvider
	}
	if callCtx.ASRModel != "" {
		callCtx.Model = callCtx.ASRModel
	}
	return callCtx
}
func llmCallContext(callCtx CallContext) CallContext {
	callCtx.Kind = model.AICallKindLLM
	if callCtx.LLMProvider != "" {
		callCtx.Provider = callCtx.LLMProvider
	}
	if callCtx.LLMModel != "" {
		callCtx.Model = callCtx.LLMModel
	}
	return callCtx
}

func baseRecord(ctx context.Context, callCtx CallContext, startedAt time.Time, inputChars, outputChars int, err error) CallRecord {
	callCtx = enrichCallContext(ctx, callCtx)
	status, errCode, errMsg := model.AICallStatusSuccess, "", ""
	if err != nil {
		status, errCode, errMsg = model.AICallStatusFailed, classifyProviderError(err), observability.SafeError(err)
	}
	return CallRecord{
		UserID: callCtx.UserID, TaskID: callCtx.TaskID, JobID: callCtx.JobID, SessionID: callCtx.SessionID,
		TraceID: callCtx.TraceID, JobType: callCtx.JobType, Stage: callCtx.Stage, Attempt: callCtx.Attempt,
		Kind: callCtx.Kind, Provider: callCtx.Provider, Model: callCtx.Model, Status: status,
		DurationMs: time.Since(startedAt).Milliseconds(), InputChars: inputChars, OutputChars: outputChars, ASRSeconds: callCtx.ASRSeconds,
		PromptTokens: callCtx.PromptTokens, CompletionTokens: callCtx.CompletionTokens, TotalTokens: callCtx.TotalTokens,
		EstimatedCost: callCtx.EstimatedCost, TokenEstimated: callCtx.TokenEstimated, Currency: callCtx.Currency,
		PriceVersion: callCtx.PriceVersion, ProviderRequestID: callCtx.ProviderRequestID, ErrorCode: errCode, ErrorMsg: errMsg,
	}
}

func enrichCallContext(ctx context.Context, callCtx CallContext) CallContext {
	correlation := observability.CorrelationFromContext(ctx)
	if callCtx.TraceID == "" {
		callCtx.TraceID = correlation.TraceID
	}
	if callCtx.TaskID == 0 {
		callCtx.TaskID = correlation.TaskID
	}
	if callCtx.JobID == 0 {
		callCtx.JobID = correlation.JobID
	}
	if callCtx.UserID == 0 {
		callCtx.UserID = correlation.UserID
	}
	if callCtx.JobType == "" {
		callCtx.JobType = correlation.JobType
	}
	if callCtx.Stage == "" {
		callCtx.Stage = correlation.Stage
	}
	if callCtx.Attempt == 0 {
		callCtx.Attempt = correlation.Attempt
	}
	return callCtx
}

func classifyProviderError(err error) string {
	if err == nil {
		return ""
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return string(providerErr.Class)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"):
		return "timeout"
	case strings.Contains(text, "429"), strings.Contains(text, "too many requests"), strings.Contains(text, "rate limit"):
		return "rate_limited"
	case strings.Contains(text, "connection"), strings.Contains(text, "network"), strings.Contains(text, "dns"):
		return "network_error"
	case strings.Contains(text, "401"), strings.Contains(text, "403"), strings.Contains(text, "unauthorized"):
		return "auth_error"
	case strings.Contains(text, "http 500"), strings.Contains(text, "http 502"), strings.Contains(text, "http 503"), strings.Contains(text, "http 504"), strings.Contains(text, "service unavailable"):
		return "provider_unavailable"
	default:
		return "provider_error"
	}
}

func countChatMessageChars(messages []ChatMessage) int {
	total := 0
	for _, message := range messages {
		total += utf8.RuneCountInString(message.Content)
	}
	return total
}
