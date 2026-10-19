package investigator

import (
	"context"
	"testing"

	"vidlens/internal/domain"
)

func TestAuthoringRepairPreservesActivityMetadataAndAcceptedChapters(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := conceptPlanInput{Title: "两个来源主题", Sections: []sectionPlan{
		{ID: "one", Title: "开始", KeyPoints: []string{"开始事实"}, EvidenceIDs: []string{"t1"}},
		{ID: "two", Title: "结束", KeyPoints: []string{"结束事实"}, EvidenceIDs: []string{"t25"}},
	}, MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{
		{ID: "root", Label: "主题"},
		{ID: "group", Label: "过程", ParentID: domain.Ptr("root")},
		{ID: "a", Label: "开始", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("one")},
		{ID: "b", Label: "结束", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("two")},
	}}, Limitations: []domain.Limitation{}}
	if body, err := planCall(s, "metadata-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	raw := `{"activity_title":"保存已核实主题","version":1,"sections":[{"id":"one","blocks":[{"type":"paragraph","origin":"video","text":"开始事实已经保存，后续纠错不能替换这段正文。","citation_ids":["t1"]}]},{"id":"two","blocks":[{"type":"figure","origin":"video","frame_ids":["f1","f2"],"caption":"需要纠正的布局","layout":"single"}]}]}`
	body, err := planCall(s, "metadata-body", "save_article_sections", raw)
	if err != nil || body["saved_section_count"] != float64(1) || body["error"] == nil {
		t.Fatal("fixture did not retain one accepted and one rejected chapter", body, err)
	}
	candidate := s.cp.LastCandidate
	patch := authoringRepairInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Version: 2,
		Patches: []authoringFieldPatch{{Op: "replace", Path: "/sections/1/blocks/0", Value: domain.JSON(map[string]any{
			"type": "paragraph", "origin": "video", "text": "结尾的独立事实由原始来源支持。", "citation_ids": []string{"t25"},
		})}}}
	for repeat := 0; repeat < 2; repeat++ {
		fixed, err := planCall(s, "metadata-repair", "repair_article_sections", patch)
		if err != nil || fixed["version"] != float64(3) || fixed["saved_section_count"] != float64(2) {
			t.Fatalf("valid field repair rejected because retained activity metadata reached body decoder: %s %v", domain.JSON(fixed), err)
		}
	}
	_, saved, err := s.loadDraft(context.Background())
	if err != nil || domain.BlockText(saved.Sections["one"].Blocks[0]) != "开始事实已经保存，后续纠错不能替换这段正文。" {
		t.Fatal("field repair changed the already accepted chapter", err)
	}
}
