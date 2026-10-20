package investigator

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestReviewV3SelectsBoundedReviewWithoutChangingV2(t *testing.T) {
	for _, version := range []string{domain.ExecutionContractV2, domain.ExecutionContractV3} {
		s := session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: version}}}
		if !s.usesBoundedReview() {
			t.Fatalf("new execution contract silently falls back to legacy review: %s", version)
		}
	}
}

func TestReviewV3PreservesMixedUncitedAndAtomicEvidenceObligations(t *testing.T) {
	catalog := map[string]domain.Citation{
		"t1": {Kind: "video_text"}, "web1": {Kind: "external_reference"},
		"f1": {Kind: "video_frame", FrameID: "frame1"}, "f2": {Kind: "video_frame", FrameID: "frame2"},
	}
	candidate := map[string]any{"blocks": []any{
		map[string]any{"text": "mixed claim", "citation_ids": []string{"f1", "t1"}},
		map[string]any{"text": "uncited execution scope"},
		map[string]any{"text": "original text claim", "citation_ids": []string{"t1"}},
		map[string]any{"text": "comparison", "frame_ids": []string{"frame2", "frame1"}},
		map[string]any{"text": "external comparison", "citation_ids": []string{"f1", "web1"}},
	}}
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		t.Fatal(err)
	}
	s := session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV3}}}
	tasks, err := s.planReviewTasks(units)
	if err != nil {
		t.Fatal(err)
	}
	obligations := map[string][]string{}
	for _, task := range tasks {
		for _, unit := range task.Units {
			obligations[unit.Path] = append(obligations[unit.Path], task.Mode)
			if unit.Path == "/blocks/3" && (!slices.Equal(unit.FrameIDs, []string{"frame1", "frame2"}) || !slices.Equal(reviewTaskFrames(task), []string{"frame1", "frame2"})) {
				t.Fatal("atomic comparison lost a required frame", unit, task)
			}
		}
	}
	for path, modes := range map[string][]string{
		"/blocks/0": {"source_text", "visual"}, "/blocks/1": {"source_text"}, "/blocks/2": {"source_text"},
		"/blocks/3": {"visual"}, "/blocks/4": {"source_text", "visual"},
	} {
		if !slices.Equal(obligations[path], modes) {
			t.Fatal("modality obligations changed", path, obligations[path], modes)
		}
	}
	// New classifier fields must not alter an existing V2 model request or cost.
	before := string(domain.JSON(units))
	for i := range units {
		units[i].HasTextEvidence = !units[i].HasTextEvidence
	}
	if string(domain.JSON(units)) != before || strings.Contains(before, "HasTextEvidence") || !json.Valid([]byte(before)) {
		t.Fatal("internal evidence classification leaked into frozen unit JSON", before)
	}
}

func TestReviewV3KeepsSectionTitleAndAnswerSummaryEvidenceContainers(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}, "f1": {Kind: "video_frame", FrameID: "frame1"}}
	for _, candidate := range []map[string]any{
		{"sections": []any{map[string]any{"title": "shared heading", "blocks": []any{map[string]any{"text": "mixed", "citation_ids": []string{"t1", "f1"}}}}}},
		{"summary": "shared summary", "blocks": []any{map[string]any{"text": "mixed", "citation_ids": []string{"t1", "f1"}}}},
	} {
		units, err := candidateReviewUnits(candidate, catalog)
		if err != nil {
			t.Fatal(err)
		}
		for _, unit := range units {
			if !unit.HasTextEvidence || !slices.Equal(unit.FrameIDs, []string{"frame1"}) {
				t.Fatal("shared evidence container was narrowed", unit)
			}
		}
	}
}

func TestReviewV3RoutesMattFrameOnlyGapWithoutDroppingVisual(t *testing.T) {
	// Minimal production failure: source_text had no pixels, returned
	// not_assessed for this gap, and stopped before the required visual pass.
	candidate := map[string]any{"limitations": []any{map[string]any{
		"code": "not_observed", "message": "全片出现 12 个章节卡,但抽样画面只看到第 01 章标题\"THE MOMENT IT CLICKED\",其余章节标题无法确认,因此正文按话题而非章节卡编号组织。",
		"section_ids": []string{"s1"}, "citation_ids": []string{"f1"},
	}}}
	catalog := map[string]domain.Citation{"f1": {Kind: "video_frame", FrameID: "actual-frame"}}
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		t.Fatal(err)
	}
	s := session{profile: config.DefaultProfile(), cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV3}}}
	tasks, err := s.planReviewTasks(units)
	if err != nil || len(tasks) != 1 || tasks[0].Mode != "visual" || len(tasks[0].Units) != 1 || !slices.Equal(reviewTaskFrames(tasks[0]), []string{"actual-frame"}) {
		t.Fatalf("frame-only gap must reach exactly one visual obligation: %+v %v", tasks, err)
	}
	legacy, err := planReviewTasks(units, s.profile.MaxOutputTokens)
	if err != nil || len(legacy) != 2 || legacy[0].Mode != "source_text" || legacy[1].Mode != "visual" {
		t.Fatalf("frozen v2 planner changed: %+v %v", legacy, err)
	}
}
