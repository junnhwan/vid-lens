package ai

import (
	"context"
	"strings"
)

// OpenAICompatibleStrategy composes the standard audio transcription and
// chat-completion adapters used by the server's default strategy.
type OpenAICompatibleStrategy struct {
	asr  AudioTranscriptionClient
	chat ChatClient
}

func NewOpenAICompatibleStrategy(apiKey, baseURL, asrModel, llmModel string) *OpenAICompatibleStrategy {
	return &OpenAICompatibleStrategy{
		asr:  NewOpenAIAudioTranscriptionClient(baseURL, apiKey, asrModel),
		chat: NewOpenAIChatClient(baseURL, apiKey, llmModel),
	}
}

func (s *OpenAICompatibleStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	return s.asr.Transcribe(ctx, audioPath)
}

func (s *OpenAICompatibleStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	return transcribeChunks(ctx, s.asr, audioPaths)
}

func (s *OpenAICompatibleStrategy) Summarize(ctx context.Context, text string) (string, error) {
	content, err := s.chat.Chat(ctx, []ChatMessage{
		{Role: "system", Content: defaultSummarySystemPrompt()},
		{Role: "user", Content: text},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stripThinkTags(content)), nil
}
