package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"net/http"
	"testing"
	"vidlens/internal/domain"
)

func TestArticleLoopOutputCapRequiresReportedUsageAndIncompleteReview(t *testing.T) {
	p := articleLoopCall{Mode: "review", Scope: &domain.ArticleReviewScope{}, OutputTokens: 32768}
	message := func(args string, tokens int) *schema.Message {
		return &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: loopReviewTool, Arguments: args}}}, ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{CompletionTokens: tokens}}}
	}
	if !loopReportedOutputCap(p, message(`{"complete":true`, 32768)) {
		t.Fatal("observed cap was ignored")
	}
	for _, m := range []*schema.Message{message(`{"complete":true}`, 32768), message(`{"complete":true`, 32767), {ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Arguments: `{"complete":true`}}}}} {
		if loopReportedOutputCap(p, m) {
			t.Fatal("guessed exhaustion or rejected complete JSON", m)
		}
	}
	author := p
	author.Mode = "author"
	if loopReportedOutputCap(author, message(`{"complete":true`, 32768)) {
		t.Fatal("review recovery changed author writes")
	}
	repair := p
	repair.ReferenceRepairKey = "immutable-ticket"
	if loopReportedOutputCap(repair, message(`{"complete":true`, 32768)) {
		t.Fatal("reference-only repair was regenerated")
	}
}

func TestArticleLoopReportedOutputCapNarrowsReviewWithWrongFinishMarker(t *testing.T) {
	reviews := 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
			return
		}
		if p["scope"] == nil {
			in := loopGoodWrite()
			text := "合成后半段必要内容。"
			in.Sections = append(in.Sections, loopSection{Key: "ending", Title: "后半段", Content: []loopContent{{Text: &text, Sources: []string{"t25"}}}})
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		reviews++
		if reviews == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			delta := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "cap-case", "type": "function", "function": map[string]any{"name": loopReviewTool, "arguments": `{"complete":true`}}}}}}}
			fmt.Fprintf(w, "data: %s\n\n", domain.JSON(delta))
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":32768,\"total_tokens\":32868}}\n\ndata: [DONE]\n\n")
			return
		}
		var scope domain.ArticleReviewScope
		_ = json.Unmarshal(p["scope"], &scope)
		if reviews == 2 && scope.Global {
			t.Error("incomplete JSON at reported output cap was resubmitted as the same full review instead of narrowing work")
		}
		writeReviewWireArgs(w, loopReviewTool, sufficiencyReport(t, p))
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	var reporting struct {
		Code    string `json:"failure_code"`
		Finish  string `json:"finish_reason"`
		Reached bool   `json:"output_limit_reached"`
		Output  struct {
			Value int `json:"value"`
		} `json:"output"`
	}
	if err := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT reporting FROM model_submissions WHERE run_id=$1 AND operation_key='model:2'", s.claim.Run.ID).Scan(&reporting); err != nil {
		t.Fatal(err)
	}
	if reporting.Code != "MODEL_OUTPUT_TRUNCATED" || reporting.Finish != "tool_calls" || !reporting.Reached || reporting.Output.Value != 32768 {
		t.Fatal("measured diagnosis lost original finish/usage evidence", reporting)
	}
}
