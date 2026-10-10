package service

import (
	"strings"
	"testing"
)

func TestPublicDecisionTitleReflectsActualSafeTool(t *testing.T) {
	valid := VideoAgentLoopDecision{Tool: VideoAgentToolSearchTranscript, PublicTitle: "检索安装步骤的文字说明"}
	if publicDecisionTitle(valid) != valid.PublicTitle {
		t.Fatal("safe action title lost")
	}
	for _, bad := range []string{"下载整个视频", "保存摘要新版本", "https://private.example/?token=secret", "检索\n安装步骤", strings.Repeat("字", 41)} {
		valid.PublicTitle = bad
		if title := publicDecisionTitle(valid); title == bad || title == "" {
			t.Fatalf("invalid tool title retained: %q", title)
		}
	}
	valid.PublicTitle = "查看配置画面"
	valid.Tool = VideoAgentToolInspectVisualWindow
	if publicDecisionTitle(valid) != valid.PublicTitle {
		t.Fatal("visual action title lost")
	}
}
