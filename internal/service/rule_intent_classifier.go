package service

import (
	"strings"

	"vid-lens/internal/model"
)

// Spec 05: RuleIntentClassifier（规则层，短路）。
//
// 三维打分（CONTEXT.md + 决策记录 §9 第 3 条，重新定维度不搬 wali 三段式）：
//  1. 关键词命中：扩 isVideoOverviewQuestion 的 contains 思路到全 taxonomy
//     （overview/direct_qa/topic_compare/series_locate/timeline_locate/small_talk）。
//  2. signal 模式：ExtractSignals 给的时间戳/比较/概览/闲聊结构化线索辅助分类。
//  3. 历史 intent 加权：会话最近 N 轮 intent 同类连续出现时提置信度。
//
// confidence 达阈值 → 短路返回，<1ms，不调 LLM。未达阈值由 IntentRouter 调
// LLMIntentClassifier 兜底（spec 05 line 30）。
//
// 阈值/权重 audit trail（决策记录 §4 硬约束"每个阈值写为什么这么定"）：
// 先填初始值 + 写理由框架；真实数值在固化 case 集调优后回填（spec line 80）。
// 见下方 scoreRuleIntent 各权重注释 + shortCircuitThreshold 注释。

// RuleIntentClassifier 是规则层 intent 分类器（spec 05）。
//
// 零外部依赖（不调 LLM、不查 DB），可被 IntentRouter 与测试直接构造。历史 intent
// 加权由 caller 传入 recentIntents（IntentRouter 从 chat_memory 解析），本结构不
// 自己取 Redis——保持纯函数便于测试与短路 <1ms。
type RuleIntentClassifier struct{}

// NewRuleIntentClassifier 构造规则层分类器。无配置参数：阈值/权重是常量（audit
// trail 在常量注释里），避免给线上注入"调一个数就能改短路率"的旋钮——调优走
// case 集回填常量 + 改注释，不走运行时配置。
func NewRuleIntentClassifier() *RuleIntentClassifier { return &RuleIntentClassifier{} }

// Classify 返回 (intent, confidence)。confidence ∈ [0,1]，达 shortCircuitThreshold
// 由 IntentRouter 短路（本层不自己短路，只产分数，短路决策在 router 统一表达，
// 便于"0.79 vs 0.81 短路"的边界 case 评测）。
//
// 入参 recentIntents 是会话最近 N 轮的 intent（caller 从 recent messages 解析，
// 可能为空）；session 决定 scope（KB → topic_compare/series_locate 倾向）。
func (c *RuleIntentClassifier) Classify(question string, session *model.ChatSession, mode ChatMode, recentIntents []Intent) (Intent, float64) {
	if c == nil {
		return IntentDirectQA, 0
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return IntentDirectQA, 0
	}

	// strict_rag 按定义必须检索：不产出 overview/small_talk/timeline（关检索或
	// 关 LLM 的 intent），强制归 direct_qa 语义。这是契约短路，置信度高。
	if mode == ChatModeStrictRAG {
		return IntentDirectQA, strictRAGConfidence
	}

	intent, confidence := c.scoreByRule(q, session, recentIntents)
	return intent, confidence
}

// shortCircuitThreshold 是规则层短路 LLM 兜底的置信度门槛。
//
// audit trail（为何这么定）：0.75 — 高于"半信半疑"（0.5）但留 25% 不确定空间
// 给 LLM 兜底。低于此值说明三维打分只有弱命中（如只有 1 个 signal 命中、无
// 关键词、无历史加权），交给 LLM 更稳。0.79 vs 0.81 边界 case 由固化集调优后
// 校准——初始 0.75 是"宁可多调一次 LLM 也别错分"的保守起点（spec line 80）。
const shortCircuitThreshold = 0.75

// 各维度权重（audit trail：为何这么分）。
//
// 关键词命中权重最高（0.5）：关键词是显式 intent 信号（"对比"/"总结"/"第15分钟"），
// 最可信。signal 模式次之（0.3）：结构化线索辅助但单独不够（"15:00" 可能是
// timeline 也可能是 direct_qa 提到时间点）。历史 intent 加权最弱（0.2）：会话
// 连续问同类 intent 提置信度，但用户随时可能切话题，不能高权重。
//
// 三维相加 = 1.0；单维满命中 ≈ 该维权重，三维同向满命中 = 1.0。shortCircuitThreshold
// 0.75 意味着至少关键词满（0.5）+ 另一维满（0.3）才稳短路，避免单维噪声误判。
const (
	weightKeyword = 0.5
	weightSignal  = 0.3
	weightHistory = 0.2
)

