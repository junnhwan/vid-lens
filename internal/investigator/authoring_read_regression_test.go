package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func sectionReadSession(t *testing.T, bounded bool) *session {
	t.Helper()
	return sectionReadSessionWithCount(t, bounded, 7)
}

func sectionReadSessionWithCount(t *testing.T, bounded bool, count int) *session {
	t.Helper()
	s := planSession(t)
	s.cp.HarnessRevision = 16
	if bounded {
		s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	}
	draft := articleDraft{Title: "已保存的修复正文", Sections: map[string]domain.Section{}, Citations: map[string]domain.Citation{}}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("topic-%d", i+1)
		draft.Plan = append(draft.Plan, sectionPlan{ID: id, Title: id})
		draft.Sections[id] = domain.Section{ID: id, Title: id, Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "原始已保存正文"}}}}}
	}
	draft.Plan = append(draft.Plan, sectionPlan{ID: "not-written", Title: "尚未保存"})
	if _, err := s.engine.Store.SaveArticleDraft(context.Background(), s.claim, "seed-read-draft", draft, 0, draft); err != nil {
		t.Fatal(err)
	}
	s.cp.Composing = true
	s.cp.DraftIssue = map[string]any{"code": "semantic_evidence", "candidate_hash": "original-review", "affected_section_ids": []string{"topic-1"}}
	s.cp.EvidenceReviewIssue = s.cp.DraftIssue
	s.cp.DraftCorrection = true
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestV2ArticleSectionReadPaginatesAffectedChapters(t *testing.T) {
	s := sectionReadSession(t, true)
	ids := []string{"topic-7", "topic-2", "topic-6", "topic-1", "topic-5", "topic-3", "topic-4"}
	collected := []string{}
	for page, offset := range []int{0, 3, 6} {
		args := map[string]any{"section_ids": ids}
		if offset > 0 {
			args["offset"] = offset
		}
		body, err := planCall(s, fmt.Sprintf("read-page-%d", page), "read_article_sections", args)
		if err != nil || body["error"] != nil || body["version"] != float64(1) {
			t.Fatalf("valid long affected-section list was rejected: %s %v", domain.JSON(body), err)
		}
		sections := body["sections"].([]any)
		if len(sections) != min(3, len(ids)-offset) {
			t.Fatalf("read did not bound one page: %s", domain.JSON(body))
		}
		for _, section := range sections {
			collected = append(collected, section.(map[string]any)["id"].(string))
		}
		if offset+len(sections) < len(ids) {
			if body["next_offset"] != float64(offset+len(sections)) || !strings.Contains(fmt.Sprint(body["next_action"]), "offset") {
				t.Fatalf("next page has no actionable cursor: %s", domain.JSON(body))
			}
		} else if body["next_offset"] != nil {
			t.Fatalf("finished read still requests a page: %s", domain.JSON(body))
		}
	}
	if string(domain.JSON(collected)) != string(domain.JSON(ids)) || s.cp.DraftIssue["candidate_hash"] != "original-review" || !s.cp.DraftCorrection {
		t.Fatalf("pagination skipped/reordered chapters or overwrote repair issue: %v %#v", collected, s.cp.DraftIssue)
	}
}

func TestV2ArticleSectionReadReportsMissingIDsWithoutReplacingRepair(t *testing.T) {
	tooMany := make([]string, maxPlanSections+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("topic-%d", i+1)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		code string
	}{
		{"unknown", map[string]any{"section_ids": []string{"s8"}}, "section_not_saved"},
		{"mixed", map[string]any{"section_ids": []string{"topic-1", "s8"}}, "section_not_saved"},
		{"pending", map[string]any{"section_ids": []string{"not-written"}}, "section_not_saved"},
		{"empty", map[string]any{"section_ids": []string{}}, "invalid_section_read"},
		{"empty-object", map[string]any{}, "invalid_section_read"},
		{"null", nil, "invalid_section_read"},
		{"too-many", map[string]any{"section_ids": tooMany}, "invalid_section_read"},
		{"offset-end", map[string]any{"section_ids": []string{"topic-1"}, "offset": 1}, "invalid_section_read"},
		{"offset", map[string]any{"section_ids": []string{"topic-1"}, "offset": 2}, "invalid_section_read"},
		{"negative-offset", map[string]any{"section_ids": []string{"topic-1"}, "offset": -1}, "invalid_section_read"},
		{"wrong-offset-type", map[string]any{"section_ids": []string{"topic-1"}, "offset": "next"}, "invalid_section_read"},
		{"wrong-id-type", map[string]any{"section_ids": "topic-1"}, "invalid_section_read"},
		{"unknown-field", map[string]any{"section_ids": []string{"topic-1"}, "section_index": 0}, "invalid_section_read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sectionReadSession(t, true)
			before := string(domain.JSON(s.cp.DraftIssue))
			body, err := planCall(s, "read-invalid-"+tc.name, "read_article_sections", tc.args)
			if err != nil {
				t.Fatalf("read error incorrectly ended the author repair: %v", err)
			}
			issue, ok := body["error"].(map[string]any)
			if !ok || issue["code"] != tc.code || issue["category"] != "tool_read" || len(body["valid_section_ids"].([]any)) != 7 || !strings.Contains(fmt.Sprint(body["next_action"]), "section_ids") {
				t.Fatalf("read error has no real IDs/correct action: %s", domain.JSON(body))
			}
			if len(body["sections"].([]any)) != 0 {
				t.Fatalf("invalid read returned a silently partial body: %s", domain.JSON(body))
			}
			if tc.code == "section_not_saved" && len(body["missing_section_ids"].([]any)) != 1 {
				t.Fatalf("missing chapter silently disappeared: %s", domain.JSON(body))
			}
			if string(domain.JSON(s.cp.DraftIssue)) != before || string(domain.JSON(s.pendingEvidenceReview())) != before || !s.cp.DraftCorrection {
				t.Fatalf("read request error overwrote the actual body repair: %#v", s.cp.DraftIssue)
			}
			if used, err := s.correctionUsage(context.Background()); err != nil || used != 0 {
				t.Fatal("read consumed a body correction", used, err)
			}
		})
	}
}

