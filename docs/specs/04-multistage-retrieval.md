# Spec 04: 多阶段检索 + 评测驱动上线（bullet B）

> 推进顺序第四份。决策母本见 `docs/specs/00-refactor-decisions.md`（第 5 节 (B)⑤证据链与跨视频）；领域语言见 `CONTEXT.md`（ExecutionPolicy / Scope / Collection / Evidence / SoT）。
> 依赖：spec 01 评测基础设施（消融流水线已验通；真实数据集 + 真实跨档数字待标注后回填）。本 spec 是 spec 01 评测结论的"消费方"。

## Problem Statement

VidLens 的 RAG 检索现在有三个真问题，从简历与面试角度都讲不清：

1. **检索参数靠散落 if 硬编码，没有 intent → 参数映射值对象**。`prepareChatByMode` + `prepareRAGChat` 里散落着 `session.ScopeType == KnowledgeBase → 强制 EnableVector=true/EnableBM25=false`、`isVideoOverviewQuestion → prepareVideoContextChat`（关检索走视频上下文直拼）、`topK > 10 → 10`、`recentLimit = 0 when KnowledgeBase`。同一类决策（"这次提问该不该检索、检索多大范围、要不要 rerank"）散在四个函数里，改一个参数要动多处。面试问"你怎么决定这次问 overiew 不检索、direct_qa 开 rerank"答不出统一映射。决策记录第 4 节明确要求"一个 switch 消掉 prepareChatByMode 硬编码"，现在没做。

2. **跨视频检索有基础设施但没接进统一路由**。`KnowledgeBase`（`internal/model/knowledge_base.go`，"groups videos owned by one user for cross-video retrieval"）+ 会话 `scope_type=knowledge_base` 绑定 `knowledge_base_id` 已存在——这不是新概念。但跨视频检索的参数（scope 放大到集合内 video_ids、recent 历史关、BM25 关）是 `prepareRAGChat` 里另一处 if 硬编码，没接进 ExecutionPolicy。决策记录第 5 节"跨视频 = 显式 Collection，会话绑定集合，topic_compare/series_locate intent 放大多视频"——Collection 已有（叫 KnowledgeBase），缺的是把它的检索语义接进 ExecutionPolicy 的 Scope 档。

3. **线上检索配置靠手拍，没评测证据背书**。`productionRetrievalConfig`（`cmd/server/wiring.go`）写死 `EnableBM25=false`、rerank 看 `cfg.RerankModel` 是否非空。CLAUDE.md 自己注明"eval defaults 是实验向，不自动是 production"——"为什么线上不开 BM25 / 为什么上 rerank"没消融报告背书。spec 01 已建好消融流水线（四档可跑、P95 双口径、paired CI），但还没产出真实跨档数字（等真实数据集标注）。决策记录第 5 节⑤"PostgreSQL video_chunks 为事实源、pgvector 为可重建投影、evidence ID 绑定"——这条证据链（rag-audit 漂移对账 + rag-reindex 断点续跑）是检索可信度的支撑，不单立 bullet，折进 (B)。

痛点从用户视角：作为维护者，我没法诚实回答"你的混合检索 P95 多少、凭什么线上开 rerank、overview 问题为什么不检索"——参数靠手拍、跨视频靠 if、评测没真实数字。这是决策记录 (B) "以评测证据支撑混合检索/rerank 线上化决策"的真稀缺点。

## Solution

分两段，明确分开，避免实现卡在等数据集：

### A 段：代码侧（现在能做，零数据依赖）

把散落的 intent/scope → 检索参数硬编码，统一成一个 **ExecutionPolicy 值对象**（CONTEXT.md 已定义：Retrieve / TopK / Rerank / Rewrite / UseSummary / UseLLM / Scope），消掉 `prepareChatByMode` 的 if 链：

1. **ExecutionPolicy = intent → 检索/生成参数映射**。intent taxonomy 复用 spec 01 共享 query 池的 `category` 取值（`video_overview`/`direct_qa`/`topic_compare`/`series_locate`/`timeline_locate`/`small_talk`）。每个 intent 映射一组参数（是否检索、top_k、rewrite、rerank、是否走 LLM、scope 单视频/集合）。
2. **scope 档接进 ExecutionPolicy**：单视频（会话绑 task）或集合（会话绑 KnowledgeBase）。`topic_compare`/`series_locate` intent 的 Scope = 集合，检索过滤到集合内 video_ids。复用现有 KnowledgeBase + ScopedSession，不新建 Collection 表。
3. **消掉散落 if**：`prepareChatByMode` 改为"识别 intent → 取 ExecutionPolicy → 按参数走检索"。`session.ScopeType == KnowledgeBase → 强制参数`、`isVideoOverviewQuestion → 关检索`、`topK > 10 → 10` 这些散落判定，由 ExecutionPolicy 的字段统一表达。
4. **⑤证据链折进**：PostgreSQL `video_chunks` 为事实源、pgvector 为可重建投影、evidence ID 绑定（现有 `rag_artifact.go` 的 `ComputeChunkManifestSHA256` + `ListEvidenceManifest`）、rag-audit 三类漂移对账 + rag-reindex 断点续跑（`internal/ragtool`）——作为检索可信度证据链，不单立 bullet。本 spec 只确认它与新 ExecutionPolicy 共存，不改它。

