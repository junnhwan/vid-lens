package service

import "vid-lens/internal/model"

// Spec 04 (A段): intent → 检索/生成参数映射值对象。
//
// ExecutionPolicy 把"这次提问该不该检索、检索多大范围、要不要 rerank、要不要
// rewrite、是否走 LLM、scope 是单视频还是集合"这一组原本散落在 prepareChatByMode
// / prepareRAGChat 里的 if 硬编码，统一成一个值对象（CONTEXT.md: ExecutionPolicy）。
//
// 它不新建检索参数：所有字段都映射到现有 RAGRetrievalConfig（rag_eval_config.go）
// 的 EnableVector/EnableBM25/RewriteQueries/TopK/RerankerMode 等。ExecutionPolicy 只是
// 这些 Config 字段的生产方（spec 04 Implementation Decisions: "ExecutionPolicy 是
// Config 的生产方，不是重建检索"）。
//
// intent taxonomy 取 spec 01 共享 query 池的 category 取值（spec 01 已拍板 category
// 复用为 intent 标签，schema 不动）。真分类器是 spec 05 的事；本包用 placeholder
// 分类器 classifyIntentPlaceholder（现有 isVideoOverviewQuestion + session.ScopeType）
// 把路由打通，spec 05 落地后替换占位为真分类器。

// Intent 是一次用户提问被识别出的处理类别（CONTEXT.md）。spec 01 共享 query 池的
// category 取值复用为 intent 标签，不另立字段。用命名类型而非裸 string，让编译器
// 抓 PolicyFor(intent, scope) 的参数顺序互换。
type Intent string

// Scope 是一次问答检索的范围档（CONTEXT.md）：单视频或集合。
type Scope string

const (
	// ScopeVideo: 检索过滤到会话当前绑定的单个 Asset。
	ScopeVideo Scope = "video"
	// ScopeCollection: 检索过滤到会话绑定的 KnowledgeBase 内 video_ids（跨视频）。
	ScopeCollection Scope = "collection"
)

// Intent taxonomy（spec 01 共享 query 池的 category 取值，复用为 intent 标签）。
const (
	IntentVideoOverview  Intent = "video_overview"  // 概览类问题，关检索走视频上下文直拼
	IntentDirectQA       Intent = "direct_qa"       // 精确问答，开检索 + rerank
	IntentTopicCompare   Intent = "topic_compare"  // 跨视频对比，Scope=collection
	IntentSeriesLocate   Intent = "series_locate"  // 跨视频系列定位，Scope=collection
	IntentTimelineLocate Intent = "timeline_locate" // 时间线定位，开检索 + Signal 时间戳过滤（Signal 提取留 spec 05）
	IntentSmallTalk      Intent = "small_talk"     // 闲聊，关检索关 LLM 直答（占位，真分类器在 spec 05）
)

// ExecutionPolicy: intent → 检索/生成参数映射值对象（CONTEXT.md）。
type ExecutionPolicy struct {
	// Retrieve 控制是否走向量/关键词检索。
	Retrieve bool
	// TopK 是检索返回的引用数上限（映射到 RAGRetrievalConfig.TopK）。caller 传入的
	// topK 超过 MaxRetrievalTopK 时被截到该上限——该散落判定（原 prepareRAGChat 的
	// topK>10→10）由 policy 层统一表达，见 ClampTopK。
	TopK int
	// Rerank 控制是否对候选做 rerank（false → RerankerModeNone，true → 保留已配置档）。
	Rerank bool
	// Rewrite 是 rewrite 查询数（>1 触发 LLM rewrite，映射到 RewriteQueries）。
	Rewrite int
	// UseSummary 控制是否走视频摘要/转写直拼而非检索（overview/兜底路径）。
	UseSummary bool
	// UseLLM 控制是否调用 LLM 生成答案（small_talk=false 留接口位，真分类器在 spec 05）。
	UseLLM bool
	// Scope 是检索范围档：ScopeVideo（会话绑 task）或 ScopeCollection（会话绑 KnowledgeBase）。
	Scope Scope
}

// MaxRetrievalTopK 是单次问答引用片段数的硬上限（原 prepareRAGChat 的 topK>10→10）。
// 超过会让 LLM prompt 噪声上升、用户难以核对证据；集中到 policy 层，消掉散落 if。
const MaxRetrievalTopK = 10

// ClampTopK 把 caller 传入的 topK 规整到 [policy.TopK 默认, MaxRetrievalTopK]。
// topK<=0 → policy 默认；topK>MaxRetrievalTopK → 截到上限。原 prepareRAGChat 两段
// 散落 if（默认值 + 上限）由本函数统一表达（spec 04 数字占位符 A段）。
func (p ExecutionPolicy) ClampTopK(topK int) int {
	if topK <= 0 {
		topK = p.TopK
	}
	if topK > MaxRetrievalTopK {
		topK = MaxRetrievalTopK
	}
	return topK
}

