package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

var (
	ErrAIProfileNotFound     = errors.New("AI 配置不存在")
	ErrAIProfileRequired     = errors.New("请先配置 AI 服务")
)


type AIProfileTester interface {
	TestProfile(ctx context.Context, profile *DecryptedAIProfile) error
}

type AIProfileService struct {
	repo   *repository.AIProfileRepository
	codec  *secret.Codec
	tester AIProfileTester
}

func NewAIProfileService(repo *repository.AIProfileRepository, codec *secret.Codec, tester AIProfileTester) *AIProfileService {
	return &AIProfileService{repo: repo, codec: codec, tester: tester}
}



type AIProfileRequest struct {
	Name              string `json:"name" binding:"required"`
	LLMProvider       string `json:"llm_provider" binding:"required"`
	LLMBaseURL        string `json:"llm_base_url" binding:"required"`
	LLMAPIKey         string `json:"llm_api_key"`
	LLMModel          string `json:"llm_model" binding:"required"`
	ASRProvider       string `json:"asr_provider" binding:"required"`
	ASRBaseURL        string `json:"asr_base_url" binding:"required"`
	ASRAPIKey         string `json:"asr_api_key"`
	ASRModel          string `json:"asr_model" binding:"required"`
	EmbeddingProvider string `json:"embedding_provider" binding:"required"`
	EmbeddingEndpoint string `json:"embedding_endpoint" binding:"required"`
	EmbeddingAPIKey   string `json:"embedding_api_key"`
	EmbeddingModel    string `json:"embedding_model" binding:"required"`
	EmbeddingDim      int    `json:"embedding_dim" binding:"required"`
	// Vision is optional multimodal caption config, separate from text LLM.
	VisionProvider string `json:"vision_provider"`
	VisionBaseURL  string `json:"vision_base_url"`
	VisionAPIKey   string `json:"vision_api_key"`
	VisionModel    string `json:"vision_model"`
	IsDefault      bool   `json:"is_default"`
}

type AIProfileResponse struct {
	ID                    int64  `json:"id"`
	Name                  string `json:"name"`
	LLMProvider           string `json:"llm_provider"`
	LLMBaseURL            string `json:"llm_base_url"`
	LLMAPIKeyMasked       string `json:"llm_api_key_masked"`
	LLMModel              string `json:"llm_model"`
	ASRProvider           string `json:"asr_provider"`
	ASRBaseURL            string `json:"asr_base_url"`
	ASRAPIKeyMasked       string `json:"asr_api_key_masked"`
	ASRModel              string `json:"asr_model"`
	EmbeddingProvider     string `json:"embedding_provider"`
	EmbeddingEndpoint     string `json:"embedding_endpoint"`
	EmbeddingAPIKeyMasked string `json:"embedding_api_key_masked"`
	EmbeddingModel        string `json:"embedding_model"`
	EmbeddingDim          int    `json:"embedding_dim"`
	VisionProvider        string `json:"vision_provider"`
	VisionBaseURL         string `json:"vision_base_url"`
	VisionAPIKeyMasked    string `json:"vision_api_key_masked"`
	VisionModel           string `json:"vision_model"`
	IsDefault             bool   `json:"is_default"`
	// Source is "user" for BYOK rows, "hosted" for the free server pack.
	Source string `json:"source,omitempty"`
	// ReadOnly marks profiles that cannot be edited/deleted (hosted free pack).
	ReadOnly bool `json:"read_only,omitempty"`
}

type DecryptedAIProfile struct {
	ID                int64
	UserID            int64
	Name              string
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
	Source            string
}

func (s *AIProfileService) Create(userID int64, req AIProfileRequest) (*AIProfileResponse, error) {
	if err := validateAIProfileRequest(req, true); err != nil {
		return nil, err
	}
	if !req.IsDefault {
		count, err := s.repo.CountByUserID(userID)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			req.IsDefault = true
		}
	}
	profile, err := s.profileFromRequest(userID, req, nil)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(profile); err != nil {
		return nil, err
	}
	return s.responseFromProfile(profile), nil
}

func (s *AIProfileService) List(userID int64) ([]AIProfileResponse, error) {
	profiles, err := s.repo.ListByUserID(userID)
	if err != nil {
		return nil, err
	}
	responses := make([]AIProfileResponse, 0, len(profiles))
	for i := range profiles {
		responses = append(responses, *s.responseFromProfile(&profiles[i]))
	}
	return responses, nil
}

