# Spec 01: 评测基础设施 —— 真实数据集与单变量消融实验

> 这是本轮重构的第一份 spec，也是 (B) 多阶段检索 + 评测驱动上线 bullet 的前置依赖。
> 决策母本见 `docs/specs/00-refactor-decisions.md`；领域语言见 `CONTEXT.md`。
> 推进顺序第一站：最便宜、立刻产数字、验证"spec→实现→验收→回填简历"流水线。

## Problem Statement

(B) 这条 bullet 想写"以评测证据支撑混合检索/rerank 线上化决策"。但现在仓库里：

1. **没有真实 strict 评测集**。`internal/eval` 已有完备的严格 schema（`Case`/`Dataset`/`SplitManifest`/`SealedAccessEvent`/`GuardTuningAllowed`）、sealed test token 门禁、SHA256 manifest、抗调参 registry、单变量消融就绪的 `RunMetadata`（`experiment_id`/`variant_id`）——这套反 overclaim 基础设施是 solid 的，不是"粗糙 AI 代码"。但 `docs/eval/rag-quant-cases.yaml` 等是 **legacy 格式**（`task_id` + `expected_chunk_keywords`，无 `case_id`/`video_id`/`source_group`/`split`/`evidence_ranges`），引用的 task 可能已删，且没有 sealed test、没有 group_id/relevance 分级、没有跨视频集合标注。**严格 schema 与标注指南都写好了，但没人按它建出一份真实数据集。**
2. **没有消融实验产物**。`EnableBM25=false`、`productionRetrievalConfig` 是线上默认，但"为什么 BM25 不开 / 为什么 rerank 不上"没有评测报告背书。CLAUDE.md 自己注明"eval defaults 是实验向，不自动是 production"——缺的就是从实验到线上的**决策证据链**。

痛点从用户视角：我作为这个项目的维护者，没法诚实回答"你的 MRR 0.78→0.93 是怎么来的、凭什么决定线上开 rerank"。没有真实数据集和消融报告，(B) 就是 overclaim。

## Solution

分三步，全部复用现有 `internal/eval` seam，不重建：

1. **建一份真实 strict 评测数据集**：从 3-5 个真实视频（公开课/讲座/教程，含中英混合与 ASR 噪声）人工标注 query，每条 case 同时带**黄金 intent 标签**（供给 spec 04 (A) 复用）与**黄金 evidence**（`evidence_ranges` 带 `group_id`/`relevance`/稳定 `context_ids`）。按 `source_group` 划分 train/dev/test 并 seal test split。
2. **跑单变量消融实验**：纯向量 / BM25 混合 / RRF 融合 / 模型 rerank 四档，每档跑同一 sealed test，产出 Recall@5 / MRR / nDCG / Context Precision / P95 检索延迟，附置信区间与 minimum effect 判定（复用 annotation guide 第 7 节口径）。
3. **据此决定线上化**：把消融结论写进 `docs/eval/experiment-registry.yaml`，再据此改 `cmd/server/wiring.go` 的 `productionRetrievalConfig`（开/关 BM25、上/下 rerank）。线上化决策**有评测证据背书**，不是"我接了"。

## User Stories

