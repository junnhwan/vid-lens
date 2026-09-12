package service

import (
	"encoding/json"
	"testing"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

func TestAgentBudgetRequestPresenceAndStrictContract(t *testing.T) {
	for _, tc := range []struct {
		data    string
		present bool
		value   bool
	}{
		{`{}`, false, false}, {`{"agent_budget":null}`, true, false},
		{`{"agent_budget":{"version":1,"max_tool_calls":2,"max_duration_seconds":90,"max_input_tokens":8193,"max_output_tokens":2048}}`, true, true},
	} {
		var req AIProfileRequest
		if err := json.Unmarshal([]byte(tc.data), &req); err != nil {
			t.Fatal(err)
		}
		if req.AgentBudget.Present != tc.present || (req.AgentBudget.Value != nil) != tc.value {
			t.Fatalf("presence: %+v", req.AgentBudget)
		}
	}
	for _, budget := range []string{
		`false`, `[]`, `"budget"`, `{}`, `{"version":2,"max_tool_calls":2,"max_duration_seconds":90,"max_input_tokens":8192,"max_output_tokens":2048}`,
		`{"version":1,"max_tool_calls":2.1,"max_duration_seconds":90,"max_input_tokens":8192,"max_output_tokens":2048}`,
		`{"version":1,"max_tool_calls":2,"max_duration_seconds":90,"max_input_tokens":8192,"max_output_tokens":2048,"unknown":1}`,
		`{"version":1,"max_tool_calls":0,"max_duration_seconds":90,"max_input_tokens":8192,"max_output_tokens":2048}`,
		`{"version":1,"max_tool_calls":2,"max_duration_seconds":999999999999999999999,"max_input_tokens":8192,"max_output_tokens":2048}`,
	} {
		var req AIProfileRequest
		if err := json.Unmarshal([]byte(`{"agent_budget":`+budget+`}`), &req); err == nil {
			t.Fatalf("accepted invalid budget %s", budget)
		}
	}
}
func TestAIProfileBudgetPersistsPreservesKeysAndResets(t *testing.T) {
	svc, repos, _ := newAIProfileServiceForTest(t)
	req := validAIProfileRequest()
	budget := config.DefaultAgentBudgetConfig().Defaults
	budget.MaxToolCalls = 4
	req.AgentBudget = model.AgentBudgetField{Present: true, Value: &budget}
	created, err := svc.Create(7, req)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repos.AIProfile.FindByIDForUser(7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	originalKey := stored.LLMAPIKeyCiphertext
	resolved, err := svc.GetDefaultConversationProfile(7)
	if err != nil || resolved.ProfileID != created.ID || resolved.EffectiveAgentBudget.Values.MaxToolCalls != 4 || resolved.Profile.LLMModel != req.LLMModel {
		t.Fatalf("snapshot: %+v %v", resolved, err)
	}
	req.AgentBudget = model.AgentBudgetField{}
	req.LLMAPIKey = ""
	req.ASRAPIKey = ""
	req.EmbeddingAPIKey = ""
	updated, err := svc.Update(7, created.ID, req)
	if err != nil || updated.AgentBudget.MaxToolCalls != 4 {
		t.Fatalf("omitted update: %+v %v", updated, err)
	}
	if _, err = svc.Update(8, created.ID, req); err != ErrAIProfileNotFound {
		t.Fatalf("cross-user update: %v", err)
	}
	req.AgentBudget = model.AgentBudgetField{Present: true}
	updated, err = svc.Update(7, created.ID, req)
	if err != nil || updated.AgentBudget != nil || updated.EffectiveAgentBudget.Source != "server_default" {
		t.Fatalf("reset: %+v %v", updated, err)
	}
	stored, err = repos.AIProfile.FindByIDForUser(7, created.ID)
	if err != nil || stored.AgentBudgetJSON != nil || stored.LLMAPIKeyCiphertext != originalKey {
		t.Fatalf("reset must write NULL and preserve secret: %v", err)
	}
	if resolved.EffectiveAgentBudget.Values.MaxToolCalls != 4 {
		t.Fatal("previously resolved snapshot changed")
	}
	bad := `{"version":2}`
	stored.AgentBudgetJSON = &bad
	if err = repos.AIProfile.UpdateForUser(7, stored); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(7)
	if err != nil || listed[0].AgentBudgetError == "" || listed[0].EffectiveAgentBudget != nil {
		t.Fatalf("corruption not repairable in response: %+v %v", listed, err)
	}
	if _, err = svc.GetDefaultConversationProfile(7); err == nil {
		t.Fatal("corrupt budget silently resolved")
	}
	if _, err = svc.GetDefaultAIProfile(7); err != nil {
		t.Fatalf("Agent budget broke ordinary Chat: %v", err)
	}
}
func TestAIProfileBudgetReducedServerLimitShowsEffectiveValue(t *testing.T) {
	svc, _, _ := newAIProfileServiceForTest(t)
	req := validAIProfileRequest()
	budget := config.DefaultAgentBudgetConfig().Defaults
	budget.MaxToolCalls = 20
	req.AgentBudget = model.AgentBudgetField{Present: true, Value: &budget}
	if _, err := svc.Create(7, req); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultAgentBudgetConfig()
	cfg.Limits.MaxToolCalls.Max = 10
	svc.WithAgentBudgetConfig(cfg)
	rows, err := svc.List(7)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].AgentBudget.MaxToolCalls != 20 || rows[0].EffectiveAgentBudget.Values.MaxToolCalls != 10 || len(rows[0].EffectiveAgentBudget.Adjustments) != 1 {
		t.Fatalf("old stored value should remain visible alongside tightened value: %+v", rows[0])
	}
}
