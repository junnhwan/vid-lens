package service

import (
	"context"
	"strings"
	"sync/atomic"
)

// docs/architecture/retrieval.md（bullet ⑨）轻量证据约束生成。
//
// 当前实现约束 确定"轻量"：不做完整 Planner-Executor-Critic Agent Loop
// （撞车 DOVideo + 工作量大），只在 LLM 生成后加一个单轮约束：
//
//   - 校验答案里 [Cx] 引用编号是否在本次检索集 evidence id 范围内（复用现有
//     Citation.EvidenceID + rag_evidence.go 的引用解析）。
//   - 在范围内 → 保留绑定，正常 finalizeAnswerCitations。
//   - 超范围（编造的编号 / 没检索到的片段）→ 标记违规 → 触发单轮重检索补证据
//     （有上限，至多一次，不无限循环、不多轮 ReAct）。
//   - 重检索后仍无证据 → 标注"无证据支撑"拒绝输出该结论。
//
// 与 ④ 正交（当前实现约束）：⑨ 是 LLM 可用时的约束，④ 是 LLM 不可用时的降级。
// LLM 可用 → 走 ⑨；LLM 不可用 → 走 ④。两者不冲突（docs/architecture/retrieval.md 当前实现说明 "与 ④ 正交"）。
//
// 不接 video_agent 实验路径（agent 不进产品默认，⑨ 是单轮约束不撞车）。

// maxEvidenceReRetrieval 是超范围违规后允许的重检索次数上限（docs/architecture/retrieval.md user
// story 13"重检索有上限不无限循环"）。1 = 单轮校验 + 至多一次重检索（当前实现约束
// §9.1 "轻量"）。该值固定为常量而非配置，避免被改成 >1 退化成多轮 ReAct
// （docs/architecture/retrieval.md 当前范围边界"不做多轮 ReAct"）。
const maxEvidenceReRetrieval = 1

// 证据约束链档数（docs/architecture/retrieval.md 待评测指标）：校验 + 重检索 = 2（单轮轻量）。
const evidenceConstraintChainTiers = 2

// evidenceConstraintOutcome 描述一次证据约束校验的结果。
type evidenceConstraintOutcome struct {
	// answer 最终交付的答案体。可能等于 LLM 原始答案（无违规 / 违规但重检索补到证据后
	// 保留），也可能是去掉超范围引用后的清洗答案，或"无证据支撑"拒绝标注。
	answer string
	// citations 最终绑回的引用集。
	citations []Citation
	// violated 是否检测到超范围引用（无论重检索后是否补到）。
	violated bool
	// reretrieved 是否触发了重检索（only true when violation 触发且 re-retrieval 实际执行）。
	reretrieved bool
	// unsupported 是否有结论被标注"无证据支撑"拒绝输出（重检索后仍无证据）。
	unsupported bool
}

// 证据约束触发计数器（进程内，长期运行需接 metrics 落盘；本 spec 只保证计数可观测，
// 违规/重检索次数留 __ 长期采集，docs/$1"不许估算"）。复用 docs/architecture/reliability.md 降级
// 计数的命名范式（ai_reliability.go）。
var (
	evidenceViolationTriggers    int64
	evidenceReretrievalTriggers  int64
	evidenceUnsupportedTriggers  int64
)

// EvidenceViolationTriggers 返回超范围引用违规触发次数（长期采集，docs/architecture/retrieval.md 待评测指标）。
func EvidenceViolationTriggers() int64 { return atomic.LoadInt64(&evidenceViolationTriggers) }

// EvidenceReretrievalTriggers 返回重检索触发次数（长期采集，docs/architecture/retrieval.md 待评测指标）。
func EvidenceReretrievalTriggers() int64 { return atomic.LoadInt64(&evidenceReretrievalTriggers) }

// EvidenceUnsupportedTriggers 返回重检索后仍无证据、标注"无证据支撑"的次数（长期采集）。
func EvidenceUnsupportedTriggers() int64 { return atomic.LoadInt64(&evidenceUnsupportedTriggers) }

