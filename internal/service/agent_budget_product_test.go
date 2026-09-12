package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

type budgetProductProfile struct {
	resolved ResolvedConversationProfile
	calls    int
}

func (p *budgetProductProfile) GetDefaultAIProfile(int64) (*ai.Profile, error) {
	panic("agent must resolve profile and budget together")
}
func (p *budgetProductProfile) GetDefaultConversationProfile(int64) (*ResolvedConversationProfile, error) {
	p.calls++
	return &p.resolved, nil
}

func TestProductBudgetFinalizesWithinFrozenLimitAndReplays(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	cfg := config.DefaultAgentBudgetConfig()
	values := cfg.Defaults
	values.MaxToolCalls = 2
	effective, err := cfg.Resolve(&values)
	if err != nil {
		t.Fatal(err)
	}
	profile := &budgetProductProfile{resolved: ResolvedConversationProfile{Profile: &ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"}, ProfileID: 19, AgentBudget: &values, EffectiveAgentBudget: effective}}
	client := &scriptedChatClient{responses: []string{`{"tool":"search_transcript","reason":"find evidence","arguments":{"question":"owner","top_k":1}}`, "已确认 owner；其他步骤尚未核对 [C1]"}}
	chat := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "owner", Content: "校验 owner"}}}, ChatConfig{TopK: 1})
	executor := NewConversationExecution(chat, NewVideoAgentService(chat), profile, productFixtureClients{chat: client})
	req := ConversationRequest{Kind: ConversationKindAgent, UserID: 7, SessionID: session.ID, Question: "整理全部步骤", RunID: "budget-product-test"}
	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent.StopReason != "budget_finalized" || !result.Agent.Degraded || result.Agent.BudgetNotice == nil {
		t.Fatalf("result=%+v", result.Agent)
	}
	run, _ := repos.AgentExecution.GetRun(context.Background(), 7, req.RunID)
	if run.Status != model.AgentRunStatusCompleted || run.ToolCallsUsed != 2 || run.LLMCallsUsed != 2 || run.PromptTokensUsed <= 0 || run.CompletionTokensUsed <= 0 {
		t.Fatalf("run=%+v", run)
	}
	oldBudget := run.BudgetSnapshot
	profile.resolved.EffectiveAgentBudget.Values.MaxToolCalls = 8
	replay, err := executor.Execute(context.Background(), req)
	if err != nil || replay.Agent.MessageID != result.Agent.MessageID || len(client.messages) != 2 {
		t.Fatalf("replay=%+v err=%v calls=%d", replay, err, len(client.messages))
	}
	run, _ = repos.AgentExecution.GetRun(context.Background(), 7, req.RunID)
	if run.BudgetSnapshot != oldBudget || profile.calls != 2 {
		t.Fatal("mutable budget or duplicated model resolution")
	}
}

func TestPlannerInputDeduplicatesEvidenceAndMatchesAdmissionEstimate(t *testing.T) {
	content := strings.Repeat("唯一的证据正文", 70)
	chunk := RetrievedChunk{TaskID: 1, ChunkID: 1, EvidenceID: "e1", Content: content, AnchorContent: content, Modality: "transcript", StartMS: 10, EndMS: 20}
	state := VideoAgentLoopState{Goal: "test", Evidence: []RetrievedChunk{chunk}, Observations: []VideoAgentLoopObservation{{NewEvidence: []RetrievedChunk{chunk}}}, Steps: []VideoAgentLoopStep{{Observation: &VideoAgentLoopObservation{NewEvidence: []RetrievedChunk{chunk}}}}}
	messages, err := buildPlannerMessages(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(messages)
	if strings.Count(string(raw), content) != 1 {
		t.Fatal("evidence body repeated")
	}
	if plannerContextChars(state, nil) != estimatedPlannerCallUsage(messages, "").ContextChars {
		t.Fatal("admission prompt mismatch")
	}
	if !strings.Contains(string(raw), "e1") || !strings.Contains(string(raw), "start_ms") {
		t.Fatal("provenance lost")
	}
}

func TestPlannerContextHasHardTokenBound(t *testing.T) {
	state := VideoAgentLoopState{Goal: "全片要点"}
	for i := 0; i < 20; i++ {
		state.Evidence = append(state.Evidence, RetrievedChunk{TaskID: 1, ChunkID: int64(i + 1), Content: strings.Repeat("长视频内容", 800)})
	}
	for i := 0; i < 6; i++ {
		state.Conversation = append(state.Conversation, ConversationContextMessage{Role: "user", Content: strings.Repeat("历史讨论", 250)})
	}
	messages, err := buildPlannerMessages(state, NewVideoAgentTools(nil, nil, nil).Registry().Definitions())
	if err != nil {
		t.Fatal(err)
	}
	if estimatedPlannerCallUsage(messages, "").PromptTokens > 8192 {
		t.Fatal("unbounded planner context")
	}
	if len([]rune(state.Evidence[0].Content)) != 4000 {
		t.Fatal("mutated durable state")
	}
}
