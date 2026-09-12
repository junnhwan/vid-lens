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

// ChatFinishError means the provider ended generation without a complete answer.
// PartialContent is preserved for the caller's usage accounting and recovery UI.
type ChatFinishError struct {
	Reason         string
	PartialContent string
}

func (e *ChatFinishError) Error() string {
	switch e.Reason {
	case "length":
		return "模型输出达到单次输出上限，回答未完整生成"
	case "content_filter":
		return "模型服务因内容过滤停止输出，回答未完整生成"
	default:
		return "模型输出未正常完成: " + e.Reason
	}
}
func chatFinishError(reason, partial string) error {
	if reason == "length" || reason == "content_filter" {
		return &ChatFinishError{Reason: reason, PartialContent: partial}
	}
	return nil
}

type chatProviderUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
}

func (u *chatProviderUsage) report(ctx context.Context) {
	if u != nil && u.PromptTokens != nil && u.CompletionTokens != nil && *u.PromptTokens >= 0 && *u.CompletionTokens >= 0 {
		reportChatUsage(ctx, *u.PromptTokens, *u.CompletionTokens)
	}
}

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

	applyChatBudget(ctx, reqBody)
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
		Usage   *chatProviderUsage `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	result.Usage.report(ctx)
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLM 返回空结果")
	}
	answer := strings.TrimSpace(stripThinkTags(result.Choices[0].Message.Content))
	return answer, chatFinishError(result.Choices[0].FinishReason, answer)
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

	applyChatBudget(ctx, reqBody)
	if _, ok := ctx.Value(chatBudgetKey{}).(chatCallBudget); ok {
		reqBody["stream_options"] = map[string]bool{"include_usage": true}
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
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return fmt.Errorf("模型未返回 SSE 流式响应，请检查模型服务的 streaming 支持")
	}
	var splitter thinkingSplitter
	var partial strings.Builder
	output := func(reasoning bool, text string) error {
		if reasoning {
			return emitProviderReasoning(ctx, text)
		}
		partial.WriteString(text)
		return emit(text)
	}
	finished := false
	finishReason := ""
	finish := func() error {
		if err := splitter.push("", true, output); err != nil {
			return err
		}
		return chatFinishError(finishReason, partial.String())
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
			return finish()
		}
		var envelope struct {
			Usage *chatProviderUsage `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			return fmt.Errorf("解析 LLM 流式响应失败: %w", err)
		}
		envelope.Usage.report(ctx)
		if envelope.Error != nil {
			return fmt.Errorf("模型流式响应失败: %s", envelope.Error.Message)
		}
		if len(envelope.Choices) > 0 {
			choice := envelope.Choices[0]
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finished = true
				finishReason = *choice.FinishReason
			}
			reasoning := choice.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = choice.Delta.Reasoning
			}
			if err := emitProviderReasoning(ctx, reasoning); err != nil {
				return err
			}
		}
		delta, err := parseChatCompletionStreamDelta(data)
		if err != nil {
			return err
		}
		if delta == "" {
			continue
		}
		if err := splitter.push(delta, false, output); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 LLM 流式响应失败: %w", err)
	}
	if !finished {
		return fmt.Errorf("模型流式响应意外中断，未收到完成标记")
	}
	return finish()
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
