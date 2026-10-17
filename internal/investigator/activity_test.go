package investigator

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestModelResponseRetainsPublicIntentWithoutClaimingCompletion(t *testing.T) {
	result := schema.AssistantMessage("", []schema.ToolCall{{Function: schema.FunctionCall{Name: "inspect_video_window", Arguments: `{"activity_title":"查看 cat.png 改名实验画面"}`}}})
	title, outcome := responseActivity(result)
	if title == "查看 cat.png 改名实验画面" || !strings.Contains(title, "准备：查看 cat.png") || !strings.Contains(outcome, "尚未确认") {
		t.Fatalf("model receipt duplicates its following execution or loses the action: title=%q outcome=%q", title, outcome)
	}
}
