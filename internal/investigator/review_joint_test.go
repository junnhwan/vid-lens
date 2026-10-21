package investigator

import (
	"slices"
	"strings"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestJointReviewIncludesOriginalSpeechAtCitedFrameTime(t *testing.T) {
	s := indexedFixture()
	s.cp.ExecutionContract.Version = domain.CurrentExecutionContract
	task := reviewTask{Mode: "multimodal", Units: []reviewUnit{{ID: "u1", FrameIDs: []string{"late-frame"}, Value: map[string]any{"caption": "后半段演示", "citation_ids": []string{"f1"}}}}}
	catalog := map[string]domain.Citation{"f1": {Kind: "video_frame", FrameID: "late-frame", TimeMS: domain.Ptr(int64(65*45000 + 1))}}
	source, err := s.indexedReviewSource(task, catalog)
	if err != nil {
		t.Fatal(err)
	}
	originals := string(domain.JSON(source["transcript"]))
	for _, text := range []string{"完整原文条件64", "完整原文条件65", "完整原文条件66"} {
		if !strings.Contains(originals, text) {
			t.Fatalf("frame review omitted original speech at its own time: %s", text)
		}
	}
}

func TestCurrentReviewDeliversMixedLimitationOnceWithAllEvidence(t *testing.T) {
	// The CMU failure: a single limitation cites speech and a black GUI frame.
	// A text-only pass cannot assess the indivisible visible-content assertion.
	candidate := map[string]any{"limitations": []any{map[string]any{
		"code": "visual_unclear", "message": "演示画面为黑色；讲者称这是2024年的GUI演示，无法核对具体页面。",
		"citation_ids": []string{"f1", "t1"},
	}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}, "f1": {Kind: "video_frame", FrameID: "gui-frame"}}
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		t.Fatal(err)
	}
	s := session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.CurrentExecutionContract}}}
	tasks, err := s.planReviewTasks(units)
	if err != nil || len(tasks) != 1 || tasks[0].Mode != "multimodal" || !slices.Equal(reviewTaskFrames(tasks[0]), []string{"gui-frame"}) || !tasks[0].Units[0].HasTextEvidence {
		t.Fatalf("mixed limitation was split into incomplete evidence obligations: %+v, %v", tasks, err)
	}
}

func TestJointReviewCanReportTextContradictionWithoutInventingVisualEvidence(t *testing.T) {
	unit := reviewUnit{ID: "u1", Path: "/limitations/0", Value: "讲者称这是2024年的GUI演示，画面为黑色。", HasTextEvidence: true, FrameIDs: []string{"gui-frame"}}
	task := reviewTask{Mode: "multimodal", Units: []reviewUnit{unit}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}, "f1": {Kind: "video_frame", FrameID: "gui-frame"}}
	report := reviewUnitReport{RequestID: "review", Results: []reviewUnitResult{{UnitID: "u1", Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: "2024年", Reason: "原文明确说的是2023年。", CitationIDs: []string{"t1"}}}}}}
	if err := validateReviewUnits(report, "review", task, catalog); err != nil {
		t.Fatalf("joint review forced a text error to cite an unrelated picture: %v", err)
	}
}

func TestJointReviewPreservesFrozenV5AndAtomicComparisons(t *testing.T) {
	units := []reviewUnit{
		{ID: "text", HasTextEvidence: true},
		{ID: "mixed", HasTextEvidence: true, FrameIDs: []string{"a"}},
		{ID: "comparison", FrameIDs: []string{"a", "b", "c", "d"}},
	}
	for _, version := range []string{domain.ExecutionContractV5, domain.ExecutionContractV6} {
		s := session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: version}}}
		tasks, err := s.planReviewTasks(units)
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, task := range tasks {
			for _, unit := range task.Units {
				counts[unit.ID]++
				if unit.ID == "comparison" && !slices.Equal(reviewTaskFrames(task), []string{"a", "b", "c", "d"}) {
					t.Fatal("comparison lost an indispensable frame", task)
				}
			}
		}
		wantMixed := 1
		if version == domain.ExecutionContractV5 {
			wantMixed = 2
			if s.boundedReviewProtocol() != "evidence-review-2026-10-20-v8" {
				t.Fatal("old review identity changed")
			}
		}
		if counts["text"] != 1 || counts["mixed"] != wantMixed || counts["comparison"] != 1 {
			t.Fatal("missing or duplicated review obligations", version, counts)
		}
	}
}
