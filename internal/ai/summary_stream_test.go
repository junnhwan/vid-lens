package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSummaryUsesStreamingChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !body.Stream {
			http.Error(w, "gateway timed out waiting for a complete response", http.StatusGatewayTimeout)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"核心摘要\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	strategies := map[string]Strategy{
		"profile": &CompositeStrategy{chat: NewOpenAIChatClient(server.URL, "", "model")},
		"legacy":  NewOpenAICompatibleStrategy("", server.URL, "", "model"),
	}
	for name, strategy := range strategies {
		t.Run(name, func(t *testing.T) {
			got, err := strategy.Summarize(context.Background(), "转写文本")
			if err != nil || got != "核心摘要" {
				t.Fatalf("summary = %q, error = %v", got, err)
			}
		})
	}
}
