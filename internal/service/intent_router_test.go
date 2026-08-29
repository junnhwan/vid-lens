package service

import (
	"context"
	"testing"

	"vid-lens/internal/model"
)

// docs/architecture/retrieval.md：IntentRouter 级联编排测外部可观察行为——规则层短路不调 LLM、
// 规则层未短路调 LLM 兜底、LLM<0.5 回退规则、router nil 降级占位（当前实现约束）。

func TestIntentRouterRuleShortCircuitsSkipsLLM(t *testing.T) {
	// overview 问题规则层达短路阈值 → 不该调 LLM。
	router := NewIntentRouter(NewRuleIntentClassifier())
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	// 用一个会记录调用的 chat client 传进去，短路时该 0 调用。
	chat := &recordingChatClient{}
	intent := router.Classify(context.Background(), "这个视频主要讲了什么", videoSession, ChatModeVideoAssistant, nil, chat)
	if intent != IntentVideoOverview {
		t.Fatalf("intent = %q, want video_overview", intent)
	}
	if chat.chatCalls != 0 {
		t.Fatalf("rule short-circuit should skip LLM, got %d chat calls", chat.chatCalls)
	}
}

func TestIntentRouterLLMFallbackOnAmbiguous(t *testing.T) {
	// 模糊问题规则层未短路 → 调 LLM 兜底；LLM 返回高置信 → 采信 LLM。
	router := NewIntentRouter(NewRuleIntentClassifier())
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	// scriptedChatClient 第一条响应给 LLM 兜底返回 timeline_locate 0.8。
	chat := &scriptedChatClient{responses: []string{`{"intent":"timeline_locate","confidence":0.8}`}}
	intent := router.Classify(context.Background(), "分布式锁和租约", videoSession, ChatModeVideoAssistant, nil, chat)
	if intent != IntentTimelineLocate {
		t.Fatalf("LLM fallback intent = %q, want timeline_locate", intent)
	}
	// responses 被消费 = LLM 兜底确实被调用。
	if len(chat.responses) != 0 {
		t.Fatalf("LLM fallback should consume the response, %d left", len(chat.responses))
	}
}

func TestIntentRouterLLMLowConfidenceFallsBackToRule(t *testing.T) {
	// LLM confidence<0.5 → 回退规则结果（当前实现约束）。
	router := NewIntentRouter(NewRuleIntentClassifier())
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	// 规则层 best 是 direct_qa（"分布式锁" 无明显 signal）；LLM 给 small_talk 0.3 < 0.5。
	chat := &scriptedChatClient{responses: []string{`{"intent":"small_talk","confidence":0.3}`}}
	intent := router.Classify(context.Background(), "分布式锁", videoSession, ChatModeVideoAssistant, nil, chat)
	if intent != IntentDirectQA {
		t.Fatalf("LLM low conf should fall back to rule, intent = %q, want direct_qa", intent)
	}
}

func TestIntentRouterNilChatSkipsLLMFallback(t *testing.T) {
	// 无 chat client → 跳过 LLM 兜底，直接采信规则层 best。
	router := NewIntentRouter(NewRuleIntentClassifier())
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent := router.Classify(context.Background(), "分布式锁", videoSession, ChatModeVideoAssistant, nil, nil)
	if intent != IntentDirectQA {
		t.Fatalf("nil chat should use rule best, intent = %q, want direct_qa", intent)
	}
}

func TestIntentRouterNilRouterDegradesToPlaceholder(t *testing.T) {
	// router nil → chat_prepare 降级占位。本测试直接验证 IntentRouter.Classify 的
	// nil 接收者降级路径（保测试稳定，当前实现约束）。
	var router *IntentRouter
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent := router.Classify(context.Background(), "这个视频主要讲了什么", videoSession, ChatModeVideoAssistant, nil, nil)
	if intent != IntentVideoOverview {
		t.Fatalf("nil router should degrade to placeholder, intent = %q, want video_overview", intent)
	}
}

func TestIntentRouterStrictRAGShortCircuitsToDirectQA(t *testing.T) {
	// strict_rag 契约短路，不调 LLM。
	router := NewIntentRouter(NewRuleIntentClassifier())
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	chat := &recordingChatClient{}
	intent := router.Classify(context.Background(), "这个视频主要讲了什么", videoSession, ChatModeStrictRAG, nil, chat)
	if intent != IntentDirectQA {
		t.Fatalf("strict_rag intent = %q, want direct_qa", intent)
	}
	if chat.chatCalls != 0 {
		t.Fatalf("strict_rag should short-circuit, got %d chat calls", chat.chatCalls)
	}
}
