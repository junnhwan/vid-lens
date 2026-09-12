package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReasoningArrivesBeforeAnswerThroughAllWrappers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"先定位证据\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-released:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"正文\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	recorder := &recordingCallRecorder{}
	client := NewObservedChatClient(RetryChat(AdmitChat(NewOpenAIChatClient(server.URL, "", "test"), nil, "test", "test"), ProviderRetryPolicy{}), recorder, CallContext{})
	var events []StreamDelta
	err := StreamResponse(ctx, client, nil, func(delta StreamDelta) error {
		events = append(events, delta)
		if delta.Kind == "reasoning" {
			close(released)
		}
		return nil
	})
	if err != nil || len(events) != 2 || events[0].Kind != "reasoning" || events[1].Text != "正文" || len(recorder.records) != 1 {
		t.Fatalf("events=%+v records=%d err=%v", events, len(recorder.records), err)
	}
}

func TestThinkingTagsAreSplitAtEveryPossibleBoundary(t *testing.T) {
	const content = "<think>思考文字</think>实际回答"
	for i := 0; i <= len(content); i++ {
		var splitter thinkingSplitter
		var reasoning, answer string
		emit := func(think bool, s string) error {
			if think {
				reasoning += s
			} else {
				answer += s
			}
			return nil
		}
		_ = splitter.push(content[:i], false, emit)
		_ = splitter.push(content[i:], false, emit)
		_ = splitter.push("", true, emit)
		if reasoning != "思考文字" || answer != "实际回答" {
			t.Fatalf("boundary %d: reasoning=%q answer=%q", i, reasoning, answer)
		}
	}
}

func TestProviderStreamRejectsTruncatedOrErrorResponse(t *testing.T) {
	for _, body := range []string{`data: {"choices":[{"delta":{"content":"half"}}]}`, `data: {"error":{"message":"upstream failed"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, body)
		}))
		err := NewOpenAIChatClient(server.URL, "", "test").StreamChat(context.Background(), nil, func(string) error { return nil })
		server.Close()
		if err == nil {
			t.Fatalf("accepted truncated/error response: %s", body)
		}
	}
}

func TestThinkTextIsNotReturnedByAnswerOnlyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"<think>private</think>answer\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	var out strings.Builder
	err := NewOpenAIChatClient(server.URL, "", "test").StreamChat(context.Background(), nil, func(s string) error { out.WriteString(s); return nil })
	if err != nil || out.String() != "answer" {
		t.Fatalf("answer=%q err=%v", out.String(), err)
	}
}
