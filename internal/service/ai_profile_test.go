package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

func TestAIProfileServiceCreateEncryptsKeysAndReturnsMaskedProfile(t *testing.T) {
	svc, repos, codec := newAIProfileServiceForTest(t)

	resp, err := svc.Create(7, AIProfileRequest{
		Name:              "default",
		LLMProvider:       "openai_compatible",
		LLMBaseURL:        "https://llm.example.com/v1",
		LLMAPIKey:         "sk-llm-secret",
		LLMModel:          "chat-model",
		ASRProvider:       "mimo",
		ASRBaseURL:        "https://token-plan-cn.xiaomimimo.com/v1",
		ASRAPIKey:         "tp-asr-secret",
		ASRModel:          "mimo-v2.5-asr",
		EmbeddingProvider: "openai_compatible",
		EmbeddingEndpoint: "https://router.tumuer.me/v1/embeddings",
		EmbeddingAPIKey:   "sk-embedding-secret",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingDim:      1536,
		IsDefault:         true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if resp.LLMAPIKeyMasked != "sk-****cret" || resp.ASRAPIKeyMasked != "tp-****cret" {
		t.Fatalf("masked keys not returned: %+v", resp)
	}

	stored, err := repos.AIProfile.FindDefaultByUserID(7)
	if err != nil {
		t.Fatalf("FindDefaultByUserID() error = %v", err)
	}
	if stored.LLMAPIKeyCiphertext == "sk-llm-secret" {
		t.Fatal("LLM key stored as plaintext")
	}
	llmKey, err := codec.Decrypt(stored.LLMAPIKeyCiphertext)
	if err != nil {
		t.Fatalf("Decrypt LLM key error = %v", err)
	}
	if llmKey != "sk-llm-secret" {
		t.Fatalf("decrypted LLM key = %q", llmKey)
	}
}

func TestAIProfileServiceUpdateWithEmptyKeysKeepsExistingCiphertexts(t *testing.T) {
	svc, repos, codec := newAIProfileServiceForTest(t)
	created, err := svc.Create(7, validAIProfileRequest())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updatedReq := validAIProfileRequest()
	updatedReq.Name = "renamed"
	updatedReq.LLMAPIKey = ""
	updatedReq.ASRAPIKey = ""
	updatedReq.EmbeddingAPIKey = ""
	updated, err := svc.Update(7, created.ID, updatedReq)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("Update() name = %q", updated.Name)
	}

	stored, err := repos.AIProfile.FindByIDForUser(7, created.ID)
	if err != nil {
		t.Fatalf("FindByIDForUser() error = %v", err)
	}
	for label, ciphertext := range map[string]string{
		"llm":       stored.LLMAPIKeyCiphertext,
		"asr":       stored.ASRAPIKeyCiphertext,
		"embedding": stored.EmbeddingAPIKeyCiphertext,
	} {
		plaintext, err := codec.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt(%s) error = %v", label, err)
		}
		if plaintext == "" {
			t.Fatalf("Decrypt(%s) returned empty plaintext", label)
		}
	}
}

func TestAIProfileServiceDefaultDecryptedProfile(t *testing.T) {
	svc, _, _ := newAIProfileServiceForTest(t)
	if _, err := svc.Create(7, validAIProfileRequest()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	profile, err := svc.GetDefaultDecrypted(7)
	if err != nil {
		t.Fatalf("GetDefaultDecrypted() error = %v", err)
	}
	if profile.LLMAPIKey != "sk-llm-secret" {
		t.Fatalf("LLMAPIKey = %q", profile.LLMAPIKey)
	}
	if profile.ASRAPIKey != "tp-asr-secret" {
		t.Fatalf("ASRAPIKey = %q", profile.ASRAPIKey)
	}
	if profile.EmbeddingAPIKey != "sk-embedding-secret" {
		t.Fatalf("EmbeddingAPIKey = %q", profile.EmbeddingAPIKey)
	}
}

func newAIProfileServiceForTest(t *testing.T) (*AIProfileService, *repository.Repositories, *secret.Codec) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.UserAIProfile{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	repos := repository.NewRepositories(db)
	codec, err := secret.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	return NewAIProfileService(repos.AIProfile, codec, nil), repos, codec
}

func validAIProfileRequest() AIProfileRequest {
	return AIProfileRequest{
		Name:              "default",
		LLMProvider:       "openai_compatible",
		LLMBaseURL:        "https://llm.example.com/v1",
		LLMAPIKey:         "sk-llm-secret",
		LLMModel:          "chat-model",
		ASRProvider:       "mimo",
		ASRBaseURL:        "https://token-plan-cn.xiaomimimo.com/v1",
		ASRAPIKey:         "tp-asr-secret",
		ASRModel:          "mimo-v2.5-asr",
		EmbeddingProvider: "openai_compatible",
		EmbeddingEndpoint: "https://router.tumuer.me/v1/embeddings",
		EmbeddingAPIKey:   "sk-embedding-secret",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingDim:      1536,
		IsDefault:         true,
	}
}
