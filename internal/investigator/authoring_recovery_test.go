package investigator

import (
	"context"
	"strings"
	"testing"
	"vidlens/internal/domain"
)

func TestAuthoringRepairCanRecoverMalformedAndEmptyBodies(t *testing.T) {
	for _, original := range []string{`{"version":1,"sections":[`, `{"version":1,"sections":[{"id":"one","blocks":[]}]}`} {
		s := planSession(t)
		s.cp.HarnessRevision = 13
		if _, err := s.makeTools(); err != nil {
			t.Fatal(err)
		}
		plan := conceptPlanInput{Title: "两个来源事实", Sections: []sectionPlan{
			{ID: "one", Title: "起始", KeyPoints: []string{"起始事实"}, EvidenceIDs: []string{"t1"}},
			{ID: "two", Title: "结束", KeyPoints: []string{"结束事实"}, EvidenceIDs: []string{"t25"}},
		}, MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{
			{ID: "root", Label: "主题"}, {ID: "group", Label: "过程", ParentID: domain.Ptr("root")},
			{ID: "a", Label: "起始", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("one")},
			{ID: "b", Label: "结束", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("two")},
		}}, Limitations: []domain.Limitation{}}
		if body, err := planCall(s, "recovery-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
			t.Fatal(body, err)
		}
		if body, err := planCall(s, "unpatchable", "save_article_sections", original); err != nil || body["error"] == nil {
			t.Fatal("invalid body not retained", body, err)
		}
		candidate := s.cp.LastCandidate
		for _, id := range []string{"recovery-read-1", "recovery-read-2"} {
			if _, err := planCall(s, id, "read_candidate_fields", map[string]any{"candidate_id": candidate["candidate_id"], "expected_hash": candidate["expected_hash"], "paths": []string{"/sections/0"}}); err != nil {
				t.Fatal(err)
			}
		}
		if s.requiredRepairTool() != "repair_article_sections" {
			t.Fatal("forced repair guard was not reached", s.cp.Progress)
		}
		replacement := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"有来源的正文","citation_ids":["t1"]}]}]}`
		args := map[string]any{"candidate_id": candidate["candidate_id"], "expected_hash": candidate["expected_hash"], "version": 1, "replacement_json": replacement}
		for replay := 0; replay < 2; replay++ {
			body, err := planCall(s, "recover-body", "repair_article_sections", args)
			if err != nil || body["saved_section_count"] != float64(1) || body["version"] != float64(2) {
				t.Fatalf("unpatchable candidate has no executable recovery: %s %v", domain.JSON(body), err)
			}
		}
		if s.cp.Progress.Stalled != 0 {
			t.Fatal("successful recovery did not record actual progress", s.cp.Progress)
		}
		var repairs int
		if err := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='field_repair'", s.claim.Run.ID).Scan(&repairs); err != nil || repairs != 1 {
			t.Fatal("replay spent another repair", repairs, err)
		}
	}
}

func TestAuthoringReplacementRemainsBoundedAndSpecificToBrokenContainers(t *testing.T) {
	broken := `{"version":1,"sections":[`
	good := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"source","citation_ids":["t1"]}]}]}`
	for _, tc := range []struct {
		name, original, replacement string
		patches                     []authoringFieldPatch
	}{
		{name: "complete_original", original: good, replacement: good},
		{name: "invalid_replacement", original: broken, replacement: broken},
		{name: "array_replacement", original: broken, replacement: `[]`},
		{name: "oversized", original: broken, replacement: `{"extra":"` + strings.Repeat("x", 64<<10) + `"}`},
		{name: "mixed_modes", original: broken, replacement: good, patches: []authoringFieldPatch{{Op: "replace", Path: "/sections/0/blocks/0/text", Value: domain.JSON("text")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := repairAuthoringBody(tc.original, authoringRepairInput{ReplacementJSON: &tc.replacement, Patches: tc.patches}); err == nil {
				t.Fatal("unsafe or unbounded batch replacement accepted")
			}
		})
	}
}

func TestAuthoringReplacementPreservesAcceptedChaptersAndRequiresCandidateIdentity(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := conceptPlanInput{Title: "来源事实", Sections: []sectionPlan{{ID: "one", Title: "起始", KeyPoints: []string{"起始"}, EvidenceIDs: []string{"t1"}}, {ID: "two", Title: "结束", KeyPoints: []string{"结束"}, EvidenceIDs: []string{"t25"}}}, MindMap: &domain.MindMap{RootID: "r", Nodes: []domain.MapNode{{ID: "r", Label: "主题"}, {ID: "g", Label: "过程", ParentID: domain.Ptr("r")}, {ID: "a", Label: "起始", ParentID: domain.Ptr("g"), SectionID: domain.Ptr("one")}, {ID: "b", Label: "结束", ParentID: domain.Ptr("g"), SectionID: domain.Ptr("two")}}}, Limitations: []domain.Limitation{}}
	if body, err := planCall(s, "preserved-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	accepted := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"原已接受正文","citation_ids":["t1"]}]}]}`
	if body, err := planCall(s, "accepted-body", "save_article_sections", accepted); err != nil || body["version"] != float64(2) {
		t.Fatal(body, err)
	}
	bad := `{"version":2,"sections":[{"id":"two","blocks":[]}]}`
	if body, err := planCall(s, "empty-body", "save_article_sections", bad); err != nil || body["error"] == nil {
		t.Fatal(body, err)
	}
	candidate := s.cp.LastCandidate
	replacement := `{"version":2,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"试图覆盖旧内容","citation_ids":["t1"]}]},{"id":"two","blocks":[{"type":"paragraph","origin":"video","text":"结束事实","citation_ids":["t25"]}]}]}`
	for _, hash := range []string{"wrong", candidate["expected_hash"].(string)} {
		body, err := planCall(s, "replace-"+hash, "repair_article_sections", map[string]any{"candidate_id": candidate["candidate_id"], "expected_hash": hash, "version": 2, "replacement_json": replacement})
		if err != nil {
			t.Fatal(err)
		}
		if hash == "wrong" && body["error"] == nil {
			t.Fatal("wrong candidate hash accepted")
		}
		if hash != "wrong" && body["saved_section_count"] != float64(2) {
			t.Fatal("pending section not recovered", body)
		}
	}
	_, draft, err := s.loadDraft(context.Background())
	if err != nil || domain.BlockText(draft.Sections["one"].Blocks[0]) != "原已接受正文" {
		t.Fatal("replacement changed an accepted chapter", err)
	}
}
