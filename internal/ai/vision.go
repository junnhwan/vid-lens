package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VisionClient captions a single image for visual indexing (PPT/board/scene).
// Product path: turn keyframes into searchable text when ASR cannot hear on-screen content.
type VisionClient interface {
	CaptionImage(ctx context.Context, imagePath, prompt string) (string, error)
}

// DefaultVisionCaptionPrompt asks for on-screen text first, then a short scene note.
const DefaultVisionCaptionPrompt = `你是长视频画面理解助手。请阅读这张关键帧，用中文简洁输出：
1) 画面上可读到的文字（PPT/板书/字幕/UI），尽量原文；没有则写「无可见文字」
2) 一句话说明画面在讲什么（场景/对象/动作）
不要编造看不清的内容。输出纯文本，不要 Markdown 标题。`

// OpenAIVisionClient calls OpenAI-compatible /chat/completions with image_url parts.
type OpenAIVisionClient struct {
	baseURL    string
	apiKey     string
	model      string
	authHeader string
	authPrefix string
	client     *http.Client
}

func NewOpenAIVisionClient(baseURL, apiKey, model string) *OpenAIVisionClient {
	return &OpenAIVisionClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		authHeader: "Authorization",
		authPrefix: "Bearer ",
		client:     &http.Client{Timeout: 3 * time.Minute},
	}
}

func NewMimoVisionClient(baseURL, apiKey, model string) *OpenAIVisionClient {
	c := NewOpenAIVisionClient(baseURL, apiKey, model)
	c.authHeader = "api-key"
	c.authPrefix = ""
	return c
}

func (c *OpenAIVisionClient) CaptionImage(ctx context.Context, imagePath, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("vision client is nil")
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = DefaultVisionCaptionPrompt
	}
	dataURL, err := imageFileToDataURL(imagePath)
	if err != nil {
		return "", err
	}

	// Multimodal content must be a JSON array; text-only ChatMessage cannot carry images.
	reqBody := map[string]interface{}{
		"model":  c.model,
		"stream": false,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]string{"url": dataURL}},
				},
			},
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set(c.authHeader, c.authPrefix+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", ProviderTransportError("openai_compatible", "vision", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", ProviderHTTPError("openai_compatible", "vision", resp.StatusCode, resp.Header, body)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 vision 响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("vision 返回空结果")
	}
	return strings.TrimSpace(stripThinkTags(result.Choices[0].Message.Content)), nil
}

func imageFileToDataURL(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("image is empty")
	}
	mime := "image/jpeg"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	case ".gif":
		mime = "image/gif"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}
