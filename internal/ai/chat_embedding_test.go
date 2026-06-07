package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIChatClientPostsChatCompletions(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		var body struct {
			Model    string        `json:"model"`
			Messages []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAIChatClient(server.URL+"/v1", "sk-chat", "chat-model")
	answer, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if answer != "answer" {
		t.Fatalf("Chat() = %q", answer)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-chat" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotModel != "chat-model" {
		t.Fatalf("model = %q", gotModel)
	}
}

func TestOpenAIChatClientStreamsChatCompletionDeltas(t *testing.T) {
	var gotStream bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotStream = body.Stream
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"第一段"}}]}`,
			`data: {"choices":[{"delta":{"content":"第二段"}}]}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer server.Close()

	client := NewOpenAIChatClient(server.URL+"/v1", "sk-chat", "chat-model")
	var deltas []string
	err := client.StreamChat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	if !gotStream {
		t.Fatal("request stream flag = false, want true")
	}
	if strings.Join(deltas, "") != "第一段第二段" {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestFactoryCreatesMimoChatClientWithAPIKeyHeader(t *testing.T) {
	var gotAPIKey string
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("api-key")
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()

	client, err := NewFactory().NewChatClient(Profile{
		LLMProvider: "mimo",
		LLMBaseURL:  server.URL + "/v1",
		LLMAPIKey:   "tp-chat",
		LLMModel:    "mimo-v2.5",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotAPIKey != "tp-chat" {
		t.Fatalf("api-key = %q, want tp-chat", gotAPIKey)
	}
	if gotAuthorization != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuthorization)
	}
}

func TestFactoryRejectsMimoEmbeddingProvider(t *testing.T) {
	_, err := NewFactory().NewEmbeddingClient(Profile{
		EmbeddingProvider: "mimo",
		EmbeddingEndpoint: "https://token-plan-cn.xiaomimimo.com/v1/embeddings",
		EmbeddingAPIKey:   "tp-embedding",
		EmbeddingModel:    "mimo-embedding",
	})
	if err == nil {
		t.Fatal("NewEmbeddingClient() succeeded for unsupported mimo embedding provider")
	}
}

func TestOpenAIEmbeddingClientPostsExactEndpoint(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotInput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotInput = body.Input
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()

	client := NewOpenAIEmbeddingClient(server.URL+"/custom/v1/embeddings", "sk-embedding", "text-embedding-3-small")
	vector, err := client.Embed(context.Background(), "question")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vector) != 3 {
		t.Fatalf("len(vector) = %d", len(vector))
	}
	if gotPath != "/custom/v1/embeddings" {
		t.Fatalf("path = %q, want exact endpoint path", gotPath)
	}
	if gotAuth != "Bearer sk-embedding" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotInput != "question" {
		t.Fatalf("input = %q", gotInput)
	}
}
