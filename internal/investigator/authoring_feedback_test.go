package investigator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestAuthoringFigureDefaultMatchesRetainedFrameCount(t *testing.T) {
	for _, tc := range []struct {
		name, frames, layout string
	}{
		{"single", `["f1"]`, "single"},
		{"multiple", `["f1","f2","f3"]`, "sequence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"version":1,"sections":[{"id":"topic","blocks":[{"type":"figure","origin":"video","frame_ids":` + tc.frames + `,"caption":"来源中的过程变化"}]}]}`
			if err := authoringShapeIssues(raw); err != nil {
				t.Fatal("advertised input rejected", err)
			}
			var in saveSectionsInput
			if err := json.Unmarshal([]byte(normalizeAuthoringBody(raw)), &in); err != nil {
				t.Fatal(err)
			}
			b := in.Sections[0].Blocks[0]
			if b.Layout != tc.layout {
				t.Fatalf("omitted layout makes %d retained frames invalid: got %q, want %q", len(b.FrameIDs), b.Layout, tc.layout)
			}
			if b.Caption != "来源中的过程变化" || string(domain.JSON(b.FrameIDs)) != tc.frames {
				t.Fatal("normalization changed authored facts or frame order")
			}
		})
	}
}

func TestAuthoringBatchFeedbackUsesRetainedCandidatePaths(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := conceptPlanInput{Title: "两个有来源的主题", Sections: []sectionPlan{
		{ID: "one", Title: "起始", KeyPoints: []string{"起始事实"}, EvidenceIDs: []string{"t1"}},
		{ID: "two", Title: "过程", KeyPoints: []string{"过程事实"}, EvidenceIDs: []string{"t25"}},
	}, MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{
		{ID: "root", Label: "主题"},
		{ID: "group", Label: "变化", ParentID: domain.Ptr("root")},
		{ID: "a", Label: "起始", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("one")},
		{ID: "b", Label: "过程", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("two")},
	}}, Limitations: []domain.Limitation{}}
	if body, err := planCall(s, "feedback-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	raw := `{"version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"起始事实已有来源，完整保留这一段正文。","citation_ids":["t1"]}]},{"id":"two","blocks":[{"type":"figure","origin":"video","frame_ids":["f1","f2"],"citation_ids":["t25"],"caption":"画面中的过程变化","layout":"single"}]}]}`
	body, err := planCall(s, "feedback-body", "save_article_sections", raw)
	if err != nil || body["saved_section_count"] != float64(1) || body["error"] == nil {
		t.Fatal("valid chapter was not retained", body, err)
	}
	feedback := string(domain.JSON(body["error"]))
	path := "/sections/1/blocks/0/layout"
	if !strings.Contains(feedback, path) || !strings.Contains(feedback, "sequence") {
		t.Fatalf("feedback cannot repair the retained input: %s", feedback)
	}
	if value, err := jsonField([]byte(raw), path); err != nil || string(value) != `"single"` {
		t.Fatal("feedback path does not identify the original field", string(value), err)
	}
	candidate := s.cp.LastCandidate
	read, err := planCall(s, "read-layout", "read_candidate_fields", map[string]any{"candidate_id": candidate["candidate_id"], "expected_hash": candidate["expected_hash"], "paths": []string{path}})
	if err != nil || read["fields"].(map[string]any)[path] != "single" {
		t.Fatal("actual field read failed", read, err)
	}
	// Replace only the rejected block with sourced prose. The accepted chapter
	// and current version must survive both the repair and its exact replay.
	patch := authoringRepairInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Version: 2, Patches: []authoringFieldPatch{{Op: "replace", Path: "/sections/1/blocks/0", Value: domain.JSON(map[string]any{"type": "paragraph", "origin": "video", "text": "结束状态由结尾的独立来源事实支持。", "citation_ids": []string{"t25"}})}}}
	for replay := 0; replay < 2; replay++ {
		fixed, err := planCall(s, "repair-layout-block", "repair_article_sections", patch)
		if err != nil || fixed["saved_section_count"] != float64(2) || fixed["version"] != float64(3) {
			t.Fatal("field repair failed or replay wrote twice", fixed, err)
		}
	}
	_, saved, err := s.loadDraft(context.Background())
	if err != nil || domain.BlockText(saved.Sections["one"].Blocks[0]) != "起始事实已有来源，完整保留这一段正文。" {
		t.Fatal("repair changed accepted prose", err)
	}
}

func TestConceptMapFeedbackLocatesLeafAndDepthDefects(t *testing.T) {
	for _, tc := range []struct {
		name string
		tree domain.MindMap
		path string
		code string
	}{
		{"leaf", domain.MindMap{RootID: "r", Nodes: []domain.MapNode{{ID: "r", Label: "主题"}, {ID: "leaf", Label: "要点", ParentID: domain.Ptr("r")}}}, "/mind_map/nodes/1/section_id", "missing_leaf_section"},
		{"depth", domain.MindMap{RootID: "r", Nodes: []domain.MapNode{{ID: "r", Label: "主题"}, {ID: "a", Label: "一", ParentID: domain.Ptr("r")}, {ID: "b", Label: "二", ParentID: domain.Ptr("a")}, {ID: "c", Label: "三", ParentID: domain.Ptr("b")}, {ID: "d", Label: "四", ParentID: domain.Ptr("c")}, {ID: "e", Label: "五", ParentID: domain.Ptr("d"), SectionID: domain.Ptr("one")}}}, "/mind_map/nodes/5/parent_id", "depth_exceeded"},
		{"cycle", domain.MindMap{RootID: "r", Nodes: []domain.MapNode{{ID: "r", Label: "主题"}, {ID: "a", Label: "一", ParentID: domain.Ptr("b")}, {ID: "b", Label: "二", ParentID: domain.Ptr("a"), SectionID: domain.Ptr("one")}}}, "/mind_map/nodes/1/parent_id", "parent_cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sections := map[string]bool{"one": true}
			cause := validateConceptMap(&tc.tree, sections)
			if cause == nil {
				t.Fatal("fixture must fail the real publication validator")
			}
			raw := string(domain.JSON(candidateIssue(authoringConceptMapIssue(&tc.tree, sections, cause))))
			if !strings.Contains(raw, tc.path) || !strings.Contains(raw, tc.code) {
				t.Fatalf("missing actionable %s path %s: %s", tc.code, tc.path, raw)
			}
		})
	}
}
