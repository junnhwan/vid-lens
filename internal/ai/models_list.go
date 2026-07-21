package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	modelsListTimeout  = 15 * time.Second
	modelsListMaxBytes = 2 << 20 // 2 MiB
	modelsListMaxIDs   = 500
)

// ListOpenAIModels calls GET {baseURL}/models with Bearer auth and returns model ids.
// baseURL should be an OpenAI-compatible root such as https://api.example.com/v1
// (not a .../chat/completions path). Embedding endpoints ending in /embeddings are normalized.
func ListOpenAIModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	root, err := normalizeModelsBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if err := validatePublicHTTPURL(root); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API Key 不能为空")
	}

	reqURL := strings.TrimRight(root, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: modelsListTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, ProviderTransportError("openai_compatible", "models", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, modelsListMaxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取模型列表失败: %w", err)
	}
	if len(body) > modelsListMaxBytes {
		return nil, fmt.Errorf("模型列表响应过大")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ProviderHTTPError("openai_compatible", "models", resp.StatusCode, resp.Header, body)
	}

	ids, err := parseOpenAIModelsResponse(body)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("上游未返回可用模型")
	}
	return ids, nil
}

func normalizeModelsBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Base URL 不能为空")
	}
	trimmed := strings.TrimRight(raw, "/")
	// .../v1/embeddings → .../v1
	if strings.HasSuffix(strings.ToLower(trimmed), "/embeddings") {
		trimmed = trimmed[:len(trimmed)-len("/embeddings")]
		trimmed = strings.TrimRight(trimmed, "/")
	}
	return trimmed, nil
}

func parseOpenAIModelsResponse(body []byte) ([]string, error) {
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err == nil && len(result.Data) > 0 {
		ids := make([]string, 0, len(result.Data))
		for _, it := range result.Data {
			if id := strings.TrimSpace(it.ID); id != "" {
				ids = append(ids, id)
			}
		}
		return uniqueSortedStrings(ids), nil
	}

	var arr []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		ids := make([]string, 0, len(arr))
		for _, it := range arr {
			if id := strings.TrimSpace(it.ID); id != "" {
				ids = append(ids, id)
			}
		}
		return uniqueSortedStrings(ids), nil
	}

	var loose struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &loose); err == nil && len(loose.Data) > 0 {
		ids := make([]string, 0, len(loose.Data))
		for _, id := range loose.Data {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		return uniqueSortedStrings(ids), nil
	}

	return nil, fmt.Errorf("无法解析模型列表响应")
}

func uniqueSortedStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= modelsListMaxIDs {
			break
		}
	}
	sort.Strings(out)
	return out
}

// validatePublicHTTPURL rejects non-http(s) and common private/link-local targets (basic SSRF guard).
func validatePublicHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("Base URL 无效: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("Base URL 仅支持 http/https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("Base URL 缺少主机名")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || lower == "0.0.0.0" {
		return fmt.Errorf("不允许访问本地地址")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("不允许访问内网或保留地址")
		}
		return nil
	}
	if strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("不允许访问本地/内网主机名")
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return false
		}
	}
	return true
}

// ProbeEmbeddingDimension calls the embeddings endpoint once and returns len(vector).
// endpoint must be the full embeddings URL (e.g. https://api.example.com/v1/embeddings).
func ProbeEmbeddingDimension(ctx context.Context, endpoint, apiKey, model string) (int, error) {
	endpoint = strings.TrimSpace(endpoint)
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	if endpoint == "" {
		return 0, fmt.Errorf("Embedding Endpoint 不能为空")
	}
	if apiKey == "" {
		return 0, fmt.Errorf("API Key 不能为空")
	}
	if model == "" {
		return 0, fmt.Errorf("Embedding 模型不能为空")
	}
	if err := validatePublicHTTPURL(endpoint); err != nil {
		return 0, err
	}

	client := NewOpenAIEmbeddingClient(endpoint, apiKey, model)
	// Short probe text; providers only need a non-empty input.
	vec, err := client.Embed(ctx, "vidlens-dim-probe")
	if err != nil {
		return 0, err
	}
	if len(vec) == 0 {
		return 0, fmt.Errorf("embedding 返回空向量")
	}
	return len(vec), nil
}
