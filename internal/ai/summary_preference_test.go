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

func TestSummaryIntermediateLimitUsesCompactNotesOnlyForIntermediateCalls(t *testing.T) {
	parent := WithSummaryPreference(context.Background(), "保留技术术语")
	messages := summaryMessages(WithSummaryIntermediateLimit(parent, 450), "完整输入")
	if !strings.Contains(messages[0].Content, "最多 450 个字符") || !strings.Contains(messages[0].Content, "中间事实笔记") {
		t.Fatalf("missing intermediate limit: %+v", messages)
	}
	if messages[len(messages)-1].Content != "完整输入" || !strings.Contains(messages[1].Content, "保留技术术语") {
		t.Fatalf("source or user preference lost: %+v", messages)
	}
	final := summaryMessages(parent, "最终合并")
	if final[0].Content != defaultSummarySystemPrompt() {
		t.Fatalf("intermediate limit leaked into final report: %+v", final)
	}
}
