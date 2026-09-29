package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

var (
	ErrHostedAIForbidden   = errors.New("仅作者可管理免费 AI 服务")
	ErrHostedAIReadOnly    = errors.New("免费 AI 配置由作者统一管理")
	ErrHostedAIUnavailable = errors.New("免费 AI 服务暂不可用，请稍后重试或使用自己的配置")
)

const hostedNotice = "当前站点提供免费的 AI 服务。服务可能因额度或维护而限流、调整或暂停；你可以随时改用自己的 AI 配置。"

type HostedAIAdminRequest struct {
	AIProfileRequest
	Enabled        bool   `json:"enabled"`
	RerankProvider string `json:"rerank_provider"`
	RerankEndpoint string `json:"rerank_endpoint"`
	RerankAPIKey   string `json:"rerank_api_key"`
	RerankModel    string `json:"rerank_model"`
}

type HostedAIAdminResponse struct {
	AIProfileResponse
	Enabled            bool   `json:"enabled"`
	RerankProvider     string `json:"rerank_provider"`
	RerankEndpoint     string `json:"rerank_endpoint"`
	RerankAPIKeyMasked string `json:"rerank_api_key_masked"`
}

type HostedAIStatus struct {
	Enabled   bool               `json:"enabled"`
	CanManage bool               `json:"can_manage"`
	Profile   *AIProfileResponse `json:"profile,omitempty"`
	Notice    string             `json:"notice"`
}

func (s *AIProfileService) WithHostedOwnerID(id int64) *AIProfileService {
	s.hostedOwnerID = id
	return s
}
func (s *AIProfileService) WithHostedEmbeddingDimension(dim int) *AIProfileService {
	s.hostedEmbeddingDim = dim
	return s
}
func (s *AIProfileService) CanManageHosted(userID int64) bool {
	return userID > 0 && s.hostedOwnerID == userID && s.repo.CanManageHosted(userID)
}

func (s *AIProfileService) hostedConfig() (*HostedAIAdminRequest, error) {
	row, err := s.repo.HostedConfig()
	if err != nil || row == nil {
		return nil, err
	}
	plain, err := s.codec.Decrypt(row.Ciphertext)
	if err != nil {
		return nil, errors.New("无法读取免费 AI 配置")
	}
	var req HostedAIAdminRequest
	if err := json.Unmarshal([]byte(plain), &req); err != nil {
		return nil, errors.New("免费 AI 配置格式无效")
	}
	req.Enabled = row.Enabled
	return &req, nil
}

func hostedPublic(req *HostedAIAdminRequest) *AIProfileResponse {
	return &AIProfileResponse{Name: req.Name, LLMModel: req.LLMModel, LLMContextTokens: req.LLMContextTokens, ASRModel: req.ASRModel,
		EmbeddingModel: req.EmbeddingModel, EmbeddingDim: req.EmbeddingDim, VisionModel: req.VisionModel, RerankModel: req.RerankModel,
		Source: "hosted", ReadOnly: true}
}

func (s *AIProfileService) GetHostedStatus(userID int64) (*HostedAIStatus, error) {
	req, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	status := &HostedAIStatus{CanManage: s.CanManageHosted(userID), Notice: hostedNotice}
	if req != nil {
		status.Enabled = req.Enabled
		status.Profile = hostedPublic(req)
	}
	return status, nil
}

func (s *AIProfileService) GetHostedAdmin(userID int64) (*HostedAIAdminResponse, error) {
	if !s.CanManageHosted(userID) {
		return nil, ErrHostedAIForbidden
	}
	req, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &HostedAIAdminRequest{AIProfileRequest: AIProfileRequest{Name: "作者免费 AI", EmbeddingDim: s.hostedEmbeddingDim}}
	}
	view := &HostedAIAdminResponse{AIProfileResponse: *hostedPublic(req), Enabled: req.Enabled, RerankProvider: req.RerankProvider, RerankEndpoint: req.RerankEndpoint}
	view.LLMProvider, view.LLMBaseURL = req.LLMProvider, req.LLMBaseURL
	view.ASRProvider, view.ASRBaseURL = req.ASRProvider, req.ASRBaseURL
	view.EmbeddingProvider, view.EmbeddingEndpoint = req.EmbeddingProvider, req.EmbeddingEndpoint
	view.VisionProvider, view.VisionBaseURL = req.VisionProvider, req.VisionBaseURL
	for _, pair := range []struct {
		key string
		dst *string
	}{
		{req.LLMAPIKey, &view.LLMAPIKeyMasked}, {req.ASRAPIKey, &view.ASRAPIKeyMasked}, {req.EmbeddingAPIKey, &view.EmbeddingAPIKeyMasked},
		{req.VisionAPIKey, &view.VisionAPIKeyMasked}, {req.RerankAPIKey, &view.RerankAPIKeyMasked},
	} {
		if pair.key != "" {
			*pair.dst = "已配置"
		}
	}
	return view, nil
}

