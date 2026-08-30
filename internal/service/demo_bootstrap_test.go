package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

func TestEnsureDemoAccountCreatesUserAndProfile(t *testing.T) {
	db := newDemoBootstrapDB(t)
	repos := repository.NewRepositories(db)
	codec := newDemoBootstrapCodec(t)

	cfg := config.Config{
		AI: config.AIConfig{
			LLMBaseURL:        "https://llm.example.com/v1",
			LLMAPIKey:         "sk-llm",
			LLMModel:          "chat-model",
			ASRBaseURL:        "https://asr.example.com/v1",
			ASRAPIKey:         "sk-asr",
			ASRModel:          "asr-model",
			EmbeddingEndpoint: "https://embed.example.com/v1/embeddings",
			EmbeddingAPIKey:   "sk-embed",
			EmbeddingModel:    "embed-model",
			EmbeddingDim:      1024,
		},
		RAG: config.RAGConfig{EmbeddingDim: 1024},
	}

	if err := EnsureDemoAccount(repos.User, repos.AIProfile, codec, cfg.AI, cfg.RAG); err != nil {
		t.Fatalf("EnsureDemoAccount() error = %v", err)
	}

	user, err := repos.User.FindByUsername(DemoUsername)
	if err != nil {
		t.Fatalf("FindByUsername() error = %v", err)
	}
	if user.Role != model.RoleDemo {
		t.Fatalf("role = %q, want %q", user.Role, model.RoleDemo)
	}

	profile, err := repos.AIProfile.FindDefaultByUserID(user.ID)
	if err != nil {
		t.Fatalf("FindDefaultByUserID() error = %v", err)
	}
	if profile == nil || profile.LLMModel != "chat-model" || profile.EmbeddingDim != 1024 {
		t.Fatalf("default profile = %+v", profile)
	}

	// Idempotent: second call must not duplicate user or profile.
	if err := EnsureDemoAccount(repos.User, repos.AIProfile, codec, cfg.AI, cfg.RAG); err != nil {
		t.Fatalf("second EnsureDemoAccount() error = %v", err)
	}
	count, err := repos.AIProfile.CountByUserID(user.ID)
	if err != nil {
		t.Fatalf("CountByUserID() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("profile count = %d, want 1", count)
	}
}

func TestEnsureDemoAccountSkipsProfileWithoutServerAIConfig(t *testing.T) {
	db := newDemoBootstrapDB(t)
	repos := repository.NewRepositories(db)
	codec := newDemoBootstrapCodec(t)

	if err := EnsureDemoAccount(repos.User, repos.AIProfile, codec, config.AIConfig{}, config.RAGConfig{}); err != nil {
		t.Fatalf("EnsureDemoAccount() error = %v", err)
	}
	user, err := repos.User.FindByUsername(DemoUsername)
	if err != nil {
		t.Fatalf("FindByUsername() error = %v", err)
	}
	count, err := repos.AIProfile.CountByUserID(user.ID)
	if err != nil {
		t.Fatalf("CountByUserID() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("profile count = %d, want 0 without server AI config", count)
	}
}

func newDemoBootstrapDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserAIProfile{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func newDemoBootstrapCodec(t *testing.T) *secret.Codec {
	t.Helper()
	codec, err := secret.NewCodecFromPassphrase("demo-bootstrap-test-secret")
	if err != nil {
		t.Fatalf("NewCodecFromPassphrase: %v", err)
	}
	return codec
}
