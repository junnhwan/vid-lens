package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AudioTranscriptionClient implements the widely supported OpenAI-style
// multipart audio transcription protocol:
// POST /audio/transcriptions with file + model form fields.
type AudioTranscriptionClient interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

type OpenAIAudioTranscriptionClient struct {
	transport *protocolClient
	model     string
}

func NewOpenAIAudioTranscriptionClient(baseURL, apiKey, model string) *OpenAIAudioTranscriptionClient {
	return &OpenAIAudioTranscriptionClient{
		transport: newProtocolClient(baseURL, apiKey, "openai_compatible", 5*time.Minute),
		model:     strings.TrimSpace(model),
	}
}

func (c *OpenAIAudioTranscriptionClient) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if c == nil || c.transport == nil {
		return "", fmt.Errorf("audio transcription client is nil")
	}
	if strings.TrimSpace(c.transport.baseURL) == "" {
		return "", fmt.Errorf("audio transcription base URL is empty")
	}
	if strings.TrimSpace(c.model) == "" {
		return "", fmt.Errorf("audio transcription model is empty")
	}

	fileBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("读取音频文件失败: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filename := filepath.Base(audioPath)
	if filename == "." || filename == "" || filename == string(filepath.Separator) {
		filename = "audio"
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("创建音频表单失败: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return "", fmt.Errorf("写入音频表单失败: %w", err)
	}
	if err := writer.WriteField("model", c.model); err != nil {
		return "", fmt.Errorf("写入 ASR 模型失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("关闭音频表单失败: %w", err)
	}

	req, err := c.transport.newRequest(ctx, http.MethodPost, "audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	responseBody, err := c.transport.do(req, "asr")
	if err != nil {
		return "", err
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("解析 ASR 响应失败: %w", err)
	}
	if strings.TrimSpace(result.Text) == "" {
		return "", fmt.Errorf("ASR 返回空结果")
	}
	return strings.TrimSpace(result.Text), nil
}

func transcribeChunks(ctx context.Context, client AudioTranscriptionClient, audioPaths []string) (string, error) {
	if len(audioPaths) == 0 {
		return "", fmt.Errorf("没有可转写的音频片段")
	}

	parts := make([]string, 0, len(audioPaths))
	for i, audioPath := range audioPaths {
		text, err := client.Transcribe(ctx, audioPath)
		if err != nil {
			return "", fmt.Errorf("第 %d 段 ASR 失败: %w", i+1, err)
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ASR 返回空结果")
	}
	return strings.Join(parts, "\n\n"), nil
}

type transcriptionStrategy struct {
	client AudioTranscriptionClient
}

func (s *transcriptionStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	return s.client.Transcribe(ctx, audioPath)
}

func (s *transcriptionStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	return transcribeChunks(ctx, s.client, audioPaths)
}

func (s *transcriptionStrategy) Summarize(context.Context, string) (string, error) {
	return "", fmt.Errorf("ASR strategy does not provide summarization")
}
