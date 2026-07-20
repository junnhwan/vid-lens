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
	VisionProvider    string
	VisionBaseURL     string
	VisionAPIKey      string
	VisionModel       string
	RerankEndpoint    string
	RerankModel       string
}

type Factory struct{ admission Admission }

func NewFactory() *Factory                                 { return &Factory{} }
func NewFactoryWithAdmission(admission Admission) *Factory { return &Factory{admission: admission} }

func (f *Factory) NewASRStrategy(profile Profile) (Strategy, error) {
	switch normalizeProvider(profile.ASRProvider) {
	case "mimo":
		return AdmitStrategy(NewMimoStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), f.admission, "mimo", profile.ASRModel, profile.ASRModel), nil
	case "siliconflow":
		return AdmitStrategy(NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), f.admission, normalizeProvider(profile.ASRProvider), profile.ASRModel, profile.ASRModel), nil
	case "openai_compatible":
		return AdmitStrategy(NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), f.admission, normalizeProvider(profile.ASRProvider), profile.ASRModel, profile.ASRModel), nil
	default:
		return nil, fmt.Errorf("不支持的 ASR provider: %s", profile.ASRProvider)
	}
}

func (f *Factory) NewChatClient(profile Profile) (ChatClient, error) {
	switch normalizeProvider(profile.LLMProvider) {
	case "openai_compatible", "siliconflow":
		return AdmitChat(NewOpenAIChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), f.admission, normalizeProvider(profile.LLMProvider), profile.LLMModel), nil
	case "mimo":
		return AdmitChat(NewMimoChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), f.admission, "mimo", profile.LLMModel), nil
	default:
		return nil, fmt.Errorf("不支持的 LLM provider: %s", profile.LLMProvider)
	}
}

func (f *Factory) NewEmbeddingClient(profile Profile) (EmbeddingClient, error) {
	switch normalizeProvider(profile.EmbeddingProvider) {
	case "openai_compatible", "siliconflow":
		return AdmitEmbedding(NewOpenAIEmbeddingClient(profile.EmbeddingEndpoint, profile.EmbeddingAPIKey, profile.EmbeddingModel), f.admission, normalizeProvider(profile.EmbeddingProvider), profile.EmbeddingModel), nil
	default:
		return nil, fmt.Errorf("不支持的 Embedding provider: %s", profile.EmbeddingProvider)
	}
}

func (f *Factory) NewRerankClient(profile Profile) (RerankClient, error) {
	switch normalizeProvider(profile.EmbeddingProvider) {
	case "openai_compatible", "siliconflow":
		endpoint := strings.TrimSpace(profile.RerankEndpoint)
		if endpoint == "" {
			derived, ok := deriveRerankEndpointFromEmbedding(profile.EmbeddingEndpoint)
			if !ok {
				return nil, fmt.Errorf("无法从 Embedding endpoint 推导 Rerank endpoint，请显式配置 rerank endpoint")
			}
			endpoint = derived
		}
		return NewOpenAIRerankClient(endpoint, profile.EmbeddingAPIKey, profile.RerankModel), nil
	default:
		return nil, fmt.Errorf("不支持的 Rerank provider: %s", profile.EmbeddingProvider)
	}
}

func (f *Factory) NewVisionClient(profile Profile) (VisionClient, error) {
	provider := normalizeProvider(profile.VisionProvider)
	baseURL := strings.TrimSpace(profile.VisionBaseURL)
	model := strings.TrimSpace(profile.VisionModel)
	apiKey := strings.TrimSpace(profile.VisionAPIKey)
	if provider == "" || baseURL == "" || model == "" || apiKey == "" {
		return nil, fmt.Errorf("vision 未配置")
	}
	switch provider {
	case "openai_compatible", "siliconflow":
		return AdmitVision(NewOpenAIVisionClient(baseURL, apiKey, model), f.admission, provider, model), nil
	case "mimo":
		return AdmitVision(NewMimoVisionClient(baseURL, apiKey, model), f.admission, "mimo", model), nil
	default:
		return nil, fmt.Errorf("不支持的 Vision provider: %s", profile.VisionProvider)
	}
}

// VisionConfigured reports whether the profile has a usable multimodal endpoint.
func VisionConfigured(profile Profile) bool {
	return strings.TrimSpace(profile.VisionProvider) != "" &&
		strings.TrimSpace(profile.VisionBaseURL) != "" &&
		strings.TrimSpace(profile.VisionModel) != "" &&
		strings.TrimSpace(profile.VisionAPIKey) != ""
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
		return fmt.Errorf("embedding 维度不匹配: 返回 %d，配置 %d", len(vector), profile.EmbeddingDim)
	}
	return nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
