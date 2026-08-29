package service

import (
	"context"
	"strings"
	"testing"
)

// docs/architecture/retrieval.md：LLM 兜底分类测外部可观察行为（intent 判定 + 回退阈值 + nil 兜底），
// 不测 prompt 实现细节（当前实现约束）。

func TestLLMIntentClassifierParsesResponse(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{`{"intent":"timeline_locate","confidence":0.9}`}}
	c := NewLLMIntentClassifier(chat)
	intent, conf, err := c.Classify(context.Background(), "第15分钟讲了什么", nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if intent != IntentTimelineLocate {
		t.Fatalf("intent = %q, want timeline_locate", intent)
	}
	if conf != 0.9 {
		t.Fatalf("confidence = %.2f, want 0.9", conf)
	}
}

func TestLLMIntentClassifierStripsCodeFence(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{"```json\n{\"intent\":\"video_overview\",\"confidence\":0.8}\n```"}}
	c := NewLLMIntentClassifier(chat)
	intent, _, err := c.Classify(context.Background(), "这个视频讲了什么", nil)
	if err != nil {
		t.Fatalf("Classify() with code fence error = %v", err)
	}
	if intent != IntentVideoOverview {
		t.Fatalf("intent = %q, want video_overview", intent)
	}
}

func TestLLMIntentClassifierClampsConfidence(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{`{"intent":"small_talk","confidence":1.5}`}}
	c := NewLLMIntentClassifier(chat)
	_, conf, err := c.Classify(context.Background(), "你好", nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if conf != 1.0 {
		t.Fatalf("confidence = %.2f, want clamped to 1.0", conf)
	}
}

func TestLLMIntentClassifierUnknownIntentFallsToDirectQA(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{`{"intent":"some_unknown","confidence":0.7}`}}
	c := NewLLMIntentClassifier(chat)
	intent, _, err := c.Classify(context.Background(), "随便", nil)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if intent != IntentDirectQA {
		t.Fatalf("unknown intent = %q, want direct_qa (safe fallback)", intent)
	}
}

func TestLLMIntentClassifierNilChatReturnsZero(t *testing.T) {
	// chat nil → IntentRouter 回退规则结果；本层产 (direct_qa, 0) 不报错。
	c := NewLLMIntentClassifier(nil)
	intent, conf, err := c.Classify(context.Background(), "随便", nil)
	if err != nil {
		t.Fatalf("nil chat should not error, got %v", err)
	}
	if intent != IntentDirectQA {
		t.Fatalf("nil chat intent = %q, want direct_qa", intent)
	}
	if conf != 0 {
		t.Fatalf("nil chat confidence = %.2f, want 0", conf)
	}
}

func TestLLMIntentClassifierUnparsableReturnsError(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{"not json at all"}}
	c := NewLLMIntentClassifier(chat)
	_, _, err := c.Classify(context.Background(), "随便", nil)
	if err == nil {
		t.Fatal("unparsable response should error")
	}
}

// TestLLMIntentClassifierPromptIncludesTaxonomy 锁定 prompt 含 vid-lens 6 类
// taxonomy（不照搬 wali，当前实现约束）。
func TestLLMIntentClassifierPromptIncludesTaxonomy(t *testing.T) {
	chat := &scriptedChatClient{responses: []string{`{"intent":"direct_qa","confidence":0.6}`}}
	c := NewLLMIntentClassifier(chat)
	_, _, _ = c.Classify(context.Background(), "为什么", nil)
	if len(chat.messages) == 0 {
		t.Fatal("no messages recorded")
	}
	joined := ""
	for _, msg := range chat.messages[0] {
		joined += msg.Content + "\n"
	}
	for _, intent := range []string{"video_overview", "direct_qa", "topic_compare", "series_locate", "timeline_locate", "small_talk"} {
		if !strings.Contains(joined, intent) {
			t.Fatalf("prompt missing taxonomy %q: %s", intent, joined)
		}
	}
}
