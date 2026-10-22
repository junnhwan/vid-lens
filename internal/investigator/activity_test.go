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

func TestStreamedActivityReadsOnlyCompletePublicMetadata(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"activity_title":"核对重命名实验的画面","sections":[`, "核对重命名实验的画面"},
		{`{"activity_title":"核对重命名`, ""},
		{`{"sections":[{"activity_title":"嵌套标题"}],"finish":true}`, ""},
		{`{"activity_title":42}`, ""},
		{`{"activity_title":"http://private"}`, ""},
		{`{"activity_title":"多行\n文本"}`, ""},
		{`{"activity_title":"核对\"截图\"中的文件名",`, `核对"截图"中的文件名`},
	} {
		if got := streamedActivityTitle(tc.input); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.input, got, tc.want)
		}
	}
}
