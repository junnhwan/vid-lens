package investigator

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestCompactQuestionReviewCombinesCompleteEvidence(t *testing.T) {
	s := focusedQuestionFixture()
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV5
	units := []reviewUnit{
		{ID: "text", Value: "The speaker introduces a cycle.", HasTextEvidence: true, CitationAliases: []string{"t1"}},
		{ID: "figure", Value: "The image shows a cycle.", FrameIDs: []string{"pixels"}, CitationAliases: []string{"f1"}},
	}
	tasks, err := s.planReviewTasks(units)
	if err != nil || len(tasks) != 1 || tasks[0].Mode != "multimodal" || len(tasks[0].Units) != 2 || len(reviewTaskFrames(tasks[0])) != 1 {
		t.Fatalf("short answer was split despite fitting one complete evidence batch: %+v, %v", tasks, err)
	}
	if len(units[0].FrameIDs) != 0 {
		t.Fatal("planning mutated the original candidate")
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV4
	legacy, err := s.planReviewTasks(units)
	if err != nil || len(legacy) != 2 {
		t.Fatal("historical review planning changed", err)
	}
}

func TestQuestionReviewVerdictProjectionCanOnlyReject(t *testing.T) {
	task := reviewTask{Mode: "source_text", Units: []reviewUnit{{ID: "u", Value: "A synthetic claim."}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}}
	for _, verdict := range []string{"supported", "not_assessed", "contradicted", "unverifiable"} {
		raw := `{"request_id":"r","results":[{"unit_id":"u","verdict":"` + verdict + `","issues":[{"claim":"synthetic claim","reason":"The reference does not support the claim.","citation_ids":["t1"]}]}]}`
		response := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: reviewUnitsTool, Arguments: raw}}}}
		projected, changed := projectQuestionReviewVerdicts(response)
		report, err := parseReviewUnits(projected, "r", task, catalog)
		if err != nil || report.Results[0].Verdict == "supported" || response.ToolCalls[0].Function.Arguments != raw {
			t.Fatal("projection approved an issue, lost evidence, or changed raw audit", verdict, err)
		}
		if (len(changed) == 1) != (verdict == "supported") {
			t.Fatal("uncertain verdict was rewritten")
		}
		projected.ToolCalls[0].Function.Arguments = strings.Replace(projected.ToolCalls[0].Function.Arguments, `"t1"`, `"unknown"`, 1)
		if _, err = parseReviewUnits(projected, "r", task, catalog); err == nil {
			t.Fatal("projection bypassed source validation")
		}
	}
}

func TestCompactQuestionReviewRetainsBatchLimits(t *testing.T) {
	units := []reviewUnit{{ID: "u", Value: "text", FrameIDs: []string{"a", "b", "c", "d"}}}
	if _, ok := compactQuestionReviewTask(units, 10000); ok {
		t.Fatal("image budget expanded")
	}
	units[0].FrameIDs = nil
	units[0].Value = strings.Repeat("source claim ", 10000)
	if _, ok := compactQuestionReviewTask(units, 10000); ok {
		t.Fatal("work budget expanded")
	}
	if _, ok := compactQuestionReviewTask(make([]reviewUnit, 9), 10000); ok {
		t.Fatal("unit budget expanded")
	}
}

func TestNotAssessedKeepsEvidenceGapReasons(t *testing.T) {
	task := reviewTask{Mode: "source_text", Units: []reviewUnit{{ID: "u", Value: "The diagram is blue."}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}}
	response := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: reviewUnitsTool, Arguments: `{"request_id":"r","results":[{"unit_id":"u","verdict":"not_assessed","issues":[{"claim":"diagram is blue","reason":"The original text does not specify color; pixels are needed.","citation_ids":["t1"]}]}]}`}}}}
	report, err := parseReviewUnits(response, "r", task, catalog)
	if err != nil || report.Results[0].Verdict != "not_assessed" || len(report.Results[0].Issues) != 1 {
		t.Fatal("an actionable evidence gap was rejected as malformed or approved", err)
	}
	delete(catalog, "t1")
	if _, err = parseReviewUnits(response, "r", task, catalog); err == nil {
		t.Fatal("unknown evidence was accepted")
	}
}
