package service

import (
	"encoding/json"
	"strings"
	"testing"

	"vid-lens/internal/ai"
)

func TestSafeAgentProfileFreezesIdentityWithoutCredentialsOrRawEndpoints(t *testing.T) {
	profile := ai.Profile{
		LLMProvider: "provider-a", LLMBaseURL: "https://llm.example/v1?token=endpoint-secret", LLMAPIKey: "llm-secret", LLMModel: "chat-a",
		EmbeddingProvider: "provider-b", EmbeddingEndpoint: "https://embed.example/v1", EmbeddingAPIKey: "embedding-secret", EmbeddingModel: "embed-b", EmbeddingDim: 1536,
		VisionProvider: "provider-c", VisionBaseURL: "https://vision.example/v1", VisionAPIKey: "vision-secret", VisionModel: "vision-c",
	}
	encoded, err := json.Marshal(safeAgentProfile(profile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"llm-secret", "embedding-secret", "vision-secret", "endpoint-secret", "https://"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("profile snapshot leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"provider-a", "chat-a", "provider-b", "embed-b", "provider-c", "vision-c", "endpoint_digest"} {
		if !strings.Contains(text, required) {
			t.Fatalf("profile snapshot omitted %q: %s", required, text)
		}
	}
}

func TestAgentPoliciesExposeActualSingleAttemptExecution(t *testing.T) {
	_, templateBudget := defaultTemplateAgentPolicy(5)
	_, researchBudget := researchAgentPolicy(5, DefaultVideoResearchPolicy())
	_, funnelBudget := evidenceFunnelAgentPolicy(defaultEvidenceFunnelPolicy(5))
	for name, budget := range map[string]frozenAgentBudget{
		"template":        templateBudget,
		"research":        researchBudget,
		"evidence_funnel": funnelBudget,
	} {
		if budget.MaxAttemptsPerStep != 1 {
			t.Fatalf("%s max attempts per step = %d, want actual single execution", name, budget.MaxAttemptsPerStep)
		}
	}
}