### B 段：评测侧（依赖 spec 01 真实数据集，后做）

5. **用 spec 01 消融结论驱动 `productionRetrievalConfig` 线上化**：等 spec 01 真实数据集标注完、跑出真实跨档 Recall@5/MRR/P95 + paired CI 后，把结论写进 `docs/eval/experiment-registry.yaml`，据此改 `cmd/server/wiring.go` 的 `productionRetrievalConfig`（开/关 BM25、上/下 rerank）。线上化决策**有评测证据背书**，不是手拍。
6. **④ 降级基线 = 消融中 `vector_only` 档**（无 rerank 的检索），作为 spec 06 ④ 的降级链参照。本 spec 不测降级，只定基线指向。

## User Stories

1. 作为项目维护者，我想要 intent → 检索/生成参数映射统一成 ExecutionPolicy 值对象，以便消掉 prepareChatByMode 的散落 if 硬编码。
2. 作为项目维护者，我想要 video_overview intent 关检索走视频上下文直拼，以便概览类问题不浪费向量检索。
3. 作为项目维护者，我想要 direct_qa intent 开 rerank + 标准检索，以便精确问答有 rerank 加持。
4. 作为项目维护者，我想要 topic_compare/series_locate intent 的 Scope = 集合，以便跨视频检索过滤到 KnowledgeBase 内 video_ids。
5. 作为项目维护者，我想要 small_talk intent 关检索关 LLM 直答，以便闲聊不烧检索 + 生成。
6. 作为项目维护者，我想要 timeline_locate intent 开检索 + Signal 时间戳过滤，以便时间线定位类问题用结构化线索缩范围（Signal 提取见 (A) spec 05，本 spec 只留接口位）。
7. 作为项目维护者，我想要 ExecutionPolicy 的 Scope 档接进现有 KnowledgeBase + ScopedSession，以便跨视频检索复用已有基础设施、不新建 Collection 表。
8. 作为项目维护者，我想要 prepareChatByMode 改为"识别 intent → 取 ExecutionPolicy → 按参数走检索"，以便改一个参数只动一处。
9. 作为项目维护者，我想要 video_chunks 为事实源、pgvector 为可重建投影、evidence ID 绑定保持不变，以便 ExecutionPolicy 改检索参数不动事实源语义。
10. 作为项目维护者，我想要 rag-audit 漂移对账 + rag-reindex 断点续跑保持工作，以便索引重建后 ExecutionPolicy 仍能检索到一致结果。
11. 作为项目维护者，我想要 productionRetrievalConfig 的 BM25/rerank 开关由 spec 01 消融结论驱动，以便线上化决策有评测证据背书而非手拍。
12. 作为项目维护者，我想要消融结论写进 experiment-registry.yaml 并指到 productionRetrievalConfig 改动，以便面试能答"评测如何驱动线上"。
13. 作为项目维护者，我想要 vector_only 档作为 ④ 降级基线指向，以便 spec 06 ④ 的降级链有延迟/质量参照。
14. 作为项目维护者，我想要 ExecutionPolicy 的参数选择有 audit trail（每档为何这么定），以便反 overclaim（决策记录第 4 节硬约束）。
15. 作为项目维护者，我想要跨视频检索的 recent 历史关断（KnowledgeBase 已有的 member-safe 设计），以便移除视频的旧答案不回灌检索/生成。
16. 作为项目维护者，我想要 ExecutionPolicy 路由可观察（不同 intent/scope 走不同参数），以便面试能答"overview 不检索、direct_qa 开 rerank 的决策路径"。

## Implementation Decisions

### 复用现有 seam，不重建