func (s *AIProfileService) Update(userID, id int64, req AIProfileRequest) (*AIProfileResponse, error) {
	if err := validateAIProfileRequest(req, false); err != nil {
		return nil, err
	}
	existing, err := s.repo.FindByIDForUser(userID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrAIProfileNotFound
	}

	profile, err := s.profileFromRequest(userID, req, existing)
	if err != nil {
		return nil, err
	}
	profile.ID = id
	if err := s.repo.UpdateForUser(userID, profile); err != nil {
		return nil, err
	}
	return s.responseFromProfile(profile), nil
}

func (s *AIProfileService) Delete(userID, id int64) error {
	return s.repo.DeleteForUser(userID, id)
}


func (s *AIProfileService) Test(ctx context.Context, req AIProfileRequest) error {
	if s.tester == nil {
		return nil
	}
	if err := validateAIProfileRequest(req, true); err != nil {
		return err
	}
	profile := &DecryptedAIProfile{
		Name:              strings.TrimSpace(req.Name),
		LLMProvider:       normalizeProvider(req.LLMProvider),
		LLMBaseURL:        strings.TrimRight(strings.TrimSpace(req.LLMBaseURL), "/"),
		LLMAPIKey:         strings.TrimSpace(req.LLMAPIKey),
		LLMModel:          strings.TrimSpace(req.LLMModel),
		ASRProvider:       normalizeProvider(req.ASRProvider),
		ASRBaseURL:        strings.TrimRight(strings.TrimSpace(req.ASRBaseURL), "/"),
		ASRAPIKey:         strings.TrimSpace(req.ASRAPIKey),
		ASRModel:          strings.TrimSpace(req.ASRModel),
		EmbeddingProvider: normalizeProvider(req.EmbeddingProvider),
		EmbeddingEndpoint: strings.TrimSpace(req.EmbeddingEndpoint),
		EmbeddingAPIKey:   strings.TrimSpace(req.EmbeddingAPIKey),
		EmbeddingModel:    strings.TrimSpace(req.EmbeddingModel),
		EmbeddingDim:      req.EmbeddingDim,
		VisionProvider:    normalizeProvider(req.VisionProvider),
		VisionBaseURL:     strings.TrimRight(strings.TrimSpace(req.VisionBaseURL), "/"),
		VisionAPIKey:      strings.TrimSpace(req.VisionAPIKey),
		VisionModel:       strings.TrimSpace(req.VisionModel),
	}
	return s.tester.TestProfile(ctx, profile)
}

func (s *AIProfileService) TestSavedProfile(ctx context.Context, userID, id int64) error {
	if s.tester == nil {
		return nil
	}
	profile, err := s.repo.FindByIDForUser(userID, id)
	if err != nil {
		return err
	}
	if profile == nil {
		return ErrAIProfileNotFound
	}
	decrypted, err := s.decryptProfile(profile)
	if err != nil {
		return err
	}
	return s.tester.TestProfile(ctx, decrypted)
}

func (s *AIProfileService) GetDefaultDecrypted(userID int64) (*DecryptedAIProfile, error) {
	profile, err := s.repo.FindDefaultByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrAIProfileRequired
	}
	return s.decryptProfile(profile)
}

func (s *AIProfileService) GetDefaultAIProfile(userID int64) (*ai.Profile, error) {
	profile, err := s.GetDefaultDecrypted(userID)
	if err != nil {
		return nil, err
	}
	return &ai.Profile{
		LLMProvider:       profile.LLMProvider,
		LLMBaseURL:        profile.LLMBaseURL,
		LLMAPIKey:         profile.LLMAPIKey,
		LLMModel:          profile.LLMModel,
		ASRProvider:       profile.ASRProvider,
		ASRBaseURL:        profile.ASRBaseURL,
		ASRAPIKey:         profile.ASRAPIKey,
		ASRModel:          profile.ASRModel,
		EmbeddingProvider: profile.EmbeddingProvider,
		EmbeddingEndpoint: profile.EmbeddingEndpoint,
		EmbeddingAPIKey:   profile.EmbeddingAPIKey,
		EmbeddingModel:    profile.EmbeddingModel,
		EmbeddingDim:      profile.EmbeddingDim,
		VisionProvider:    profile.VisionProvider,
		VisionBaseURL:     profile.VisionBaseURL,
		VisionAPIKey:      profile.VisionAPIKey,
		VisionModel:       profile.VisionModel,
	}, nil
}




