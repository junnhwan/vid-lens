package ai

import (
	"fmt"
	"strings"
)

// ActionCapabilities is the model dependency contract. Resource readiness and
// ownership remain the responsibility of the existing submission services.
var actionCapabilities = map[string][]string{
	"upload": {}, "align": {}, "ocr": {},
	"transcribe": {"asr"}, "summary": {"llm"}, "study": {"llm"}, "revise": {"llm"},
	"caption": {"vision"}, "index": {"embedding"},
	"chat": {"llm", "embedding"}, "agent": {"llm", "embedding"},
}

func ActionCapabilities(action string) ([]string, bool) {
	keys, ok := actionCapabilities[action]
	return append([]string{}, keys...), ok
}

func ModelConfigured(p Profile, key string) bool {
	var provider, endpoint, secret, model string
	switch key {
	case "llm":
		provider, endpoint, secret, model = p.LLMProvider, p.LLMBaseURL, p.LLMAPIKey, p.LLMModel
	case "asr":
		provider, endpoint, secret, model = p.ASRProvider, p.ASRBaseURL, p.ASRAPIKey, p.ASRModel
	case "embedding":
		if p.EmbeddingDim <= 0 {
			return false
		}
		provider, endpoint, secret, model = p.EmbeddingProvider, p.EmbeddingEndpoint, p.EmbeddingAPIKey, p.EmbeddingModel
	case "vision":
		provider, endpoint, secret, model = p.VisionProvider, p.VisionBaseURL, p.VisionAPIKey, p.VisionModel
	default:
		return false
	}
	return strings.TrimSpace(provider) != "" && strings.TrimSpace(endpoint) != "" && strings.TrimSpace(secret) != "" && strings.TrimSpace(model) != ""
}

func RequireAction(p Profile, action string) error {
	keys, ok := ActionCapabilities(action)
	if !ok {
		return fmt.Errorf("未知 AI 操作: %s", action)
	}
	for _, key := range keys {
		if !ModelConfigured(p, key) {
			return fmt.Errorf("%s 配置不完整，请检查默认 AI 服务", key)
		}
	}
	return nil
}
