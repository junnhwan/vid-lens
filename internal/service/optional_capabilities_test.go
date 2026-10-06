package service

import (
	"context"
	"errors"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

type optionalProfileFixture struct{ profile ai.Profile }

func (f optionalProfileFixture) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	p := f.profile
	return &p, nil
}

type unavailableHostedProfileFixture struct{}

func (unavailableHostedProfileFixture) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	return nil, ErrHostedAIUnavailable
}

func TestRerankCanBeDisabledWhenHostedAIBecomesUnavailable(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 101, Username: "hosted-offline", RerankEnabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := NewUserService(repos.User, config.JWTConfig{}).WithOptionalCapabilities(unavailableHostedProfileFixture{}, true, DefaultRAGRetrievalConfig(), false)
	view, err := svc.SetRerankPreference(context.Background(), 101, false)
	if err != nil || view.RerankEnabled || view.RerankAvailable {
		t.Fatalf("cannot disable with unavailable provider: view=%+v err=%v", view, err)
	}
}

func TestOptionalRerankPreferencePersistsAndDoesNotAffectAnotherUser(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	for _, id := range []int64{101, 102} {
		if err := repos.User.Create(&model.User{ID: id, Username: string(rune(id)), PasswordHash: "unused"}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultRAGRetrievalConfig()
	cfg.RerankerMode, cfg.RerankerVersion = RerankerModeModel, "configured-ranker"
	profiles := optionalProfileFixture{ai.Profile{EmbeddingEndpoint: "https://example.com/v1/embeddings", EmbeddingAPIKey: "test-key"}}
	svc := NewUserService(repos.User, config.JWTConfig{}).WithOptionalCapabilities(profiles, true, cfg, true)
	view, err := svc.OptionalCapabilities(context.Background(), 101)
	if err != nil || view.RerankEnabled || !view.RerankAvailable || !view.AlignmentConfigured || view.RerankModel != "configured-ranker" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if _, err := svc.SetRerankPreference(context.Background(), 101, true); err != nil {
		t.Fatal(err)
	}
	// A fresh service reads durable account state, not a request-local flag.
	fresh := NewUserService(repos.User, config.JWTConfig{}).WithOptionalCapabilities(profiles, true, cfg, true)
	view, err = fresh.OptionalCapabilities(context.Background(), 101)
	other, otherErr := fresh.OptionalCapabilities(context.Background(), 102)
	if err != nil || otherErr != nil || !view.RerankEnabled || other.RerankEnabled {
		t.Fatalf("own=%+v other=%+v errs=%v/%v", view, other, err, otherErr)
	}
	if _, err := fresh.SetRerankPreference(context.Background(), 101, false); err != nil {
		t.Fatal(err)
	}
	view, _ = fresh.OptionalCapabilities(context.Background(), 101)
	if view.RerankEnabled {
		t.Fatal("explicit false was not persisted")
	}
}

func TestUnavailableRerankCannotBeEnabled(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 101, Username: "unavailable", PasswordHash: "unused"}); err != nil {
		t.Fatal(err)
	}
	svc := NewUserService(repos.User, config.JWTConfig{})
	if _, err := svc.SetRerankPreference(context.Background(), 101, true); !errors.Is(err, ErrRerankUnavailable) {
		t.Fatalf("err=%v", err)
	}
	enabled, err := repos.User.RerankEnabled(context.Background(), 101)
	if err != nil || enabled {
		t.Fatalf("unavailable preference enabled=%v err=%v", enabled, err)
	}
}

func TestUserRerankOptInGatesModelFactoryAndPreservesDeploymentConfig(t *testing.T) {
	repos := newChatServiceTestRepositories(t)
	if err := repos.User.Create(&model.User{ID: 101, Username: "opt-in", PasswordHash: "unused"}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultRAGRetrievalConfig()
	cfg.RerankerMode, cfg.RerankerVersion = RerankerModeModel, "deployment-ranker"
	calls := 0
	svc := NewChatService(repos, &fakeRetriever{}, ChatConfig{Retrieval: &cfg, ModelRerankerFactory: func(p ai.Profile) Reranker { calls++; return DeterministicReranker{} }})
	profile := ai.Profile{RerankModel: "hosted-ranker"}
	for _, enabled := range []bool{false, true, false} {
		if err := repos.User.SetRerankEnabled(context.Background(), 101, enabled); err != nil {
			t.Fatal(err)
		}
		p, err := svc.newUserRetrievalPipeline(context.Background(), 101, 5, nil, profile)
		if err != nil {
			t.Fatal(err)
		}
		p.applyPolicy(PolicyFor(IntentDirectQA, ScopeCollection))
		if enabled && (p.reranker == nil || p.Config.RerankerVersion != "hosted-ranker") || !enabled && (p.reranker != nil || p.Config.RerankerMode != RerankerModeNone) {
			t.Fatalf("enabled=%v pipeline=%+v", enabled, p)
		}
	}
	if calls != 1 || cfg.RerankerMode != RerankerModeModel || cfg.RerankerVersion != "deployment-ranker" {
		t.Fatalf("factory calls=%d shared config=%+v", calls, cfg)
	}
}
