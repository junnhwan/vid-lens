package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChatBudgetHTTPOutputLimitAndProviderUsage(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		wantReports int
	}{
		{"provider", `,"usage":{"prompt_tokens":123,"completion_tokens":17}`, 1},
		{"missing", "", 0}, {"null", `,"usage":null`, 0},
		{"empty_object", `,"usage":{}`, 0},
		{"missing_completion", `,"usage":{"prompt_tokens":123}`, 0},
		{"missing_prompt", `,"usage":{"completion_tokens":17}`, 0},
		{"negative", `,"usage":{"prompt_tokens":-1,"completion_tokens":17}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"choices":[{"message":{"content":"answer"}}]%s}`, tc.usage)
			}))
			defer server.Close()
			var reports []ChatUsage
			ctx := WithChatBudget(context.Background(), 73, func(usage ChatUsage) { reports = append(reports, usage) })
			messages := []ChatMessage{{Role: "system", Content: "Use current evidence."}, {Role: "user", Content: "Question"}}
			answer, err := NewOpenAIChatClient(server.URL, "", "fixture").Chat(ctx, messages)
			if err != nil || answer != "answer" {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
			if string(request["max_tokens"]) != "73" || string(request["stream"]) != "false" {
				t.Fatalf("output budget not sent: %v", request)
			}
			var sent []ChatMessage
			if err := json.Unmarshal(request["messages"], &sent); err != nil || !reflect.DeepEqual(sent, messages) {
				t.Fatalf("messages changed: %+v %v", sent, err)
			}
			if len(reports) != tc.wantReports {
				t.Fatalf("usage callbacks=%+v", reports)
			}
			if len(reports) > 0 && reports[0] != (ChatUsage{PromptTokens: 123, CompletionTokens: 17}) {
				t.Fatalf("usage=%+v", reports)
			}
		})
	}
}

func TestChatBudgetStreamReadsUsageAfterFinishReason(t *testing.T) {
	var request map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}],\"usage\":null}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":98,\"completion_tokens\":12}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	var reports []ChatUsage
	var answer strings.Builder
	ctx := WithChatBudget(context.Background(), 51, func(usage ChatUsage) { reports = append(reports, usage) })
	err := NewOpenAIChatClient(server.URL, "", "fixture").StreamChat(ctx, nil, func(delta string) error { answer.WriteString(delta); return nil })
	if err != nil || answer.String() != "answer" {
		t.Fatalf("answer=%q err=%v", answer.String(), err)
	}
	if string(request["max_tokens"]) != "51" {
		t.Fatalf("max_tokens missing: %v", request)
	}
	var options struct {
		IncludeUsage bool `json:"include_usage"`
	}
	if err := json.Unmarshal(request["stream_options"], &options); err != nil || !options.IncludeUsage {
		t.Fatalf("usage not requested: %v", request)
	}
	if !reflect.DeepEqual(reports, []ChatUsage{{PromptTokens: 98, CompletionTokens: 12}}) {
		t.Fatalf("tail usage lost or counted twice: %+v", reports)
	}
}

func TestChatBudgetStreamMissingUsageAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		wantErr    bool
		wantUsage  bool
	}{
		{"normal_without_usage", "data: [DONE]\n\n", false, false},
		{"truncated_without_usage", "", true, false},
		{"truncated_with_confirmed_usage", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":19,\"completion_tokens\":3}}\n\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"+tc.tail)
			}))
			defer server.Close()
			var reports []ChatUsage
			var partial strings.Builder
			ctx := WithChatBudget(context.Background(), 9, func(usage ChatUsage) { reports = append(reports, usage) })
			err := NewOpenAIChatClient(server.URL, "", "fixture").StreamChat(ctx, nil, func(delta string) error { partial.WriteString(delta); return nil })
			if (err != nil) != tc.wantErr || partial.String() != "partial" {
				t.Fatalf("partial=%q err=%v", partial.String(), err)
			}
			if (len(reports) > 0) != tc.wantUsage {
				t.Fatalf("missing usage fabricated or confirmed usage discarded: %+v", reports)
			}
		})
	}
}

func TestChatBudgetDoesNotAffectOrdinaryChatAndHonorsConsumerStop(t *testing.T) {
	sentinel := errors.New("consumer stopped")
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%v", stream), func(t *testing.T) {
			var request map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"choices":[{"message":{"content":"answer"}}]}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client := NewOpenAIChatClient(server.URL, "", "fixture")
			if stream {
				if err := client.StreamChat(context.Background(), nil, func(string) error { return sentinel }); !errors.Is(err, sentinel) {
					t.Fatalf("consumer stop lost: %v", err)
				}
			} else {
				if _, err := client.Chat(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			}
			if request["max_tokens"] != nil || request["stream_options"] != nil {
				t.Fatalf("ordinary Chat inherited Agent budget: %v", request)
			}
		})
	}
}

func TestChatBudgetProviderIncompleteFinishPreservesAnswerAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, reason := range []string{"length", "content_filter"} {
			t.Run(fmt.Sprintf("stream_%v_%s", stream, reason), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"choices":[{"message":{"content":"<think>reasoning</think>partial answer"},"finish_reason":%q}],"usage":{"prompt_tokens":25,"completion_tokens":8}}`, reason)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"<think>reasoning</think>partial answer\"}}]}\n\n")
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\n", reason)
					fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":25,\"completion_tokens\":8}}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				var reports []ChatUsage
				ctx := WithChatBudget(context.Background(), 8, func(usage ChatUsage) { reports = append(reports, usage) })
				client := NewOpenAIChatClient(server.URL, "", "fixture")
				var answer string
				var err error
				if stream {
					var out strings.Builder
					err = client.StreamChat(ctx, nil, func(s string) error { out.WriteString(s); return nil })
					answer = out.String()
				} else {
					answer, err = client.Chat(ctx, nil)
				}
				var finish *ChatFinishError
				if !errors.As(err, &finish) || finish.Reason != reason || finish.PartialContent != "partial answer" || answer != "partial answer" {
					t.Fatalf("answer=%q finish=%+v err=%v", answer, finish, err)
				}
				if !reflect.DeepEqual(reports, []ChatUsage{{PromptTokens: 25, CompletionTokens: 8}}) {
					t.Fatalf("usage lost on incomplete finish: %+v", reports)
				}
			})
		}
	}
}
