package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"vidlens/internal/domain"
)

func TestEfficientSchemaMatchesAcceptedMetadataAndBodyTypes(t *testing.T) {
	p, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &p}, cp: checkpoint{HarnessRevision: 7, LayoutVersion: draftLayout}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"begin_article", "save_article_sections", "repair_article_plan"} {
		info, err := s.tools[name].Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		sc, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		raw := string(domain.JSON(sc))
		if !strings.Contains(raw, `"activity_title"`) {
			t.Fatalf("metadata missing: %s", name)
		}
		if name == "repair_article_plan" && strings.Contains(raw, "replacement_json") {
			t.Fatal("advertised an unsupported repair")
		}
		if name == "save_article_sections" && (!strings.Contains(raw, `"maxItems":12`) || !strings.Contains(raw, `"enum":["paragraph","quote","list","code","table","figure"]`)) {
			t.Fatal("body contract unbounded or untyped", raw)
		}
	}
}

func TestFlexibleBatchRetainsValidChaptersAndScopesCorrectionsDurably(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 7
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := badPlan()
	plan.Sections[0].EvidenceIDs = []string{"t1"}
	plan.Sections[1].EvidenceIDs = []string{"t25"}
	raw := map[string]any{}
	_ = json.Unmarshal(domain.JSON(plan), &raw)
	raw["activity_title"] = "组织两个主题"
	// Exhaust the plan's scoped corrections, then successfully save the plan.
	for n := 0; n < 4; n++ {
		bad := plan
		bad.Title = ""
		var input any = bad
		if n == 3 {
			input = raw
		}
		if body, err := planCall(s, fmt.Sprint("plan", n), "begin_article", input); err != nil || n == 3 && body["version"] != float64(1) {
			t.Fatal(err)
		}
	}
	sec := func(id, kind, ref string) domain.Section {
		return domain.Section{ID: id, Blocks: []domain.Block{{Type: kind, Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源中已经说明的事实。"}, {Type: "citation", CitationID: ref}}}}}
	}
	batch := saveSectionsInput{Version: 1, Sections: []domain.Section{sec("one", "paragraph", "t1"), sec("two", "note", "t25")}}
	body, err := planCall(s, "mixed", "save_article_sections", batch)
	if err != nil || body["version"] != float64(2) || body["saved_section_count"] != float64(1) || body["error"] == nil {
		t.Fatalf("partial batch lost progress: %s %v", domain.JSON(body), err)
	}
	replay, err := planCall(s, "mixed", "save_article_sections", batch)
	if err != nil || replay["version"] != body["version"] || replay["error"] == nil {
		t.Fatal("replay lost the durable receipt", err)
	}
	body, err = planCall(s, "fixed", "save_article_sections", saveSectionsInput{Version: 2, Sections: []domain.Section{sec("two", "paragraph", "t25")}})
	if err != nil || body["saved_section_count"] != float64(2) || body["error"] != nil {
		t.Fatalf("new chapter inherited planning errors: %s %v", domain.JSON(body), err)
	}
	// Replay after version advancement cannot grant another correction or write.
	if _, err = planCall(s, "fixed", "save_article_sections", saveSectionsInput{Version: 2, Sections: []domain.Section{sec("two", "paragraph", "t25")}}); err != nil {
		t.Fatal(err)
	}
}

func TestFlexibleBatchAcceptsFiveChaptersAndLegacyReplayKeepsExactBody(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 7
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := beginArticleInput{Title: "五个独立主题"}
	sections := []domain.Section{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprint("topic", i)
		plan.Sections = append(plan.Sections, sectionPlan{ID: id, Title: id, StartMS: 0, EndMS: 2000, KeyPoints: []string{id}, EvidenceIDs: []string{"t1"}})
		sections = append(sections, domain.Section{ID: id, Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "这个主题在来源中有对应事实。"}, {Type: "citation", CitationID: "t1"}}}}})
	}
	if body, err := planCall(s, "plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	in := saveSectionsInput{Version: 1, Sections: sections}
	if body, err := planCall(s, "five", "save_article_sections", in); err != nil || body["saved_section_count"] != float64(5) {
		t.Fatal("five chapters rejected", body, err)
	}
	// Simulate a pre-upgrade accepted write whose hash included assigned IDs.
	s.cp.HarnessRevision = 5
	canonical := saveSectionsInput{}
	_ = json.Unmarshal(domain.JSON(in), &canonical)
	for i := range canonical.Sections {
		canonical.Sections[i].Blocks[0].ID = canonical.Sections[i].ID + "_b1"
	}
	draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var data articleDraft
	_ = json.Unmarshal(draft.Data, &data)
	if _, err = s.engine.Store.SaveArticleDraft(context.Background(), s.claim, "legacy", canonical, 2, data); err != nil {
		t.Fatal(err)
	}
	if _, err = planCall(s, "legacy", "save_article_sections", in); err != nil {
		t.Fatal("old canonical-argument hash cannot replay", err)
	}
	changed := saveSectionsInput{}
	_ = json.Unmarshal(domain.JSON(in), &changed)
	changed.Sections[0].Blocks[0].Content[0].Text = "different body"
	if _, err = planCall(s, "legacy", "save_article_sections", changed); err == nil {
		t.Fatal("legacy compatibility accepted different text")
	}
}
