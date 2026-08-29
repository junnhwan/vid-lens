package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type RerankClient interface {
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error)
}

type RerankResult struct {
	Index int
	Score float64
}

type OpenAIRerankClient struct {
	transport *protocolClient
	endpoint  string // retained for compatibility with existing diagnostics/tests
	model     string
}

func NewOpenAIRerankClient(endpoint, apiKey, model string) *OpenAIRerankClient {
	return NewOpenAIRerankClientWithProvider(endpoint, apiKey, model, "openai_compatible")
}

func NewOpenAIRerankClientWithProvider(endpoint, apiKey, model, provider string) *OpenAIRerankClient {
	return &OpenAIRerankClient{
		transport: newProviderProtocolClient(endpoint, apiKey, provider, 2*time.Minute),
		endpoint:  strings.TrimSpace(endpoint),
		model:     strings.TrimSpace(model),
	}
}

func (c *OpenAIRerankClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if c == nil {
		return nil, fmt.Errorf("rerank client is nil")
	}
	if c.transport == nil || strings.TrimSpace(c.transport.baseURL) == "" {
		return nil, fmt.Errorf("rerank endpoint is empty")
	}
	if strings.TrimSpace(c.model) == "" {
		return nil, fmt.Errorf("rerank model is empty")
	}
	if len(documents) == 0 {
		return nil, nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}
	reqBody := map[string]interface{}{
		"model":     c.model,
		"query":     query,
		"documents": documents,
		"top_n":     topN,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := c.transport.newRequest(ctx, http.MethodPost, "", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.transport.do(req, "rerank")
	if err != nil {
		return nil, err
	}

	results, err := parseRerankResponse(body)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func parseRerankResponse(body []byte) ([]RerankResult, error) {
	var parsed struct {
		Results []struct {
			Index          int      `json:"index"`
			RelevanceScore *float64 `json:"relevance_score"`
			Score          *float64 `json:"score"`
		} `json:"results"`
		Data []struct {
			Index          int      `json:"index"`
			RelevanceScore *float64 `json:"relevance_score"`
			Score          *float64 `json:"score"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 Rerank 响应失败: %w", err)
	}
	items := parsed.Results
	if len(items) == 0 {
		items = parsed.Data
	}
	results := make([]RerankResult, 0, len(items))
	for _, item := range items {
		score := item.Score
		if item.RelevanceScore != nil {
			score = item.RelevanceScore
		}
		if score == nil {
			continue
		}
		results = append(results, RerankResult{Index: item.Index, Score: *score})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("Rerank 返回空结果")
	}
	return results, nil
}

func deriveRerankEndpointFromEmbedding(endpoint string) (string, bool) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", false
	}
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(trimmed, "/embeddings") {
		return strings.TrimSuffix(trimmed, "/embeddings") + "/rerank", true
	}
	return "", false
}
