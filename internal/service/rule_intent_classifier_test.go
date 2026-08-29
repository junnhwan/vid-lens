package service

import (
	"testing"

	"vid-lens/internal/model"
)

// docs/architecture/retrieval.md：规则层分类只测外部可观察行为（intent 判定 + 是否达短路阈值），
// 不测内部权重数值（当前实现约束）。短路阈值通过 confidence ≥ shortCircuitThreshold
// 可观察。

func TestRuleIntentClassifierOverviewShortCircuits(t *testing.T) {
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("这个视频主要讲了什么", videoSession, ChatModeVideoAssistant, nil)
	if intent != IntentVideoOverview {
		t.Fatalf("overview question intent = %q, want video_overview", intent)
	}
	if conf < shortCircuitThreshold {
		t.Fatalf("overview confidence = %.2f, want ≥ %.2f (keyword+signal short-circuit)", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierCompareShortCircuits(t *testing.T) {
	c := NewRuleIntentClassifier()
	kbSession := &model.ChatSession{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 9}
	intent, conf := c.Classify("两个视频对比一下失败恢复", kbSession, ChatModeVideoAssistant, nil)
	if intent != IntentTopicCompare {
		t.Fatalf("compare question intent = %q, want topic_compare", intent)
	}
	if conf < shortCircuitThreshold {
		t.Fatalf("compare confidence = %.2f, want ≥ %.2f (kb+keyword+signal)", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierTimelineShortCircuits(t *testing.T) {
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("第15分钟讲了什么", videoSession, ChatModeVideoAssistant, nil)
	if intent != IntentTimelineLocate {
		t.Fatalf("timeline question intent = %q, want timeline_locate", intent)
	}
	if conf < shortCircuitThreshold {
		t.Fatalf("timeline confidence = %.2f, want ≥ %.2f (keyword+signal)", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierSmallTalkShortCircuits(t *testing.T) {
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("你好，谢谢", videoSession, ChatModeVideoAssistant, nil)
	if intent != IntentSmallTalk {
		t.Fatalf("small talk question intent = %q, want small_talk", intent)
	}
	if conf < shortCircuitThreshold {
		t.Fatalf("small talk confidence = %.2f, want ≥ %.2f", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierStrictRAGForcesDirectQA(t *testing.T) {
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("这个视频主要讲了什么", videoSession, ChatModeStrictRAG, nil)
	if intent != IntentDirectQA {
		t.Fatalf("strict_rag intent = %q, want direct_qa (must retrieve)", intent)
	}
	if conf < shortCircuitThreshold {
		t.Fatalf("strict_rag confidence = %.2f, want ≥ %.2f (contract short-circuit)", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierKBSeriesLocateForOverview(t *testing.T) {
	// KB 概览问法 → 跨视频召回 = series_locate，不归 overview 关检索（与占位一致）。
	c := NewRuleIntentClassifier()
	kbSession := &model.ChatSession{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 9}
	intent, _ := c.Classify("总结一下这些视频", kbSession, ChatModeVideoAssistant, nil)
	if intent == IntentVideoOverview {
		t.Fatalf("KB overview question must not classify video_overview (关检索), got %q", intent)
	}
	if intent != IntentSeriesLocate {
		t.Fatalf("KB overview question intent = %q, want series_locate", intent)
	}
}

func TestRuleIntentClassifierAmbiguousQuestionBelowShortCircuit(t *testing.T) {
	// 无明显 intent signal 的模糊问题应低于短路阈值，交给 LLM 兜底。
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("分布式锁", videoSession, ChatModeVideoAssistant, nil)
	if intent != IntentDirectQA {
		t.Fatalf("ambiguous question intent = %q, want direct_qa fallback", intent)
	}
	if conf >= shortCircuitThreshold {
		t.Fatalf("ambiguous question confidence = %.2f, want < %.2f (defer to LLM)", conf, shortCircuitThreshold)
	}
}

func TestRuleIntentClassifierHistoryBoostsConfidence(t *testing.T) {
	// 同 intent 连续出现 → 历史 intent 加权提置信度。
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	// 单轮 overview：达短路（keyword+signal）。
	_, conf1 := c.Classify("这个视频主要讲了什么", videoSession, ChatModeVideoAssistant, nil)
	// 加历史 overview 加权：置信度应更高。
	_, conf2 := c.Classify("这个视频主要讲了什么", videoSession, ChatModeVideoAssistant, []Intent{IntentVideoOverview, IntentVideoOverview})
	if conf2 <= conf1 {
		t.Fatalf("history boost should raise confidence: %.3f → %.3f", conf1, conf2)
	}
}

func TestRuleIntentClassifierDirectQAExactQuestion(t *testing.T) {
	c := NewRuleIntentClassifier()
	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	intent, conf := c.Classify("为什么需要校验 owner？", videoSession, ChatModeVideoAssistant, nil)
	if intent != IntentDirectQA {
		t.Fatalf("exact qa question intent = %q, want direct_qa", intent)
	}
	// direct_qa 是兜底 intent，弱命中不应高置信短路——避免误压其他 intent 的
	// LLM 兜底。"为什么" 关键词 0.6*weightKeyword + signal 0.5*weightSignal ≈ 0.45，
	// 低于 shortCircuitThreshold 0.75 → 交给 LLM 兜底。注：若调优后该 case 确实
	// 应短路，回填常量并改本断言 + audit trail（当前实现约束）。
	if conf >= shortCircuitThreshold {
		t.Fatalf("direct_qa weak-hit confidence = %.2f, want < %.2f (defer to LLM)", conf, shortCircuitThreshold)
	}
}
