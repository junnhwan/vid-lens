package investigator

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestUnplannedChapterDoesNotTurnAcceptedPlannedChaptersIntoCorrections(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	if result, err := planCall(s, "plan", "begin_article", thematicPlanFixture()); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	var input map[string]any
	_ = json.Unmarshal([]byte(deterministicDraftBody), &input)
	sections := input["sections"].([]any)
	input["sections"] = append(sections, map[string]any{"id": "not-in-plan", "blocks": []any{map[string]any{"type": "paragraph", "origin": "video", "text": "计划外内容不能改变来源或计划。", "citation_ids": []string{"not-a-real-source"}}}})
	result, err := planCall(s, "save-with-extra", "save_article_sections", input)
	if err != nil || result["error"] != nil || result["version"] != float64(2) {
		t.Fatal("an extraneous chapter turned a valid saved batch into author repair", result, err)
	}
	ignored, _ := result["ignored_unplanned_sections"].([]any)
	if len(ignored) != 1 || ignored[0] != "not-in-plan" || s.cp.DraftIssue != nil {
		t.Fatal("ignored chapter was hidden or left a false correction", result, s.cp.DraftIssue)
	}
	_, draft, err := s.loadDraft(context.Background())
	if err != nil || len(draft.Plan) != 3 || len(draft.Sections) != 3 {
		t.Fatal("the saved plan or its required chapters were changed", draft, err)
	}
	if _, exists := draft.Sections["not-in-plan"]; exists {
		t.Fatal("unplanned prose entered the authoritative draft")
	}
	if result, err := planCall(s, "save-with-extra", "save_article_sections", input); err != nil || result["version"] != float64(2) || string(domain.JSON(result["ignored_unplanned_sections"])) != `["not-in-plan"]` {
		t.Fatal("the saved batch receipt was not replayed", result, err)
	}
}

func TestOnlyUnplannedChaptersKeepPendingWorkAndCorrection(t *testing.T) {
	s := deterministicDraftSession(t)
	issue := map[string]any{"code": "semantic_evidence", "candidate_hash": "still-needs-correction"}
	s.cp.DraftIssue, s.cp.EvidenceReviewIssue = issue, issue
	s.cp.DraftCorrection, s.cp.DraftRepairVersion = true, 2
	input := saveSectionsInput{Version: 2, Sections: []domain.Section{{ID: "extra", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "计划外内容。"}}}}}}}
	result, err := planCall(s, "only-extra", "save_article_sections", input)
	if err != nil || result["version"] != float64(2) || result["outcome"] != "noop" || s.cp.DraftIssue["candidate_hash"] != "still-needs-correction" || !s.cp.DraftCorrection {
		t.Fatal("ignored input changed the pending correction or its version", result, err)
	}
	if err := s.autoFinalize(context.Background()); err != nil {
		t.Fatal("ignored input attempted to finalize an unchanged correction", err)
	}
}
