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

func TestEmptyProviderResponseIsNotAccepted(t *testing.T) {
	for _, delta := range []string{`{}`, `{"content":"  "}`, `{"reasoning_content":"internal only"}`} {
		t.Run(delta, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":\"stop\"}],\"usage\":{\"completion_tokens\":205}}\n\ndata: [DONE]\n\n", delta)
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL = server.URL
			base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := completeModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("Continue saved chapters")})
			var work *domain.WorkError
			if result != nil || !errors.As(err, &work) || work.Code != "PROVIDER_UNAVAILABLE" || !work.Transient {
				t.Fatalf("empty response was treated as a usable agent turn: result=%v err=%v", result, err)
			}
		})
	}
}

func TestReasoningGPTStreamsWithCompatibleParameters(t *testing.T) {
	for _, name := range []string{"gpt-5.5", "gpt-5.6-sol", "gpt-6-astra", "openai/gpt-5.5", "deepseek/deepseek-v4.1-flash"} {
		t.Run(name, func(t *testing.T) {
			gpt := !strings.HasPrefix(name, "deepseek/")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if string(body["stream"]) != "true" || len(body["tools"]) == 0 {
					t.Error("streaming or tools missing from actual request")
				}
				if gpt {
					if string(body["max_completion_tokens"]) != "128000" || len(body["max_tokens"]) != 0 || len(body["temperature"]) != 0 {
						t.Error("reasoning GPT request has incompatible generation parameters")
					}
				} else if string(body["max_tokens"]) != "128000" || string(body["temperature"]) != "0.2" || len(body["max_completion_tokens"]) != 0 {
					t.Error("non-GPT provider generation parameters changed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			p := config.DefaultProfile()
			p.ModelBaseURL, p.ModelName, p.MaxOutputTokens = server.URL, name, 128000
			base, err := newModelWithClient(context.Background(), p, "synthetic-test-key", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			base, err = base.WithTools([]*schema.ToolInfo{{Name: "inspect_test_pattern", Desc: "Inspect synthetic pixels."}})
			if err != nil {
				t.Fatal(err)
			}
			messages := []*schema.Message{schema.SystemMessage("Follow the synthetic check instruction."), schema.UserMessage("Reply OK.")}
			result, err := completeModelResponse(context.Background(), base, messages)
			if err != nil || result == nil || result.Content != "OK" || calls.Load() != 1 {
				t.Fatalf("streaming failed before or after provider submission: result=%v err=%v calls=%d", result, err, calls.Load())
			}
		})
	}
}

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

func TestWorkflowBufferedToolArgumentsUseTheWholeBoundedResponseWindow(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"one-call","type":"function","function":{"name":"write_video_chapters","arguments":""}}]}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(140 * time.Millisecond):
		}
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"sections\":[]}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	base, err := newModelWithClient(context.Background(), p, "fixture", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	s := &session{workflowStep: &workflowStep{Phase: "write"}}
	limits := s.responseLimits()
	if limits.Total != 300*time.Second || limits.First != limits.Total || limits.Idle != limits.Total {
		t.Fatal("staged result was preempted by a shorter streaming cadence assumption", limits)
	}
	limits.Total, limits.First, limits.Idle = limits.Total/1000, limits.First/1000, limits.Idle/1000
	result, err := receiveModelResponse(context.Background(), base, []*schema.Message{schema.UserMessage("write once")}, limits)
	if err != nil || len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Arguments != `{"sections":[]}` || sends.Load() != 1 {
		t.Fatal("buffered valid completion lost or resent", result, err, sends.Load())
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
