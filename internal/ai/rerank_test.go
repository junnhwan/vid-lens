package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIRerankClientPostsQueryDocumentsAndParsesScores(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotModel string
	var gotQuery string
	var gotDocuments []string
	var gotTopN int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model     string   `json:"model"`
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = body.Model
		gotQuery = body.Query
		gotDocuments = body.Documents
		gotTopN = body.TopN
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.91},{"index":0,"score":0.42}]}`))
	}))
	defer server.Close()

	client := NewOpenAIRerankClient(server.URL+"/v1/rerank", "sk-rerank", "Qwen/Qwen3-Reranker-4B")
	results, err := client.Rerank(context.Background(), "owner risk", []string{"doc a", "doc b"}, 2)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if gotPath != "/v1/rerank" {
		t.Fatalf("path = %q, want /v1/rerank", gotPath)
	}
	if gotAuth != "Bearer sk-rerank" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotModel != "Qwen/Qwen3-Reranker-4B" || gotQuery != "owner risk" {
		t.Fatalf("model/query = %q/%q", gotModel, gotQuery)
	}
	if len(gotDocuments) != 2 || gotDocuments[0] != "doc a" || gotDocuments[1] != "doc b" {
		t.Fatalf("documents = %#v", gotDocuments)
	}
	if gotTopN != 2 {
		t.Fatalf("top_n = %d, want 2", gotTopN)
	}
	if len(results) != 2 || results[0].Index != 1 || results[0].Score != 0.91 || results[1].Index != 0 || results[1].Score != 0.42 {
		t.Fatalf("results = %#v", results)
	}
}

func TestFactoryCreatesRerankClientFromEmbeddingEndpoint(t *testing.T) {
	client, err := NewFactory().NewRerankClient(Profile{
		EmbeddingProvider: "openai_compatible",
		EmbeddingEndpoint: "https://api.example.com/v1/embeddings",
		EmbeddingAPIKey:   "sk-embedding",
		RerankModel:       "Qwen/Qwen3-Reranker-4B",
	})
	if err != nil {
		t.Fatalf("NewRerankClient() error = %v", err)
	}
	openaiClient, ok := client.(*OpenAIRerankClient)
	if !ok {
		t.Fatalf("client type = %T, want *OpenAIRerankClient", client)
	}
	if openaiClient.endpoint != "https://api.example.com/v1/rerank" {
		t.Fatalf("endpoint = %q, want derived /v1/rerank", openaiClient.endpoint)
	}
}