1. 作为项目维护者，我想要一份按 strict schema 标注的真实评测数据集，以便 (B) 的 MRR 数字有可复现来源而非估算。
2. 作为项目维护者，我想要每条 case 同时带黄金 intent 标签和黄金 evidence，以便 spec 04 (A) 的意图分类评测复用同一数据集，不维护两套。
3. 作为项目维护者，我想要 source_group 维度的 train/dev/test 划分并 seal test，以便防止在 test 上反向调参。
4. 作为项目维护者，我想要 sealed test 访问被 token + access registry 强制，以便任何 test 访问留痕且不可静默复用旧 dataset version。
5. 作为项目维护者，我想要纯向量 / BM25 混合 / RRF 融合 / 模型 rerank 四档在同一 test 上的单变量消融报告，以便回答"为什么线上开/不开 BM25、上/不上 rerank"。
6. 作为项目维护者，我想要每档的 Recall@5 / MRR / nDCG / Context Precision / Complete Evidence Recall 与 P95 检索延迟，以便 (B) 简历点的数字占位符能被真实数字替换。
7. 作为项目维护者，我想要每档附置信区间与 minimum effect 判定，以便避免"单次运行结论"和"点估计越过阈值就声称提升"的 overclaim。
8. 作为项目维护者，我想要 executor 失败的 answerable case 进入所有检索指标分母按 0 计，以便系统性失败不被包装成检索提升。
9. 作为项目维护者，我想要无答案 case（`answerable: false`）带 `negative_confusers`，以便检验错误接受。
10. 作为项目维护者，我想要跨视频集合 case（`source_group` 含多个 `video_id`，对应 `topic_compare`/`series_locate` intent），以便 (B) 的跨视频检索能力有评测覆盖。
11. 作为项目维护者，我想要消融报告产出 Markdown/CSV/JSON 三种产物（复用现有 `internal/eval/report.go`），以便简历点能指到具体可跑文件。
12. 作为项目维护者，我想要语料/chunk/向量/prompt 的 SHA256 由文件字节计算（`BindArtifactFileDigests`）而非手填，以便评测产物可复现且文件被替换后失败。
13. 作为项目维护者，我想要线上化决策写进 `experiment-registry.yaml` 并指到 `productionRetrievalConfig` 的改动，以便面试能答"评测如何驱动线上"。
14. 作为项目维护者，我想要至少 20% 样例（优先 hard / 无答案 / 跨片段）由第二标注者独立复核，以便标注可信。
15. 作为项目维护者，我想要 P95 检索延迟随消融一起产出，以便 (B) 可写"混合检索 P95 __ms"且 ④ 降级基线有延迟参照。

## Implementation Decisions

### 复用现有 seam，不新建模块

- 数据集加载、schema 校验、sealed test 门禁、manifest/content SHA256、`RunMetadata`、`EvaluateMetrics`、`Runner.Run`、`BindArtifactFileDigests` —— 全部复用 `internal/eval` 现有类型与函数。本 spec **不新增** 评测框架，只新增**数据**与**实验配置**。
- 简历允许写的是"我建了真实评测集 + 跑了消融驱动线上决策"，**不是**"我写了评测框架"（框架是上一轮已有的，写在 spec 里是为避免重造，不作为简历点）。

### 数据集形态

- 物理分离四文件：manifest + train cases + dev cases + sealed test cases（annotation guide 第 2 节要求的格式）。
- `source_group` 按内容来源定义（如 "操作系统课程系列"、"AI 编程讲座系列"），每个 source_group 含 1-N 个 `video_id`。跨视频集合 case 的 `source_group` 含多个 `video_id`。
- 划分以 source_group 为最小单位，同 source_group / video_id 不得跨 split。
- test split 必须 `sealed: true` + `content_sha256` + `access_token_sha256`，token 明文不进仓库/日志/示例命令。
- 规模目标：**30-50 条 case**，覆盖各 intent 类别（`video_overview`/`direct_qa`/`topic_compare`/`series_locate`/`timeline_locate`/`small_talk`）与三档 difficulty，含 `answerable: false` 无答案 case。这是"有统计意义"的下限，spec 第 5 节简历点据此写"__条"占位符。
- 每条 case 的 `category` 字段复用为 intent 标签载体（**用户已拍板**，与 spec 04 (A) 共享 query 池的契约）。`category` 取值即 intent taxonomy（`video_overview`/`direct_qa`/`topic_compare`/`series_locate`/`timeline_locate`/`small_talk`），不另立 `intent` 字段，schema 不动。

### 证据定位

- ASR baseline 必须用 `video_chunks.vector_id` 作为 `context_ids`，通过 `ListEvidenceManifest` 导出核验（annotation guide 第 5 节约束）。
- 索引重建改变分块或内容时，必须重新导出 manifest 并升级 dataset version。
- 在 ASR 真正保存可靠时间范围前，不得由字符位置估算时间戳 —— 这条作为本 spec 的硬约束写入。

### 消融实验配置

