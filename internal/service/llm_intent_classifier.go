package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vid-lens/internal/ai"
)

// docs/architecture/retrieval.md: LLMIntentClassifier（LLM 兜底）。
//
// 规则层未短路（confidence < shortCircuitThreshold）时调用，独立低温廉价模型，
// 输出 (intent, confidence)。confidence<0.5 回退规则结果（CONTEXT.md 已定，
// 当前实现约束）。
//
// 不照搬 wali 三段式阈值/0.8 短路/二次提取全套（当前实现约束 "不 1:1 移植 wali"）：
//   - 用 vid-lens 自己的 prompt（当前实现约束），只问"这是哪类 intent + 多确信"，
//     不做 wali 式二次提取（指代消解走 Signal 无副作用正则，不走 LLM 二次提取）。
//   - 单次 LLM 调用，不级联多轮。
//
// 与规则层边界（当前实现约束 / CONTEXT.md）：规则层是"快、便宜、可能漏判"，
// LLM 兜底是"慢、贵、覆盖边界"。规则层短路省的就是这一层 LLM 调用（当前实现约束
// §4：省 LLM = 短路率派生结论）。

// LLMIntentClassifier 是 LLM 兜底 intent 分类器（docs/architecture/retrieval.md）。
type LLMIntentClassifier struct {
	chat ai.ChatClient
}

// NewLLMIntentClassifier 构造 LLM 兜底分类器。chat 为 nil 时 Classify 返回
// (IntentDirectQA, 0)——IntentRouter 会回退规则结果（当前实现约束）。
func NewLLMIntentClassifier(chat ai.ChatClient) *LLMIntentClassifier {
	return &LLMIntentClassifier{chat: chat}
}

// llmFallbackThreshold 是 LLM 结果回退规则结果的置信度门槛（当前实现约束）。
//
// audit trail（为何这么定）：0.5 — "半信半疑"线。LLM 给的 confidence 低于此
// 说明 LLM 自己也不确定，硬采信会把不确定错分带进 ExecutionPolicy 路由，不如
// 回退规则层的（至少基于显式关键词/Signal 的）判定。0.5 是 LLM 兜底可信度的
// 自然下限，与规则层 shortCircuitThreshold 0.75 形成两道闸：规则 ≥0.75 短路，
// LLM ≥0.5 采信，区间 [0.5,0.75) 走 LLM 兜底命中。
const llmFallbackThreshold = 0.5

// Classify 调 LLM 判 intent。返回 (intent, confidence)。
//
// LLM 不可用（chat nil）或返回不可解析 / confidence<llmFallbackThreshold 时，
// caller（IntentRouter）回退规则结果——本方法不自己回退，只产 LLM 判定，回退
// 决策在 router 统一表达（便于评测"LLM 兜底命中率"）。
func (c *LLMIntentClassifier) Classify(ctx context.Context, question string, recent []string) (Intent, float64, error) {
	if c == nil || c.chat == nil {
		return IntentDirectQA, 0, nil
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return IntentDirectQA, 0, nil
	}
	resp, err := c.chat.Chat(ctx, buildIntentClassifyMessages(q, recent))
	if err != nil {
		return IntentDirectQA, 0, err
	}
	intent, conf, err := parseIntentResponse(resp)
	if err != nil {
		return IntentDirectQA, 0, err
	}
	return intent, conf, nil
}

// buildIntentClassifyMessages 构造 LLM intent 分类 prompt（vid-lens 自己语义，
// 不照搬 wali 三段式，当前实现约束）。
//
// audit trail：prompt 只问"哪类 intent + 多确信"，要求严格 JSON 输出。不给
// 示例（避免 LLM 抄示例）、不做二次提取（指代消解走 Signal 正则，不走 LLM）。
// taxonomy 用 vid-lens 6 类（当前实现约束），不用 wali 的 DIAGNOSE/CONFIGURE。
func buildIntentClassifyMessages(question string, recent []string) []ai.ChatMessage {
	var recentText strings.Builder
	for i, msg := range recent {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			continue
		}
		fmt.Fprintf(&recentText, "%d. %s\n", i+1, msg)
	}
	userPrompt := fmt.Sprintf(`判断下面这条视频问答问题的 intent 类别，并给一个 0~1 的置信度。

intent 类别（vid-lens 视频 RAG 语义）：
- video_overview：问视频整体讲了什么/总结/概览
- direct_qa：精确事实问答（是什么/为什么/谁/哪些）
- topic_compare：跨视频对比（对比/比较/异同/区别）
- series_locate：在系列/多视频中定位某主题（哪些视频提到/哪期讲过）
- timeline_locate：定位到视频某一时间点（第几分钟/某个时刻/哪一段）
- small_talk：闲聊（你好/谢谢/在吗，与视频内容无关）

要求：
- 严格输出 JSON：{"intent":"<类别>","confidence":<0~1>}
- 只输出 JSON，不要解释、不要 markdown 代码块。
- 不确定时给较低 confidence，不要硬猜。

最近对话（可空，用于消歧指代）：
%s

当前问题：%s`, recentText.String(), question)

	return []ai.ChatMessage{
		{Role: "system", Content: "你是 VidLens 视频 RAG 的意图分类器。只输出 JSON。"},
		{Role: "user", Content: userPrompt},
	}
}

type parsedIntent struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
}

func parseIntentResponse(text string) (Intent, float64, error) {
	text = strings.TrimSpace(text)
	text = stripIntentCodeFence(text)
	var p parsedIntent
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		return IntentDirectQA, 0, fmt.Errorf("parse intent response: %w", err)
	}
	intent := normalizeLLMIntent(p.Intent)
	conf := p.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return intent, conf, nil
}

// normalizeLLMIntent 把 LLM 返回的 intent 串规整到 taxonomy，未识别归 direct_qa
// （最安全的检索+rerank+LLM，当前实现约束 不照搬 wali）。
func normalizeLLMIntent(s string) Intent {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "video_overview":
		return IntentVideoOverview
	case "direct_qa":
		return IntentDirectQA
	case "topic_compare":
		return IntentTopicCompare
	case "series_locate":
		return IntentSeriesLocate
	case "timeline_locate":
		return IntentTimelineLocate
	case "small_talk":
		return IntentSmallTalk
	default:
		return IntentDirectQA
	}
}

func stripIntentCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[idx+1:]
	}
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
