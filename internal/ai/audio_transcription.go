package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"vid-lens/internal/model"
)

// AudioTranscriptionClient implements the widely supported OpenAI-style
// multipart audio transcription protocol:
// POST /audio/transcriptions with file + model form fields.
type AudioTranscriptionClient interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

type OpenAIAudioTranscriptionClient struct {
	transport         *protocolClient
	model             string
	formatUnsupported atomic.Bool
}

func NewOpenAIAudioTranscriptionClient(baseURL, apiKey, model string) *OpenAIAudioTranscriptionClient {
	return &OpenAIAudioTranscriptionClient{
		transport: newProtocolClient(baseURL, apiKey, "openai_compatible", 5*time.Minute),
		model:     strings.TrimSpace(model),
	}
}

func (c *OpenAIAudioTranscriptionClient) Transcribe(ctx context.Context, audioPath string) (string, error) {
	result, err := c.TranscribeDetailed(ctx, audioPath)
	return result.Text, err
}

func (c *OpenAIAudioTranscriptionClient) TranscribeDetailed(ctx context.Context, audioPath string) (TranscriptionResult, error) {
	if c == nil || c.transport == nil {
		return TranscriptionResult{}, fmt.Errorf("audio transcription client is nil")
	}
	if strings.TrimSpace(c.transport.baseURL) == "" {
		return TranscriptionResult{}, fmt.Errorf("audio transcription base URL is empty")
	}
	if strings.TrimSpace(c.model) == "" {
		return TranscriptionResult{}, fmt.Errorf("audio transcription model is empty")
	}

	fileBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("读取音频文件失败: %w", err)
	}
	// Whisper-compatible models expose native segment timing in verbose_json.
	// Other models keep their default response format; native segments in that
	// response are still retained. Unsupported format rejection is the only
	// condition that permits a second, default-format request.
	timedFormat := strings.Contains(strings.ToLower(c.model), "whisper") && !c.formatUnsupported.Load()
	responseBody, err := c.requestTranscription(ctx, audioPath, fileBytes, timedFormat)
	if err != nil && timedFormat && unsupportedTranscriptionFormat(err) {
		c.formatUnsupported.Store(true)
		responseBody, err = c.requestTranscription(ctx, audioPath, fileBytes, false)
	}
	if err != nil {
		return TranscriptionResult{}, err
	}
	return parseTranscriptionResult(responseBody)
}

func (c *OpenAIAudioTranscriptionClient) requestTranscription(ctx context.Context, audioPath string, fileBytes []byte, timedFormat bool) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filename := filepath.Base(audioPath)
	if filename == "." || filename == "" || filename == string(filepath.Separator) {
		filename = "audio"
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("创建音频表单失败: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, fmt.Errorf("写入音频表单失败: %w", err)
	}
	if err := writer.WriteField("model", c.model); err != nil {
		return nil, fmt.Errorf("写入 ASR 模型失败: %w", err)
	}
	if timedFormat {
		if err := writer.WriteField("response_format", "verbose_json"); err != nil {
			return nil, err
		}
		if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭音频表单失败: %w", err)
	}

	req, err := c.transport.newRequest(ctx, http.MethodPost, "audio/transcriptions", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return c.transport.do(req, "asr")
}

func unsupportedTranscriptionFormat(err error) bool {
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(providerErr.SafeMessage)
	if !strings.Contains(message, "response_format") && !strings.Contains(message, "verbose_json") && !strings.Contains(message, "timestamp_granularities") {
		return false
	}
	for _, phrase := range []string{"not support", "unsupported", "not allowed", "invalid", "unrecognized", "unknown", "not permitted"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func parseTranscriptionResult(responseBody []byte) (TranscriptionResult, error) {
	type nativeSegment struct {
		Text  string   `json:"text"`
		Start *float64 `json:"start"`
		End   *float64 `json:"end"`
	}
	var result struct {
		Text     *string           `json:"text"`
		Segments []json.RawMessage `json:"segments"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return TranscriptionResult{}, fmt.Errorf("解析 ASR 响应失败: %w", err)
	}
	if result.Text == nil {
		return TranscriptionResult{}, fmt.Errorf("ASR 响应缺少有效 text 字段")
	}
	out := TranscriptionResult{Text: strings.TrimSpace(*result.Text)}
	// Individual word timings need exact source alignment and semantic grouping
	// before they can become quote evidence. Keep word-only responses coarse.
	for _, rawSegment := range result.Segments {
		var segment nativeSegment
		if err := json.Unmarshal(rawSegment, &segment); err != nil {
			continue
		}
		text := strings.TrimSpace(segment.Text)
		if text == "" || segment.Start == nil || segment.End == nil ||
			math.IsNaN(*segment.Start) || math.IsInf(*segment.Start, 0) ||
			math.IsNaN(*segment.End) || math.IsInf(*segment.End, 0) ||
			*segment.Start < 0 || *segment.End <= *segment.Start || *segment.End >= float64(math.MaxInt64)/1000 {
			continue
		}
		startMS, endMS := int64(math.Round(*segment.Start*1000)), int64(math.Round(*segment.End*1000))
		if endMS <= startMS {
			continue
		}
		out.Segments = append(out.Segments, model.TranscriptionSegment{Text: text, StartMS: startMS, EndMS: endMS})
	}
	if out.Text == "" && len(result.Segments) > 0 {
		return TranscriptionResult{}, fmt.Errorf("ASR 空文字响应包含不一致的语音片段")
	}
	// An explicit empty text and no segments is a successful silent window.
	// Only the assembled whole video should fail when it contains no speech.
	return out, nil
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

func (s *transcriptionStrategy) TranscribeDetailed(ctx context.Context, audioPath string) (TranscriptionResult, error) {
	return TranscribeDetailed(ctx, s.client, audioPath)
}

func (s *transcriptionStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	return transcribeChunks(ctx, s.client, audioPaths)
}

func (s *transcriptionStrategy) Summarize(context.Context, string) (string, error) {
	return "", fmt.Errorf("ASR strategy does not provide summarization")
}
