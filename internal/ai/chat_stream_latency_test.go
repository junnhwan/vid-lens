package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The provider cannot send the remainder until the application consumes the
// first delta. A buffered implementation times out instead of silently passing.
func TestChatStreamDeliversBeforeProviderCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-first:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	client := NewObservedChatClient(RetryChat(AdmitChat(NewOpenAIChatClient(server.URL, "", "test"), nil, "test", "test"), ProviderRetryPolicy{}), discardCallRecorder{}, CallContext{})
	var answer string
	err := client.(StreamingChatClient).StreamChat(ctx, nil, func(delta string) error {
		if answer == "" {
			close(first)
		}
		answer += delta
		return nil
	})
	if err != nil || answer != "firstsecond" {
		t.Fatalf("incremental delivery: answer=%q err=%v", answer, err)
	}
}
