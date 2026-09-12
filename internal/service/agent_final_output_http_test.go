package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

func TestConversationFinalHTTPOutputUsesFrozenRemainingBudget(t *testing.T) {
	for _, scenario := range []string{"remaining", "zero_remaining", "negative_remaining"} {
		t.Run(scenario, func(t *testing.T) {
			repos, task, session := newVideoAgentTestSession(t)
			cfg := config.DefaultAgentBudgetConfig()
			values := cfg.Defaults
			values.MaxToolCalls = 2
			values.MaxOutputTokens = 8192
			effective, err := cfg.Resolve(&values)
			if err != nil {
				t.Fatal(err)
			}
			total := int64(effective.Values.MaxOutputTokens)
			plannerOutput := int64(100)
			if scenario == "zero_remaining" {
				plannerOutput = total
			}
			if scenario == "negative_remaining" {
				plannerOutput = total + 1
			}
			profile := &budgetProductProfile{resolved: ResolvedConversationProfile{Profile: &ai.Profile{LLMModel: "fixture", EmbeddingModel: "embed"}, ProfileID: 19, AgentBudget: &values, EffectiveAgentBudget: effective}}
			type requestBody struct {
				MaxTokens int64            `json:"max_tokens"`
				Messages  []ai.ChatMessage `json:"messages"`
			}
			requests := make(chan requestBody, 8)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body requestBody
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				requests <- body
				number := calls.Add(1)
				text := `{"tool":"search_transcript","reason":"find evidence","arguments":{"question":"owner","top_k":1}}`
				completion := plannerOutput
				if number > 1 {
					text = "已核对 owner [C1]"
					completion = 200
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}, "usage": map[string]int64{"prompt_tokens": 120, "completion_tokens": completion}})
			}))
			defer server.Close()
			client := ai.NewOpenAIChatClient(server.URL, "", "fixture")
			chat := NewChatService(repos, &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, ChunkID: 1, EvidenceID: "owner", Content: "校验 owner"}}}, ChatConfig{TopK: 1})
			executor := NewConversationExecution(chat, NewVideoAgentService(chat), profile, productFixtureClients{chat: client})
			req := ConversationRequest{Kind: ConversationKindAgent, UserID: 7, SessionID: session.ID, Question: "整理全部步骤", RunID: "http-final-" + scenario}
			result, err := executor.Execute(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			run, err := repos.AgentExecution.GetRun(context.Background(), 7, req.RunID)
			if err != nil {
				t.Fatal(err)
			}
			first := <-requests
			if first.MaxTokens != 1024 {
				t.Fatalf("planner max_tokens=%d", first.MaxTokens)
			}
			wantCalls := int32(1)
			if scenario == "remaining" {
				wantCalls = 2
				if calls.Load() != wantCalls {
					t.Fatalf("calls=%d", calls.Load())
				}
				final := <-requests
				if final.MaxTokens != run.MaxCompletionTokens-plannerOutput || final.MaxTokens <= 2048 {
					t.Fatalf("final max_tokens=%d frozen=%d planner=%d", final.MaxTokens, run.MaxCompletionTokens, plannerOutput)
				}
				if run.Status != model.AgentRunStatusCompleted || run.CompletionTokensUsed != 300 {
					t.Fatalf("usage/terminal=%+v", run)
				}
				records, err := repos.AgentExecution.GetExecution(context.Background(), 7, req.RunID)
				if err != nil {
					t.Fatal(err)
				}
				actualCalls := 0
				for _, call := range records.ToolCalls {
					if call.CompletionTokens > 0 {
						if call.UsageSource != model.AgentCallUsageActual {
							t.Fatalf("provider usage not retained: %+v", call)
						}
						actualCalls++
					}
				}
				if actualCalls != 2 {
					t.Fatalf("actual provider calls=%d", actualCalls)
				}
			} else {
				if calls.Load() != wantCalls || run.Status != model.AgentRunStatusBudgetExhausted || run.CompletionTokensUsed != plannerOutput {
					t.Fatalf("nonpositive remaining invoked writer or lost usage: calls=%d run=%+v", calls.Load(), run)
				}
			}
			frozen := run.BudgetSnapshot
			profile.resolved.EffectiveAgentBudget.Values.MaxOutputTokens = 32768
			replay, err := executor.Execute(context.Background(), req)
			if err != nil || replay.Agent.MessageID != result.Agent.MessageID || calls.Load() != wantCalls {
				t.Fatalf("replay invoked provider: calls=%d err=%v", calls.Load(), err)
			}
			run, err = repos.AgentExecution.GetRun(context.Background(), 7, req.RunID)
			if err != nil || run.BudgetSnapshot != frozen {
				t.Fatalf("replay changed frozen budget: %v", err)
			}
		})
	}
}