func (s *AIProfileService) SaveHostedAdmin(userID int64, req HostedAIAdminRequest) (*HostedAIAdminResponse, error) {
	if !s.CanManageHosted(userID) {
		return nil, ErrHostedAIForbidden
	}
	old, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	// A pause-only request retains the bundle; all new resolutions fail closed.
	if !req.Enabled && strings.TrimSpace(req.Name) == "" && old != nil {
		req = *old
		req.Enabled = false
	}
	if old != nil {
		for _, pair := range []struct {
			dst *string
			old string
		}{
			{&req.LLMAPIKey, old.LLMAPIKey}, {&req.ASRAPIKey, old.ASRAPIKey}, {&req.EmbeddingAPIKey, old.EmbeddingAPIKey},
			{&req.VisionAPIKey, old.VisionAPIKey}, {&req.RerankAPIKey, old.RerankAPIKey},
		} {
			if strings.TrimSpace(*pair.dst) == "" {
				*pair.dst = pair.old
			}
		}
	}
	if err := validateAIProfileRequest(req.AIProfileRequest, true); err != nil {
		return nil, err
	}
	if req.VisionModel == "" || req.VisionAPIKey == "" || req.RerankModel == "" || req.RerankAPIKey == "" || req.RerankProvider == "" {
		return nil, fmt.Errorf("免费服务需完整配置五项模型")
	}
	if s.hostedEmbeddingDim > 0 && req.EmbeddingDim != s.hostedEmbeddingDim {
		return nil, fmt.Errorf("向量维度必须与服务器一致")
	}
	for _, raw := range []string{req.LLMBaseURL, req.ASRBaseURL, req.EmbeddingEndpoint, req.VisionBaseURL, req.RerankEndpoint} {
		if !strings.HasPrefix(raw, "https://") || ai.ValidateProbeURL(raw) != nil {
			return nil, errors.New("免费服务必须使用公共 HTTPS 接口")
		}
	}
	if !strings.HasSuffix(strings.TrimRight(req.RerankEndpoint, "/"), "/rerank") {
		return nil, errors.New("重排序接口须以 /rerank 结尾")
	}
	req.IsDefault = false
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cipher, err := s.codec.Encrypt(string(data))
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveHostedConfig(&model.HostedAIConfig{ID: 1, Enabled: req.Enabled, Ciphertext: cipher}); err != nil {
		return nil, err
	}
	return s.GetHostedAdmin(userID)
}

func (s *AIProfileService) ActivateHosted(userID int64) (*AIProfileResponse, error) {
	req, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	if req == nil || !req.Enabled {
		return nil, ErrHostedAIUnavailable
	}
	row, err := s.repo.ActivateHosted(userID)
	if err != nil {
		return nil, err
	}
	view := hostedPublic(req)
	view.ID = row.ID
	view.IsDefault = true
	return view, nil
}

func (s *AIProfileService) hostedResponse(row *model.UserAIProfile) (*AIProfileResponse, error) {
	req, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return &AIProfileResponse{ID: row.ID, Name: "作者免费 AI（暂不可用）", Source: "hosted", ReadOnly: true, IsDefault: row.IsDefault}, nil
	}
	view := hostedPublic(req)
	view.ID = row.ID
	view.IsDefault = row.IsDefault
	return view, nil
}

func (s *AIProfileService) decryptHosted(row *model.UserAIProfile) (*DecryptedAIProfile, error) {
	req, err := s.hostedConfig()
	if err != nil {
		return nil, err
	}
	if req == nil || !req.Enabled {
		return nil, ErrHostedAIUnavailable
	}
	return &DecryptedAIProfile{ID: row.ID, UserID: row.UserID, Name: req.Name, Source: "hosted",
		LLMProvider: req.LLMProvider, LLMBaseURL: req.LLMBaseURL, LLMAPIKey: req.LLMAPIKey, LLMModel: req.LLMModel, LLMContextTokens: req.LLMContextTokens,
		ASRProvider: req.ASRProvider, ASRBaseURL: req.ASRBaseURL, ASRAPIKey: req.ASRAPIKey, ASRModel: req.ASRModel,
		EmbeddingProvider: req.EmbeddingProvider, EmbeddingEndpoint: req.EmbeddingEndpoint, EmbeddingAPIKey: req.EmbeddingAPIKey, EmbeddingModel: req.EmbeddingModel, EmbeddingDim: req.EmbeddingDim,
		VisionProvider: req.VisionProvider, VisionBaseURL: req.VisionBaseURL, VisionAPIKey: req.VisionAPIKey, VisionModel: req.VisionModel,
		RerankProvider: req.RerankProvider, RerankEndpoint: req.RerankEndpoint, RerankAPIKey: req.RerankAPIKey, RerankModel: req.RerankModel}, nil
}

func (s *AIProfileService) requireUserOwnedProfile(userID, id int64) error {
	row, err := s.repo.FindByIDForUser(userID, id)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrAIProfileNotFound
	}
	if row.Source == "hosted" {
		return ErrHostedAIReadOnly
	}
	return nil
}
