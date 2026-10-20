package investigator

import (
	"context"
	"encoding/json"
	"testing"
)

const deterministicDraftBody = `{"version":1,"sections":[{"id":"start","blocks":[{"type":"paragraph","origin":"video","text":"开始事实。","citation_ids":["t1"]}]},{"id":"finish","blocks":[{"type":"paragraph","origin":"video","text":"结束事实。","citation_ids":["t25"]}]},{"id":"review","blocks":[{"type":"paragraph","origin":"video","text":"回顾事实。","citation_ids":["t2"]}]}]}`

func deterministicDraftSession(t *testing.T) *session {
	t.Helper()
	s := planSession(t)
	s.cp.HarnessRevision = 16
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	if result, err := planCall(s, "initial-plan", "begin_article", thematicPlanFixture()); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "initial-body", "save_article_sections", deterministicDraftBody); err != nil || result["version"] != float64(2) {
		t.Fatal(result, err)
	}
	return s
}

func TestUnchangedDraftKeepsPendingCorrectionAndDoesNotResetProgress(t *testing.T) {
	s := deterministicDraftSession(t)
	issue := map[string]any{"code": "semantic_evidence", "issues": []any{map[string]any{"section_id": "finish", "path": "/sections/1/blocks/0/content/0/text"}}}
	s.cp.DraftIssue, s.cp.EvidenceReviewIssue = issue, issue
	s.cp.DraftCorrection, s.cp.DraftRepairVersion = true, 2
	s.cp.Progress.Stalled = 3
	var body map[string]any
	_ = json.Unmarshal([]byte(deterministicDraftBody), &body)
	body["version"] = 2
	result, err := planCall(s, "unchanged-body", "save_article_sections", body)
	if err != nil || result["error"] != nil || result["version"] != float64(2) || result["outcome"] != "noop" {
		t.Fatal("unchanged content was rejected or advanced its version", result, err)
	}
	if !s.cp.DraftCorrection || s.cp.DraftIssue == nil || s.pendingEvidenceReview() == nil || s.cp.Progress.Stalled != 4 {
		t.Fatal("noop cleared a correction or counted as content progress", s.cp)
	}
	version, _, err := s.loadDraft(context.Background())
	if err != nil || version != 2 {
		t.Fatal(version, err)
	}
	if err = s.autoFinalize(context.Background()); err != nil {
		t.Fatal("an unchanged pending correction attempted publication", err)
	}
}