- `RAGRetrievalConfig`（`internal/service/rag_pipeline.go`）已有 EnableVector/EnableBM25/RewriteQueries/TopK/CandidateK/RRFK/MinVectorScore/RerankerMode 字段——ExecutionPolicy 不新建检索参数，只把 intent/scope → 这些参数的映射统一成值对象。
- `KnowledgeBase` + `ScopedSession`（`internal/model/knowledge_base.go` + `chat_sessions.go`）已有跨视频基础设施——Scope 档接进它，不新建 Collection 表（决策记录第 5 节"跨视频 = 显式 Collection"的 Collection = 现有 KnowledgeBase）。
- `RetrievalPipeline.Retrieve`（`rag_pipeline.go`）已有按 Config 走检索的能力——ExecutionPolicy 只是 Config 的生产方。
- rag-audit/reindex（`internal/ragtool`）+ evidence 绑定（`rag_artifact.go`）——已有，本 spec 不改，只确认共存。

### ExecutionPolicy 形态

- 值对象，字段对齐 CONTEXT.md：`Retrieve bool` / `TopK int` / `Rerank bool` / `RewriteQueries int` / `UseSummary bool` / `UseLLM bool` / `Scope string`（"video" | "collection"）。
- 映射 = intent + scope → ExecutionPolicy。一个 switch / map 消掉散落 if。
- intent taxonomy 取 spec 01 共享 query 池的 `category` 取值（不另立 intent 字段，schema 不动——spec 01 已拍板）。
- **每档参数选择写 audit trail**（决策记录第 4 节硬约束）：overview 为何关检索、direct_qa 为何开 rerank、topic_compare 为何 scope=collection，写理由进 spec + 代码注释。

### prepareChatByMode 改造

- 现有散落 if（`ScopeType==KnowledgeBase → 强制参数` / `isVideoOverviewQuestion → 关检索` / `topK>10 → 10` / `recentLimit=0 when KB`）→ 由 ExecutionPolicy 字段统一表达。
- 改造后路径：识别 intent（(A) spec 05 的分类器，本 spec 先用现有 `isVideoOverviewQuestion` + scope 作占位分类）→ 取 ExecutionPolicy → 按参数走检索/生成。
- **(A) 分类器未做前的占位**：本 spec 用现有 `isVideoOverviewQuestion` + `session.ScopeType` 作 intent 占位分类，把 ExecutionPolicy 路由打通；(A) spec 05 落地后替换占位为真分类器。这点必须写清，否则 (A) 没法接。

### 跨视频检索

- `topic_compare`/`series_locate` intent → Scope=collection → 检索过滤到 `KnowledgeBase` 内 video_ids（复用 `sessionRetrievalTaskIDs` 已有的 KB 成员解析）。
- recent 历史关断（KB 已有 member-safe 设计，保持）。
- 答案按视频分组引用（现有 citation 已带 video_id，复用）。

### 线上化决策（B 段，依赖 spec 01 真实数据）

- 等 spec 01 真实数据集标注完 → 跑四档消融 → 真实 Recall@5/MRR/P95 + paired CI → 写 `docs/eval/experiment-registry.yaml` → 改 `productionRetrievalConfig`。
- 本 spec 只定"怎么用消融结论"契约：消融选定档的参数 → productionRetrievalConfig 的 EnableBM25/rerank 配置。不重跑评测（spec 01 已建流水线）。

### 两段分开（防卡死）

- A 段（ExecutionPolicy 路由 + 跨视频接进 + 证据链共存）现在能做，零数据依赖。
- B 段（评测驱动线上化）依赖 spec 01 真实数据集标注完，后做。
- spec 明确：A 段先实现验收，B 段等数据集。实现会话只做 A 段，B 段留 `__` 占位。

### 单一测试 seam

- A 段验收 seam：`internal/service` 的 ExecutionPolicy 路由行为测试。外部行为 = 同问题在不同 intent/scope 下走不同检索参数。复用 `chat_ask_test.go` / `chat_prepare_test.go` 的 fake retriever 范式。
- B 段验收 seam：spec 01 的 `Runner.Run` 跑真实数据集（已建，本 spec 不重跑）。

## Testing Decisions

### 什么算好测试

- 只测外部行为：不同 intent/scope 下检索参数（是否检索、top_k、rewrite、rerank、scope）的可观察差异。
- 不测 ExecutionPolicy 内部 struct 字段赋值细节。
- 不测 rag-audit/reindex（已有测试覆盖，本 spec 只确认共存）。
- 不测线上化决策（B 段依赖真实数据集，留 spec 01 跑）。

### 测试模块

- `internal/service/execution_policy_test.go`（新增）：复用 `chat_ask_test.go` 的 fake retriever 范式，断言各 intent/scope 走对应参数。
- 现有 `chat_prepare_test.go` 作为改造后行为不回归保障。