func (s *AIProfileService) profileFromRequest(userID int64, req AIProfileRequest, existing *model.UserAIProfile) (*model.UserAIProfile, error) {
	llmCipher, err := s.encryptOrKeep(strings.TrimSpace(req.LLMAPIKey), existing, "llm")
	if err != nil {
		return nil, err
	}
	asrCipher, err := s.encryptOrKeep(strings.TrimSpace(req.ASRAPIKey), existing, "asr")
	if err != nil {
		return nil, err
	}
	embeddingCipher, err := s.encryptOrKeep(strings.TrimSpace(req.EmbeddingAPIKey), existing, "embedding")
	if err != nil {
		return nil, err
	}
	visionCipher, err := s.encryptOrKeepOptional(strings.TrimSpace(req.VisionAPIKey), existing, "vision")
	if err != nil {
		return nil, err
	}

	return &model.UserAIProfile{
		UserID:                    userID,
		Name:                      strings.TrimSpace(req.Name),
		LLMProvider:               normalizeProvider(req.LLMProvider),
		LLMBaseURL:                strings.TrimRight(strings.TrimSpace(req.LLMBaseURL), "/"),
		LLMAPIKeyCiphertext:       llmCipher,
		LLMModel:                  strings.TrimSpace(req.LLMModel),
		ASRProvider:               normalizeProvider(req.ASRProvider),
		ASRBaseURL:                strings.TrimRight(strings.TrimSpace(req.ASRBaseURL), "/"),
		ASRAPIKeyCiphertext:       asrCipher,
		ASRModel:                  strings.TrimSpace(req.ASRModel),
		EmbeddingProvider:         normalizeProvider(req.EmbeddingProvider),
		EmbeddingEndpoint:         strings.TrimSpace(req.EmbeddingEndpoint),
		EmbeddingAPIKeyCiphertext: embeddingCipher,
		EmbeddingModel:            strings.TrimSpace(req.EmbeddingModel),
		EmbeddingDim:              req.EmbeddingDim,
		VisionProvider:            normalizeProvider(req.VisionProvider),
		VisionBaseURL:             strings.TrimRight(strings.TrimSpace(req.VisionBaseURL), "/"),
		VisionAPIKeyCiphertext:    visionCipher,
		VisionModel:               strings.TrimSpace(req.VisionModel),
		IsDefault:                 req.IsDefault,
	}, nil
}

func (s *AIProfileService) encryptOrKeep(plaintext string, existing *model.UserAIProfile, kind string) (string, error) {
	if plaintext != "" {
		return s.codec.Encrypt(plaintext)
	}
	if existing == nil {
		return "", fmt.Errorf("%s api key required", kind)
	}
	switch kind {
	case "llm":
		return existing.LLMAPIKeyCiphertext, nil
	case "asr":
		return existing.ASRAPIKeyCiphertext, nil
	case "embedding":
		return existing.EmbeddingAPIKeyCiphertext, nil
	case "vision":
		return existing.VisionAPIKeyCiphertext, nil
	default:
		return "", fmt.Errorf("unknown api key kind: %s", kind)
	}
}

// encryptOrKeepOptional allows empty keys for optional services (vision).
func (s *AIProfileService) encryptOrKeepOptional(plaintext string, existing *model.UserAIProfile, kind string) (string, error) {
	if plaintext != "" {
		return s.codec.Encrypt(plaintext)
	}
	if existing == nil {
		return "", nil
	}
	switch kind {
	case "vision":
		return existing.VisionAPIKeyCiphertext, nil
	default:
		return s.encryptOrKeep(plaintext, existing, kind)
	}
}

func (s *AIProfileService) responseFromProfile(profile *model.UserAIProfile) *AIProfileResponse {
	return &AIProfileResponse{
		ID:                    profile.ID,
		Name:                  profile.Name,
		LLMProvider:           profile.LLMProvider,
		LLMBaseURL:            profile.LLMBaseURL,
		LLMAPIKeyMasked:       s.maskCiphertext(profile.LLMAPIKeyCiphertext),
		LLMModel:              profile.LLMModel,
		ASRProvider:           profile.ASRProvider,
		ASRBaseURL:            profile.ASRBaseURL,
		ASRAPIKeyMasked:       s.maskCiphertext(profile.ASRAPIKeyCiphertext),
		ASRModel:              profile.ASRModel,
		EmbeddingProvider:     profile.EmbeddingProvider,
		EmbeddingEndpoint:     profile.EmbeddingEndpoint,
		EmbeddingAPIKeyMasked: s.maskCiphertext(profile.EmbeddingAPIKeyCiphertext),
		EmbeddingModel:        profile.EmbeddingModel,
		EmbeddingDim:          profile.EmbeddingDim,
		VisionProvider:        profile.VisionProvider,
		VisionBaseURL:         profile.VisionBaseURL,
		VisionAPIKeyMasked:    s.maskOptionalCiphertext(profile.VisionAPIKeyCiphertext),
		VisionModel:           profile.VisionModel,
		IsDefault:             profile.IsDefault,
		Source:                "user",
		ReadOnly:              false,
	}
}

