package config

import "testing"

func TestAIConfigProfileResolvesIndependentCapabilityEndpoints(t *testing.T) {
	profile := (AIConfig{
		Provider:          "openai_compatible",
		LLMBaseURL:        "https://chat.example.com/v1",
		LLMAPIKey:         "chat-key",
		LLMModel:          "chat-model",
		ASRBaseURL:        "https://asr.example.com/v1",
		ASRAPIKey:         "asr-key",
		ASRModel:          "asr-model",
		EmbeddingEndpoint: "https://embedding.example.com/v1/embeddings",
		EmbeddingAPIKey:   "embedding-key",
		EmbeddingModel:    "embedding-model",
		EmbeddingDim:      1024,
		RerankEndpoint:    "https://rerank.example.com/v1/rerank",
		RerankAPIKey:      "rerank-key",
		RerankModel:       "rerank-model",
		VisionBaseURL:     "https://vision.example.com/v1",
		VisionAPIKey:      "vision-key",
		VisionModel:       "vision-model",
	}).Profile()

	if profile.LLMBaseURL != "https://chat.example.com/v1" || profile.LLMAPIKey != "chat-key" || profile.LLMModel != "chat-model" {
		t.Fatalf("LLM profile = %+v", profile)
	}
	if profile.ASRBaseURL != "https://asr.example.com/v1" || profile.ASRAPIKey != "asr-key" || profile.ASRModel != "asr-model" {
		t.Fatalf("ASR profile = %+v", profile)
	}
	if profile.EmbeddingEndpoint != "https://embedding.example.com/v1/embeddings" || profile.EmbeddingAPIKey != "embedding-key" || profile.EmbeddingDim != 1024 {
		t.Fatalf("Embedding profile = %+v", profile)
	}
	if profile.RerankEndpoint != "https://rerank.example.com/v1/rerank" || profile.RerankAPIKey != "rerank-key" || profile.RerankModel != "rerank-model" {
		t.Fatalf("Rerank profile = %+v", profile)
	}
	if profile.VisionBaseURL != "https://vision.example.com/v1" || profile.VisionAPIKey != "vision-key" || profile.VisionModel != "vision-model" {
		t.Fatalf("Vision profile = %+v", profile)
	}
}

func TestAIConfigProfileNormalizesProviderLabels(t *testing.T) {
	profile := (AIConfig{
		Provider:    "openai_compatible",
		LLMBaseURL:  "https://chat.example.com/v1",
		LLMAPIKey:   "chat-key",
		LLMModel:    "chat-model",
		ASRProvider: "relay-a",
		ASRBaseURL:  "https://asr.example.com/v1",
		ASRAPIKey:   "asr-key",
		ASRModel:    "asr-model",
	}).Profile()

	if profile.LLMProvider != "openai_compatible" || profile.LLMBaseURL != "https://chat.example.com/v1" {
		t.Fatalf("LLM profile = %+v", profile)
	}
	if profile.ASRProvider != "relay-a" || profile.ASRBaseURL != "https://asr.example.com/v1" || profile.ASRAPIKey != "asr-key" {
		t.Fatalf("ASR labeled profile = %+v", profile)
	}
}
