package ai

import (
	"context"
	"fmt"
	"strings"
)

type Profile struct {
	LLMProvider       string
	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	ASRProvider       string
	ASRBaseURL        string
	ASRAPIKey         string
	ASRModel          string
	EmbeddingProvider string
	EmbeddingEndpoint string
	EmbeddingAPIKey   string
	EmbeddingModel    string
	EmbeddingDim      int
}

type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) NewASRStrategy(profile Profile) (Strategy, error) {
	switch normalizeProvider(profile.ASRProvider) {
	case "mimo":
		return NewMimoStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	case "siliconflow":
		return NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	case "openai_compatible":
		return NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	default:
		return nil, fmt.Errorf("不支持的 ASR provider: %s", profile.ASRProvider)
	}
}

func (f *Factory) NewChatClient(profile Profile) (ChatClient, error) {
	switch normalizeProvider(profile.LLMProvider) {
	case "openai_compatible", "siliconflow":
		return NewOpenAIChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), nil
	case "mimo":
		return NewMimoChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), nil
	default:
		return nil, fmt.Errorf("不支持的 LLM provider: %s", profile.LLMProvider)
	}
}

func (f *Factory) NewEmbeddingClient(profile Profile) (EmbeddingClient, error) {
	switch normalizeProvider(profile.EmbeddingProvider) {
	case "openai_compatible", "siliconflow":
		return NewOpenAIEmbeddingClient(profile.EmbeddingEndpoint, profile.EmbeddingAPIKey, profile.EmbeddingModel), nil
	default:
		return nil, fmt.Errorf("不支持的 Embedding provider: %s", profile.EmbeddingProvider)
	}
}

func (f *Factory) NewAnalysisStrategy(profile Profile) (Strategy, error) {
	asr, err := f.NewASRStrategy(profile)
	if err != nil {
		return nil, err
	}
	chat, err := f.NewChatClient(profile)
	if err != nil {
		return nil, err
	}
	return &CompositeStrategy{asr: asr, chat: chat}, nil
}

type CompositeStrategy struct {
	asr  Strategy
	chat ChatClient
}

func (s *CompositeStrategy) Transcribe(ctx context.Context, audioPath string) (string, error) {
	return s.asr.Transcribe(ctx, audioPath)
}

func (s *CompositeStrategy) TranscribeChunks(ctx context.Context, audioPaths []string) (string, error) {
	return s.asr.TranscribeChunks(ctx, audioPaths)
}

func (s *CompositeStrategy) Summarize(ctx context.Context, text string) (string, error) {
	return s.chat.Chat(ctx, []ChatMessage{
		{Role: "system", Content: defaultSummarySystemPrompt()},
		{Role: "user", Content: text},
	})
}

type ProfileTester struct {
	factory *Factory
}

func NewProfileTester(factory *Factory) *ProfileTester {
	return &ProfileTester{factory: factory}
}

func (t *ProfileTester) TestProfile(ctx context.Context, profile Profile) error {
	chatClient, err := t.factory.NewChatClient(profile)
	if err != nil {
		return err
	}
	if _, err := chatClient.Chat(ctx, []ChatMessage{
		{Role: "system", Content: "Return a short health check response."},
		{Role: "user", Content: "ping"},
	}); err != nil {
		return err
	}

	embeddingClient, err := t.factory.NewEmbeddingClient(profile)
	if err != nil {
		return err
	}
	vector, err := embeddingClient.Embed(ctx, "VidLens embedding health check")
	if err != nil {
		return err
	}
	if profile.EmbeddingDim > 0 && len(vector) != profile.EmbeddingDim {
		return fmt.Errorf("Embedding 维度不匹配: 返回 %d，配置 %d", len(vector), profile.EmbeddingDim)
	}
	return nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
