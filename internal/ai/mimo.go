package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const mimoMaxAudioDataBytes = 10 * 1024 * 1024

// MimoStrategy 基于小米 MiMo API 的 AI 策略实现。
// ASR 与 LLM 均走 OpenAI-compatible chat/completions 接口。
type MimoStrategy struct {
	transport *protocolClient
	asrModel  string
	llmModel  string
}

func NewMimoStrategy(apiKey, baseURL, asrModel, llmModel string) *MimoStrategy {
	if baseURL == "" {
		baseURL = "https://api.xiaomimimo.com/v1"
	}
	if asrModel == "" {
		asrModel = "mimo-v2.5-asr"
	}
	if llmModel == "" {
		llmModel = "mimo-v2.5"
	}
	return &MimoStrategy{
		transport: newLegacyMimoProtocolClient(baseURL, apiKey, 5*time.Minute),
		asrModel:  asrModel,
		llmModel:  llmModel,
	}
}

func (s *MimoStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	dataURL, err := audioDataURL(audioPath)
	if err != nil {
		return "", err
	}
	if len(dataURL) > mimoMaxAudioDataBytes {
		return "", fmt.Errorf("MiMo ASR 音频 base64 超过 10MB，请压缩音频或按片段转录")
	}

	reqBody := map[string]interface{}{
		"model":  s.asrModel,
		"stream": false,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "input_audio",
						"input_audio": map[string]string{
							"data": dataURL,
						},
					},
				},
			},
		},
		"asr_options": map[string]string{
			"language": "auto",
		},
	}

	return s.chatCompletion(ctx, reqBody, "MiMo ASR")
}

func (s *MimoStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	if len(audioPaths) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}

	parts := make([]string, 0, len(audioPaths))
	for i, audioPath := range audioPaths {
		text, err := s.Transcribe(ctx, audioPath)
		if err != nil {
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, err)
		}
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("MiMo ASR 返回空结果")
	}
	return strings.Join(parts, "\n\n"), nil
}

func (s *MimoStrategy) Summarize(ctx context.Context, text string) (string, error) {
	reqBody := map[string]interface{}{
		"model":  s.llmModel,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": defaultSummarySystemPrompt()},
			{"role": "user", "content": text},
		},
	}

	content, err := s.chatCompletion(ctx, reqBody, "MiMo LLM")
	if err != nil {
		return "", err
	}
	return stripThinkTags(content), nil
}

func (s *MimoStrategy) chatCompletion(ctx context.Context, reqBody map[string]interface{}, label string) (string, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := s.transport.newRequest(ctx, http.MethodPost, "chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := s.transport.do(req, label)
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
		return "", fmt.Errorf("解析 %s 响应失败: %w", label, err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("%s 返回空结果", label)
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func audioDataURL(audioPath string) (string, error) {
	fileBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("读取音频文件失败: %w", err)
	}

	mimeType := "audio/mpeg"
	if strings.EqualFold(filepath.Ext(audioPath), ".wav") {
		mimeType = "audio/wav"
	}

	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(fileBytes)), nil
}
