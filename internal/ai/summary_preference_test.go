package ai

import (
	"context"
	"strings"
	"testing"
)

func TestSummaryPreferenceKeepsProductPromptAndAddsScopedPreference(t *testing.T) {
	messages := summaryMessages(WithSummaryPreference(context.Background(), "请简洁回答"), "转写正文")
	if len(messages) != 4 || !strings.Contains(messages[0].Content, "核心摘要") || !strings.Contains(messages[1].Content, "请简洁回答") || messages[3].Content != "转写正文" {
		t.Fatalf("summary messages = %+v", messages)
	}
}