- 四档 variant：`vector_only` / `bm25_hybrid` / `rrf_fusion` / `model_rerank`，同一 `experiment_id`、不同 `variant_id`，复用 `RunMetadata`。
- 每档冻结 `k`、`boundary_tolerance_ms`、`max_chunk_duration_ms`、`min_evidence_coverage`（annotation guide 第 6 节），运行后不得按结果改口径。
- **model_rerank 代理**（实现会话升级决策）：离线 eval 路径的 `configuredReranker` 仅支持 none/deterministic，无线上 `ModelRerankerFactory`。`model_rerank` 档以 deterministic reranker 作代理跑通四档，**诚实标注**"离线 eval 无真实 ModelRerankerFactory，model_rerank 档以 deterministic 代理"。四档必须全可跑（不留占位档，破坏"四档单变量消融"叙事）。**禁止**把 deterministic 代理档的差异说成真实模型 rerank 提升——真实模型 rerank 线上效果由 (B) 评测在真实链路测，不在本 spec 离线 eval 装。
- **CI 形式**（实现会话升级决策）：以 `vector_only` 为 baseline，`bm25_hybrid`/`rrf_fusion`/`model_rerank` 各自作 candidate 跑三个独立配对对比，复用现有 `AnalyzePairedRunArtifacts`。不新写 bootstrap 函数（spec 禁止重写框架）。minimum effect 判定口径复用 annotation guide 第 7 节。
- 报告口径复用 `EvaluateMetrics` 产出的 `MetricReport`（Overall + ByCategory + ByVideo + BySourceGroup），失败 case 不从分母删除（annotation guide 第 7 节）。
- 比较候选方案用置信区间 + minimum effect 判定，不只看点估计。
- P95 检索延迟：在 `EvaluationCaseResult` 侧加一个轻量 `RetrieveLatencyMS` 字段（**用户已拍板**），报告聚合 P95。这是本 spec 唯一允许的 schema 扩展，必须不破坏现有 strict schema 校验（dataset-schema.yaml 的 `case` 不动，只在 executor 产物侧加字段）。
- **P95 聚合口径**（实现会话升级决策）：失败 case 的 `RetrieveLatencyMS` 计 0 并进 P95 样本，**不排除**——与"executor 失败的 answerable case 进入所有检索指标分母按 0 计"同口径（spec 第 7 节）。排除失败会让 P95 虚高（失败越多数字越好看），反 overclaim。但同时报一个**成功检索子集 P95** 作对照（只算 RetrieveLatencyMS>0 的 case），两个数都给：主 P95 含失败（按 0）、副 P95 只算成功检索。面试能答清"含失败的 P95 和健康检索 P95 各是多少"。

### 线上化决策

- 消融结论写进 `docs/eval/experiment-registry.yaml`（已有文件），记录每档指标 + 选中档 + 理由。
- 据此改 `cmd/server/wiring.go` 的 `productionRetrievalConfig`：`EnableBM25` / rerank 配置由评测结论决定，不是手拍。
- ④ 降级基线 = 消融中 `vector_only` 档（无 rerank 的检索），作为 ④ spec 的降级链参照。

### 单一测试 seam

- 整个 spec 的验收**只有一个外部行为 seam**：库层 `Runner.Run` + `LoadSplitDataset(test)` 直接跑 sealed test 产出 `RunArtifact`（JSON），其 `Summary` 含 Recall@5/MRR/nDCG/Context Precision/P95 + 置信区间。所有 user story 的验收都通过这一个库层调用的产物校验，不新增多个测试入口。
- **CLI 防护不破**：`cmd/rag-eval` 的"sealed test execution intentionally disabled in this dev command"守卫保持不动——它是反 overclaim 红线（防开发期随手跑 test split 调参）。本 spec 验收走库层 `Runner.Run`，不通过 CLI。CLI 的 disabled 守卫不在本 spec 破。

## Testing Decisions

### 什么算好测试

- 只测外部行为：`cmd/rag-eval` 在 sealed test 上跑出的 `RunArtifact` 是否含齐备指标、失败 case 是否进分母、SHA256 是否绑文件字节、sealed test 无 token 是否被拒。
- 不测 `internal/eval` 内部函数的实现细节（这些已被现有 `*_test.go` 覆盖）。

### 测试模块

- `internal/eval` 现有测试（dataset/runner/metrics/registry/hash）作为 schema 不变性保障，本 spec 不改它们。
- 新增 `cmd/rag-eval` 端到端验收测试：用一份 mini strict dataset（含 sealed test），跑四档消融，断言 `RunArtifact.Summary` 字段齐备 + 失败 case 进分母 + 无 token 拒绝 test 执行。

### Prior art

- `internal/eval/dataset_test.go`、`runner_test.go`、`metrics_test.go`、`registry_test.go` —— strict dataset 加载、sealed test 门禁、指标计算、access registry 的现有测试范式。
- `cmd/rag-eval/legacy_baseline_test.go`、`cmd/rag-eval/strict_eval.go` —— 端到端 runner 调用的现有范式。

