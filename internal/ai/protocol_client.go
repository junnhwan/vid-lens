package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// protocolClient contains the transport concerns shared by protocol
// adapters: endpoint joining, authentication, timeout handling, and typed
// provider errors. Capability-specific request and response shapes stay in
// the adapters that use this client.
type protocolClient struct {
	baseURL    string
	apiKey     string
	provider   string
	authHeader string
	authPrefix string
	client     *http.Client
}

func newProtocolClient(baseURL, apiKey, provider string, timeout time.Duration) *protocolClient {
	if strings.TrimSpace(provider) == "" {
		provider = "openai_compatible"
	}
	return &protocolClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		provider:   provider,
		authHeader: "Authorization",
		authPrefix: "Bearer ",
		client:     &http.Client{Timeout: timeout},
	}
}

func (c *protocolClient) endpoint(path string) string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(path) == "" {
		return c.baseURL
	}
	return strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (c *protocolClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("protocol client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set(c.authHeader, c.authPrefix+c.apiKey)
	}
	return req, nil
}

func (c *protocolClient) send(req *http.Request, operation string) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("protocol client is nil")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, ProviderTransportError(c.provider, operation, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, ProviderHTTPError(c.provider, operation, resp.StatusCode, resp.Header, body)
	}
	return resp, nil
}

func (c *protocolClient) do(req *http.Request, operation string) ([]byte, error) {
	resp, err := c.send(req, operation)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 响应失败: %w", operation, err)
	}
	return body, nil
}
