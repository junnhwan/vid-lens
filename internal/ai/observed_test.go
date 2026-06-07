package ai

import (
	"context"
	"testing"
)

func TestObservedChatClientDoesNotExposeStreamingWhenBaseDoesNotStream(t *testing.T) {
	base := &nonStreamingChatClient{}
	wrapped := NewObservedChatClient(base, discardCallRecorder{}, CallContext{UserID: 7})

	if _, ok := wrapped.(StreamingChatClient); ok {
		t.Fatal("observed wrapper exposed StreamChat for a non-streaming base client")
	}
}

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
	if recorder.records[0].Kind != "asr" || recorder.records[0].Provider != "mimo" || recorder.records[0].Model != "mimo-v2.5-asr" {
		t.Fatalf("asr record = %+v", recorder.records[0])
	}
	if recorder.records[1].Kind != "llm" || recorder.records[1].Provider != "openai_compatible" || recorder.records[1].Model != "chat-model" {
		t.Fatalf("llm record = %+v", recorder.records[1])
	}
}

type nonStreamingChatClient struct{}

func (c *nonStreamingChatClient) Chat(context.Context, []ChatMessage) (string, error) {
	return "answer", nil
}

type discardCallRecorder struct{}

func (discardCallRecorder) RecordAICall(context.Context, CallRecord) error {
	return nil
}

type recordingCallRecorder struct {
	records []CallRecord
}

func (r *recordingCallRecorder) RecordAICall(_ context.Context, record CallRecord) error {
	r.records = append(r.records, record)
	return nil
}

type recordingStrategy struct{}

func (s *recordingStrategy) Transcribe(context.Context, string) (string, error) {
	return "转写文本", nil
}

func (s *recordingStrategy) TranscribeChunks(context.Context, []string) (string, error) {
	return "转写文本", nil
}

func (s *recordingStrategy) Summarize(context.Context, string) (string, error) {
	return "总结文本", nil
}
