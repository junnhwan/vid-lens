package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func visionBudgetImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(path, []byte("fixture-image"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVisionBudgetHTTPOutputLimitAndProviderUsage(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		want        *ChatUsage
	}{
		{"provider", `,"usage":{"prompt_tokens":123,"completion_tokens":17,"completion_tokens_details":{"reasoning_tokens":4}}`, &ChatUsage{PromptTokens: 123, CompletionTokens: 17, ReasoningTokens: 4}},
		{"explicit_zero", `,"usage":{"prompt_tokens":0,"completion_tokens":0}`, &ChatUsage{}},
		{"missing", "", nil},
		{"null", `,"usage":null`, nil},
		{"empty", `,"usage":{}`, nil},
		{"missing_completion", `,"usage":{"prompt_tokens":123}`, nil},
		{"missing_prompt", `,"usage":{"completion_tokens":17}`, nil},
		{"negative_prompt", `,"usage":{"prompt_tokens":-1,"completion_tokens":17}`, nil},
		{"negative_completion", `,"usage":{"prompt_tokens":123,"completion_tokens":-1}`, nil},
		{"invalid_reasoning", `,"usage":{"prompt_tokens":123,"completion_tokens":17,"completion_tokens_details":{"reasoning_tokens":18}}`, &ChatUsage{PromptTokens: 123, CompletionTokens: 17}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"choices":[{"message":{"content":"<think>internal</think>caption"},"finish_reason":"stop"}]%s}`, tc.usage)
			}))
			defer server.Close()
			var reports []ChatUsage
			ctx := WithChatBudget(context.Background(), 73, func(u ChatUsage) { reports = append(reports, u) })
			answer, err := NewOpenAIVisionClient(server.URL, "", "fixture").CaptionImage(ctx, visionBudgetImage(t), "Describe this exact frame.")
			if err != nil || answer != "caption" {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
			if string(request["max_tokens"]) != "73" || string(request["stream"]) != "false" || string(request["model"]) != `"fixture"` {
				t.Fatalf("vision output cap or request changed: %v", request)
			}
			var messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			}
			if err := json.Unmarshal(request["messages"], &messages); err != nil || len(messages) != 1 || messages[0].Role != "user" || len(messages[0].Content) != 2 {
				t.Fatalf("multimodal request changed: %+v %v", messages, err)
			}
			parts := messages[0].Content
			if parts[0].Type != "text" || parts[0].Text != "Describe this exact frame." || parts[1].Type != "image_url" || parts[1].ImageURL.URL != "data:image/png;base64,Zml4dHVyZS1pbWFnZQ==" {
				t.Fatal("budget changed the frozen prompt or image")
			}
			if tc.want == nil {
				if len(reports) != 0 {
					t.Fatalf("unknown usage became actual zero: %+v", reports)
				}
			} else if !reflect.DeepEqual(reports, []ChatUsage{*tc.want}) {
				t.Fatalf("usage callbacks=%+v want=%+v", reports, tc.want)
			}
		})
	}
}

func TestVisionBudgetIncompleteFinishPreservesAnswerAndUsage(t *testing.T) {
	for _, reason := range []string{"length", "content_filter"} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_known_%t", reason, known), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					usage := ""
					if known {
						usage = `,"usage":{"prompt_tokens":25,"completion_tokens":8}`
					}
					fmt.Fprintf(w, `{"choices":[{"message":{"content":"<think>internal</think>partial caption"},"finish_reason":%q}]%s}`, reason, usage)
				}))
				defer server.Close()
				var reports []ChatUsage
				ctx := WithChatBudget(context.Background(), 8, func(u ChatUsage) { reports = append(reports, u) })
				answer, err := NewOpenAIVisionClient(server.URL, "", "fixture").CaptionImage(ctx, visionBudgetImage(t), "Inspect frame.")
				var finish *ChatFinishError
				if !errors.As(err, &finish) || finish.Reason != reason || finish.PartialContent != "partial caption" || answer != finish.PartialContent {
					t.Fatalf("answer=%q finish=%+v err=%v", answer, finish, err)
				}
				if known && !reflect.DeepEqual(reports, []ChatUsage{{PromptTokens: 25, CompletionTokens: 8}}) || !known && len(reports) != 0 {
					t.Fatalf("finish lost or invented usage: %+v", reports)
				}
			})
		}
	}
}

func TestVisionBudgetOrdinaryCallsKeepRequestContract(t *testing.T) {
	for _, cap := range []int64{-1, 0, 73} {
		t.Run(fmt.Sprintf("cap_%d", cap), func(t *testing.T) {
			var request map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"caption"}}]}`)
			}))
			defer server.Close()
			ctx := context.Background()
			if cap >= 0 {
				ctx = WithChatBudget(ctx, cap, nil)
			}
			if _, err := NewOpenAIVisionClient(server.URL, "", "fixture").CaptionImage(ctx, visionBudgetImage(t), ""); err != nil {
				t.Fatal(err)
			}
			if cap <= 0 && request["max_tokens"] != nil || cap > 0 && string(request["max_tokens"]) != "73" {
				t.Fatalf("unexpected max_tokens: %s", request["max_tokens"])
			}
			if request["response_format"] != nil || request["enable_thinking"] != nil || request["stream_options"] != nil || !strings.Contains(string(request["messages"]), "画面上可读到的文字") {
				t.Fatal("ordinary vision inherited unrelated structured or stream policy")
			}
		})
	}
}

func TestVisionBudgetEmptyChoicesStillReportsSpentUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[],"usage":{"prompt_tokens":19,"completion_tokens":3}}`)
	}))
	defer server.Close()
	var reports []ChatUsage
	ctx := WithChatBudget(context.Background(), 8, func(u ChatUsage) { reports = append(reports, u) })
	if _, err := NewOpenAIVisionClient(server.URL, "", "fixture").CaptionImage(ctx, visionBudgetImage(t), "Inspect frame."); err == nil {
		t.Fatal("empty choices accepted")
	}
	if !reflect.DeepEqual(reports, []ChatUsage{{PromptTokens: 19, CompletionTokens: 3}}) {
		t.Fatalf("provider spend lost on empty output: %+v", reports)
	}
}

func TestVisionBudgetDoesNotChangeStructuredResponsePolicy(t *testing.T) {
	var request map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"caption"}}]}`)
	}))
	defer server.Close()
	ctx := WithStructuredJSON(WithChatBudget(context.Background(), 19, nil))
	if _, err := NewOpenAIVisionClient(server.URL, "", "qwen3.6-plus").CaptionImage(ctx, visionBudgetImage(t), "Inspect frame."); err != nil {
		t.Fatal(err)
	}
	if string(request["max_tokens"]) != "19" || request["response_format"] != nil || request["enable_thinking"] != nil {
		t.Fatalf("output cap changed existing vision policy: %v", request)
	}
}
