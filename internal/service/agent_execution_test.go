package service

import (
	"context"
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

func TestAgentPoliciesExposeActualAttemptExecution(t *testing.T) {
	_, templateBudget := defaultTemplateAgentPolicy(5)
	_, researchBudget := researchAgentPolicy(5, DefaultVideoResearchPolicy())
	_, funnelBudget := evidenceFunnelAgentPolicy(defaultEvidenceFunnelPolicy(5))
	for name, test := range map[string]struct {
		budget frozenAgentBudget
		want   int
	}{
		"template":        {budget: templateBudget, want: 1},
		"research":        {budget: researchBudget, want: 1},
		"evidence_funnel": {budget: funnelBudget, want: 2},
	} {
		if test.budget.MaxAttemptsPerStep != test.want {
			t.Fatalf("%s max attempts per step = %d, want %d", name, test.budget.MaxAttemptsPerStep, test.want)
		}
	}
}

func TestEnsureAgentRunResumesFrozenPolicyAndBudgetAfterDefaultsChange(t *testing.T) {
	repos, _, session := newVideoAgentTestSession(t)
	agent := NewVideoAgentService(NewChatService(repos, &fakeRetriever{}, ChatConfig{}))
	profile := ai.Profile{EmbeddingModel: "embed", LLMModel: "chat-model"}
	policy := defaultEvidenceFunnelPolicy(1)
	frozenPolicy, budget := evidenceFunnelAgentPolicy(policy)

	created, err := agent.ensureAgentRun(context.Background(), "frozen-config-run", 7, session, "owner", string(VideoAgentEvidenceFunnelTemplate), "bounded-evidence-funnel", profile, frozenPolicy, budget)
	if err != nil {
		t.Fatalf("create ensureAgentRun() error = %v", err)
	}
	storedPolicy, storedBudget := created.PolicySnapshot, created.BudgetSnapshot

	changedPolicy := frozenPolicy
	changedPolicy.MaxVisualCandidates++
	changedBudget := budget
	changedBudget.MaxAttemptsPerStep++
	resumed, err := agent.ensureAgentRun(context.Background(), "frozen-config-run", 7, session, "owner", string(VideoAgentEvidenceFunnelTemplate), "bounded-evidence-funnel", profile, changedPolicy, changedBudget)
	if err != nil {
		t.Fatalf("resume ensureAgentRun() rejected historical frozen config: %v", err)
	}
	if resumed.PolicySnapshot != storedPolicy || resumed.BudgetSnapshot != storedBudget || resumed.MaxAttemptsPerStep != budget.MaxAttemptsPerStep {
		t.Fatalf("resume replaced frozen config: %+v", resumed)
	}
	resumedPolicy, err := evidenceFunnelPolicyFromRun(resumed)
	if err != nil {
		t.Fatalf("evidenceFunnelPolicyFromRun() error = %v", err)
	}
	if resumedPolicy.MaxVisualCandidates != policy.MaxVisualCandidates {
		t.Fatalf("runtime policy used changed defaults: %+v", resumedPolicy)
	}
}