// PolicyFor 返回 intent + scope → ExecutionPolicy 的映射。
//
// 一个 switch 消掉 prepareChatByMode 的散落 if（决策记录第 4 节硬约束）。
// 每档参数选择的 audit trail 写在 case 注释里（反 overclaim：为什么 overview 关检索、
// direct_qa 开 rerank、topic_compare scope=collection，以及每个数值为何这么定——
// 决策记录 §4 硬约束"每个阈值必须写为什么这么定"）。
func PolicyFor(intent Intent, scope Scope) ExecutionPolicy {
	switch intent {
	case IntentVideoOverview:
		// overview 类问题（"讲了什么/总结一下"）的答案来自视频整体摘要/转写，
		// 向量检索按片段切，召回的是局部片段而非整体概览，开了浪费检索预算。
		// 故关检索、走 UseSummary 直拼、仍走 LLM 做概览生成。
		// TopK/Rewrite/Rerank 在关检索路径下不消费，留零值。
		return ExecutionPolicy{
			Retrieve:   false,
			UseSummary: true,
			UseLLM:     true,
			Scope:      scope,
		}
	case IntentDirectQA:
		// 精确问答靠片段证据，必须检索；rerank 把最相关片段顶到前面，避免 LLM
		// 被噪声片段带偏（productionRetrievalConfig 默认 deterministic rerank）。
		return directQAPolicy(scope)
	case IntentTopicCompare, IntentSeriesLocate:
		// 跨视频对比/系列定位必须放大检索范围到集合内 video_ids。
		// BM25 在多 task 下不支持（rag_pipeline.go 检测 len(taskIDs)!=1 报错），
		// 故集合档关 BM25、纯向量。recent 历史关断（KB member-safe，见 prepareRAGChat）。
		return directQAPolicy(ScopeCollection)
	case IntentTimelineLocate:
		// 时间线定位类问题靠检索 + Signal 时间戳过滤缩范围。Signal 提取（时间戳
		// 正则等）是 spec 05 的事，本 spec 只留接口位：Retrieve 开、参数同 direct_qa，
		// Signal 过滤在 spec 05 接到 Retrieve 路径后补。
		return directQAPolicy(scope)
	case IntentSmallTalk:
		// 闲聊不烧检索 + 生成。占位分类器当前不会产出此 intent（见
		// classifyIntentPlaceholder），policy 留接口位，真分类器（spec 05）落地后启用。
		return ExecutionPolicy{
			Retrieve: false,
			UseLLM:   false,
			Scope:    scope,
		}
	default:
		// 未知 intent 走 direct_qa 语义（最安全的"检索 + rerank + LLM"）。
		return PolicyFor(IntentDirectQA, scope)
	}
}

// directQAPolicy 是"检索 + rerank + LLM"档的共享构造（direct_qa / topic_compare /
// series_locate / timeline_locate 共用，消掉 PolicyFor 里三份近似 struct 字面量）。
//
// audit trail（决策记录 §4 硬约束，每个阈值写理由）：
//   - TopK=5：单次问答引用超过 5 条片段会让 LLM prompt 噪声上升、用户难以核对；
//     5 是 productionRetrievalConfig 默认值，与线上 wiring 一致。prepareRAGChat 的
//     topK>10→10 上限是防 caller 传过大值打爆 prompt，5 在上限内不受影响。
//   - Rewrite=3：query rewrite 多表述同义问法提升召回；>3 收益递减且每次 rewrite
//     多一轮 embedding+检索成本，3 是成本/召回平衡点，与 productionRetrievalConfig
//     默认 RewriteQueries=3 一致。
//   - Rerank=true：rerank 把最相关片段顶到前位，避免 LLM 被噪声片段带偏；
//     具体档（deterministic/model）由 productionRetrievalConfig 决定，policy 只控开关。
func directQAPolicy(scope Scope) ExecutionPolicy {
	return ExecutionPolicy{
		Retrieve: true,
		TopK:     5,
		Rerank:   true,
		Rewrite:  3,
		UseLLM:   true,
		Scope:    scope,
	}
}

// classifyIntentPlaceholder 是 spec 04 A段的占位分类器。
//
// Deprecated: spec 05 已落地 RuleIntentClassifier + LLMIntentClassifier 级联
// （IntentRouter）。本函数仅作降级 fallback（IntentRouter 为 nil 时）与测试桩
// 保留——生产路径走 ChatService.classifyIntent → IntentRouter.Classify。删除风险：
// 多个 spec 04 测试直接调用本函数断言占位行为，删它需同步改测试。
//
// 用现有 isVideoOverviewQuestion（概览句式命中）+ session.ScopeType（KnowledgeBase
// → 跨视频 intent）作 intent 占位分类，把 ExecutionPolicy 路由打通。
//
// 占位边界（用户已拍板）：KB / strict_rag 模式不产出 small_talk（仍走检索），
// small_talk 仅 video scope 单视频下留接口位，避免破坏 strict_rag 必须检索的契约。
func classifyIntentPlaceholder(question string, session *model.ChatSession, mode ChatMode) Intent {
	// strict_rag 模式按定义必须检索（不产出 overview 关检索 / small_talk），
	// 必须在概览句式判定之前短路，否则 strict 下的"总结一下"会被归 overview 关检索，
	// 破坏 strict_rag fail-closed 契约。
	if mode == ChatModeStrictRAG {
		return IntentDirectQA
	}
	// 跨视频集合会话：问对比/系列定位。KB 会话的概览问法（"总结一下"）在 KB 下
	// 仍应走跨视频检索（集合概览 = 跨视频召回），不归 overview 关检索。
	if session != nil && session.ScopeType == model.ChatScopeKnowledgeBase {
		if isVideoOverviewQuestion(question) {
			return IntentSeriesLocate
		}
		return IntentTopicCompare
	}
	// 单视频：概览句式命中 → overview（关检索走摘要/转写直拼）。
	if isVideoOverviewQuestion(question) {
		return IntentVideoOverview
	}
	return IntentDirectQA
}

// scopeOfSession 把 session.ScopeType 映射到 ExecutionPolicy.Scope 档。
func scopeOfSession(session *model.ChatSession) Scope {
	if session != nil && session.ScopeType == model.ChatScopeKnowledgeBase {
		return ScopeCollection
	}
	return ScopeVideo
}
