# Spec 06: AI 调用可靠性层（bullet ④）

> 推进顺序第六份。决策母本见 `docs/specs/00-refactor-decisions.md`（第 6 节 ④ 降级档位 + 第 10 节无 LLM 模式呈现）；领域语言见 `CONTEXT.md`（BYOK Profile）。
> 依赖：spec 04 (B) 检索链路稳定（降级基线 = 消融中 `vector_only` 档 = 无 rerank 的检索）。spec 04 A 段已建 ExecutionPolicy，本 spec 的降级在它之上。

## Problem Statement

VidLens 的 AI 调用（LLM 摘要/RAG 生成、ASR、embedding、rerank）依赖外部服务，会挂、会超时、会拒配额。现在的处理有两个真问题：

1. **LLM 失败没有功能性降级，请求直接废**。LLM 摘要/生成失败时，整个问答请求返回错误——用户拿不到任何东西。但检索其实已经跑完、chunk 已经拿到、摘要可能也有（spec 03 内容去重已能复用已有摘要）。决策记录第 6 节稀缺点 = "档2功能性降级：LLM 失败 → 回退'无 LLM 模式'（检索片段 + 摘要直拼，标注'参考片段无生成'）"。现在没做这个档——LLM 一挂，前面检索的功夫全废，请求全错。这是普通熔断（等恢复）和功能性降级（换档交付）的区别：普通熔断是"挂着等"，功能性降级是"挂着也给你降级答案"。④ 的稀缺点是后者。

2. **rerank 失败降级有雏形但不显式成档1**。`rag_pipeline.go` rerank 失败有 `Fallbacks` 标记（`rag_expand.go` 的 fallback 机制），但没有显式"rerank 失败 → 回退向量基线"的档1降级链。决策记录第 6 节"档1（rerank 失败→向量基线）作为降级链的一环，不单独作为稀缺点"——档1是降级链的一环，要接进档2的链，不是单独卖点。

痛点从用户视角：作为维护者，我没法诚实回答"LLM 服务挂了你怎么办"——现在答"请求失败"，但诚实答案是"应该给你降级答案（检索片段+已有摘要）而不是全废"。决策记录第 6 节稀缺点 = "系统有意识设计两个交付档"，现在只有一档（全成或全废）。

## Solution

建功能性降级链（档1→档2），复用现有 admission/quota + rerank fallback + spec 03 内容去重，不重建：

### 降级链

1. **档0（正常）**：rerank 成功 + LLM 生成成功 → 完整答案。
2. **档1（rerank 失败 → 向量基线）**：rerank 失败/超时 → 回退无 rerank 的向量检索结果（= 消融 `vector_only` 档，spec 04 已定基线指向）。标记 fallback。继续走 LLM 生成。这是降级链一环，不单立稀缺点。
3. **档2（LLM 失败 → 无 LLM 模式）**：LLM 生成失败/超时 → 回退"检索片段 + 已有摘要（spec 03 内容去重复用）直拼"，标注 `degraded: true`。不调 LLM。**这是 ④ 的稀缺点。**

### 前置诚信检查（决策记录第 6 节硬约束）

- spec 必须标注"无 LLM 模式"是**新建能力还是现有能力补全**。现状盘点：检索片段拼装（`buildRAGMessages`）已有，但"LLM 失败时不调 LLM、直接返回片段+摘要+degraded 标志"这条路径没有——是**现有能力的降级补全**，不是从零新建生成路径。
- 摘要复用接 spec 03：档2 触发时，若该内容已有成功摘要（spec 03 的 `FindByMD5` 跨 task 复用），直接用；没有则只给检索片段。

### degraded 呈现（决策记录第 10 节拍板）

- API 响应带 `degraded: true` 标志。
- UI 标注"参考片段（AI 摘要暂不可用）"并折叠生成区。
- 显式告知降级态但不暴露内部术语。面试能答"用户知道当前降级"。

### 接 spec 04 ExecutionPolicy

- 降级在 ExecutionPolicy 之上：ExecutionPolicy 决定"该不该走 LLM"（如 small_talk UseLLM=false 本来就不调 LLM），④ 决定"该走 LLM 但 LLM 挂了怎么办"。
- 档2 降级基线 = spec 04 的 ExecutionPolicy UseLLM=true 但 LLM 失败时的回退。

### BYOK/令牌桶折进（决策记录第 1 节"⑥不写，BYOK/令牌桶折进④"）

- 现有 `internal/ai/admission.go` 的 Admission + `internal/pkg/quota` 的令牌桶限流——已有，本 spec 不重建，只确认它与降级链共存：admission 拒配额（`AdmissionError` + `RetryAfter`）时，若 RetryAfter 在阈值内走重试、超阈值触发档2降级。
- BYOK Profile（CONTEXT.md）作为 AI 凭证来源，失败时降级链触发。

