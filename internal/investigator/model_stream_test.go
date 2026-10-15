package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

// Exercise the real Eino HTTP adapter: a relay may withhold non-stream headers
// for the entire generation, while streaming sends headers and useful chunks promptly.
func TestModelGenerationOutlivesHeaderTimeoutWithoutResubmission(t *testing.T) {
	var submissions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submissions.Add(1)
		var input struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if !input.Stream {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"one-generation","choices":[{"index":0,"message":{"role":"assistant","content":"complete result"},"finish_reason":"stop"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		for i, content := range []string{"complete ", "result"} {
			fmt.Fprintf(w, "data: {\"id\":\"one-generation\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\n", content)
			w.(http.Flusher).Flush()
			if i == 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
		fmt.Fprint(w, "data: {\"id\":\"one-generation\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL, p.ModelName = server.URL, "test-model"
	tr := &http.Transport{ResponseHeaderTimeout: 75 * time.Millisecond}
	defer tr.CloseIdleConnections()
	base, err := newModelWithClient(context.Background(), p, "local-test", &http.Client{Transport: safeTransport{tr}, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := completeModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("produce the complete result")})
	if err != nil {
		t.Fatalf("long generation was interrupted and would be retried: %v", err)
	}
	if result.Content != "complete result" {
		t.Fatalf("incomplete generation: %q", result.Content)
	}
	if submissions.Load() != 1 {
		t.Fatalf("submitted %d times", submissions.Load())
	}
	if result.ResponseMeta == nil || result.ResponseMeta.Usage == nil || result.ResponseMeta.Usage.TotalTokens != 9 {
		t.Fatal("final usage was not retained")
	}
}

func TestModelStreamDeadlinesAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		body       func(http.ResponseWriter, *http.Request)
	}{
		{"first_valid_response", "有效响应", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		{"idle_after_content", "没有新内容", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"}}]}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		{"total_even_with_progress", "超过期限", func(w http.ResponseWriter, r *http.Request) {
			for {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"x\"}}]}\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}},
		{"truncated_result", "未完整结束", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"submit_article\",\"arguments\":\"{\"}}]}}]}\n\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				tc.body(w, r)
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL, p.ModelName = server.URL, "test-model"
			base, err := newModelWithClient(context.Background(), p, "local-test", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := receiveModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("test")}, modelResponseLimits{Total: 180 * time.Millisecond, First: 60 * time.Millisecond, Idle: 60 * time.Millisecond})
			var work *domain.WorkError
			if result != nil || !errors.As(err, &work) || !work.Transient || !strings.Contains(work.Message, tc.want) {
				t.Fatalf("result=%v error=%v; want %s", result, err, tc.want)
			}
		})
	}
}

func TestModelStreamConcatenatesToolArgumentsAndDiscardsThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"one-generation\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"private-thought\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"one-generation\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"submit_article\",\"arguments\":\"{\\\"title\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"one-generation\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"complete\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL, p.ModelName = server.URL, "test-model"
	base, err := newModelWithClient(context.Background(), p, "local-test", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := completeModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("test")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "call-a" || result.ToolCalls[0].Function.Arguments != `{"title":"complete"}` {
		t.Fatalf("broken tool result: %+v", result.ToolCalls)
	}
	stored, _ := json.Marshal(result)
	if strings.Contains(string(stored), "private-thought") {
		t.Fatal("thinking remained in finalized response")
	}
}
