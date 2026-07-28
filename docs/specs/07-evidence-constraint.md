# Spec 07: 轻量证据约束生成（bullet ⑨）

> 推进顺序第七份（收尾）。决策母本见 `docs/specs/00-refactor-decisions.md`（第 9.1 节 ⑨ 轻量证据约束）；领域语言见 `CONTEXT.md`（Evidence / Citation / SoT）。
> 依赖：spec 04 (B) 的 Evidence 体系（Citation + EvidenceID 已有）。本 spec 是生成阶段的证据约束补全，与 (A)/(B) 检索给证据形成完整可追溯叙事。
> 共享：与 spec 06 ④ 的无 LLM 模式正交——⑨ 是 LLM 可用时的证据约束，④ 是 LLM 不可用时的降级。

## Problem Statement

VidLens 的 RAG 生成现在 prompt 要求 LLM 引用证据编号（`chat_messages.go:27`："在对应事实后使用独立格式 [C1][C2] 标注证据"），Citation + EvidenceID 体系已有（`chat.go` 的 `Citation.EvidenceID` + `RetrievedChunk.EvidenceID`）。但有两个真问题：

1. **LLM 引用不校验，可能编造或超范围引用**。prompt 要求引用 [C1][C2]，但生成完不校验 LLM 标的编号是否真在检索集里——LLM 可能编造一个不存在的 [C5]、或引用没检索到的片段。决策记录第 9.1 节 ⑨ 拍板"在生成阶段加'LLM 生成必须引用检索到的 evidence id，超范围结论被拒'机制"——现在没有这个约束，LLM 引用全靠自觉。

2. **没有"超范围结论被拒→重检索"的闭环**。⑨ 的差异化在"证据约束"不在"Agent"——不做完整 Planner-Executor-Critic Agent Loop（撞车 DOVideo + 工作量大），但在生成后加一个轻量校验：LLM 引用的 evidence id 超出检索集 → 该结论被拒 → 重检索补证据。这是"轻量"——单轮校验 + 重检索，不是多轮 ReAct。

痛点从用户视角：作为维护者，我没法诚实回答"你怎么保证 LLM 答案引用的是真检索到的证据、不是编的"——现在 prompt 要求了但不校验，LLM 编造引用不会被拒。这是可追溯叙事的最后一环：(A)/(B) 检索给证据 → ⑨ 生成约束引用证据 → 超范围被拒。没有 ⑨，前面的可追溯链断在生成阶段。

## Solution

在生成后加轻量证据约束校验 + 超范围重检索，复用现有 Citation/EvidenceID + finalizeAnswerCitations，不重建：

### 1. 证据约束校验（生成后，轻量）

- LLM 生成完，解析答案里的 [C1][C2] 引用编号。
- 校验每个引用编号是否在本次检索集的 evidence id 范围内。
- 在范围内 → 保留引用、绑回 Citation。
- 超范围（编造的编号或没检索到的片段）→ 标记违规。

### 2. 超范围处理（被拒→重检索）

- 违规引用被拒：不直接展示违规结论。
- 触发重检索：以违规结论涉及的内容重新检索补证据。
- 重检索后仍无证据 → 结论标注"无证据支撑"或拒绝输出该结论。
- **单轮校验 + 重检索，不是多轮 ReAct**（决策记录第 9.1 节"轻量"，不做完整 Agent Loop）。

### 3. 与 (A)/(B)/(④) 的边界

- (A) 给 intent、(B) 给检索证据、⑨ 约束生成引用证据、④ 处理 LLM 不可用降级。
- ⑨ 是 LLM 可用时的证据约束；④ 是 LLM 不可用时的降级——正交，不冲突。
- 与 video_agent 实验路径边界：video_agent（`video_agent.go`）是完整 tool-loop，⑨ 是单轮约束，不撞车（agent 不进产品默认，⑨ 进）。

## User Stories

1. 作为项目维护者，我想要 LLM 生成后校验引用编号是否在检索集内，以便 LLM 编造引用被拒。
2. 作为项目维护者，我想要超范围引用触发重检索补证据，以便违规结论不直接展示。
3. 作为项目维护者，我想要重检索后仍无证据的结论标注"无证据支撑"，以便诚实拒绝而非硬编。
4. 作为项目维护者，我想要单轮校验 + 重检索而非多轮 ReAct，以便"轻量"（决策记录第 9.1 节）。
5. 作为项目维护者，我想要复用现有 Citation + EvidenceID 体系，以便不重建证据绑定。
6. 作为项目维护者，我想要复用 finalizeAnswerCitations 的后处理范式，以便约束校验接进现有答案处理链。
7. 作为项目维护者，我想要在范围内的引用绑回 Citation，以便答案可追溯。
8. 作为项目维护者，我想要 ⑨ 与 ④ 正交（LLM 可用约束 vs 不可用降级），以便不冲突。
9. 作为项目维护者，我想要 ⑨ 与 video_agent 边界清晰（单轮约束 vs 完整 loop），以便不撞车。
10. 作为项目维护者，我想要 (A)/(B)/⑨/④ 形成完整可追溯叙事，以便面试能答"检索给证据→生成约束引用→超范围被拒→LLM 挂了降级"。
11. 作为项目维护者，我想要证据约束可观测（违规拒绝次数、重检索次数），以便面试能答约束频率。
12. 作为项目维护者，我想要 prompt 里的 [C1][C2] 引用要求保留，以便 ⑨ 校验有引用可解析。
13. 作为项目维护者，我想要重检索有上限（不无限循环），以便单轮轻量不退化成多轮。

## Implementation Decisions

### 复用现有 seam，不重建