// strictRAGConfidence 是 strict_rag 模式强制 direct_qa 的置信度。
//
// audit trail：0.9 — strict_rag 契约短路是确定性判定（不是概率），给高置信度
// 直接短路 LLM；留 0.1 不为 1.0 是诚实标注"这是契约判定不是统计置信"。
const strictRAGConfidence = 0.9

// scoreByRule 是三维打分主体。返回得分最高的 intent 及其 confidence。
//
// 打分流程：对每个 taxonomy intent 算三维分（keyword + signal + history），
// 取最高分 intent。所有 intent 分都 < shortCircuitThreshold 时由 IntentRouter
// 兜底（本函数只产分数，不短路）。
func (c *RuleIntentClassifier) scoreByRule(q string, session *model.ChatSession, recentIntents []Intent) (Intent, float64) {
	sig := ExtractSignals(q)
	lq := strings.ToLower(q)

	// KB scope 倾向：跨视频会话把 topic_compare/series_locate 提权（KB 下问
	// 概览也是跨视频召回 = series_locate，不归 overview 关检索，与占位分类器一致）。
	kbScope := session != nil && session.ScopeType == model.ChatScopeKnowledgeBase

	scores := map[Intent]float64{
		IntentVideoOverview:  c.videoOverviewScore(lq, sig, kbScope),
		IntentDirectQA:       c.directQAScore(lq, sig),
		IntentTopicCompare:   c.topicCompareScore(lq, sig, kbScope),
		IntentSeriesLocate:   c.seriesLocateScore(lq, sig, kbScope),
		IntentTimelineLocate: c.timelineLocateScore(lq, sig),
		IntentSmallTalk:      c.smallTalkScore(lq, sig),
	}
	// 历史 intent 加权：同 intent 连续出现提置信度（决策记录 §9 第 3 条）。
	for intent, bump := range historyBump(recentIntents) {
		scores[intent] += bump
	}

	best := IntentDirectQA
	bestScore := 0.0
	// 确定顺序遍历保证平分时取 direct_qa（最安全的检索+rerank+LLM）。
	for _, intent := range []Intent{IntentDirectQA, IntentVideoOverview, IntentTopicCompare, IntentSeriesLocate, IntentTimelineLocate, IntentSmallTalk} {
		if scores[intent] > bestScore {
			best, bestScore = intent, scores[intent]
		}
	}
	if bestScore > 1.0 {
		bestScore = 1.0
	}
	return best, bestScore
}

// historyBump 计算历史 intent 加权分（≤ weightHistory）。
//
// audit trail：连续同 intent 出现 N 次，加权 = weightHistory * min(N/historyWindow, 1)。
// historyWindow=3：3 轮连续同类即满加权——少于 3 轮信号弱（用户可能只是连续问
// 两个 overview），多于 3 轮边际收益低。N 从 recentIntents 尾部往前数连续同类。
const historyWindow = 3

func historyBump(recentIntents []Intent) map[Intent]float64 {
	bumps := make(map[Intent]float64)
	if len(recentIntents) == 0 {
		return bumps
	}
	last := recentIntents[len(recentIntents)-1]
	n := 1
	for i := len(recentIntents) - 2; i >= 0 && recentIntents[i] == last; i-- {
		n++
	}
	if n > historyWindow {
		n = historyWindow
	}
	bumps[last] = weightHistory * float64(n) / float64(historyWindow)
	return bumps
}

// --- 各 intent 维度打分 ---

// 关键词词表（扩 isVideoOverviewQuestion 的 contains 思路到全 taxonomy）。
// audit trail：词表只收"几乎无歧义"的显式 intent 词，避免 over-claim 覆盖率。
// overview/compare/smallTalk 词表复用 signal_extract.go 的单一事实源（避免两份
// 平行词表漂移）；series/timeline 词表只规则层用（Signal 无对应 flag）。
var (
	seriesLocateKeywords = []string{
		"这些视频", "这组视频", "系列", "哪些视频", "哪个视频提到",
		"哪期", "哪一期",
	}
	timelineKeywords = []string{
		"第几分钟", "第几分", "几点", "哪个时间", "哪个时刻", "哪一段",
		"时间线", "时间轴",
	}
)

