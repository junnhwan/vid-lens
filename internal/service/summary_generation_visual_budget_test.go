package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/processing"
	"vid-lens/internal/repository"
)

func visualBudgetExecutionFixture(t *testing.T, client ai.ChatClient) (*summaryVisualExecution, *generationFixture) {
	t.Helper()
	f := newGenerationFixture(t, false)
	var frozen processing.GenerationSnapshot
	_ = json.Unmarshal([]byte(f.job.InputSnapshotJSON), &frozen)
	lease := repository.SummaryGenerationLease{UserID: f.task.UserID, TaskID: f.task.ID, GenerationID: f.job.GenerationID, SourceID: f.source.ID, SourceDigest: f.source.SourceDigest, LeaseToken: f.job.ProcessingToken}
	run := &model.AgentRun{ID: f.job.GenerationID, UserID: f.task.UserID, TaskID: f.task.ID, SubjectKind: model.AgentRunSubjectSummaryGeneration, SubjectID: f.job.GenerationID, ExecutionKind: "artifact", RecipeVersion: processing.Recipe, ProfileSnapshot: "{}", PolicySnapshot: "{}", BudgetSnapshot: frozen.Intent.BudgetJSON, Status: model.AgentRunStatusRunning, Stage: "text_summary", MaxSteps: 18, MaxToolCalls: 8, MaxLLMCalls: 16, MaxAttemptsPerStep: 2, MaxPromptTokens: 24000, MaxCompletionTokens: 4096, MaxDurationMs: 90000, MaxContextChars: 24000 * 8, CompletionTokensUsed: 3000}
	if _, err := f.repos.StartSummaryGeneration(context.Background(), lease, run); err != nil {
		t.Fatal(err)
	}
	return &summaryVisualExecution{service: NewSummaryVisualService(f.repos, nil, nil), task: f.task, snapshot: frozen, source: f.source, lease: lease, journal: NewAgentExecutionJournal(repository.NewSummaryGenerationExecutionStore(f.repos, f.task.UserID, f.task.ID, f.job.GenerationID)), client: nonStreamingGenerationClient{client}, profile: f.profiles.profile, output: 2048}, f
}

func TestSummaryGenerationVisualRepairUsesRemainingSharedOutputBudget(t *testing.T) {
	client, requests, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{"unexpected":"not a plan"}`, usage: &ai.ChatUsage{PromptTokens: 20, CompletionTokens: 300}}, summaryBudgetResponse{content: `{"public_title":"核对画面","reason":"没有额外收益","targets":[]}`, usage: &ai.ChatUsage{PromptTokens: 25, CompletionTokens: 200}})
	e, f := visualBudgetExecutionFixture(t, client)
	var plan summaryVisualPlan
	if err := e.chatStep(context.Background(), "visual-plan", 1000, "核对真实画面", "返回规定计划JSON。", "只分析授权候选。", &plan); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("bounded repair changed")
	}
	for _, want := range []string{"1096", "796"} {
		if got := string((<-requests)["max_tokens"]); got != want {
			t.Fatalf("shared budget exceeded: %s want %s", got, want)
		}
	}
	run, err := e.journal.GetRun(context.Background(), f.task.UserID, f.job.GenerationID)
	if err != nil || run.CompletionTokensUsed != 3500 || run.MaxCompletionTokens != 4096 {
		t.Fatal("actual shared usage or frozen cap changed", err)
	}
}

func TestSummaryGenerationVisualRepairCannotCallAfterBudgetExhaustion(t *testing.T) {
	client, _, calls := summaryBudgetProvider(t, summaryBudgetResponse{content: `{"unexpected":"not a plan"}`, usage: &ai.ChatUsage{PromptTokens: 20, CompletionTokens: 1096}})
	e, _ := visualBudgetExecutionFixture(t, client)
	var plan summaryVisualPlan
	err := e.chatStep(context.Background(), "visual-plan", 1000, "核对真实画面", "返回规定计划JSON。", "只分析授权候选。", &plan)
	var domain *artifact.Error
	if !errors.As(err, &domain) || domain.Code != "visual_budget_exhausted" || calls.Load() != 1 {
		t.Fatalf("spent beyond frozen total: %v calls=%d", err, calls.Load())
	}
}