- `Citation` + `EvidenceID`（`chat.go`）——证据绑定已有，⑨ 只校验不重建。
- `finalizeAnswerCitations`（`chat_ask.go`）——答案后处理已有，⑨ 接它之后加校验。
- prompt [C1][C2] 引用要求（`chat_messages.go:27`）——已有，⑨ 解析它。
- spec 04 检索链路——超范围重检索复用它。

### 约束校验形态

- 解析答案 [Cx] 引用 → 映射 evidence id → 校验是否在检索集。
- 在范围内：绑回 Citation。
- 超范围：违规标记 → 重检索（以违规内容为 query）→ 重检索结果补证据或标注无支撑。

### 轻量边界（反 Agent Loop）

- 单轮校验 + 至多一次重检索（上限），不无限循环。
- 不做 Planner-Executor-Critic 多轮 ReAct（决策记录第 9.1 节"撞车 + 工作量大"）。
- 不接 video_agent 实验路径（agent 不进产品默认）。

### 与 ④ 正交

- ⑨：LLM 可用 → 校验引用证据。
- ④：LLM 不可用 → 降级无 LLM 模式。
- 两者不冲突：LLM 可用走 ⑨，不可用走 ④。

### 单一测试 seam

- 验收 seam：`internal/service` 的证据约束行为测试。外部行为 = LLM 引用在范围内保留、超范围被拒重检索、重检索后无支撑标注。
- 复用 `chat_ask_test.go` 的 fake LLM 范式（fake LLM 产出含 [Cx] 引用的答案，注入超范围引用）。

## Testing Decisions

### 什么算好测试

- 只测外部行为：引用范围内保留、超范围被拒重检索、重检索后无支撑标注、重检索有上限不无限循环。
- 不测 Citation/EvidenceID 内部（已有测试）。
- 不测 video_agent（实验路径）。

### 测试模块

- `internal/service/evidence_constraint_test.go`（新增）：fake LLM 注入超范围引用，断言约束链。
- 现有 `chat_ask_test.go` 作为不回归保障。

### Prior art

- `internal/service/chat_ask_test.go` —— chat + LLM fake 范式。
- `internal/service/chat.go` 的 Citation/finalizeAnswerCitations —— 证据绑定范式。
- `internal/service/chat_messages.go` 的 prompt [C1][C2] —— 引用要求范式。

## Out of Scope

- **不做完整 Planner-Executor-Critic Agent Loop**。决策记录第 9.1 节"撞车 + 工作量大"，⑨ 是轻量单轮约束。
- **不接 video_agent 实验路径**。agent 不进产品默认，⑨ 是单轮约束不撞车。
- **不重建 Citation/EvidenceID**。已有，⑨ 只校验。
- **不做多轮 ReAct**。单轮校验 + 至多一次重检索，有上限。
- **不与 ④ 降级合并**。正交，LLM 可用走 ⑨、不可用走 ④。
- **不改 prompt 的 [C1][C2] 要求**。已有，⑨ 解析它。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- ⑨ 轻量证据约束生成（决策记录第 9.1 节）✅
- 不做完整 Agent Loop，差异化在"证据约束"不在"Agent"（决策记录第 9.1 节）✅
- 与 (A)/(B) 检索给证据形成完整可追溯叙事（决策记录第 9.1 节）✅
- ⑨ 是候选池扩展进池项（决策记录第 9.1 节"新增候选进池"）✅

### 数字占位符（本 spec 产出的简历可用数字）

- 证据约束链档数 `2`（校验 + 重检索，单轮轻量）。验收命令：
  `go test ./internal/service/ -run TestEvidenceConstraintChainTierCount -v` → 断言
  `evidenceConstraintChainTiers == 2` 且 `maxEvidenceReRetrieval == 1`（单轮校验 +
  至多一次重检索，非多轮 ReAct）。完整约束链行为测试：
  `go test ./internal/service/ -run TestEvidenceConstraint -v`（范围内保留 / 超范围重检索补证据 /
  重检索空标"无证据支撑" / 不无限循环 / Ask 端到端不回归）。
- 违规引用拒绝次数 `__`（长期运行采集，进程内计数 `EvidenceViolationTriggers()`）
- 重检索触发次数 `__`（长期运行采集，进程内计数 `EvidenceReretrievalTriggers()`）
- 重检索后无支撑标注次数 `__`（长期运行采集，进程内计数 `EvidenceUnsupportedTriggers()`）

### 简历允许写什么 / 禁止写什么（本 spec 对应 ⑨ bullet 的预演）

**允许写**：
- "生成阶段轻量证据约束：LLM 结论必须引用检索集内 evidence id，超范围引用被拒并触发单轮重检索补证据（非多轮 ReAct Agent），重检索后仍无支撑标注'无证据支撑'拒绝输出——与 (A)/(B) 检索给证据形成完整可追溯链。"
- "差异化在'证据约束'不在'Agent'：不做完整 Planner-Executor-Critic Loop（撞车 + 工作量大），单轮校验 + 重检索有上限。"
- 具体数字（违规拒绝次数）—— **必须**在 fake LLM 注入超范围场景测出后填，不许估算。

**禁止写**：
- "我建了 Agent Loop" —— ⑨ 是轻量单轮约束，不是 Agent（决策记录第 9.1 节）。
- "多轮 ReAct 证据校验" —— 单轮 + 重检索有上限，不是多轮。
- "LLM 自动验证事实" —— ⑨ 校验引用编号在不在检索集，不是事实核查。
- "与 video_agent 集成" —— agent 是实验路径不进默认，⑨ 不接它。
- 任何未在证据约束测试下产出的数字。