// resetEvidenceConstraintCountersForTest 重置证据约束计数器到 0（仅测试用，隔离用例）。
func resetEvidenceConstraintCountersForTest() {
	atomic.StoreInt64(&evidenceViolationTriggers, 0)
	atomic.StoreInt64(&evidenceReretrievalTriggers, 0)
	atomic.StoreInt64(&evidenceUnsupportedTriggers, 0)
}

// enforceEvidenceConstraint 是 Ask / AskStream 共用的证据约束入口（docs/architecture/retrieval.md）。
//
// 在 LLM 成功生成后、finalizeAnswerCitations 之前调用。它复用 finalizeAnswerCitations
// 的引用解析范式（rag_evidence.go）解析 [Cx] 编号，校验是否在检索集 evidence id
// 范围内。违规触发单轮重检索补证据；重检索后仍无证据 → 标注"无证据支撑"。
//
// 复用现有 seam（docs/architecture/retrieval.md 当前实现约束"复用现有 seam，不重建"）：
//   - Citation + EvidenceID（chat.go）：已有证据绑定，⑨ 只校验不重建。
//   - finalizeAnswerCitations（rag_evidence.go）：答案后处理已有，⑨ 接它之前加校验。
//   - prompt [C1][C2] 引用要求（chat_messages.go）：已有，⑨ 解析它。
//   - docs/architecture/retrieval.md 检索链路（rag_pipeline.Retrieve）：超范围重检索复用它。
//
// 轻量边界（反 Agent Loop，docs/architecture/retrieval.md 当前范围边界）：
//   - 单轮校验 + 至多一次重检索（maxEvidenceReRetrieval=1），不无限循环。
//   - 不做 Planner-Executor-Critic 多轮 ReAct。
//   - 不接 video_agent 实验路径。
//
// re-retriever 注入重检索能力（docs/architecture/retrieval.md Retrieve）。生产路径 = s.newRetrievalPipeline +
// Retrieve；测试路径用 fake re-retriever 控制证据补全（docs/architecture/retrieval.md 测试约定：
// fake LLM 注入超范围引用，断言约束链）。
func (s *ChatService) enforceEvidenceConstraint(
	ctx context.Context,
	prepared *preparedRAGChat,
	llmAnswer string,
	reRetrieve func(ctx context.Context, query string) ([]RetrievedChunk, error),
) evidenceConstraintOutcome {
	if prepared == nil {
		return evidenceConstraintOutcome{answer: llmAnswer, citations: nil}
	}

	// 引用映射，用于判断超范围。即使 LLM 引用范围都合法，也走 finalizeAnswerCitations
	// 的标准清洗（去 token + 选 referenced citations）。
	evidenceRange := prepared.evidenceIDSet()
	inRange := func(evidenceID string) bool {
		if evidenceID == "" {
			return false
		}
		_, ok := evidenceRange[evidenceID]
		return ok
	}

	// 违规引用映射；空 = 无违规。
	violationRefs := collectViolationEvidenceIDs(llmAnswer, prepared.Citations, inRange)
	if len(violationRefs) == 0 {
		// 无违规：走标准 finalize（保留绑定）。
		finalized := finalizeAnswerCitations(llmAnswer, prepared.Citations)
		return evidenceConstraintOutcome{
			answer:     finalized.Answer,
			citations:  finalized.Citations,
			violated:   false,
			reretrieved: false,
			unsupported: false,
		}
	}

	// 违规：计一次违规触发（docs/architecture/retrieval.md 行为约束 11"证据约束可观测"）。
	atomic.AddInt64(&evidenceViolationTriggers, 1)

	// 单轮重检索补证据（docs/architecture/retrieval.md Solution 2"超范围处理：被拒→重检索"）。
	// 以违规引用涉及的 LLM 原始答案片段作为重检索 query——LLM 既然引用了某编号说明
	// 它认为那里有相关内容，用 LLM 的原始答案（去 [Cx] token 后）作 query 比用原
	// 问题更贴合"补这条结论的证据"语义。
	reretrievalQuery := stripCitationTokens(llmAnswer)
	if reretrievalQuery == "" {
		reretrievalQuery = prepared.Question
	}

	var reretrieved []RetrievedChunk
	if reRetrieve != nil {
		atomic.AddInt64(&evidenceReretrievalTriggers, 1)
		chunks, err := reRetrieve(ctx, reretrievalQuery)
		if err == nil {
			reretrieved = chunks
		}
	}

	// 重检索补证据（docs/architecture/retrieval.md Solution 2"超范围处理：被拒→重检索"）。
	//
	// 关键决策（spec review A1/C1 修复）："补到证据"的判据 = 重检索是否带回【原检索集
	// 没有的新 evidence id】，而不是 len(finalized.Citations)>0。后者会被 finalizeAnswerCitations
	// 的 fallback 回填（rag_evidence.go fallbackCitations）永远撑到 ≥1，导致"无证据支撑"分支
	// 不可达——spec Solution 2"重检索后仍无证据"指的是没有支撑该结论的新证据，不是 chunk 数为 0。
	//
	// 诚实语义：重检索带回原检索集没有的新证据 = 命中 LLM 结论遗漏的支撑 → 保留结论，
	// 用并入候选集走标准 finalize 绑回引用；重检索带回的全是原检索集已有（重复）或空 =
	// 没补到新证据 → 标"无证据支撑"。
	newEvidence := reretrievalNewEvidenceIDs(reretrieved, evidenceRange)
	if len(newEvidence) > 0 {
		merged := append([]RetrievedChunk(nil), prepared.Contexts...)
		seen := make(map[string]struct{}, len(merged))
		for _, c := range merged {
			seen[c.EvidenceID] = struct{}{}
		}
		for _, c := range reretrieved {
			if c.EvidenceID != "" {
				if _, dup := seen[c.EvidenceID]; dup {
					continue
				}
				seen[c.EvidenceID] = struct{}{}
			}
			merged = append(merged, c)
		}
		mergedContexts, mergedCitations := buildCitationSet(prepared.Question, merged)
		finalized := finalizeAnswerCitations(llmAnswer, mergedCitations)
		// 侧效：把并入证据写回 prepared，让后续 saveChatExchange 的快照含新证据。
		prepared.Contexts = mergedContexts
		prepared.Citations = mergedCitations
		return evidenceConstraintOutcome{
			answer:      finalized.Answer,
			citations:   finalized.Citations,
			violated:    true,
			reretrieved: true,
			unsupported: false,
		}
	}

	// 重检索后仍无证据（reretrieved 为空 / 全是原检索集已有的重复）→ 标注"无证据支撑"
	// 拒绝输出该结论（docs/architecture/retrieval.md Solution 2 + 行为约束 3）。
	atomic.AddInt64(&evidenceUnsupportedTriggers, 1)
	rejected := buildUnsupportedAnswer(llmAnswer, violationRefs)
	// 无证据支撑的结论不绑回任何 Citation（拒绝输出该结论）。
	return evidenceConstraintOutcome{
		answer:      rejected,
		citations:   []Citation{},
		violated:    true,
		// reretrieved 计一次重检索执行（即便没补到新证据，重检索确实跑了）；与
		// evidenceReretrievalTriggers 计数对齐（spec review C2：flag 与 counter 一致）。
		reretrieved: reRetrieve != nil,
		unsupported: true,
	}
}

