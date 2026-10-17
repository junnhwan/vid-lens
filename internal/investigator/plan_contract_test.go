package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"vidlens/internal/domain"
)

func TestPlanRepairAcceptsReportedEvidenceItemPath(t *testing.T) {
	input := beginArticleInput{Title: "完整计划", Sections: []sectionPlan{{
		ID: "one", Title: "原章", StartMS: 0, EndMS: 1000,
		KeyPoints: []string{"保留事实"}, EvidenceIDs: []string{"t1", "t2"},
	}}}
	raw, err := replaceFields(domain.JSON(input), []fieldPatch{{Op: "replace", Path: "/sections/0/evidence_ids/0", Value: domain.JSON("t3")}}, planRepairAllowed)
	var repaired beginArticleInput
	if err != nil || json.Unmarshal(raw, &repaired) != nil {
		t.Fatalf("validator-reported evidence item path rejected: %v", err)
	}
	if !slices.Equal(repaired.Sections[0].EvidenceIDs, []string{"t3", "t2"}) || !slices.Equal(repaired.Sections[0].KeyPoints, input.Sections[0].KeyPoints) {
		t.Fatal("single reference repair changed other references or supported points")
	}
	for _, path := range []string{"/sections/0/evidence_ids/2", "/sections/0/evidence_ids/-", "/sections/0/evidence_ids/0/id", "/sections/0/id", "/sections"} {
		if _, err = replaceFields(domain.JSON(input), []fieldPatch{{Op: "replace", Path: path, Value: domain.JSON("t3")}}, planRepairAllowed); err == nil {
			t.Fatalf("structural or nonexistent reference path accepted: %s", path)
		}
	}
}

func TestContentPlanSchemaRequiresBoundedPointsAndEvidence(t *testing.T) {
	policy, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &policy}, cp: checkpoint{HarnessRevision: 3, LayoutVersion: draftLayout}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	info, err := s.tools["begin_article"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(domain.JSON(sc), &raw); err != nil {
		t.Fatal(err)
	}
	sections := raw["properties"].(map[string]any)["sections"].(map[string]any)
	if sections["minItems"] != float64(1) || sections["maxItems"] != float64(40) {
		t.Errorf("actual transmitted chapter schema has no 1..40 bound: %s", domain.JSON(sections))
	}
	item := sections["items"].(map[string]any)
	required := []string{}
	for _, name := range item["required"].([]any) {
		required = append(required, name.(string))
	}
	for name, maximum := range map[string]float64{"key_points": 12, "evidence_ids": 24} {
		field := item["properties"].(map[string]any)[name].(map[string]any)
		if !slices.Contains(required, name) || field["minItems"] != float64(1) || field["maxItems"] != maximum {
			t.Errorf("%s is not required and bounded in the actual tool schema: %s", name, domain.JSON(field))
		}
	}
}

func TestExistingContentPlanProtocolKeepsItsOriginalToolContract(t *testing.T) {
	policy, _ := domain.InterpretationPolicy("standard")
	s := &session{spec: domain.GenerationSpec{DetailPolicy: &policy}, cp: checkpoint{HarnessRevision: 2, LayoutVersion: draftLayout}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	if s.tools["repair_article_plan"] != nil {
		t.Fatal("old checkpoint silently upgraded tools")
	}
	info, err := s.tools["begin_article"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Desc != "Finish investigation and persist a full-video article outline. No giant article JSON. Save body chapters in bounded batches with save_article_sections, then submit_article_draft with its version." {
		t.Fatal("old description changed")
	}
	sc, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(domain.JSON(sc), &raw); err != nil {
		t.Fatal(err)
	}
	sections := raw["properties"].(map[string]any)["sections"].(map[string]any)
	if sections["maxItems"] != nil {
		t.Fatal("new bounds changed an existing protocol schema")
	}
}

func TestPlanFeedbackReportsCountAndRangeProblemsTogether(t *testing.T) {
	refs := []string{}
	catalog := map[string]domain.Citation{}
	for i := 1; i <= 25; i++ {
		id := fmt.Sprint("t", i)
		refs = append(refs, id)
		catalog[id] = domain.Citation{Kind: "video_text", Range: &domain.Range{StartMS: 0, EndMS: 1000}}
	}
	catalog["f1"] = domain.Citation{Kind: "video_frame", TimeMS: domain.Ptr(int64(1500))}
	plan := []sectionPlan{
		{ID: "one", Title: "第一章", StartMS: 0, EndMS: 1000, KeyPoints: []string{"第一个不同事实"}, EvidenceIDs: refs},
		{ID: "two", Title: "第二章", StartMS: 1000, EndMS: 2000, KeyPoints: []string{"第二个不同事实"}, EvidenceIDs: []string{"t1", "missing"}},
	}
	issue := candidateIssue(validatePlanFacts(plan, catalog))
	raw := domain.JSON(issue)
	var feedback struct {
		Issues []struct {
			Code   string `json:"code"`
			Path   string `json:"path"`
			Actual any    `json:"actual"`
		} `json:"issues"`
		IssueCount int `json:"issue_count"`
	}
	if err := json.Unmarshal(raw, &feedback); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, p := range feedback.Issues {
		found[p.Code+":"+p.Path] = true
	}
	for _, key := range []string{"evidence_count_exceeded:/sections/0/evidence_ids", "evidence_out_of_range:/sections/1/evidence_ids/0", "unknown_evidence:/sections/1/evidence_ids/1"} {
		if !found[key] {
			t.Errorf("missing actionable issue %s in %s", key, raw)
		}
	}
	if feedback.IssueCount < 3 {
		t.Errorf("hidden issues are not counted: %s", raw)
	}
}

func TestPlanEvidenceCountBoundaries(t *testing.T) {
	for _, count := range []int{0, 24, 25} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			refs := []string{}
			catalog := map[string]domain.Citation{}
			for i := 1; i <= count; i++ {
				id := fmt.Sprint("t", i)
				refs = append(refs, id)
				catalog[id] = domain.Citation{Range: &domain.Range{StartMS: 0, EndMS: 1000}}
			}
			err := validatePlanFacts([]sectionPlan{{ID: "one", Title: "唯一主题", StartMS: 0, EndMS: 1000, KeyPoints: []string{"有支持的事实"}, EvidenceIDs: refs}}, catalog)
			if count == 24 {
				if err != nil {
					t.Fatal("valid upper boundary rejected", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid reference count accepted")
			}
		})
	}
}
