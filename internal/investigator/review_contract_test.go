package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestReviewUnitsKeepComparisonEvidenceAndCanonicalPaths(t *testing.T) {
	catalog := map[string]domain.Citation{}
	for i := 1; i <= 4; i++ {
		catalog[fmt.Sprint("f", i)] = domain.Citation{Kind: "video_frame", FrameID: fmt.Sprint("frame", i)}
	}
	candidate := map[string]any{"sections": []any{map[string]any{"title": "Comparisons", "blocks": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"text": "A relationship across four observations."}}, "citation_ids": []string{"f1", "f2", "f3", "f4"}}}}}}
	before := string(domain.JSON(candidate))
	units, err := candidateReviewUnits(candidate, catalog)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := planReviewTasks(units, 393216)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]int{}
	for _, task := range tasks {
		if task.Mode != "visual" {
			continue
		}
		for _, unit := range task.Units {
			covered[unit.Path]++
			if unit.Path == "/sections/0/blocks/0" && !slices.Equal(unit.FrameIDs, []string{"frame1", "frame2", "frame3", "frame4"}) {
				t.Fatal("comparison evidence was split", unit)
			}
		}
	}
	if covered["/sections/0/blocks/0"] != 1 || string(domain.JSON(candidate)) != before {
		t.Fatal("atomic comparison or canonical candidate changed", covered)
	}
}

func TestReviewCapacityComesFromModelAndRemainingContext(t *testing.T) {
	p := config.DefaultProfile()
	p.MaxOutputTokens = 393216
	p.ContextTokens = 1048576
	p.InputCharsLimit = 2000000
	s := session{profile: p, reviewing: true, cp: checkpoint{HarnessRevision: 16}, reviewStep: &reviewStep{RequestID: "step", Units: 1, WorkTokens: 100, OutputTokens: 65536}}
	b, err := s.budget(context.Background(), nil, 0)
	if err != nil || b.OutputTokens != 65536 || b.ConfiguredOutputTokens != 393216 || b.AllocationReason != "review_truncation_growth" {
		t.Fatalf("review retained the historical hidden cap: %+v %v", b, err)
	}
	s.profile.ContextTokens = 30000
	b, err = s.budget(context.Background(), nil, 0)
	if err != nil || b.OutputTokens >= 30000 || b.OutputTokens != b.AvailableOutputTokens {
		t.Fatalf("review exceeded shared context: %+v %v", b, err)
	}
	s.reviewStep = nil
	s.profile.ContextTokens = 1048576
	b, err = s.budget(context.Background(), nil, 0)
	if err != nil || b.OutputTokens != 32768 {
		t.Fatal("historical review contract changed", b, err)
	}
}

func TestReviewCoverageRejectsSilenceForeignClaimsAndPixels(t *testing.T) {
	units := []reviewUnit{{ID: "u1", Path: "/summary", Value: "The supplied claim.", FrameIDs: []string{"own-frame"}}, {ID: "u2", Path: "/title", Value: "Another claim.", FrameIDs: []string{}}}
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}, "f1": {Kind: "video_frame", FrameID: "own-frame"}, "f2": {Kind: "video_frame", FrameID: "another-frame"}}
	base := reviewUnitReport{RequestID: "request", Results: []reviewUnitResult{{UnitID: "u1", Verdict: "supported", Issues: []reviewUnitIssue{}}, {UnitID: "u2", Verdict: "supported", Issues: []reviewUnitIssue{}}}}
	for _, fault := range []string{"omitted", "duplicate", "wrong_identity", "foreign_unit", "missing_issues", "unknown_verdict", "foreign_quote", "wrong_modality", "undelivered_pixels"} {
		t.Run(fault, func(t *testing.T) {
			var report reviewUnitReport
			_ = json.Unmarshal(domain.JSON(base), &report)
			task := reviewTask{Mode: "source_text", Units: units}
			switch fault {
			case "omitted":
				report.Results = report.Results[:1]
			case "duplicate":
				report.Results[1].UnitID = "u1"
			case "wrong_identity":
				report.RequestID = "another-request"
			case "foreign_unit":
				report.Results[0].UnitID = "u999"
			case "missing_issues":
				report.Results[0].Issues = nil
			case "unknown_verdict":
				report.Results[0].Verdict = "approved"
			default:
				i := reviewUnitIssue{Claim: "supplied claim", Reason: "A material source difference.", CitationIDs: []string{"t1"}}
				if fault == "foreign_quote" {
					i.Claim = "Another claim."
				}
				if fault == "wrong_modality" {
					i.CitationIDs = []string{"f1"}
				}
				if fault == "undelivered_pixels" {
					task.Mode = "visual"
					i.CitationIDs = []string{"f2"}
				}
				report.Results[0].Verdict = "contradicted"
				report.Results[0].Issues = []reviewUnitIssue{i}
			}
			if err := validateReviewUnits(report, "request", task, catalog); err == nil {
				t.Fatal("invalid review coverage accepted", fault)
			}
		})
	}
	valid := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: reviewUnitsTool, Arguments: string(domain.JSON(base))}}}}
	if _, err := parseReviewUnits(valid, "request", reviewTask{Mode: "source_text", Units: units}, catalog); err != nil {
		t.Fatal(err)
	}
}