func TestV1ArticleSectionReadContractStaysFrozen(t *testing.T) {
	s := sectionReadSession(t, false)
	info, err := s.tools["read_article_sections"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || strings.Contains(string(domain.JSON(sc)), "offset") || info.Desc != "Read 1-3 saved chapters for a local correction, without regenerating other chapters." {
		t.Fatal("legacy frozen read contract changed", info, err)
	}
	body, err := planCall(s, "legacy-unknown-read", "read_article_sections", map[string]any{"section_ids": []string{"s8"}})
	if err != nil || body["error"] != nil || len(body["sections"].([]any)) != 0 || body["valid_section_ids"] != nil {
		t.Fatal("legacy tool behavior changed", body, err)
	}
}

func TestV2ArticleSectionPagesDeliverNewInformationWithoutHidingRepeatReads(t *testing.T) {
	s := sectionReadSessionWithCount(t, true, 19)
	ids := []string{}
	for i := 1; i <= 19; i++ {
		ids = append(ids, fmt.Sprintf("topic-%d", i))
	}
	for offset := 0; offset < len(ids); offset += 3 {
		body, err := planCall(s, fmt.Sprintf("many-read-page-%d", offset), "read_article_sections", map[string]any{"section_ids": ids, "offset": offset})
		if err != nil || body["error"] != nil || s.cp.Progress.Stalled != 0 {
			t.Fatalf("new page was treated as a stalled repair: offset=%d stalled=%d result=%s error=%v", offset, s.cp.Progress.Stalled, domain.JSON(body), err)
		}
	}
	// Merely changing the list/cursor must not turn the same body into new
	// information. The loop detector receives the actual returned sections.
	if body, err := planCall(s, "repeat-first-page", "read_article_sections", map[string]any{"section_ids": ids[:3]}); err != nil || body["error"] != nil || s.cp.Progress.Stalled != 1 {
		t.Fatalf("identical delivered chapters reset the loop guard: stalled=%d result=%s error=%v", s.cp.Progress.Stalled, domain.JSON(body), err)
	}
	version, _, err := s.loadDraft(context.Background())
	if err != nil || version != 1 || s.cp.DraftIssue["candidate_hash"] != "original-review" {
		t.Fatal("reading altered the candidate revision or review", version, s.cp.DraftIssue, err)
	}
}

func TestV2ArticleSectionReadMissingArgumentsKeepsContentIssue(t *testing.T) {
	s := sectionReadSession(t, true)
	before := string(domain.JSON(s.cp.DraftIssue))
	result, err := s.tools["read_article_sections"].InvokableRun(context.WithValue(context.Background(), callIDKey{}, "read-nil-args"), nil)
	if err != nil || result == nil || len(result.Parts) != 1 {
		t.Fatalf("missing arguments ended the author run: %v %#v", err, result)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &body); err != nil {
		t.Fatal(err)
	}
	issue, ok := body["error"].(map[string]any)
	if !ok || issue["category"] != "tool_read" || issue["code"] != "invalid_section_read" || len(body["valid_section_ids"].([]any)) != 7 || string(domain.JSON(s.cp.DraftIssue)) != before {
		t.Fatalf("missing read parameters lost actual IDs or content issue: %s", domain.JSON(body))
	}
}
