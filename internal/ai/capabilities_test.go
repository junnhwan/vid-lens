package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMOnlySummaryUsesNoOtherModel(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected hidden model call: %s", r.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"summary\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	p := Profile{LLMProvider: "openai", LLMBaseURL: server.URL + "/v1", LLMAPIKey: "test", LLMModel: "llm"}
	strategy, err := NewFactory().NewAnalysisStrategy(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.Summarize(context.Background(), "existing transcript"); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.Transcribe(context.Background(), "unused"); err == nil {
		t.Fatal("absent ASR admitted")
	}
	if _, err := NewFactory().NewEmbeddingClient(p); err == nil {
		t.Fatal("absent embedding admitted")
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
