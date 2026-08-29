package repository

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestAIProfileRepositoryKeepsSingleDefaultPerUser(t *testing.T) {
	repo := newAIProfileTestRepo(t)

	first := validAIProfile(1, "first")
	first.IsDefault = true
	if err := repo.Create(first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}

	second := validAIProfile(1, "second")
	second.IsDefault = true
	if err := repo.Create(second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	profiles, err := repo.ListByUserID(1)
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}
	defaults := 0
	for _, profile := range profiles {
		if profile.IsDefault {
			defaults++
			if profile.ID != second.ID {
				t.Fatalf("default profile ID = %d, want second ID %d", profile.ID, second.ID)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("default count = %d, want 1", defaults)
	}
}

func TestAIProfileRepositoryFindDefaultByUserIDIsUserScoped(t *testing.T) {
	repo := newAIProfileTestRepo(t)

	userOne := validAIProfile(1, "user one")
	userOne.IsDefault = true
	if err := repo.Create(userOne); err != nil {
		t.Fatalf("Create(userOne) error = %v", err)
	}

	userTwo := validAIProfile(2, "user two")
	userTwo.IsDefault = true
	if err := repo.Create(userTwo); err != nil {
		t.Fatalf("Create(userTwo) error = %v", err)
	}

	got, err := repo.FindDefaultByUserID(2)
	if err != nil {
		t.Fatalf("FindDefaultByUserID() error = %v", err)
	}
	if got == nil || got.ID != userTwo.ID {
		t.Fatalf("FindDefaultByUserID(2) = %+v, want user two profile", got)
	}
}

func TestAIProfileRepositoryUpdateRejectsCrossUserAccess(t *testing.T) {
	repo := newAIProfileTestRepo(t)
	profile := validAIProfile(1, "owned")
	if err := repo.Create(profile); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	profile.Name = "updated"
	err := repo.UpdateForUser(2, profile)
	if err == nil {
		t.Fatal("UpdateForUser() with wrong user succeeded, want error")
	}
}

func TestAIProfileRepositoryUpdatePreservesCreatedAt(t *testing.T) {
	repo := newAIProfileTestRepo(t)
	profile := validAIProfile(1, "original")
	if err := repo.Create(profile); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if profile.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero after Create(), test setup invalid")
	}

	updated := validAIProfile(1, "updated")
	updated.ID = profile.ID
	updated.IsDefault = true
	updated.ASRModel = "mimo-v2.5-asr"
	if err := repo.UpdateForUser(1, updated); err != nil {
		t.Fatalf("UpdateForUser() error = %v", err)
	}

	got, err := repo.FindByIDForUser(1, profile.ID)
	if err != nil {
		t.Fatalf("FindByIDForUser() error = %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt was overwritten with zero time")
	}
	if !got.CreatedAt.Equal(profile.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want preserved %v", got.CreatedAt, profile.CreatedAt)
	}
	if got.Name != "updated" {
		t.Fatalf("Name = %q, want updated", got.Name)
	}
}

func newAIProfileTestRepo(t *testing.T) *AIProfileRepository {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.UserAIProfile{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return NewAIProfileRepository(db)
}

func validAIProfile(userID int64, name string) *model.UserAIProfile {
	return &model.UserAIProfile{
		UserID:                    userID,
		Name:                      name,
		LLMProvider:               "openai_compatible",
		LLMBaseURL:                "https://llm.example.com/v1",
		LLMAPIKeyCiphertext:       "encrypted-llm",
		LLMModel:                  "chat-model",
		ASRProvider:               "mimo",
		ASRBaseURL:                "https://token-plan-cn.xiaomimimo.com/v1",
		ASRAPIKeyCiphertext:       "encrypted-asr",
		ASRModel:                  "mimo-v2.5-asr",
		EmbeddingProvider:         "openai_compatible",
		EmbeddingEndpoint:         "https://router.tumuer.me/v1/embeddings",
		EmbeddingAPIKeyCiphertext: "encrypted-embedding",
		EmbeddingModel:            "text-embedding-3-small",
		EmbeddingDim:              1536,
	}
}
