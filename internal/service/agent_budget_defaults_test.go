package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
)

func TestLargerDefaultStudyBudgetCoversMoreThanEightSegmentsAndStaysFrozen(t *testing.T) {
	svc, db, calls := artifactFixture(t, artifactModelResponse)
	if err := db.Model(&model.VideoTranscription{}).Where("task_id=?", 42).Update("content", strings.Repeat("事务保证原子性。提交后修改生效，回滚撤销修改。", 2000)).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := svc.Submit(ctx, 7, "large-default", artifactRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := svc.repos.Artifact.Run(ctx, 7, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.MaxLLMCalls != 32 || run.MaxPromptTokens != 262144 || run.MaxCompletionTokens != 65536 || run.MaxDurationMs != 1200000 {
		t.Fatalf("new run budget=%+v", run)
	}
	// A change to defaults after submission must not change the queued run.
	changed := config.DefaultAgentBudgetConfig()
	changed.Defaults.MaxToolCalls = 2
	svc.profiles.WithAgentBudgetConfig(changed)
	if err = svc.ExecuteArtifact(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	final, err := svc.Run(ctx, 7, view.ID)
	if err != nil || final.Status != "completed" || final.Progress.TotalSegments <= 8 || final.Progress.CoveredSegments != final.Progress.TotalSegments || calls.Load() <= 8 {
		t.Fatalf("larger study failed: view=%+v calls=%d err=%v", final, calls.Load(), err)
	}
	if final.Budget.MaxLLMCalls != 32 {
		t.Fatal("queued run picked up changed default")
	}
}

func TestLargerDefaultConversationBudgetIncludesPlanningAndReplanning(t *testing.T) {
	cfg := config.DefaultAgentBudgetConfig()
	effective, err := cfg.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), resolvedBudgetContextKey{}, &ResolvedConversationProfile{EffectiveAgentBudget: effective})
	policy, budget := loopAgentPolicy(1, DefaultVideoAgentLoopPolicy())
	applyResolvedAgentBudget(ctx, &policy, &budget, false)
	if policy.MaxSteps != 32 || policy.MaxReplans != 8 || budget.MaxLLMCalls != 65 || budget.MaxPromptTokens != 262144 || budget.MaxCompletionTokens != 65536 || budget.MaxDurationMs != 1200000 {
		t.Fatalf("policy=%+v budget=%+v", policy, budget)
	}
	if err = (VideoAgentLoopPolicy{MaxSteps: policy.MaxSteps, MaxReplans: policy.MaxReplans}).Validate(); err != nil {
		t.Fatal(err)
	}
	// Snapshots retain the earlier limits independently of today's resolver.
	encoded, _ := json.Marshal(budget)
	var frozen frozenAgentBudget
	if err = json.Unmarshal(encoded, &frozen); err != nil || frozen.MaxToolCalls != 32 {
		t.Fatalf("snapshot=%+v %v", frozen, err)
	}
}
