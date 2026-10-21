package investigator

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// Theo's complete response contained both useful verdicts and a finding with
// missing evidence. A malformed finding must not discard unrelated coverage.
func TestWorkflowReviewRetainsIndependentValidResults(t *testing.T) {
	task := reviewTask{Mode: "evidence", Units: []reviewUnit{
		{ID: "u1", Path: "/sections/0/blocks/0", Value: "第一项事实"},
		{ID: "u2", Path: "/sections/0/blocks/1", Value: "第二项事实"},
		{ID: "u3", Path: "/sections/0/blocks/2", Value: "第三项事实"},
	}}
	report := reviewUnitReport{RequestID: "real-failure-pattern", Results: []reviewUnitResult{
		{UnitID: "u1", Verdict: "supported", Issues: []reviewUnitIssue{}},
		{UnitID: "u2", Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: "第二项事实", Reason: "该判断没有附带原文身份。", CitationIDs: []string{}}}},
		{UnitID: "u3", Verdict: "supported", Issues: []reviewUnitIssue{}},
	}}
	response := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: reviewUnitsTool, Arguments: string(domain.JSON(report))}}}}
	accepted, err := parseReviewUnits(response, report.RequestID, task, map[string]domain.Citation{})
	if err == nil || len(accepted.Results) != 2 || accepted.Results[0].UnitID != "u1" || accepted.Results[1].UnitID != "u3" {
		t.Fatalf("malformed unit discarded or contaminated independent coverage: accepted=%d err=%v", len(accepted.Results), err)
	}
}
