package service

import (
	"context"
	"strings"
	"testing"
)

type recordingCapabilityProber struct {
	got     *DecryptedAIProfile
	purpose string
}

func (*recordingCapabilityProber) TestProfile(context.Context, *DecryptedAIProfile) error { return nil }
func (p *recordingCapabilityProber) ProbeCapability(_ context.Context, profile *DecryptedAIProfile, purpose string) (int, error) {
	p.got, p.purpose = profile, purpose
	return 1024, nil
}

func TestProfileURLRulesMatchProtocolJoining(t *testing.T) {
	request := validAIProfileRequest()
	request.LLMBaseURL = "https://api.example.com/v1/chat/completions"
	if err := validateAIProfileRequest(request, true); err == nil || !strings.Contains(err.Error(), "基础地址") {
		t.Fatalf("full chat endpoint accepted: %v", err)
	}
	request = validAIProfileRequest()
	request.EmbeddingEndpoint = "https://api.example.com/v1"
	if err := validateAIProfileRequest(request, true); err == nil || !strings.Contains(err.Error(), "/embeddings") {
		t.Fatalf("incomplete embedding endpoint accepted: %v", err)
	}
	request = validAIProfileRequest()
	request.LLMBaseURL = "https://api.example.com/v1/v1"
	if err := validateAIProfileRequest(request, true); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate /v1 accepted: %v", err)
	}
	request = validAIProfileRequest()
	request.LLMBaseURL = "https://api.openai.com"
	if err := validateAIProfileRequest(request, true); err == nil || !strings.Contains(err.Error(), "/v1") {
		t.Fatalf("OpenAI root accepted: %v", err)
	}
	request.LLMBaseURL = "https://api.deepseek.com"
	if err := validateAIProfileRequest(request, true); err != nil {
		t.Fatalf("DeepSeek root rejected: %v", err)
	}
}

func TestProbeCapabilityResolvesOnlyOwnedStoredCredential(t *testing.T) {
	svc, _, _ := newAIProfileServiceForTest(t)
	created, err := svc.Create(7, validAIProfileRequest())
	if err != nil {
		t.Fatal(err)
	}
	prober := &recordingCapabilityProber{}
	svc.tester = prober
	_, err = svc.ProbeCapability(context.Background(), 8, ProbeCapabilityRequest{Purpose: "embedding", ProfileID: created.ID})
	if err != ErrAIProfileNotFound {
		t.Fatalf("cross-user probe = %v", err)
	}
	dim, err := svc.ProbeCapability(context.Background(), 7, ProbeCapabilityRequest{Purpose: "embedding", ProfileID: created.ID})
	if err != nil || dim != 1024 || prober.purpose != "embedding" || prober.got.EmbeddingAPIKey != "sk-embedding-secret" {
		t.Fatalf("stored-key probe: dim=%d err=%v", dim, err)
	}
}

func TestPromptPreferencesAreUserScopedAndRestoreDefault(t *testing.T) {
	svc, repos, _ := newAIProfileServiceForTest(t)
	if err := svc.SetPromptPreference(7, "chat", "请简洁回答"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.PromptPreferences(7)
	if err != nil || first[0].UserInstruction != "请简洁回答" || !strings.Contains(first[0].EffectivePreview, "请简洁回答") {
		t.Fatalf("own preference: %+v, %v", first, err)
	}
	other, err := svc.PromptPreferences(8)
	if err != nil || other[0].UserInstruction != "" {
		t.Fatalf("other user preference: %+v, %v", other, err)
	}
	if err := svc.SetPromptPreference(7, "chat", ""); err != nil {
		t.Fatal(err)
	}
	preference, err := repos.AIProfile.PromptPreference(7, "chat")
	if err != nil || preference != "" {
		t.Fatalf("restored preference = %q, %v", preference, err)
	}
}

func TestAnswerPreferenceDoesNotReplaceEvidenceInstruction(t *testing.T) {
	messages := appendUserPromptPreference(buildRAGMessages(nil, nil, "问题"), "请只给一句话")
	if len(messages) < 4 || !strings.Contains(messages[0].Content, "引用") || !strings.Contains(messages[1].Content, "请只给一句话") || !strings.Contains(messages[2].Content, "产品指令") {
		t.Fatalf("answer preference order = %+v", messages)
	}
}