func (c *RuleIntentClassifier) videoOverviewScore(lq string, sig Signals, kbScope bool) float64 {
	if kbScope {
		// KB 概览 → 跨视频召回 = series_locate，不归 overview 关检索（与占位一致）。
		return 0
	}
	score := 0.0
	if containsAny(lq, overviewKeywords...) {
		score += weightKeyword
	}
	if sig.HasOverview {
		score += weightSignal
	}
	return score
}

func (c *RuleIntentClassifier) directQAScore(lq string, sig Signals) float64 {
	// direct_qa 是兜底 intent：无明显其他 intent 信号时该走它。给基础分让它在
	// 平票时胜出，但不给满权重——避免抢走真有 signal 的 intent。
	score := 0.0
	// 精确问答词（"是什么"/"为什么"/"谁"/"哪"）给弱关键词分。
	if containsAny(lq, []string{"是什么", "为什么", "怎么样", "如何", "谁", "什么是", "哪些"}...) {
		score += weightKeyword * 0.6
	}
	// 无 timeline/compare/overview/small_talk signal → direct_qa 是默认归宿，
	// 给 signal 维度一半分（"无反例信号"也是弱正信号）。
	if !sig.HasCompare && !sig.HasOverview && !sig.HasSmallTalk && len(sig.Timestamps) == 0 {
		score += weightSignal * 0.5
	}
	return score
}

func (c *RuleIntentClassifier) topicCompareScore(lq string, sig Signals, kbScope bool) float64 {
	score := 0.0
	if containsAny(lq, compareKeywords...) {
		score += weightKeyword
	}
	if sig.HasCompare {
		score += weightSignal
	}
	if kbScope {
		// KB scope 下 compare 倾向提权（跨视频对比是 KB 主用例）。
		score += weightKeyword * 0.4
	}
	return score
}

func (c *RuleIntentClassifier) seriesLocateScore(lq string, sig Signals, kbScope bool) float64 {
	score := 0.0
	if containsAny(lq, seriesLocateKeywords...) {
		score += weightKeyword
	}
	if kbScope {
		// KB 概览问法（"总结一下这些视频"）→ 跨视频召回 = series_locate。
		if sig.HasOverview {
			score += weightSignal
		}
		score += weightKeyword * 0.3
	}
	return score
}

func (c *RuleIntentClassifier) timelineLocateScore(lq string, sig Signals) float64 {
	score := 0.0
	if containsAny(lq, timelineKeywords...) {
		score += weightKeyword
	}
	// audit trail：时间戳 Signal 既给 signal 维度分，也给 keyword 维度分——
	// 显式秒数引用（"第15分钟"/"15:00"）就是 timeline intent 的最强信号，等价
	// 于关键词命中。避免与 overview 句式同现时（"第15分钟讲了什么"）被 overview
	// 抢走：timeline 拿 keyword+signal 满分，overview 拿 keyword+signal 满分，
	// 平票按 scoreByRule 顺序 direct_qa 优先... 故 timeline 须再加历史/KB 提权？
	// 不——更干净的做法：timeline 的时间戳是"定位到具体时刻"的唯一性信号，
	// 比 overview 的"整体概览"更具体，平票时更具体的 intent 胜出。这里给
	// timeline 一个 0.1 的"具体性提权"，让它在平票时压过 overview。
	if len(sig.Timestamps) > 0 {
		score += weightSignal
		if !containsAny(lq, timelineKeywords...) {
			// 时间戳但无 timeline 关键词：时间戳本身就是 keyword 级信号。
			score += weightKeyword
		}
		score += 0.1 // 具体性提权：定位到时刻 > 整体概览
	}
	return score
}

func (c *RuleIntentClassifier) smallTalkScore(lq string, sig Signals) float64 {
	score := 0.0
	if containsAny(lq, smallTalkKeywords...) {
		score += weightKeyword
	}
	if sig.HasSmallTalk {
		score += weightSignal
	}
	return score
}
