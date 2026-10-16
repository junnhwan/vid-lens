package investigator

import (
	"testing"
	"vidlens/internal/domain"
)

func TestTopicPlanCanExplainOneRangeTwiceWithoutDroppingCoverage(t *testing.T) {
	plan := []sectionPlan{{ID: "parameters", Title: "参数", StartMS: 0, EndMS: 15000}, {ID: "transaction", Title: "事务", StartMS: 15000, EndMS: 30000}, {ID: "queue", Title: "排队", StartMS: 30000, EndMS: 45000}, {ID: "diagnose", Title: "排障", StartMS: 30000, EndMS: 45000}}
	if err := validateSectionPlan(plan, 45000, true); err != nil {
		t.Fatal("real detailed outline rejected despite full source coverage", err)
	}
	if err := validateSectionPlan(plan, 45000, false); err == nil {
		t.Fatal("historical disjoint protocol changed")
	}
	plan[2].StartMS = 31000
	if err := validateSectionPlan(plan, 45000, true); err == nil {
		t.Fatal("a gap in the full-video plan was accepted")
	}
	plan[2].StartMS = 30000
	plan[3].ID = "queue"
	if err := validateSectionPlan(plan, 45000, true); err == nil {
		t.Fatal("duplicate topic id accepted")
	}
}

func TestContentPlanKeepsPointsDistinctAndCitationsInRange(t *testing.T) {
	plan := []sectionPlan{{ID: "first", Title: "主题", StartMS: 0, EndMS: 1000, KeyPoints: []string{"关键事实与条件"}, EvidenceIDs: []string{"t1"}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text", Range: &domain.Range{StartMS: 0, EndMS: 1000}}}
	if err := validatePlanFacts(plan, catalog); err != nil {
		t.Fatal(err)
	}
	plan[0].EvidenceIDs = []string{"invented"}
	if err := validatePlanFacts(plan, catalog); err == nil {
		t.Fatal("invented content-plan evidence accepted")
	}
	plan[0].EvidenceIDs = []string{"t1"}
	plan[0].StartMS = 2000
	plan[0].EndMS = 3000
	if err := validatePlanFacts(plan, catalog); err == nil {
		t.Fatal("out-of-range plan evidence accepted")
	}
	plan[0].StartMS = 0
	plan[0].EndMS = 1000
	plan = append(plan, plan[0])
	plan[1].ID = "second"
	if err := validatePlanFacts(plan, catalog); err == nil {
		t.Fatal("duplicate planned knowledge counted as new content")
	}
}
