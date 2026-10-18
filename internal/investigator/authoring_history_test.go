package investigator

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestAuthoringHistoryKeepsCandidateFieldsUntilRepair(t *testing.T) {
	messages := []*schema.Message{
		schema.SystemMessage("frozen"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "plan", Function: schema.FunctionCall{Name: "begin_article", Arguments: `{"private_plan":"retained on server"}`}}}},
		{Role: schema.Tool, ToolCallID: "plan", Content: `{"error":{"code":"invalid_evidence"},"next_action":"repair_article_plan"}`},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-one", Function: schema.FunctionCall{Name: "read_candidate_fields", Arguments: `{"candidate_id":"plan","paths":["/sections/8/evidence_ids"]}`}}}},
		{Role: schema.Tool, ToolCallID: "read-one", Content: `{"candidate_id":"plan","expected_hash":"original-hash","fields":{"/sections/8/evidence_ids":["t90","f11"]}}`},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-two", Function: schema.FunctionCall{Name: "read_candidate_fields", Arguments: `{"candidate_id":"plan","paths":["/sections/10/evidence_ids"]}`}}}},
		{Role: schema.Tool, ToolCallID: "read-two", Content: `{"candidate_id":"plan","expected_hash":"original-hash","fields":{"/sections/10/evidence_ids":["t110","f12"]}}`},
	}
	original := string(domain.JSON(messages))
	for round := 0; round < 3; round++ {
		messages = projectToolHistory(projectAuthoringHistory(messages), true)
		raw := string(domain.JSON(messages))
		for _, needed := range []string{"invalid_evidence", "repair_article_plan", "original-hash", "/sections/8/evidence_ids", "t90", "/sections/10/evidence_ids", "t110"} {
			if !strings.Contains(raw, needed) {
				t.Fatalf("round %d lost repair information %q: %s", round, needed, raw)
			}
		}
		if strings.Contains(raw, "private_plan") {
			t.Fatal("unrequested full candidate copied into context")
		}
		if err := validateProtocol(messages); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(original, "private_plan") {
		t.Fatal("fixture missing original candidate")
	}
}

func TestAuthoringHistoryDropsSupersededReads(t *testing.T) {
	for _, result := range []string{`{"version":1}`, `{"error":{"code":"new_issue"}}`} {
		messages := []*schema.Message{
			{Role: schema.User, Name: "vidlens_last_feedback", Content: `{"error":{"code":"old_issue"}}`},
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read", Function: schema.FunctionCall{Name: "read_candidate_fields"}}}},
			{Role: schema.Tool, ToolCallID: "read", Content: `{"fields":{"/mind_map/root_id":"old-root"}}`},
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "repair", Function: schema.FunctionCall{Name: "repair_article_plan"}}}},
			{Role: schema.Tool, ToolCallID: "repair", Content: result},
		}
		out := projectAuthoringHistory(messages)
		raw := string(domain.JSON(out))
		if strings.Contains(raw, "old-root") || strings.Contains(raw, "old_issue") {
			t.Fatal("superseded repair context retained", raw)
		}
		if strings.Contains(result, "new_issue") && !strings.Contains(raw, "new_issue") {
			t.Fatal("current rejection lost", raw)
		}
		if err := validateProtocol(out); err != nil {
			t.Fatal(err)
		}
	}
}
