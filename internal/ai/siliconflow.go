package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

// SiliconFlowStrategy 基于硅基流动平台的 AI 策略实现
// 面试亮点：选型理由 —— 免费额度、一个平台覆盖 ASR + LLM、中文效果好
type SiliconFlowStrategy struct {
	apiKey   string
	baseURL  string
	asrModel string
	llmModel string
	client   *http.Client
}

// NewSiliconFlowStrategy 创建硅基流动策略实例
func NewSiliconFlowStrategy(apiKey, baseURL, asrModel, llmModel string) *SiliconFlowStrategy {
	return &SiliconFlowStrategy{
		apiKey:   apiKey,
		baseURL:  baseURL,
		asrModel: asrModel,
		llmModel: llmModel,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Transcribe 调用 ASR 接口：音频 → 文字
// 面试亮点：指数退避重试（1s → 2s → 4s），客户端错误不重试
func (s *SiliconFlowStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			waitTime := time.Second * time.Duration(1<<(attempt-1)) // 1s, 2s, 4s
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(waitTime):
			}
		}

		text, err := s.doTranscribe(ctx, audioPath)
		if err == nil {
			return text, nil
		}
		lastErr = err

		// 客户端错误（如 401/400）不重试
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "400") {
			break
		}
	}

	return "", fmt.Errorf("ASR 重试 3 次后仍失败: %w", lastErr)
}

// doTranscribe 执行一次 ASR 请求
func (s *SiliconFlowStrategy) doTranscribe(ctx context.Context, audioPath string) (string, error) {
	// 读取音频文件
	fileBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("读取音频文件失败: %w", err)
	}

	// 构建 multipart 请求
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "audio.mp3")
	if err != nil {
		return "", fmt.Errorf("创建表单文件失败: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return "", err
	}

	writer.WriteField("model", s.asrModel)
	writer.Close()

	// 发送请求
	url := s.baseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ASR 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ASR 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 ASR 响应失败: %w", err)
	}

	return result.Text, nil
}

// Summarize 调用 LLM 接口生成总结
// 面试亮点：结构化 Prompt（Role + Task + Constraint + Output Format）
func (s *SiliconFlowStrategy) Summarize(ctx context.Context, text string) (string, error) {
	systemPrompt := `# Role
你是一位拥有认知心理学背景的资深信息架构师。你的专长是从杂乱的语音转录文本中提取高价值信息，并进行逻辑重构。

# Input Context
用户将提供一段由视频生成的语音识别（ASR）文本。文本可能包含口语废话、重复、语气词或识别错误。

# Goals
请忽略文本中的噪音，对内容进行深度降噪和逻辑精炼，最终输出一份结构清晰、语气专业的分析报告。

# Constraints
1. **必须**严格遵守下方的输出格式。
2. 语气保持客观、理性、犀利。
3. 如果文本内容过短或无意义，直接输出"无法提取有效信息"。
4. 禁止输出任何开场白或结束语（如"好的，我来分析..."），直接输出 Markdown 内容。

# Output Format (Markdown)
请严格按照以下模块输出：

## 核心摘要
（精简概括视频到底讲了什么，直击本质，全面贴切，但要一针见血地概括视频主旨。）

## 深度洞察
（提取 3-5 个核心观点，每个观点使用三级标题格式）

### 1. [4-8 字强观点标题]
不要复述原话。请用专业的语言解释这个观点背后的逻辑、动因或对观众的启示。

### 2. [第二个强观点标题]
（对应的深度分析...）

### 3. [第三个强观点标题]
（对应的深度分析...）

## 原始内容精选
> "引用视频中最有价值的原话（修正错别字后）"

## 🏷️ 领域标签
#标签1 #标签2 #标签3`

	reqBody := map[string]interface{}{
		"model":  s.llmModel,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)
	url := s.baseURL + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
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

	content := stripThinkTags(result.Choices[0].Message.Content)
	return content, nil
}

// stripThinkTags 清理 DeepSeek R1 模型的思考过程标签
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think")
		if end == -1 {
			break
		}
		s = s[:start] + s[end+len("</think"):]
		s = strings.TrimPrefix(s, ">")
		s = strings.TrimPrefix(s, "\n")
	}
	return strings.TrimSpace(s)
}
