package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type EmbeddingClient interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

type OpenAIEmbeddingClient struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

func NewOpenAIEmbeddingClient(endpoint, apiKey, model string) *OpenAIEmbeddingClient {
	return &OpenAIEmbeddingClient{
		endpoint: strings.TrimSpace(endpoint),
		apiKey:   apiKey,
		model:    model,
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (c *OpenAIEmbeddingClient) Embed(ctx context.Context, input string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"model": c.model,
		"input": input,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, ProviderTransportError("openai_compatible", "embedding", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, ProviderHTTPError("openai_compatible", "embedding", resp.StatusCode, resp.Header, body)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 Embedding 响应失败: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding 返回空向量")
	}
	return result.Data[0].Embedding, nil
}
