package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStructuredJSONIsScopedToKnownHybridModelsAndKeepsReasoningUsage(t *testing.T) {
	for _, tc := range []struct {
		model            string
		structured, want bool
	}{{"qwen3.6-flash", true, true}, {"qwen/qwen3.6-flash", true, true}, {"qwen3.6-flash", false, false}, {"other-model", true, false}} {
		t.Run(fmt.Sprintf("%s_%t", tc.model, tc.structured), func(t *testing.T) {
			var sent map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&sent)
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":90,"completion_tokens_details":{"reasoning_tokens":80}}}`)
			}))
			defer server.Close()
			var usage ChatUsage
			ctx := WithChatBudget(context.Background(), 128, func(u ChatUsage) { usage = u })
			if tc.structured {
				ctx = WithStructuredJSON(ctx)
			}
			if _, err := NewOpenAIChatClient(server.URL, "", tc.model).Chat(ctx, nil); err != nil {
				t.Fatal(err)
			}
			if (string(sent["enable_thinking"]) == "false") != tc.want || (sent["response_format"] != nil) != tc.want {
				t.Fatalf("request option scope changed: %s", sent)
			}
			if usage.CompletionTokens != 90 || usage.ReasoningTokens != 80 {
				t.Fatalf("reasoning was lost/double counted: %+v", usage)
			}
		})
	}
}
