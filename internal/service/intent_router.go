package service

import (
	"context"
	"strings"

	"vid-lens/internal/ai"
	"vid-lens/internal/model"
)

// Spec 05: IntentRouter 级联编排（规则层短路 + LLM 兜底）。
//
// 级联（CONTEXT.md + spec line 41）：
//  1. 规则层 RuleIntentClassifier 先跑：<1ms，confidence ≥ shortCircuitThreshold → 短路返回，不调 LLM。
//  2. 规则层未短路 → 调 LLMIntentClassifier 兜底。
//  3. LLM confidence ≥ llmFallbackThreshold → 采信 LLM 结果。
//  4. LLM confidence < llmFallbackThreshold 或 LLM 不可用/出错 → 回退规则结果。
//
// 替换 spec 04 A段的 classifyIntentPlaceholder 占位。占位保留作降级 fallback：
// router 为 nil 时（测试或 router 不可用）chat_prepare 降级到占位分类，保证测试稳定。

// IntentRouter 是规则层 + LLM 兜底的级联 intent 分类器（spec 05）。
type IntentRouter struct {
	rule *RuleIntentClassifier
}

// NewIntentRouter 构造级联分类器。rule 不可为空。LLM 兜底用本次请求的 chat
// client（per-request，caller 在 Classify 传入），故本结构不持 chat——避免全局
// profile 解析路径，复用 caller 已解析的 profile。
func NewIntentRouter(rule *RuleIntentClassifier) *IntentRouter {
	if rule == nil {
		rule = NewRuleIntentClassifier()
	}
	return &IntentRouter{rule: rule}
}

// Classify 返回本次提问的 intent（级联短路 + LLM 兜底 + 规则回退）。
//
// chat 是本次请求的 LLM client（caller = prepareChatByMode 已解析好），
// 用于 LLM 兜底；为 nil 时跳过 LLM 兜底，直接用规则层结果（规则层未短路也采信
// 其 best——诚实标注"无 LLM 兜底时规则层兜底"）。
//
// recent 是会话最近消息文本（用于 LLM 兜底消歧指代，由 caller 从 recent
// messages 抽取 Content）。
func (r *IntentRouter) Classify(ctx context.Context, question string, session *model.ChatSession, mode ChatMode, recentIntents []Intent, chat ai.ChatClient) Intent {
	if r == nil {
		// router 不可用 → 占位降级（保测试稳定，spec line 65）。
		return classifyIntentPlaceholder(question, session, mode)
	}

	ruleIntent, ruleConf := r.rule.Classify(question, session, mode, recentIntents)

	// 规则层短路：confidence 达阈值，不调 LLM（省 LLM 调用 = 短路率派生，决策记录 §4）。
	if ruleConf >= shortCircuitThreshold {
		return ruleIntent
	}

	// 规则层未短路 → LLM 兜底。无 chat client → 直接采信规则层 best
	// （诚实：无 LLM 兜底路径时不硬调，走规则层兜底）。
	if chat == nil {
		return ruleIntent
	}
	// 用本次请求的 chat client 构造 LLM 兜底（per-request，复用已解析 profile，
	// 不另起全局 profile 解析路径）。
	llmClassifier := &LLMIntentClassifier{chat: chat}
	llmIntent, llmConf, err := llmClassifier.Classify(ctx, question, recentTexts(recentIntents))
	if err != nil {
		return ruleIntent // LLM 出错 → 回退规则（spec line 57）
	}
	if llmConf >= llmFallbackThreshold {
		return llmIntent
	}
	// LLM confidence<0.5 → 回退规则结果（spec line 57）。
	return ruleIntent
}

// recentTexts 把历史 intent 串成最近消息文本给 LLM 兜底消歧指代。
// audit trail：用 intent 标签而非原始消息——避免把用户原始对话全文灌进分类
// prompt（噪声 + 隐私），intent 序列已足够给 LLM "上一轮在问什么类"的消歧信号。
func recentTexts(recentIntents []Intent) []string {
	if len(recentIntents) == 0 {
		return nil
	}
	out := make([]string, 0, len(recentIntents))
	for _, it := range recentIntents {
		out = append(out, string(it))
	}
	return out
}

// parseRecentIntents 从 recent messages 粗提历史 intent 序列（spec 05 历史 intent
// 加权数据源）。复用 chat_memory.go GetRecentMessages，由 caller 在 prepare 阶段
// 调用后传入 IntentRouter.Classify 的 recentIntents。
//
// audit trail（为何从 messages 而非持久化 intent 字段）：当前 ChatMessage 不存
// intent 标签（schema 未动），故从 message role/content 粗提——user 消息按规则层
// 跑一遍拿 intent，assistant 消息跳过（生成态 intent 无意义）。这是粗提，足够做
// "连续同类加权"信号；真持久化 intent 是后续 schema 优化项，不进本 spec。
func parseRecentIntents(messages []model.ChatMessage, rule *RuleIntentClassifier, session *model.ChatSession, mode ChatMode) []Intent {
	if rule == nil {
		return nil
	}
	var intents []Intent
	for _, msg := range messages {
		if !strings.EqualFold(msg.Role, "user") {
			continue
		}
		intent, _ := rule.Classify(msg.Content, session, mode, nil)
		intents = append(intents, intent)
	}
	return intents
}