## Out of Scope

- **不重写 `internal/eval` 框架**。schema/runner/metrics/registry 已完备，本 spec 只加数据与实验配置。若发现框架缺能力（如 P95 字段），按"最小扩展不破坏 strict schema"原则补，不重构。
- **不做 (A) 意图分类评测**。case 带黄金 intent 标签是为 (A) 复用，但 (A) 的分类准确率/短路率评测是 spec 04 的事，本 spec 只产出共享 query 池。
- **不做 ④ 降级模式评测**。降级模式 MRR/可用率是 ④ spec 的事，本 spec 的消融只测完整链路。
- **不做线上 A/B**。线上化决策靠离线 sealed test 消融，不上线上实验。
- **不引入 LLM Judge 替代人工证据**。annotation guide 第 1 节明确 LLM Judge 只作辅助诊断，不作主证据。
- **不建数据标注 UI**。标注直接在 YAML 文件里做，复用 schema 校验。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- 评测集来源 = 真实视频 + 人工标注（决策记录第 5 节）✅
- (A)/(B) 共享 query 池，每条带两类标签（决策记录第 3 节）✅
- (B) MRR 只在完整链路算，`small_talk`/`video_overview` 零检索 intent 不进 MRR 分母（决策记录第 3 节）——本 spec 的 `category` 即 intent，`EvaluateMetrics` 已按 `ByCategory` 聚合，MRR 分母只在 `answerable: true` 且该检索类 case 上算 ✅
- 降级模式不进 (B) 评测（决策记录第 3 节）✅
- 跨视频 = 显式 Collection，case 的 `source_group` 含多 `video_id`（决策记录第 5 节）✅

### 数字占位符（本 spec 产出的简历可用数字）

本 spec 验收只跑通了**结构**（字段齐备 / 失败进分母 / P95 含失败按 0 计 / 四档可跑 / sealed test 无 token 被拒）。真实跨档差异数字必须由真实线上 retriever 跑 sealed test 产出——离线 hermetic retriever（测试用 fake）无法产出跨档差异（它不读 variant config，四档数字相同）。**离线 hermetic 数字（结构验证，非简历用）**：mini 3 条 case / 1 个视频 / 1 个 source_group，P95=0ms（fake retriever 零成本），Recall@5/MRR 各档均 0.500（含 1 条 forced-failure answerable case 进分母按 0 计）——这些数字证明流水线通，但**禁止**作为简历数字。简历可写数字仍是下列真实跑出来的（用真实线上 retriever 跑 sealed test 后回填，由 (B) 推进时补）：

- `__` 条 case（规模目标 30-50）/ `__` 个视频 / `__` 个 source_group
- Recall@5 `__`→`__`（vector_only → 选定档）
- MRR `__`→`__`
- P95 检索延迟 `__`ms（含失败按 0 计 的主 P95 + 成功检索子集 P95 两数）
- 规则层短路率 `__`%（这是 (A) 的数字，但共享 query 池在本 spec 产出，故 (A) spec 依赖本 spec 的数据集）

### 简历允许写什么 / 禁止写什么（本 spec 对应 (B) bullet 的预演）

**允许写**：
- "建立固定 RAG 评测集（__ 条 case，__ 个真实视频）与离线单变量消融：纯向量 / BM25 混合 / RRF 融合 / 模型 rerank，以 Recall@5 / MRR / P95 与置信区间判定，据此决定线上默认开 BM25 混合 + rerank 超时降级基线。"
- 具体数字（Recall@5、MRR、P95）—— **必须**在本 spec 的验收命令跑出来后填，不许估算。
- "评测驱动线上化决策"叙事 —— 有 `experiment-registry.yaml` + `productionRetrievalConfig` 改动可指。

**禁止写**：
- "我写了评测框架" —— 框架是上一轮已有的，不是本轮简历点。
- "MRR 0.78→0.93" —— 这是旧稿数字，必须用本 spec 真实跑出的数字替换，且必须能指到 `RunArtifact` 文件 + 可跑命令。
- "线上 A/B 验证提升" —— 不做线上 A/B，不能写。
- "LLM Judge 评测" —— LLM Judge 不作主证据，不能写成评测方法。
- 任何未在本 spec 验收命令下产出的数字。
