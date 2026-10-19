package store

import (
	"context"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestRunDiagnosticsExplainsRepairDeadEndsWithoutCandidateContent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, err := s.CreateRequest(ctx, session(t, s), domain.ID(), "local://diagnostic-repair.mp4", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	if err = s.Pool.QueryRow(ctx, "SELECT active_run_id FROM generation_requests WHERE id=$1", a.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(ctx, runID, 1, "diagnostic")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	for key, reason := range map[string]string{
		"missing-field": "字段/sections/3/start_ms不存在",
		"count":         `{"code":"plan_invalid","issues":[{"code":"evidence_count_exceeded","path":"/sections/2/evidence_ids","actual":"private-candidate-text","expected":"private-provider-credential"}],"detail":"private-question-text"}`,
		"opaque":        "private-prompt-and-signed-url",
	} {
		if _, err = s.BeginOperation(ctx, *c, key, "tool_feedback", nil, false); err != nil {
			t.Fatal(err)
		}
		if err = s.FinishOperation(ctx, *c, key, map[string]any{"tool": "repair_article_plan", "call_id": key, "rejected": true, "rejection_reason": reason, "progress": map[string]any{"stalled": 4}}, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	diagnostic, err := s.RunDiagnostics(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(domain.JSON(diagnostic))
	for _, want := range []string{"tool_rejections", "repair_field_missing", "/sections/3/start_ms", "evidence_count_exceeded", "/sections/2/evidence_ids"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing safe failure explanation %q", want)
		}
	}
	if strings.Contains(raw, "private-") {
		t.Fatal("read-only diagnostics leaked private candidate, prompt, credential or URL")
	}
}

func TestToolRejectionDiagnosticsUnwrapsEncodedAndNestedValidationIssues(t *testing.T) {
	for _, raw := range []string{
		`{"code":"plan_invalid","issues":[{"code":"evidence_count_exceeded","path":"/sections/2/evidence_ids","actual":"private-candidate-text"}]}`,
		`{"code":"chapter_validation","affected_sections":[{"section_id":"s3","issue":{"code":"candidate_schema","issues":[{"code":"evidence_count_exceeded","path":"/sections/2/evidence_ids","detail":"private-candidate-text"}]}}]}`,
	} {
		// String-valued tool errors are JSON strings in the execution ledger.
		reason := string(domain.JSON(raw))
		issues := diagnosticRejectionIssues(reason)
		if len(issues) != 1 || issues[0].Code != "evidence_count_exceeded" || issues[0].Path != "/sections/2/evidence_ids" {
			t.Fatalf("real tool error envelope concealed the actionable leaf: %s", domain.JSON(issues))
		}
		if strings.Contains(string(domain.JSON(issues)), "private-") {
			t.Fatal("unwrapping a tool error exposed private source values")
		}
	}
}