func (s *AIProfileService) maskOptionalCiphertext(ciphertext string) string {
	if strings.TrimSpace(ciphertext) == "" {
		return ""
	}
	return s.maskCiphertext(ciphertext)
}

func (s *AIProfileService) maskCiphertext(ciphertext string) string {
	plaintext, err := s.codec.Decrypt(ciphertext)
	if err != nil {
		return "****"
	}
	return secret.MaskAPIKey(plaintext)
}

func (s *AIProfileService) decryptProfile(profile *model.UserAIProfile) (*DecryptedAIProfile, error) {
	llmKey, err := s.codec.Decrypt(profile.LLMAPIKeyCiphertext)
	if err != nil {
		return nil, err
	}
	asrKey, err := s.codec.Decrypt(profile.ASRAPIKeyCiphertext)
	if err != nil {
		return nil, err
	}
	embeddingKey, err := s.codec.Decrypt(profile.EmbeddingAPIKeyCiphertext)
	if err != nil {
		return nil, err
	}
	var visionKey string
	if strings.TrimSpace(profile.VisionAPIKeyCiphertext) != "" {
		visionKey, err = s.codec.Decrypt(profile.VisionAPIKeyCiphertext)
		if err != nil {
			return nil, err
		}
	}
	return &DecryptedAIProfile{
		ID:                profile.ID,
		UserID:            profile.UserID,
		Name:              profile.Name,
		LLMProvider:       profile.LLMProvider,
		LLMBaseURL:        profile.LLMBaseURL,
		LLMAPIKey:         llmKey,
		LLMModel:          profile.LLMModel,
		ASRProvider:       profile.ASRProvider,
		ASRBaseURL:        profile.ASRBaseURL,
		ASRAPIKey:         asrKey,
		ASRModel:          profile.ASRModel,
		EmbeddingProvider: profile.EmbeddingProvider,
		EmbeddingEndpoint: profile.EmbeddingEndpoint,
		EmbeddingAPIKey:   embeddingKey,
		EmbeddingModel:    profile.EmbeddingModel,
		EmbeddingDim:      profile.EmbeddingDim,
		VisionProvider:    profile.VisionProvider,
		VisionBaseURL:     profile.VisionBaseURL,
		VisionAPIKey:      visionKey,
		VisionModel:       profile.VisionModel,
		Source:            "user",
	}, nil
}

func validateAIProfileRequest(req AIProfileRequest, requireKeys bool) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("配置名称不能为空")
	}
	if strings.TrimSpace(req.LLMProvider) == "" || strings.TrimSpace(req.LLMBaseURL) == "" || strings.TrimSpace(req.LLMModel) == "" {
		return fmt.Errorf("LLM 配置不完整")
	}
	if strings.TrimSpace(req.ASRProvider) == "" || strings.TrimSpace(req.ASRBaseURL) == "" || strings.TrimSpace(req.ASRModel) == "" {
		return fmt.Errorf("ASR 配置不完整")
	}
	if strings.TrimSpace(req.EmbeddingProvider) == "" || strings.TrimSpace(req.EmbeddingEndpoint) == "" || strings.TrimSpace(req.EmbeddingModel) == "" {
		return fmt.Errorf("embedding 配置不完整")
	}
	if req.EmbeddingDim <= 0 {
		return fmt.Errorf("embedding 维度必须大于 0")
	}
	if requireKeys && (strings.TrimSpace(req.LLMAPIKey) == "" || strings.TrimSpace(req.ASRAPIKey) == "" || strings.TrimSpace(req.EmbeddingAPIKey) == "") {
		return fmt.Errorf("API Key 不能为空")
	}
	// Vision is optional; if any field is set, provider/url/model must all be present.
	vp := strings.TrimSpace(req.VisionProvider)
	vb := strings.TrimSpace(req.VisionBaseURL)
	vm := strings.TrimSpace(req.VisionModel)
	vk := strings.TrimSpace(req.VisionAPIKey)
	if vp != "" || vb != "" || vm != "" || vk != "" {
		if vp == "" || vb == "" || vm == "" {
			return fmt.Errorf("Vision 配置不完整（需 provider、base_url、model；也可全部留空表示不用多模态）")
		}
		if requireKeys && vk == "" {
			return fmt.Errorf("Vision API Key 不能为空")
		}
	}
	return nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