## User Stories

1. 作为项目维护者，我想要 LLM 失败时回退无 LLM 模式（检索片段+摘要直拼），以便请求不全废、用户拿到降级答案。
2. 作为项目维护者，我想要无 LLM 模式标注 degraded:true，以便用户知道当前降级（决策记录第 10 节）。
3. 作为项目维护者，我想要 rerank 失败回退向量基线（档1），以便 rerank 挂了检索仍可用。
4. 作为项目维护者，我想要档1是降级链一环不单立稀缺点，以便叙事聚焦档2功能性降级。
5. 作为项目维护者，我想要降级链档0→档1→档2清晰，以便面试能答"LLM 挂了你怎么办"。
6. 作为项目维护者，我想要档2复用 spec 03 已有摘要（FindByMD5 跨 task），以便降级答案有摘要加持而非纯片段。
7. 作为项目维护者，我想要 admission 拒配额时按 RetryAfter 决定重试或降级，以便限流与降级链协同。
8. 作为项目维护者，我想要 BYOK Profile 失败时触发降级链，以便用户凭证问题不全废请求。
9. 作为项目维护者，我想要降级基线 = spec 04 消融 vector_only 档，以便降级质量有评测参照。
10. 作为项目维护者，我想要降级模式诚实标注"MRR 略低但保证可用性避免请求全废"，以便不把降级包装成提升（决策记录第 3 节）。
11. 作为项目维护者，我想要 UI 标注"参考片段（AI 摘要暂不可用）"并折叠生成区，以便体验与诚实平衡。
12. 作为项目维护者，我想要降级触发可观测（档1/档2 触发次数），以便面试能答降级频率。
13. 作为项目维护者，我想要 spec 标注"无 LLM 模式是现有能力降级补全而非从零新建"，以便前置诚信检查通过（决策记录第 6 节）。
14. 作为项目维护者，我想要 ExecutionPolicy UseLLM=false 的 intent（如 small_talk）不触发档2，以便本来不调 LLM 的不误降级。

## Implementation Decisions

### 复用现有 seam，不重建

- `internal/ai/admission.go` 的 Admission + `internal/pkg/quota` 令牌桶——已有，限流/配额，本 spec 只接降级链。
- `rag_expand.go` 的 `Fallbacks []string` + `appendFallback`——已有 fallback 标记，档1 rerank 失败复用它标 fallback。
- spec 03 的 `FindByMD5` 跨 task 摘要复用——档2 复用已有摘要。
- `buildRAGMessages`（`chat_messages.go`）——档2 片段拼装已有，只补"不调 LLM + degraded 标志"路径。
- spec 04 ExecutionPolicy——降级在其之上。

### 降级链形态

- 档0：rerank + LLM 都成 → 完整答案。
- 档1：rerank 失败 → 向量基线（= vector_only 档），标 fallback，继续 LLM。
- 档2：LLM 失败 → 片段 + 已有摘要直拼 + `degraded: true`，不调 LLM。
- 链是顺序的：档1 失败才考虑档2（rerank 挂了仍走 LLM，LLM 也挂了才档2）。

### degraded 呈现

- API 响应 `degraded: true`（决策记录第 10 节）。
- UI "参考片段（AI 摘要暂不可用）" + 折叠生成区。
- 不暴露"档2/vector_only"内部术语。

### 前置诚信检查（spec 必写）

- 无 LLM 模式 = 现有能力降级补全（`buildRAGMessages` 已有，补不调 LLM + degraded 路径），非从零新建。
- 降级模式 MRR 略低但保证可用性——诚实标注，不包装成提升。

### admission 协同

- `AdmissionError` + `RetryAfter`：RetryAfter 在阈值内重试，超阈值触发档2。
- 与 BYOK Profile 共存：凭证失败触发降级链。

### 单一测试 seam

- 验收 seam：`internal/service` 的降级行为测试。外部行为 = LLM 失败时返回 degraded 答案而非错误、rerank 失败回退向量基线、档2 复用已有摘要。
- 复用 `chat_ask_test.go` 的 fake LLM/reranker 范式（fake 注入失败）。

## Testing Decisions

### 什么算好测试

- 只测外部行为：LLM 失败 → degraded 答案、rerank 失败 → 向量基线 fallback、档2 复用已有摘要、ExecutionPolicy UseLLM=false 不误触发。
- 不测 admission/quota 内部（已有测试）。
- 不测降级模式 MRR（决策记录第 3 节，降级评测是 ④ 自己的事，但本 spec 不跑端到端 MRR，只测降级行为可观察）。

### 测试模块

