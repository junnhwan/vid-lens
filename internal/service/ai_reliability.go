package service

import (
	"errors"
	"sync/atomic"

	"vid-lens/internal/ai"
)

// docs/architecture/reliability.md（bullet ④）AI 调用功能性降级链。
//
// 降级链形态（当前实现约束）：
//   - 档0（正常）：rerank + LLM 都成 → 完整答案。
//   - 档1（rerank 失败 → 向量基线）：rerank 失败/超时 → 回退无 rerank 的向量检索结果
//     （= docs/architecture/retrieval.md 消融 vector_only 档），标 fallback，继续走 LLM。是降级链一环，不单立稀缺点。
//   - 档2（LLM 失败 → 无 LLM 模式）：LLM 失败/超时 → 回退"检索片段 + 已有摘要直拼 +
//     degraded:true"，不调 LLM。**这是 ④ 的稀缺点。**
//
// 前置诚信检查（当前实现约束硬约束）：无 LLM 模式 = 现有 buildRAGMessages 降级补全
// （补"不调 LLM + degraded 标志"路径），非从零新建生成路径。本包不新建 prompt 构造。
//
// 与 docs/architecture/retrieval.md ExecutionPolicy 的关系：ExecutionPolicy 决定"该不该走 LLM"（small_talk
// UseLLM=false 本来就不调 LLM），④ 决定"该走 LLM 但 LLM 挂了怎么办"。UseLLM=false 的
// intent 不触发档2（本来就不调 LLM，无所谓 LLM 失败降级）。
//
// 与 admission/quota 的协同（当前实现约束"BYOK/令牌桶折进④"）：AdmissionError +
// RetryAfter 在阈值内走重试，超阈值触发档2降级。admission.go / quota 令牌桶已有，本
// spec 不重建，只接降级链。

// 降级档标识，用于计数与可观测（当前实现约束确定：降级触发可观测只做计数不做 trace）。
const (
	degradationTier0Normal    = 0 // 正常档（rerank+LLM 都成）
	degradationTier1Rerank    = 1 // rerank 失败 → 向量基线
	degradationTier2LLM       = 2 // LLM 失败 → 无 LLM 模式
	// admissionRetryAfterCutoff：admission 拒配额 RetryAfter 超 5s 触发档2降级，
	// 5s 内由 caller 重试（admission 协同 docs/architecture/reliability.md）。本会话确定子决策（见 docs/architecture/reliability.md
	// 当前实现说明 "本会话确定的子决策" 1）：5s 是普通 LLM 请求重试等待的合理上限，
	// 超过则用户体验已显著劣化，不如给降级答案。未经真实流量标定，长期可调。
	admissionRetryAfterCutoff = 5
)

// 降级触发计数器（进程内，长期运行需接 metrics 落盘；本 spec 只保证计数可观测、
// 验收命令能跑出真实数字，当前实现约束"不许估算"）。违规触发次数留 __（长期采集）。
var (
	degradationTier1Triggers int64
	degradationTier2Triggers int64
)

// DegradationTier1Triggers 返回档1（rerank 失败→向量基线）触发次数。
func DegradationTier1Triggers() int64 { return atomic.LoadInt64(&degradationTier1Triggers) }

// DegradationTier2Triggers 返回档2（LLM 失败→无 LLM 模式）触发次数。
func DegradationTier2Triggers() int64 { return atomic.LoadInt64(&degradationTier2Triggers) }

func recordDegradationTier1() { atomic.AddInt64(&degradationTier1Triggers, 1) }
func recordDegradationTier2() { atomic.AddInt64(&degradationTier2Triggers, 1) }

// resetDegradationCountersForTest 重置降级计数器到 0（仅测试用，隔离用例）。
func resetDegradationCountersForTest() {
	atomic.StoreInt64(&degradationTier1Triggers, 0)
	atomic.StoreInt64(&degradationTier2Triggers, 0)
}

// DegradationLevel 表达一次问答最终落到哪一档降级，供 AskResult/ChatStreamEvent 携带
// degraded 标志（当前实现约束确定：API 响应带 degraded:true）。
type DegradationLevel int

