package service

import (
	"fmt"
	"strings"
	"vid-lens/internal/model"
)

type profileRequestGroup struct {
	name                      string
	provider, url, key, model *string
	extra                     *int
}

func (g profileRequestGroup) present() bool {
	return strings.TrimSpace(*g.provider) != "" || strings.TrimSpace(*g.url) != "" || strings.TrimSpace(*g.key) != "" || strings.TrimSpace(*g.model) != "" || g.extra != nil && *g.extra != 0
}
func profileRequestGroups(r *AIProfileRequest) []profileRequestGroup {
	return []profileRequestGroup{
		{"llm", &r.LLMProvider, &r.LLMBaseURL, &r.LLMAPIKey, &r.LLMModel, &r.LLMContextTokens},
		{"asr", &r.ASRProvider, &r.ASRBaseURL, &r.ASRAPIKey, &r.ASRModel, nil},
		{"embedding", &r.EmbeddingProvider, &r.EmbeddingEndpoint, &r.EmbeddingAPIKey, &r.EmbeddingModel, &r.EmbeddingDim},
		{"vision", &r.VisionProvider, &r.VisionBaseURL, &r.VisionAPIKey, &r.VisionModel, nil},
	}
}

// Omitted/empty groups preserve existing configuration. Only clear_groups can
// remove a stored group, and combining removal with new fields is rejected.
func mergeProfileGroups(req AIProfileRequest, existing *model.UserAIProfile) (AIProfileRequest, error) {
	clear := map[string]bool{}
	for _, key := range req.ClearGroups {
		if existing == nil {
			return req, fmt.Errorf("新配置不能清除模型组")
		}
		if key != "llm" && key != "asr" && key != "embedding" && key != "vision" || clear[key] {
			return req, fmt.Errorf("clear_groups 包含未知或重复模型组")
		}
		clear[key] = true
	}
	if existing == nil {
		return req, nil
	}
	old := AIProfileRequest{LLMProvider: existing.LLMProvider, LLMBaseURL: existing.LLMBaseURL, LLMModel: existing.LLMModel, LLMContextTokens: existing.LLMContextTokens,
		ASRProvider: existing.ASRProvider, ASRBaseURL: existing.ASRBaseURL, ASRModel: existing.ASRModel,
		EmbeddingProvider: existing.EmbeddingProvider, EmbeddingEndpoint: existing.EmbeddingEndpoint, EmbeddingModel: existing.EmbeddingModel, EmbeddingDim: existing.EmbeddingDim,
		VisionProvider: existing.VisionProvider, VisionBaseURL: existing.VisionBaseURL, VisionModel: existing.VisionModel}
	oldGroups := profileRequestGroups(&old)
	for i, group := range profileRequestGroups(&req) {
		if clear[group.name] {
			if group.present() {
				return req, fmt.Errorf("清除模型组 %s 时不能同时填写该组", group.name)
			}
			continue
		}
		if !group.present() {
			previous := oldGroups[i]
			*group.provider, *group.url, *group.model = *previous.provider, *previous.url, *previous.model
			if group.extra != nil {
				*group.extra = *previous.extra
			}
		}
	}
	return req, nil
}

func (s *AIProfileService) encryptProfileGroupKey(key string, existing *model.UserAIProfile, group string, active bool) (string, error) {
	if !active {
		return "", nil
	}
	cipher, err := s.encryptOrKeep(key, existing, group)
	if err != nil {
		return "", err
	}
	if cipher == "" {
		return "", fmt.Errorf("%s API Key 不能为空", group)
	}
	return cipher, nil
}
func (s *AIProfileService) decryptOptionalKey(cipher string) (string, error) {
	if strings.TrimSpace(cipher) == "" {
		return "", nil
	}
	return s.codec.Decrypt(cipher)
}
