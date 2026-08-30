package service

import (
	"fmt"
	"log"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

const (
	DemoUsername = "test"
	DemoPassword = "test0236"
	demoNickname = "演示账号"
	demoProfile  = "演示配置"
)

// EnsureDemoAccount creates the read-only demo user and seeds a default AI profile
// from process-level AI configuration when the account has no profiles yet.
func EnsureDemoAccount(
	userRepo *repository.UserRepository,
	profileRepo *repository.AIProfileRepository,
	codec *secret.Codec,
	aiCfg config.AIConfig,
	ragCfg config.RAGConfig,
) error {
	user, created, err := ensureDemoUser(userRepo)
	if err != nil {
		return err
	}
	if created {
		log.Printf("✅ 演示账号已创建: %s / %s", DemoUsername, DemoPassword)
	}

	count, err := profileRepo.CountByUserID(user.ID)
	if err != nil {
		return fmt.Errorf("count demo profiles: %w", err)
	}
	if count > 0 {
		return nil
	}

	req, ok := demoProfileRequestFromConfig(aiCfg, ragCfg)
	if !ok {
		log.Printf("⚠️ 演示账号尚无 AI Profile：请在 .env / config.yaml 配置 LLM、ASR、Embedding 后重启服务")
		return nil
	}

	svc := NewAIProfileService(profileRepo, codec, nil)
	if _, err := svc.Create(user.ID, req); err != nil {
		return fmt.Errorf("seed demo profile: %w", err)
	}
	log.Printf("✅ 演示账号默认 AI Profile 已初始化（%s）", demoProfile)
	return nil
}

func ensureDemoUser(userRepo *repository.UserRepository) (*model.User, bool, error) {
	existing, err := userRepo.FindByUsername(DemoUsername)
	if err == nil && existing != nil {
		if existing.Role != model.RoleDemo {
			existing.Role = model.RoleDemo
			if err := userRepo.UpdateRole(existing.ID, model.RoleDemo); err != nil {
				return nil, false, fmt.Errorf("update demo role: %w", err)
			}
		}
		return existing, false, nil
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(DemoPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, false, fmt.Errorf("hash demo password: %w", err)
	}
	user := &model.User{
		Username:     DemoUsername,
		PasswordHash: string(hashed),
		Nickname:     demoNickname,
		Role:         model.RoleDemo,
	}
	if err := userRepo.Create(user); err != nil {
		return nil, false, fmt.Errorf("create demo user: %w", err)
	}
	return user, true, nil
}

func demoProfileRequestFromConfig(aiCfg config.AIConfig, ragCfg config.RAGConfig) (AIProfileRequest, bool) {
	profile := aiCfg.Profile()
	dim := profile.EmbeddingDim
	if dim <= 0 {
		dim = ragCfg.EmbeddingDim
	}
	if profile.LLMBaseURL == "" || profile.LLMModel == "" || profile.LLMAPIKey == "" ||
		profile.ASRBaseURL == "" || profile.ASRModel == "" || profile.ASRAPIKey == "" ||
		profile.EmbeddingEndpoint == "" || profile.EmbeddingModel == "" || profile.EmbeddingAPIKey == "" ||
		dim <= 0 {
		return AIProfileRequest{}, false
	}

	req := AIProfileRequest{
		Name:              demoProfile,
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
		EmbeddingDim:      dim,
		IsDefault:         true,
	}
	req.VisionProvider, req.VisionBaseURL, req.VisionAPIKey, req.VisionModel = visionRequestFields(profile)
	return req, true
}

func visionRequestFields(profile ai.Profile) (provider, baseURL, apiKey, model string) {
	if strings.TrimSpace(profile.VisionBaseURL) == "" || strings.TrimSpace(profile.VisionModel) == "" {
		return "", "", "", ""
	}
	return profile.VisionProvider, profile.VisionBaseURL, profile.VisionAPIKey, profile.VisionModel
}