- `internal/service/ai_reliability_test.go`（新增）：fake LLM/reranker 注入失败，断言降级链。
- 现有 `chat_ask_test.go` 作为不回归保障。

### Prior art

- `internal/service/chat_ask_test.go` —— chat + LLM 的 fake 范式。
- `internal/service/rag_expand.go` 的 `Fallbacks` —— fallback 标记范式。
- `internal/ai/admission_test.go` —— admission 限流范式。

## Out of Scope

- **不重写 admission/quota**。已有，本 spec 只接降级链。
- **不新建生成路径**。无 LLM 模式是 `buildRAGMessages` 降级补全。
- **不单独测降级模式 MRR**。决策记录第 3 节降级评测边界：降级 MRR 略低但保证可用性，本 spec 测降级行为不跑端到端 MRR。
- **不单立档1 稀缺点**。档1 是降级链一环。
- **不做 ⑥ OTel 全链路**。决策记录第 1 节已砍，降级触发可观测只做计数不做 trace。
- **不做协作式取消**。决策记录第 1 节 ⑦ 留候选。
- **不改 spec 04 ExecutionPolicy**。降级在其之上，不改参数语义。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- ④ AI 调用可靠性层（决策记录第 1 节）✅
- 档2功能性降级 = 稀缺点，档1 一环不单立（决策记录第 6 节）✅
- 无 LLM 模式 = 现有能力降级补全（前置诚信检查，决策记录第 6 节）✅
- degraded:true 呈现（决策记录第 10 节拍板）✅
- BYOK/令牌桶折进 ④（决策记录第 1 节"⑥不写"）✅
- 降级基线 = vector_only 档（决策记录第 8 节推进顺序）✅
- 降级模式不包装成提升（决策记录第 3 节）✅

### 本会话拍板的子决策

1. **admission RetryAfter 阈值 = 5s**（钉死在代码 `admissionRetryAfterCutoff`）：
   admission 拒配额 `RetryAfter` 超 5s 触发档2降级（不再等恢复，给降级答案），
   5s 内由 caller 重试（admission 协同 spec 06）。理由：5s 是普通 LLM 请求重试等待的
   合理上限，超 5s 用户体验已显著劣化，不如给降级答案。阈值未经真实流量标定，
   长期运行后可视降级频率回调。`TestDegradationAdmissionRetryAfterOverCutoffTriggersTier2`
   断言 10s 触发 / 2s 不触发。
2. **档1 不标 degraded**：档1（rerank 失败→向量基线）后 LLM 仍生成完整答案，用户
   感知不到降级；degraded:true 只针对档2的"参考片段无生成"（决策记录第 10 节）。
   `DegradationLevel.Degraded()` 仅档2返回 true。
3. **降级答案体诚实标注**：档2 答案是"检索片段+已有摘要直拼"的可读文本，对外不暴露
   "档2/vector_only"内部术语（决策记录第 10 节"不暴露内部术语"）；UI 侧由 degraded:true
   触发"参考片段（AI 摘要暂不可用）"折叠呈现。

### 数字占位符（本 spec 产出的简历可用数字）

- 降级链档数 `3`（档0正常/档1 rerank→向量基线/档2 LLM→无LLM模式）—— `TestDegradationChainTierCount`
- 档1 触发次数 `__`（长期运行采集）
- 档2 触发次数 `__`（长期运行采集）
- 降级可用率 `100`%（LLM 失败场景下返回 degraded 答案而非错误的占比）—— `TestDegradationAvailabilityRate` 造 5 个 LLM 失败场景，100% 返回 degraded 答案非错误

### 简历允许写什么 / 禁止写什么（本 spec 对应 ④ bullet 的预演）

**允许写**：
- "AI 调用功能性降级链：rerank 失败回退向量基线（档1）、LLM 失败回退无 LLM 模式（检索片段+已有摘要直拼、degraded:true 标注，档2），LLM 服务异常时返回降级答案而非全废请求；admission 令牌桶拒配额按 RetryAfter 决定重试或降级。"
- "降级基线 = 消融 vector_only 档（无 rerank 检索），降级模式 MRR 略低但保证可用性。"
- 具体数字（降级可用率 100%）—— 已由 `TestDegradationAvailabilityRate` 造 5 个 LLM 失败场景测出，非估算。

**禁止写**：
- "我写了熔断器" —— ④ 是功能性降级不是普通熔断（决策记录第 6 节）。
- "降级提升 MRR" —— 降级 MRR 略低，不能写成提升（决策记录第 3 节）。
- "全链路可观测" —— ⑥ OTel 已砍，只做降级计数不做 trace。
- "无 LLM 模式是我新建的" —— 是现有 buildRAGMessages 降级补全，非从零新建（前置诚信检查）。
- 任何未在降级行为测试下产出的数字。
