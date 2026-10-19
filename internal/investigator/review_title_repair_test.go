package investigator

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestReviewedChapterTitleCanBeCorrectedWithoutChangingPlanIdentity(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := thematicPlanFixture()
	if result, err := planCall(s, "title-plan", "begin_article", plan); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	body := `{"version":1,"sections":[{"id":"start","blocks":[{"type":"paragraph","origin":"video","text":"开始事实。","citation_ids":["t1"]}]},{"id":"finish","blocks":[{"type":"paragraph","origin":"video","text":"结束事实。","citation_ids":["t25"]}]},{"id":"review","blocks":[{"type":"paragraph","origin":"video","text":"回顾事实。","citation_ids":["t2"]}]}]}`
	if result, err := planCall(s, "title-body", "save_article_sections", body); err != nil || result["saved_section_count"] != float64(3) {
		t.Fatal(result, err)
	}
	_, before, err := s.loadDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A reviewer may identify the wrong body claim and mention its heading in
	// the same correction, without duplicating the issue at /title.
	s.cp.EvidenceReviewIssue = map[string]any{"code": "semantic_evidence", "issues": []any{map[string]any{"path": "/sections/1/blocks/0/content/0/text", "section_id": "finish"}}}
	revised := `{"version":2,"sections":[{"id":"finish","title":"有来源的结束条件","blocks":[{"type":"paragraph","origin":"video","text":"结束事实。","citation_ids":["t25"]}]}]}`
	result, err := planCall(s, "correct-reviewed-title", "save_article_sections", revised)
	if err != nil || result["version"] != float64(3) {
		t.Fatal("reviewed heading correction was silently replaced by the old plan title", result, err)
	}
	_, after, err := s.loadDraft(context.Background())
	if err != nil || after.Plan[1].Title != "有来源的结束条件" || after.Sections["finish"].Title != "有来源的结束条件" {
		t.Fatal("corrected heading did not survive the authoritative saved draft", after, err)
	}
	// Only this explicitly reviewed display title may change; plan identity,
	// coverage, evidence, prose and every unrelated chapter remain intact.
	var normalized articleDraft
	if err = json.Unmarshal(domain.JSON(after), &normalized); err != nil {
		t.Fatal(err)
	}
	normalized.Plan[1].Title = before.Plan[1].Title
	section := normalized.Sections["finish"]
	section.Title = before.Sections["finish"].Title
	normalized.Sections["finish"] = section
	if string(domain.JSON(normalized.Plan)) != string(domain.JSON(before.Plan)) || string(domain.JSON(normalized.Sections)) != string(domain.JSON(before.Sections)) {
		t.Fatal("heading repair changed evidence, coverage or accepted prose")
	}
}