// reretrievalNewEvidenceIDs 返回重检索结果中 evidence id 不在 originalRange（原检索集）
// 的集合。空 = 重检索没带回任何新证据（重复或空），spec Solution 2"仍无证据"信号。
func reretrievalNewEvidenceIDs(reretrieved []RetrievedChunk, originalRange map[string]struct{}) []string {
	if len(originalRange) == 0 {
		// 原检索集空（不应发生——LLM 引用必基于非空检索集），任何非空重检索都算新证据。
		newIDs := make([]string, 0, len(reretrieved))
		for _, c := range reretrieved {
			if id := strings.TrimSpace(c.EvidenceID); id != "" {
				newIDs = append(newIDs, id)
			}
		}
		return newIDs
	}
	var newIDs []string
	for _, c := range reretrieved {
		id := strings.TrimSpace(c.EvidenceID)
		if id == "" {
			continue
		}
		if _, dup := originalRange[id]; dup {
			continue
		}
		newIDs = append(newIDs, id)
	}
	return newIDs
}

// collectViolationEvidenceIDs 解析 LLM 答案里的 [Cx] 引用，返回超范围（编造/没检索到）
// 的 evidence id 集合。复用 rag_evidence.go 的 token 解析（extractCitationTokenRanges
// + collectReferencedCitationIDs）——⑨ 只校验不重建解析逻辑。
//
// inRange 判定一条 evidence id 是否在检索集范围内（caller 注入，便于测试 fake）。
func collectViolationEvidenceIDs(answer string, candidates []Citation, inRange func(string) bool) []string {
	// 复用 parseReferencedCitationIDs（rag_evidence.go），与 finalizeAnswerCitations
	// 走同一套 token 解析，避免重复实现（代码复用约束 parse pipeline）。
	referenced := parseReferencedCitationIDs(answer)

	// candidates 是检索集对应的 Citation（CitationID 是 C1..Cn，EvidenceID 是真实证据）。
	// LLM 引用的 [Cx] 映射到 candidates[x-1].EvidenceID；超 x 范围 = 编造编号。
	violations := make([]string, 0)
	seen := make(map[string]struct{})
	for ref := range referenced {
		evidenceID := evidenceIDForCitationRef(ref, candidates)
		if evidenceID == "" {
			// LLM 引用了 Cn+ 之类的编号，candidates 里没有 = 编造编号。
			violations = appendMissing(violations, seen, ref)
			continue
		}
		if !inRange(evidenceID) {
			violations = appendMissing(violations, seen, ref)
		}
	}
	return violations
}

