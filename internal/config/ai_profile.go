package config

import (
	"strings"

	"vid-lens/internal/ai"
)

// Profile resolves process-level AI configuration into the same capability
// profile used by user BYOK settings and the protocol factory. Each
// capability can point at a different endpoint/key; the shared fields are
// only fallbacks for simple deployments.
func (cfg AIConfig) Profile() ai.Profile {
	globalProvider := normalizeAIProvider(cfg.Provider)
	if globalProvider == "" {
		globalProvider = "openai_compatible"
	}
	sharedBaseURL := strings.TrimSpace(cfg.BaseURL)
	sharedAPIKey := strings.TrimSpace(cfg.APIKey)

	providerFor := func(specific string) string {
		if provider := normalizeAIProvider(specific); provider != "" {
			return provider
		}
		return globalProvider
	}
	baseURLFor := func(specific string) string {
		if specific = strings.TrimSpace(specific); specific != "" {
			return specific
		}
		return sharedBaseURL
	}
	apiKeyFor := func(specific string) string {
		if specific = strings.TrimSpace(specific); specific != "" {
			return specific
		}
		return sharedAPIKey
	}

	llmProvider := providerFor(cfg.LLMProvider)
	asrProvider := providerFor(cfg.ASRProvider)
	embeddingProvider := providerFor(cfg.EmbeddingProvider)
	rerankProvider := providerFor(cfg.RerankProvider)
	visionProvider := providerFor(cfg.VisionProvider)

	embeddingEndpoint := strings.TrimSpace(cfg.EmbeddingEndpoint)
	if embeddingEndpoint == "" {
		if baseURL := baseURLFor(""); baseURL != "" {
			embeddingEndpoint = strings.TrimRight(baseURL, "/") + "/embeddings"
		}
	}

	return ai.Profile{
		LLMProvider:       llmProvider,
		LLMBaseURL:        baseURLFor(cfg.LLMBaseURL),
		LLMAPIKey:         apiKeyFor(cfg.LLMAPIKey),
		LLMModel:          strings.TrimSpace(cfg.LLMModel),
		ASRProvider:       asrProvider,
		ASRBaseURL:        baseURLFor(cfg.ASRBaseURL),
		ASRAPIKey:         apiKeyFor(cfg.ASRAPIKey),
		ASRModel:          strings.TrimSpace(cfg.ASRModel),
		EmbeddingProvider: embeddingProvider,
		EmbeddingEndpoint: embeddingEndpoint,
		EmbeddingAPIKey:   apiKeyFor(cfg.EmbeddingAPIKey),
		EmbeddingModel:    strings.TrimSpace(cfg.EmbeddingModel),
		EmbeddingDim:      cfg.EmbeddingDim,
		RerankProvider:    rerankProvider,
		RerankEndpoint:    strings.TrimSpace(cfg.RerankEndpoint),
		RerankAPIKey:      strings.TrimSpace(cfg.RerankAPIKey),
		RerankModel:       strings.TrimSpace(cfg.RerankModel),
		VisionProvider:    visionProvider,
		VisionBaseURL:     baseURLFor(cfg.VisionBaseURL),
		VisionAPIKey:      apiKeyFor(cfg.VisionAPIKey),
		VisionModel:       strings.TrimSpace(cfg.VisionModel),
	}
}

func normalizeAIProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