const (
	// DegradationNone = 档0正常，rerank+LLM 都成。
	DegradationNone DegradationLevel = iota
	// DegradationRerankFallback = 档1：rerank 失败回退向量基线，但 LLM 仍生成。
	// 对外不标 degraded:true（答案仍是 LLM 生成的完整答案，只是检索用了向量基线）。
	DegradationRerankFallback
	// DegradationLLMUnavailable = 档2：LLM 失败，回退片段+摘要直拼，不调 LLM。
	// 对外标 degraded:true（当前实现约束）。
	DegradationLLMUnavailable
)

// Degraded 对外是否标 degraded:true。只有档2（LLM 失败→无 LLM 模式）对外标降级态：
// 档1 rerank 失败回退向量基线后 LLM 仍生成完整答案，用户感知不到降级，故不标。
// 当前实现约束"显式告知降级态"针对的是档2的"参考片段无生成"。
func (l DegradationLevel) Degraded() bool { return l == DegradationLLMUnavailable }

// shouldTriggerLLMDegradation 判定一次 LLM 失败是否触发档2降级。
//
// 触发条件（当前实现约束 + docs/architecture/reliability.md admission 协同）：
//   - policy.UseLLM=true（该走 LLM 的 intent 才谈 LLM 失败降级；small_talk UseLLM=false
//     本来就不调 LLM，不误触发，docs/architecture/reliability.md 行为约束 14）。
//   - LLM 返回错误（含 admission 拒配额：RetryAfter 超阈值即触发档2，阈值内由 caller 重试）。
//
// admission 协同：admission.go 的 AdmissionError + RetryAfter，RetryAfter 在阈值内走重试、
// 超阈值触发档2（docs/architecture/reliability.md 当前实现约束: admission 协同）。本函数只判定"给定
// 失败是否该降级"，重试由 Ask 层负责。
func shouldTriggerLLMDegradation(policy ExecutionPolicy, llmErr error) bool {
	if !policy.UseLLM {
		// 本来就不调 LLM 的 intent（small_talk）不触发档2：没有"LLM 失败"可言。
		return false
	}
	if llmErr == nil {
		return false
	}
	// admission 拒配额：RetryAfter 在阈值内 → 不降级，由 caller 重试（admission 协同
	// docs/architecture/reliability.md）。只有超阈值才触发档2（不再等恢复，给降级答案）。非 admission 的普通
	// LLM 错误（超时/5xx/网络）→ 直接触发档2功能性降级（当前实现约束稀缺点）。
	if after := admissionRetryAfter(llmErr); after > 0 {
		return after > admissionRetryAfterCutoff
	}
	return true
}

// admissionRetryAfter 从错误链里提取 RetryAfter（秒）。0 表示无 RetryAfter 语义。
// 同时覆盖两类携带 RetryAfter 的错误（docs/architecture/reliability.md admission 协同一致性）：
//   - *ai.AdmissionError：admission.go 令牌桶拒配额（Decision.RetryAfter）。
//   - *ai.ProviderError：provider_error.go 的 HTTP 429 等限流响应（RetryAfter）。
//
// 两者都用同一个 admissionRetryAfterCutoff 阈值判定"阈值内重试、超阈值触发档2"，
// 避免 429 短 RetryAfter 误触发档2（应为重试路径，docs/architecture/reliability.md 行为约束 7）。
func admissionRetryAfter(err error) int {
	var ae *ai.AdmissionError
	if errors.As(err, &ae) && ae != nil {
		return int(ae.Decision.RetryAfter.Seconds())
	}
	var pe *ai.ProviderError
	if errors.As(err, &pe) && pe != nil && pe.RetryAfter > 0 {
		return int(pe.RetryAfter.Seconds())
	}
	return 0
}

// firstRerankFallback 在 rerank 后的 citations 上找 rerank 失败 fallback 标记。
// 复用 rag_expand.go 的 Fallbacks 范式（docs/architecture/reliability.md 当前实现约束）：
// ModelReranker 失败时通过 fallbackRerankOrder 在 chunk 上标 model_rerank_failed
// 或 model_rerank_unavailable。返回非空即档1降级已触发。
func firstRerankFallback(citations []RetrievedChunk) string {
	for _, c := range citations {
		for _, fb := range c.Fallbacks {
			if fb == "model_rerank_failed" || fb == "model_rerank_unavailable" {
				return fb
			}
		}
	}
	return ""
}

