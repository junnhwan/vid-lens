package ai

import (
	"context"
	"time"
	"unicode/utf8"

	"vid-lens/internal/model"
)

type CallContext struct {
	UserID      int64
	TaskID      int64
	SessionID   int64
	Provider    string
	Model       string
	ASRProvider string
	ASRModel    string
	LLMProvider string
	LLMModel    string
	Kind        string
	ASRSeconds  int
}

type CallRecord struct {
	UserID      int64
	TaskID      int64
	SessionID   int64
	Kind        string
	Provider    string
	Model       string
	Status      string
	DurationMs  int64
	InputChars  int
	OutputChars int
	ASRSeconds  int
	ErrorCode   string
	ErrorMsg    string
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
	if base == nil || recorder == nil || callCtx.UserID <= 0 {
		return base
	}
	if callCtx.Kind == "" {
		callCtx.Kind = model.AICallKindLLM
	}
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
	err := c.streaming.StreamChat(ctx, messages, func(delta string) error {
		outputChars += utf8.RuneCountInString(delta)
		return emit(delta)
	})
	c.record(ctx, startedAt, countChatMessageChars(messages), outputChars, err)
	return err
}

func (c *observedChatClient) record(ctx context.Context, startedAt time.Time, inputChars, outputChars int, err error) {
	record := baseRecord(c.callCtx, startedAt, inputChars, outputChars, err)
	_ = c.recorder.RecordAICall(ctx, record)
}

type observedEmbeddingClient struct {
	base     EmbeddingClient
	recorder CallRecorder
	callCtx  CallContext
}

func NewObservedEmbeddingClient(base EmbeddingClient, recorder CallRecorder, callCtx CallContext) EmbeddingClient {
	if base == nil || recorder == nil || callCtx.UserID <= 0 {
		return base
	}
	callCtx.Kind = model.AICallKindEmbedding
	return &observedEmbeddingClient{base: base, recorder: recorder, callCtx: callCtx}
}

func (c *observedEmbeddingClient) Embed(ctx context.Context, input string) ([]float32, error) {
	startedAt := time.Now()
	vector, err := c.base.Embed(ctx, input)
	record := baseRecord(c.callCtx, startedAt, utf8.RuneCountInString(input), 0, err)
	_ = c.recorder.RecordAICall(ctx, record)
	return vector, err
}

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

func (s *observedStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	startedAt := time.Now()
	text, err := s.base.Transcribe(ctx, audioPath)
	callCtx := asrCallContext(s.callCtx)
	record := baseRecord(callCtx, startedAt, 0, utf8.RuneCountInString(text), err)
	_ = s.recorder.RecordAICall(ctx, record)
	return text, err
}

func (s *observedStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	startedAt := time.Now()
	text, err := s.base.TranscribeChunks(ctx, audioPaths)
	callCtx := asrCallContext(s.callCtx)
	record := baseRecord(callCtx, startedAt, 0, utf8.RuneCountInString(text), err)
	_ = s.recorder.RecordAICall(ctx, record)
	return text, err
}

func (s *observedStrategy) Summarize(ctx context.Context, text string) (string, error) {
	startedAt := time.Now()
	summary, err := s.base.Summarize(ctx, text)
	callCtx := llmCallContext(s.callCtx)
	record := baseRecord(callCtx, startedAt, utf8.RuneCountInString(text), utf8.RuneCountInString(summary), err)
	_ = s.recorder.RecordAICall(ctx, record)
	return summary, err
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

func baseRecord(callCtx CallContext, startedAt time.Time, inputChars, outputChars int, err error) CallRecord {
	status := model.AICallStatusSuccess
	errCode := ""
	errMsg := ""
	if err != nil {
		status = model.AICallStatusFailed
		errCode = "provider_error"
		errMsg = err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
	}
	return CallRecord{
		UserID:      callCtx.UserID,
		TaskID:      callCtx.TaskID,
		SessionID:   callCtx.SessionID,
		Kind:        callCtx.Kind,
		Provider:    callCtx.Provider,
		Model:       callCtx.Model,
		Status:      status,
		DurationMs:  time.Since(startedAt).Milliseconds(),
		InputChars:  inputChars,
		OutputChars: outputChars,
		ASRSeconds:  callCtx.ASRSeconds,
		ErrorCode:   errCode,
		ErrorMsg:    errMsg,
	}
}

func countChatMessageChars(messages []ChatMessage) int {
	total := 0
	for _, message := range messages {
		total += utf8.RuneCountInString(message.Content)
	}
	return total
}
