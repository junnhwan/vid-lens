package investigator

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func TestInspectionSelectorMistakesAreCorrectedBeforeSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		accepted   bool
	}{
		{"provider_flat_window", `{"question_id":"q-retro-items456","question":"查看实际画面","start_ms":765000,"end_ms":810000,"count":4,"mode":"detail"}`, false},
		{"missing_selector", `{"question_id":"q","question":"查看实际画面"}`, false},
		{"conflicting_selectors", `{"window":{"start_ms":0,"end_ms":1000,"count":1,"mode":"detail"},"frame_ids":["frame"]}`, false},
		{"nested_window", `{"window":{"start_ms":765000,"end_ms":810000,"count":4,"mode":"detail"}}`, true},
		{"reload_frames", `{"frame_ids":["actual-frame"]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			base, err := utils.InferEnhancedTool("inspect_video_window", "test", func(context.Context, inspectInput) (*schema.ToolResult, error) {
				calls++
				return textResult(map[string]bool{"delegated": true}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			checked := &argumentCheckedTool{EnhancedInvokableTool: base, name: "inspect_video_window"}
			result, err := checked.InvokableRun(context.Background(), &schema.ToolArgument{Text: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if tc.accepted {
				if calls != 1 {
					t.Fatal("valid inspection did not reach the evidence tool")
				}
				return
			}
			wire := string(result.Parts[0].Text)
			if calls != 0 || !strings.Contains(wire, "correct_arguments_and_try_again") || !strings.Contains(wire, "window") || !strings.Contains(wire, "frame_ids") {
				t.Fatalf("bad selector reached question/operation side effects or lacked actionable correction: calls=%d result=%s", calls, wire)
			}
		})
	}
}
