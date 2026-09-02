package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	transport *protocolClient
	model     string
}

func NewOpenAIChatClient(baseURL, apiKey, model string) *OpenAIChatClient {
	return &OpenAIChatClient{
		transport: newProtocolClient(baseURL, apiKey, "openai_compatible", 5*time.Minute),
		model:     strings.TrimSpace(model),
	}
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

	req, err := c.transport.newRequest(ctx, http.MethodPost, "chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.transport.do(req, "chat")
	if err != nil {
		return "", err
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

	req, err := c.transport.newRequest(ctx, http.MethodPost, "chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.transport.send(req, "chat_stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
