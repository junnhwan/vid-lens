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

	// Preserve old installations that only set MIMO/SiliconFlow variables,
	// while avoiding inference when any new capability-specific setting exists.
	hasCapabilityConfig := strings.TrimSpace(cfg.LLMProvider) != "" ||
		strings.TrimSpace(cfg.LLMBaseURL) != "" ||
		strings.TrimSpace(cfg.LLMAPIKey) != "" ||
		strings.TrimSpace(cfg.ASRProvider) != "" ||
		strings.TrimSpace(cfg.ASRBaseURL) != "" ||
		strings.TrimSpace(cfg.ASRAPIKey) != "" ||
		strings.TrimSpace(cfg.EmbeddingProvider) != "" ||
		strings.TrimSpace(cfg.EmbeddingEndpoint) != "" ||
		strings.TrimSpace(cfg.EmbeddingAPIKey) != "" ||
		strings.TrimSpace(cfg.RerankProvider) != "" ||
		strings.TrimSpace(cfg.RerankEndpoint) != "" ||
		strings.TrimSpace(cfg.RerankAPIKey) != "" ||
		strings.TrimSpace(cfg.VisionProvider) != "" ||
		strings.TrimSpace(cfg.VisionBaseURL) != "" ||
		strings.TrimSpace(cfg.VisionAPIKey) != ""
	if globalProvider == "openai_compatible" && sharedBaseURL == "" && sharedAPIKey == "" && !hasCapabilityConfig {
		switch {
		case strings.TrimSpace(cfg.MimoAPIKey) != "":
			globalProvider = "mimo"
		case strings.TrimSpace(cfg.SiliconFlowAPIKey) != "":
			globalProvider = "siliconflow"
		}
	}

	providerFor := func(specific string) string {
		if provider := normalizeAIProvider(specific); provider != "" {
			return provider
		}
		return globalProvider
	}
	baseURLFor := func(provider, specific string) string {
		if specific = strings.TrimSpace(specific); specific != "" {
			return specific
		}
		if sharedBaseURL != "" {
			return sharedBaseURL
		}
		switch provider {
		case "mimo":
			return strings.TrimSpace(cfg.MimoBaseURL)
		case "siliconflow":
			return strings.TrimSpace(cfg.SiliconFlowBaseURL)
		default:
			return ""
		}
	}
	apiKeyFor := func(provider, specific string) string {
		if specific = strings.TrimSpace(specific); specific != "" {
			return specific
		}
		if sharedAPIKey != "" {
			return sharedAPIKey
		}
		switch provider {
		case "mimo":
			return strings.TrimSpace(cfg.MimoAPIKey)
		case "siliconflow":
			return strings.TrimSpace(cfg.SiliconFlowAPIKey)
		default:
			return ""
		}
	}

	llmProvider := providerFor(cfg.LLMProvider)
	asrProvider := providerFor(cfg.ASRProvider)
	embeddingProvider := providerFor(cfg.EmbeddingProvider)
	rerankProvider := providerFor(cfg.RerankProvider)
	visionProvider := providerFor(cfg.VisionProvider)

	embeddingEndpoint := strings.TrimSpace(cfg.EmbeddingEndpoint)
	if embeddingEndpoint == "" && embeddingProvider != "mimo" {
		if baseURL := baseURLFor(embeddingProvider, ""); baseURL != "" {
			embeddingEndpoint = strings.TrimRight(baseURL, "/") + "/embeddings"
		}
	}

	return ai.Profile{
		LLMProvider:       llmProvider,
		LLMBaseURL:        baseURLFor(llmProvider, cfg.LLMBaseURL),
		LLMAPIKey:         apiKeyFor(llmProvider, cfg.LLMAPIKey),
		LLMModel:          strings.TrimSpace(cfg.LLMModel),
		ASRProvider:       asrProvider,
		ASRBaseURL:        baseURLFor(asrProvider, cfg.ASRBaseURL),
		ASRAPIKey:         apiKeyFor(asrProvider, cfg.ASRAPIKey),
		ASRModel:          strings.TrimSpace(cfg.ASRModel),
		EmbeddingProvider: embeddingProvider,
		EmbeddingEndpoint: embeddingEndpoint,
		EmbeddingAPIKey:   apiKeyFor(embeddingProvider, cfg.EmbeddingAPIKey),
		EmbeddingModel:    strings.TrimSpace(cfg.EmbeddingModel),
		EmbeddingDim:      cfg.EmbeddingDim,
		RerankProvider:    rerankProvider,
		RerankEndpoint:    strings.TrimSpace(cfg.RerankEndpoint),
		RerankAPIKey:      strings.TrimSpace(cfg.RerankAPIKey),
		RerankModel:       strings.TrimSpace(cfg.RerankModel),
		VisionProvider:    visionProvider,
		VisionBaseURL:     baseURLFor(visionProvider, cfg.VisionBaseURL),
		VisionAPIKey:      apiKeyFor(visionProvider, cfg.VisionAPIKey),
		VisionModel:       strings.TrimSpace(cfg.VisionModel),
	}
}

func normalizeAIProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
