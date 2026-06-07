package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatClient interface {
	Chat(ctx context.Context, messages []ChatMessage) (string, error)
}

type StreamingChatClient interface {
	StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error
}

type OpenAIChatClient struct {
	baseURL    string
	apiKey     string
	model      string
	authHeader string
	authPrefix string
	client     *http.Client
}

func NewOpenAIChatClient(baseURL, apiKey, model string) *OpenAIChatClient {
	return &OpenAIChatClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		authHeader: "Authorization",
		authPrefix: "Bearer ",
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func NewMimoChatClient(baseURL, apiKey, model string) *OpenAIChatClient {
	client := NewOpenAIChatClient(baseURL, apiKey, model)
	client.authHeader = "api-key"
	client.authPrefix = ""
	return client
}

func (c *OpenAIChatClient) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	reqBody := map[string]interface{}{
		"model":    c.model,
		"stream":   false,
		"messages": messages,
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
		return "", fmt.Errorf("LLM 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLM 返回空结果")
	}
	return strings.TrimSpace(stripThinkTags(result.Choices[0].Message.Content)), nil
}

func (c *OpenAIChatClient) StreamChat(ctx context.Context, messages []ChatMessage, emit func(delta string) error) error {
	if emit == nil {
		return fmt.Errorf("stream emit 不能为空")
	}
	reqBody := map[string]interface{}{
		"model":    c.model,
		"stream":   true,
		"messages": messages,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set(c.authHeader, c.authPrefix+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("LLM 流式请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LLM 流式返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		delta, err := parseChatCompletionStreamDelta(data)
		if err != nil {
			return err
		}
		if delta == "" {
			continue
		}
		if err := emit(delta); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 LLM 流式响应失败: %w", err)
	}
	return nil
}

func parseChatCompletionStreamDelta(data string) (string, error) {
	var result struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return "", fmt.Errorf("解析 LLM 流式响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", nil
	}
	return result.Choices[0].Delta.Content, nil
}
