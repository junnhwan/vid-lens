package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/ai"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

func hostedTestService(t *testing.T) (*AIProfileService, *repository.Repositories, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserAIProfile{}, &model.HostedAIConfig{}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []model.User{{ID: 2, Username: "owner", Role: model.RoleUser}, {ID: 3, Username: "visitor", Role: model.RoleUser}, {ID: 4, Username: "another", Role: model.RoleUser}} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := repository.NewRepositories(db)
	c, _ := secret.NewCodec("0123456789abcdef0123456789abcdef")
	return NewAIProfileService(r.AIProfile, c, nil).WithHostedOwnerID(2).WithHostedEmbeddingDimension(1024), r, db
}

func hostedTestRequest() HostedAIAdminRequest {
	r := validAIProfileRequest()
	r.ASRBaseURL = "https://asr.example.com/v1"
	r.EmbeddingEndpoint = "https://embed.example.com/v1/embeddings"
	r.EmbeddingDim = 1024
	r.VisionProvider, r.VisionBaseURL, r.VisionAPIKey, r.VisionModel = "openai_compatible", "https://vision.example.com/v1", "private-vision-key", "vision"
	return HostedAIAdminRequest{AIProfileRequest: r, Enabled: true, RerankProvider: "openai_compatible", RerankEndpoint: "https://rank.example.com/v1/rerank", RerankAPIKey: "private-rerank-key", RerankModel: "reranker"}
}

func TestHostedAISelectionSyncPrivacyAndPause(t *testing.T) {
	s, r, db := hostedTestService(t)
	req := hostedTestRequest()
	if _, err := s.SaveHostedAdmin(3, req); !errors.Is(err, ErrHostedAIForbidden) {
		t.Fatalf("visitor save: %v", err)
	}
	if _, err := s.SaveHostedAdmin(2, req); err != nil {
		t.Fatal(err)
	}
	row, _ := r.AIProfile.HostedConfig()
	if strings.Contains(row.Ciphertext, req.LLMAPIKey) || strings.Contains(row.Ciphertext, req.LLMBaseURL) {
		t.Fatal("plaintext bundle")
	}
	byok, err := s.Create(3, validAIProfileRequest())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.ActivateHosted(3)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.ActivateHosted(3)
	if err != nil || a.ID != b.ID {
		t.Fatalf("activation not idempotent: %v", err)
	}
	if _, err := s.ActivateHosted(4); err != nil {
		t.Fatalf("second user activation: %v", err)
	}
	stored, _ := r.AIProfile.FindByIDForUser(3, a.ID)
	if stored.LLMAPIKeyCiphertext != "" || stored.LLMBaseURL != "" {
		t.Fatal("credentials copied to user")
	}
	old, _ := r.AIProfile.FindByIDForUser(3, byok.ID)
	if old == nil || old.IsDefault {
		t.Fatal("BYOK not preserved/switched")
	}
	list, _ := s.List(3)
	status, _ := s.GetHostedStatus(3)
	if a.Name != "Free API" || status.Profile == nil || status.Profile.Name != "Free API" {
		t.Fatalf("user-facing hosted profile name should be Free API: profile=%#v status=%#v", a.Name, status.Profile)
	}
	if status.CanManage {
		t.Fatal("visitor is manager")
	}
	data, _ := json.Marshal([]any{a, status, list[0]})
	for _, v := range []string{req.LLMAPIKey, req.RerankAPIKey, req.LLMBaseURL, req.RerankEndpoint, "sk-****"} {
		if strings.Contains(string(data), v) {
			t.Fatalf("public disclosure: %q", v)
		}
	}
	admin, _ := s.GetHostedAdmin(2)
	data, _ = json.Marshal(admin)
	if strings.Contains(string(data), req.LLMAPIKey) || admin.LLMAPIKeyMasked != "已配置" {
		t.Fatal("admin key disclosure")
	}
	req.LLMModel = "replacement-model"
	req.RerankModel = "replacement-reranker"
	req.LLMAPIKey = ""
	if _, err := s.SaveHostedAdmin(2, req); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetDefaultConversationProfile(3)
	if err != nil || p.Profile.LLMModel != req.LLMModel || p.Profile.RerankModel != req.RerankModel || p.Profile.LLMAPIKey == "" {
		t.Fatalf("live sync or retained key failed: %v", err)
	}
	if _, err := s.SaveHostedAdmin(2, HostedAIAdminRequest{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDefaultAIProfile(3); !errors.Is(err, ErrHostedAIUnavailable) {
		t.Fatalf("pause fails open: %v", err)
	}
	if _, err := s.ActivateHosted(4); !errors.Is(err, ErrHostedAIUnavailable) {
		t.Fatal("activate paused allowed")
	}
	if err := db.Delete(&model.User{}, 2).Error; err != nil {
		t.Fatal(err)
	}
	if s.CanManageHosted(2) {
		t.Fatal("deleted owner allowed")
	}
}

func TestHostedAIRejectsCredentialExfiltrationAndEdits(t *testing.T) {
	s, _, _ := hostedTestService(t)
	if _, err := s.SaveHostedAdmin(2, hostedTestRequest()); err != nil {
		t.Fatal(err)
	}
	p, err := s.ActivateHosted(3)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, update := s.Update(3, p.ID, validAIProfileRequest())
	_, models := s.ListModels(ctx, 3, ListModelsRequest{ProfileID: p.ID, BaseURL: "https://attacker.example/v1"})
	_, dim := s.ProbeEmbeddingDim(ctx, 3, ProbeEmbeddingDimRequest{ProfileID: p.ID, Endpoint: "https://attacker.example/v1/embeddings"})
	_, probe := s.ProbeCapability(ctx, 3, ProbeCapabilityRequest{ProfileID: p.ID, BaseURL: "https://attacker.example/v1", Purpose: "llm"})
	for name, err := range map[string]error{"update": update, "models": models, "dimension": dim, "probe": probe, "test": s.TestSavedProfile(ctx, 3, p.ID), "delete": s.Delete(3, p.ID)} {
		if !errors.Is(err, ErrHostedAIReadOnly) {
			t.Fatalf("%s allowed: %v", name, err)
		}
	}
}

func TestHostedAIRejectsInvalidDimensionAndURL(t *testing.T) {
	s, _, _ := hostedTestService(t)
	r := hostedTestRequest()
	r.EmbeddingDim = 1536
	if _, err := s.SaveHostedAdmin(2, r); err == nil {
		t.Fatal("dimension mismatch accepted")
	}
	r = hostedTestRequest()
	r.RerankEndpoint = "https://127.0.0.1/v1/rerank"
	if _, err := s.SaveHostedAdmin(2, r); err == nil {
		t.Fatal("private endpoint accepted")
	}
}

func TestHostedRerankerOverridesWithoutMutatingGlobalConfig(t *testing.T) {
	cfg := DefaultRAGRetrievalConfig()
	called := false
	s := &ChatService{cfg: ChatConfig{Retrieval: &cfg, ModelRerankerFactory: func(p ai.Profile) Reranker {
		called = p.RerankModel == "hosted-ranker" && p.RerankAPIKey == "hosted-key"
		return DeterministicReranker{}
	}}}
	p := s.newRetrievalPipeline(5, nil, ai.Profile{RerankModel: "hosted-ranker", RerankAPIKey: "hosted-key"})
	if !called || cfg.RerankerMode != RerankerModeDeterministic || p.Config.RerankerVersion != "hosted-ranker" {
		t.Fatal("hosted reranking not isolated/effective")
	}
}
