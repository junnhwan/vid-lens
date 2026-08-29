package ai

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/observability"
)

func TestObservedChatFillsBusinessCorrelationFromContextAndKeepsUsageUnknown(t *testing.T) {
	recorder := &recordingCallRecorder{}
	client := NewObservedChatClient(&nonStreamingChatClient{}, recorder, CallContext{Provider: "mimo", Model: "mimo-v2.5"})
	ctx := observability.WithCorrelation(context.Background(), observability.Correlation{
		TraceID: "trace-ai", TaskID: 42, JobID: 8, UserID: 7, JobType: "analyze", Stage: "summarizing", Attempt: 2,
	})
	if _, err := client.Chat(ctx, []ChatMessage{{Role: "user", Content: "question"}}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records=%d", len(recorder.records))
	}
	got := recorder.records[0]
	if got.TraceID != "trace-ai" || got.TaskID != 42 || got.JobID != 8 || got.UserID != 7 || got.JobType != "analyze" || got.Stage != "summarizing" || got.Attempt != 2 {
		t.Fatalf("incomplete correlation: %+v", got)
	}
	if got.PromptTokens != nil || got.CompletionTokens != nil || got.TotalTokens != nil || got.EstimatedCost != nil {
		t.Fatalf("unknown usage must stay nil: %+v", got)
	}
}

func TestObservedFailuresUseControlledErrorCodes(t *testing.T) {
	tests := []struct {
		name   string
		client ChatClient
		want   string
	}{
		{"timeout", errorChatClient{context.DeadlineExceeded}, "timeout"},
		{"rate limited", errorChatClient{errors.New("provider returned HTTP 429 too many requests")}, "rate_limited"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingCallRecorder{}
			client := NewObservedChatClient(tt.client, recorder, CallContext{UserID: 7, Provider: "mimo", Model: "mimo-v2.5"})
			_, _ = client.Chat(context.Background(), nil)
			if len(recorder.records) != 1 || recorder.records[0].ErrorCode != tt.want {
				t.Fatalf("record=%+v want code=%s", recorder.records, tt.want)
			}
		})
	}

	recorder := &recordingCallRecorder{}
	embedding := NewObservedEmbeddingClient(errorEmbeddingClient{}, recorder, CallContext{UserID: 7, Provider: "mimo", Model: "embed"})
	_, _ = embedding.Embed(context.Background(), "text")
	if len(recorder.records) != 1 || recorder.records[0].ErrorCode != "network_error" {
		t.Fatalf("embedding record=%+v", recorder.records)
	}
}

type errorChatClient struct{ err error }

func (c errorChatClient) Chat(context.Context, []ChatMessage) (string, error) { return "", c.err }

type errorEmbeddingClient struct{}

func (errorEmbeddingClient) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("network connection reset")
}

func TestObservedChatUsesLLMProviderAndModelFromCallContext(t *testing.T) {
	recorder := &recordingCallRecorder{}
	client := NewObservedChatClient(&nonStreamingChatClient{}, recorder, CallContext{
		LLMProvider: "openai_compatible",
		LLMModel:    "fault-chat",
	})
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "title"}}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records=%d", len(recorder.records))
	}
	got := recorder.records[0]
	if got.Kind != "llm" || got.Provider != "openai_compatible" || got.Model != "fault-chat" {
		t.Fatalf("chat audit identity = kind:%q provider:%q model:%q", got.Kind, got.Provider, got.Model)
	}
}
