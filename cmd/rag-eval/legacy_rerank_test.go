package main

import (
	"testing"

	"vid-lens/internal/ai"
)

type recordingRerankClientFactory struct {
	calls   int
	profile ai.Profile
	client  ai.RerankClient
	err     error
}

func (f *recordingRerankClientFactory) NewRerankClient(profile ai.Profile) (ai.RerankClient, error) {
	f.calls++
	f.profile = profile
	return f.client, f.err
}

func TestNewLegacyModelRerankerDoesNotCreateClientWithoutExplicitModel(t *testing.T) {
	factory := &recordingRerankClientFactory{}

	if reranker := newLegacyModelReranker(factory, ai.Profile{}); reranker == nil {
		t.Fatal("newLegacyModelReranker() returned nil; want fallback-capable reranker")
	}
	if factory.calls != 0 {
		t.Fatalf("NewRerankClient() calls = %d, want 0", factory.calls)
	}
}

func TestNewLegacyModelRerankerCreatesClientWithExplicitExperimentConfig(t *testing.T) {
	factory := &recordingRerankClientFactory{}
	profile := ai.Profile{
		RerankEndpoint: "https://api.example.com/v1/rerank",
		RerankModel:    "Qwen/Qwen3-Reranker-4B",
	}

	if reranker := newLegacyModelReranker(factory, profile); reranker == nil {
		t.Fatal("newLegacyModelReranker() returned nil")
	}
	if factory.calls != 1 {
		t.Fatalf("NewRerankClient() calls = %d, want 1", factory.calls)
	}
	if factory.profile.RerankEndpoint != profile.RerankEndpoint || factory.profile.RerankModel != profile.RerankModel {
		t.Fatalf("factory profile = %+v, want endpoint/model from explicit experiment config", factory.profile)
	}
}
