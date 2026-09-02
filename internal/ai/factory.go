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
	// Rerank is intentionally runtime-only for now: the production profile
	// schema does not enable model rerank, while the legacy eval path can
	// provide an explicit endpoint/model and optionally a separate key.
	RerankProvider string
	RerankEndpoint string
	RerankAPIKey   string
	RerankModel    string
}

type Factory struct{ admission Admission }

func NewFactory() *Factory                                 { return &Factory{} }
func NewFactoryWithAdmission(admission Admission) *Factory { return &Factory{admission: admission} }

func (f *Factory) NewASRStrategy(profile Profile) (Strategy, error) {
	provider := profileProvider(profile.ASRProvider)
	asr := &transcriptionStrategy{client: NewOpenAIAudioTranscriptionClient(profile.ASRBaseURL, profile.ASRAPIKey, profile.ASRModel)}
	return AdmitStrategy(asr, f.admission, provider, profile.ASRModel, profile.ASRModel), nil
}

func (f *Factory) NewChatClient(profile Profile) (ChatClient, error) {
	provider := profileProvider(profile.LLMProvider)
	return AdmitChat(NewOpenAIChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), f.admission, provider, profile.LLMModel), nil
}

func (f *Factory) NewEmbeddingClient(profile Profile) (EmbeddingClient, error) {
	provider := profileProvider(profile.EmbeddingProvider)
	return AdmitEmbedding(NewOpenAIEmbeddingClient(profile.EmbeddingEndpoint, profile.EmbeddingAPIKey, profile.EmbeddingModel), f.admission, provider, profile.EmbeddingModel), nil
}

func (f *Factory) NewRerankClient(profile Profile) (RerankClient, error) {
	provider := profileProvider(profile.RerankProvider)
	if normalizeProvider(profile.RerankProvider) == "" {
		provider = profileProvider(profile.EmbeddingProvider)
	}
	endpoint := strings.TrimSpace(profile.RerankEndpoint)
	if endpoint == "" {
		derived, ok := deriveRerankEndpointFromEmbedding(profile.EmbeddingEndpoint)
		if !ok {
			return nil, fmt.Errorf("无法从 Embedding endpoint 推导 Rerank endpoint，请显式配置 rerank endpoint")
		}
		endpoint = derived
	}
	apiKey := strings.TrimSpace(profile.RerankAPIKey)
	if apiKey == "" {
		apiKey = profile.EmbeddingAPIKey
	}
	return NewOpenAIRerankClientWithProvider(endpoint, apiKey, profile.RerankModel, provider), nil
}

func (f *Factory) NewVisionClient(profile Profile) (VisionClient, error) {
	provider := profileProvider(profile.VisionProvider)
	baseURL := strings.TrimSpace(profile.VisionBaseURL)
	model := strings.TrimSpace(profile.VisionModel)
	apiKey := strings.TrimSpace(profile.VisionAPIKey)
	if provider == "" || baseURL == "" || model == "" || apiKey == "" {
		return nil, fmt.Errorf("vision 未配置")
	}
	return AdmitVision(NewOpenAIVisionClient(baseURL, apiKey, model), f.admission, provider, model), nil
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

func profileProvider(provider string) string {
	// Provider values on profiles are historical labels, not a closed enum.
	// Every label uses the standard OpenAI-compatible wire protocol; the raw
	// label is preserved for admission and observability records.
	if normalizeProvider(provider) == "" {
		return "openai_compatible"
	}
	return normalizeProvider(provider)
}
