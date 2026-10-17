package store

import (
	"errors"
	"testing"
	"vidlens/internal/domain"
)

func TestActivitySeparatesModelCallsSendsAndPlanFromCurrentAction(t *testing.T) {
	plan := &domain.ExecutionPlan{Items: []domain.PlanItem{{ID: "write", Title: "写作", Status: "active"}}}
	steps := []domain.Step{{ID: "plan", Kind: "execution_plan", Status: "completed", Plan: plan}, {ID: "model", Kind: "model_response", ModelCall: 1, Status: "running"}, {ID: "send1", Kind: "model_submission", ModelCall: 1, Submission: 1, Status: "failed"}, {ID: "send2", Kind: "model_submission", ModelCall: 1, Submission: 2, Status: "running"}}
	out := activitySummary(steps, "available", nil, "/request", true)
	if out["model_calls"] != 1 || out["model_submissions"] != 2 || *out["current_step_id"].(*string) != "model" || out["plan"] != plan {
		t.Fatal(out)
	}
	legacy := activitySummary([]domain.Step{{ID: "old", Kind: "model_response", Status: "completed"}}, "available", nil, "/request", false)
	if legacy["model_submissions"] != nil {
		t.Fatal("invented unrecorded historical sends")
	}
}

func TestModelSubmissionLimitPreservesExplicitCheckpointRecovery(t *testing.T) {
	var p *domain.Problem
	if !errors.As(modelSubmissionLimit(3), &p) || p.Meta["can_retry"] != true || p.Meta["submissions"] != 3 {
		t.Fatal(p)
	}
}