// evidenceIDForCitationRef 把 LLM 引用的 "C1" / "C3" 映射到 candidates 对应位置的
// EvidenceID。返回空串 = LLM 引用了超出 candidates 长度的编号（编造）。
func evidenceIDForCitationRef(ref string, candidates []Citation) string {
	idx := parseCitationIndex(ref)
	if idx <= 0 || idx > len(candidates) {
		return ""
	}
	return candidates[idx-1].EvidenceID
}

// parseCitationIndex 把 "C1" 解析成 1；非正整数返回 0。复用 normalizeCitationID
// 的格式约束（C 后跟数字），避免引入第二套解析。
func parseCitationIndex(ref string) int {
	normalized, ok := normalizeCitationID(ref)
	if !ok || len(normalized) < 2 {
		return 0
	}
	n := 0
	for _, r := range normalized[1:] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func appendMissing(slice []string, seen map[string]struct{}, v string) []string {
	if _, ok := seen[v]; ok {
		return slice
	}
	seen[v] = struct{}{}
	return append(slice, v)
}

// stripCitationTokens 去掉答案里的 [Cx] 引用 token，得到纯文本作为重检索 query。
// 复用 rag_evidence.go 的 stripCitationTokensVisible（spec review: Middle Man —
// 不再以 nil 候选调 finalizeAnswerCitations 取副作用，而是命名清晰的可复用函数）。
func stripCitationTokens(answer string) string {
	return strings.TrimSpace(stripCitationTokensVisible(answer))
}

// buildUnsupportedAnswer 构造"无证据支撑"拒绝标注的答案体（docs/architecture/retrieval.md Solution 2
// "重检索后仍无证据 → 标注'无证据支撑'拒绝输出该结论"）。
//
// 诚实拒绝而非硬编：保留 LLM 原始答案的可见文本（去 [Cx] token），但在末尾标注
// 哪些结论无证据支撑，让用户能核对而不是悄悄吞掉。无证据的结论不绑回 Citation。
func buildUnsupportedAnswer(llmAnswer string, violationRefs []string) string {
	visible := stripCitationTokens(llmAnswer)
	if visible == "" {
		visible = llmAnswer
	}
	if len(violationRefs) == 0 {
		return visible
	}
	// 拒绝标注：列出无证据支撑的引用编号。不暴露 evidence_id 等内部术语。
	parts := make([]string, 0, len(violationRefs)+1)
	parts = append(parts, visible)
	parts = append(parts, "\n\n[以下结论无证据支撑，已拒绝输出："+strings.Join(violationRefs, "、")+"]")
	return strings.Join(parts, "")
}
