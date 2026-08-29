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

type EmbeddingClient interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

type OpenAIEmbeddingClient struct {
	transport *protocolClient
	model     string
}

func NewOpenAIEmbeddingClient(endpoint, apiKey, model string) *OpenAIEmbeddingClient {
	return &OpenAIEmbeddingClient{
		transport: newProtocolClient(endpoint, apiKey, "openai_compatible", 2*time.Minute),
		model:     strings.TrimSpace(model),
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

	req, err := c.transport.newRequest(ctx, http.MethodPost, "", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.transport.do(req, "embedding")
	if err != nil {
		return nil, err
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
