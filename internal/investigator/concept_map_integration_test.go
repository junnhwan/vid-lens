package investigator

import (
	"context"
	"encoding/json"
	"testing"
	"vidlens/internal/domain"
)

func TestConceptPlanAndExactReplaySurviveChapterBatches(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 9
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	m := &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{{ID: "root", Label: "事实之间的联系"}, {ID: "theme", Label: "两个来源维度", ParentID: domain.Ptr("root")}, {ID: "first", Label: "起始条件", ParentID: domain.Ptr("theme"), SectionID: domain.Ptr("one")}, {ID: "second", Label: "结束状态", ParentID: domain.Ptr("theme"), SectionID: domain.Ptr("two")}}}
	input := conceptPlanInput{Title: "来源事实", Sections: []sectionPlan{{ID: "one", Title: "第一章", StartMS: 0, EndMS: 1000, KeyPoints: []string{"开头的事实"}, EvidenceIDs: []string{"t1"}}, {ID: "two", Title: "第二章", StartMS: 1000, EndMS: 2000, KeyPoints: []string{"结尾的独立事实"}, EvidenceIDs: []string{"t25"}}}, MindMap: m, Limitations: []domain.Limitation{}}
	if result, err := planCall(s, "plan", "begin_article", input); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "plan", "begin_article", input); err != nil || result["version"] != float64(1) {
		t.Fatal("exact plan replay lost", result, err)
	}
	section := domain.Section{ID: "one", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "开头提供了来源事实。"}, {Type: "citation", CitationID: "t1"}}}}}
	if result, err := planCall(s, "batch", "save_article_sections", saveSectionsInput{Version: 1, Sections: []domain.Section{section}}); err != nil || result["saved_section_count"] != float64(1) {
		t.Fatal(result, err)
	}
	// Reload the durable JSON, as a new worker does, before assembling output.
	saved, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var draft articleDraft
	if err = json.Unmarshal(saved.Data, &draft); err != nil {
		t.Fatal(err)
	}
	if string(domain.JSON(draft.MindMap)) != string(domain.JSON(m)) {
		t.Fatal("chapter batch replaced the persisted concept tree")
	}
	draft.Sections["two"] = domain.Section{ID: "two", Title: "第二章"}
	article, err := s.assembleArticle(draft)
	if err != nil || string(domain.JSON(article.MindMap)) != string(domain.JSON(m)) {
		t.Fatal("publication flattened the concept tree", err)
	}
}
