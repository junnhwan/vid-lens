package investigator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

// The live failures used the authoring schema, which intentionally has no
// chapter timestamps. A closing topic can refer back to earlier source facts.
func TestAuthoringPlanAcceptsSourceCoverageAcrossThematicChapterOrder(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	in := thematicPlanFixture()
	raw := authoringPlanArguments(t, in)
	body, err := planCall(s, "thematic-plan", "begin_article", raw)
	if err != nil || body["version"] != float64(1) {
		t.Fatalf("source-complete thematic outline cannot be saved: %s %v", domain.JSON(body), err)
	}
	_, saved, err := s.loadDraft(context.Background())
	if err != nil || len(saved.Plan) != 3 || saved.Plan[2].ID != "review" || saved.Plan[2].EvidenceIDs[0] != "t2" {
		t.Fatal("acceptance changed authored chapter order or evidence", err)
	}
	// Existing call identity must replay the accepted plan rather than writing
	// a second version or consuming correction budget.
	body, err = planCall(s, "thematic-plan", "begin_article", raw)
	if err != nil || body["version"] != float64(1) {
		t.Fatal("accepted outline replay changed its version", body, err)
	}
}

func thematicPlanFixture() conceptPlanInput {
	return conceptPlanInput{
		Title: "有来源的过程与回顾",
		Sections: []sectionPlan{
			{ID: "start", Title: "开始", KeyPoints: []string{"开始时的事实"}, EvidenceIDs: []string{"t1"}},
			{ID: "finish", Title: "结束", KeyPoints: []string{"结尾的变化"}, EvidenceIDs: []string{"t25"}},
			{ID: "review", Title: "回顾", KeyPoints: []string{"回顾开头的限制"}, EvidenceIDs: []string{"t2"}},
		},
		MindMap: &domain.MindMap{RootID: "root", Nodes: []domain.MapNode{
			{ID: "root", Label: "过程"},
			{ID: "group", Label: "主题", ParentID: domain.Ptr("root")},
			{ID: "a", Label: "开始", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("start")},
			{ID: "b", Label: "结束", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("finish")},
			{ID: "c", Label: "回顾", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("review")},
		}},
		Limitations: []domain.Limitation{},
	}
}

func authoringPlanArguments(t *testing.T, in conceptPlanInput) string {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(domain.JSON(in), &fields); err != nil {
		t.Fatal(err)
	}
	for _, value := range fields["sections"].([]any) {
		section := value.(map[string]any)
		delete(section, "start_ms")
		delete(section, "end_ms")
	}
	raw := string(domain.JSON(fields))
	if strings.Contains(raw, "start_ms") || strings.Contains(raw, "end_ms") {
		t.Fatal("fixture supplied timestamps omitted from the authoring schema")
	}
	return raw
}

func TestAuthoringPlanDerivedRangeFeedbackNamesEditableFields(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	in := thematicPlanFixture()
	in.Sections[1].EvidenceIDs = []string{"missing"}
	raw := authoringPlanArguments(t, in)
	body, err := planCall(s, "missing-coverage", "begin_article", raw)
	if err != nil || body["error"] == nil {
		t.Fatal("missing source evidence was not rejected", body, err)
	}
	var feedback struct {
		Issues []struct {
			Path string `json:"path"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(domain.JSON(body["error"]), &feedback); err != nil {
		t.Fatal(err)
	}
	if len(feedback.Issues) == 0 || feedback.Issues[0].Path != "/sections/1/evidence_ids" {
		t.Fatalf("derived range feedback cannot be repaired through the advertised schema: %s", domain.JSON(body))
	}
	if _, err := jsonField([]byte(raw), feedback.Issues[0].Path); err != nil {
		t.Fatal("feedback names a field absent from retained arguments", err)
	}
	candidate := s.cp.LastCandidate
	patch := candidatePatchInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Patches: []fieldPatch{{Op: "replace", Path: feedback.Issues[0].Path, Value: domain.JSON([]string{"t25"})}}}
	body, err = planCall(s, "correct-evidence", "repair_article_plan", patch)
	if err != nil || body["version"] != float64(1) || s.cp.DraftIssue != nil || s.cp.Progress.Stalled != 0 {
		t.Fatalf("advertised field repair cannot advance the retained plan: %s %v", domain.JSON(body), err)
	}
	before := s.cp.Progress.Total
	body, err = planCall(s, "correct-evidence", "repair_article_plan", patch)
	if err != nil || body["version"] != float64(1) || s.cp.Progress.Total != before {
		t.Fatal("repair replay consumed more work or created another draft", body, err)
	}
}

func TestAuthoringPlanStillRejectsGapsAndInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		plan []sectionPlan
	}{
		{"gap", []sectionPlan{{ID: "a", Title: "a", StartMS: 0, EndMS: 400}, {ID: "b", Title: "b", StartMS: 600, EndMS: 1000}}},
		{"missing_end", []sectionPlan{{ID: "a", Title: "a", StartMS: 0, EndMS: 900}}},
		{"outside_source", []sectionPlan{{ID: "a", Title: "a", StartMS: 0, EndMS: 1001}}},
		{"duplicate_id", []sectionPlan{{ID: "a", Title: "a", StartMS: 0, EndMS: 1000}, {ID: "a", Title: "b", StartMS: 0, EndMS: 1000}}},
		{"empty_id", []sectionPlan{{Title: "a", StartMS: 0, EndMS: 1000}}},
		{"empty_title", []sectionPlan{{ID: "a", StartMS: 0, EndMS: 1000}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := string(domain.JSON(tc.plan))
			if err := validateAuthoringPlan(tc.plan, 1000); err == nil {
				t.Fatal("invalid plan escaped final structural coverage checks")
			}
			if string(domain.JSON(tc.plan)) != before {
				t.Fatal("validation mutated the author's chapter order or evidence")
			}
		})
	}
}