### Prior art

- `internal/service/chat_ask_test.go`、`chat_prepare_test.go` —— chat + retrieval 的现有测试范式。
- `internal/service/rag_pipeline.go` 的 `RetrievalPipeline.Retrieve` —— 按 Config 走检索的现有范式。
- spec 01 的 `Runner.Run` —— B 段线上化数字的产出 seam（已建）。

## Out of Scope

- **不做 (A) 意图分类器**。本 spec 用现有 `isVideoOverviewQuestion` + scope 作 intent 占位，真分类器是 spec 05 的事。
- **不做 spec 01 真实数据集标注**。B 段线上化数字依赖真实数据集，标注是用户的事，本 spec 只定契约。
- **不做 ④ 降级模式**。降级链是 spec 06 的事，本 spec 只定 vector_only 基线指向。
- **不新建 Collection 表**。跨视频复用现有 KnowledgeBase + ScopedSession。
- **不改 rag-audit/reindex/evidence 绑定**。证据链已有，本 spec 只确认共存。
- **不重写 RetrievalPipeline**。ExecutionPolicy 是 Config 的生产方，不是重建检索。
- **不做 Signal 提取**。timeline_locate 的 Signal 时间戳过滤留给 (A) spec 05，本 spec 只留接口位。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- (B) 多阶段检索 + 评测驱动上线（决策记录第 1 节）✅
- ⑤事实源/投影分离折进 (B)（决策记录第 5 节）✅
- 跨视频 = 显式 Collection，会话绑定集合（决策记录第 5 节）—— Collection = 现有 KnowledgeBase ✅
- 一个 switch 消掉 prepareChatByMode 硬编码（决策记录第 4 节）✅
- (A)/(B) 共享 query 池，category 复用为 intent 标签（决策记录第 3 节）✅
- ④ 降级基线 = vector_only 档（决策记录第 8 节推进顺序）✅

### 数字占位符（本 spec 产出的简历可用数字）

A 段（代码侧，现在产出）：
- 消掉散落 if 硬编码 `4` 处 → 由 ExecutionPolicy 统一（`session.ScopeType==KnowledgeBase → 强制参数` / `isVideoOverviewQuestion → 关检索` / `topK>10 → 10`（含 topK 默认值，现由 `ExecutionPolicy.ClampTopK` 统一表达） / `recentLimit=0 when KB`，现全部由 ExecutionPolicy.Retrieve/Scope/TopK/Rerank/Rewrite 字段 + `applyPolicy` 表达；见 `internal/service/execution_policy.go` + `chat_prepare.go` + `rag_pipeline.go` 的 `applyPolicy`）

B 段（评测侧，依赖 spec 01 真实数据集，后填）：
- Recall@5 `__`→`__`（vector_only → 选定档，spec 01 跑出）
- MRR `__`→`__`
- P95 检索延迟 `__`ms（含失败按 0 + 成功子集双口径，spec 01 跑出）
- 跨视频检索覆盖 `__` 个 intent（topic_compare/series_locate）

### 简历允许写什么 / 禁止写什么（本 spec 对应 (B) bullet 的预演）

**允许写**：
- "以 intent → ExecutionPolicy 值对象统一检索路由（是否检索/top_k/rewrite/rerank/scope），消掉散落 if 硬编码；跨视频检索复用 KnowledgeBase 集合 scope、topic_compare/series_locate intent 放大到集合内 video_ids。"
- "以 spec 01 离线消融（纯向量/BM25混合/RRF融合/模型rerank 四档，Recall@5/MRR/P95 + paired CI）驱动 productionRetrievalConfig 线上化：BM25/rerank 开关由评测结论定而非手拍，结论写 experiment-registry.yaml 可指。"
- "PostgreSQL video_chunks 为事实源、pgvector 为可重建投影、evidence ID 绑定，rag-audit 三类漂移对账 + rag-reindex 断点续跑保检索可信度。"
- 具体数字（Recall@5、MRR、P95）—— **必须**在 spec 01 真实数据集跑出后填，不许估算。

**禁止写**：
- "我建了跨视频检索框架" —— KnowledgeBase + ScopedSession 已有，本 spec 是接进 ExecutionPolicy，不是新建。
- "我写了评测框架" —— 框架是 spec 01 已有，(B) 是消费评测结论。
- "线上 A/B 验证提升" —— 不做线上 A/B（spec 01 已禁）。
- "ExecutionPolicy 智能路由" —— 占位分类器是现有 isVideoOverviewQuestion，真分类器是 (A)，不能把占位写成智能。
- 任何未在 spec 01 验收命令下产出的数字。
