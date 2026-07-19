# RAG 混合检索严格量化实验设计

## 1. 目标

本实验用于验证 VidLens 当前正式 RAG 基线中，pgvector 向量检索加入 Go 侧 BM25 风格关键词检索和 RRF 融合后，是否比 vector-only 更稳定地把正确引用片段排在前面。

实验结论只覆盖检索排序和证据上下文，不等价于 LLM 回答准确率、事实正确率或生产 RAG 效果。最终简历数字必须来自当前 PostgreSQL/pgvector 运行时、全新严格数据集和一次 sealed test，不得恢复旧的 50 条 legacy 评测数字。

## 2. 非目标

- 不比较 Milvus 与 pgvector 的性能优劣；
- 不评估 query rewrite、multi-query、neighbor expansion 或 model rerank；
- 不把 LLM Judge 或 Ragas 分数作为主要简历证据；
- 不为了提升 BM25 数字只构造精确关键词问题；
- 不使用 dev 结果代替 sealed test；
- 不在 test 结果未达门槛后修改同一数据集版本继续调参。

## 3. 旧证据隔离

正式实验开始前执行以下证据重置：

- `docs/eval/resume-quant-results.md` 增加醒目的 legacy/deprecated 标记；
- `docs/eval/rag-quant-cases.yaml` 保留历史用途，但明确不可作为当前简历证据；
- `docs/eval/legacy-baseline-manifest.json` 继续保持 `resume_evidence_eligible=false`；
- 新数据集使用独立版本，例如 `rag-resume-v2`；
- 新 experiment registry 只登记当前 commit、当前 pgvector artifact 和新数据集。

旧报告中的任务 ID、MySQL 语料身份和指标不得进入新报告的聚合或对照。

## 4. 单变量实验

实验只允许一个配置变量变化：`enable_bm25`。

### 4.1 Baseline

```text
pgvector vector retrieval
enable_bm25 = false
query_mode = original
neighbor_window = 0
reranker_mode = none
```

### 4.2 Candidate

```text
pgvector vector retrieval
enable_bm25 = true
RRF fusion enabled by the existing hybrid pipeline
query_mode = original
neighbor_window = 0
reranker_mode = none
```

以下变量必须完全相同：Embedding Provider/模型、chunker strategy/version、chunk size、overlap、TopK、CandidateK、RRF K、语料、向量 artifact、用户和 task 权限范围。

严格 CLI 现有的 `ValidateSingleVariableAblation` 必须通过。任何同时修改第二个检索变量的运行都不属于本实验。

## 5. 数据集规模与划分

数据集固定为 15 个 source group、至少 15 个视频和 120 条 case。每个 source group 恰好标注 8 条 case，其中 7 条 answerable、1 条 unanswerable。主指标只使用 answerable case，因此每个 source group 对主指标固定贡献 7 条 case，使 source-group 宏平均与 answerable case 整体平均具有一致权重：

| Split | Source group 数 | Case 数 | 用途 |
| --- | ---: | ---: | --- |
| Train | 3 | 24 | 熟悉标注口径和验证工具，不参与最终结论 |
| Dev | 6 | 48 | 验证配置、数据和执行流程 |
| Sealed Test | 6 | 48 | 最终验收，只按门禁运行一次 |

划分规则：

- 先按内容来源确定 `source_group`，再划分 split；
- 同一系列、同一课程、同一来源或高度相似内容必须处于同一 source group；
- source group 和 video ID 不得跨 split；
- 如果视频来源彼此独立，使用一视频一 source group；同系列视频必须合并为同一 source group，并在该 group 内合计标注 8 条 case；
- 不能用少量视频堆积大量相似问题冒充大样本。

所有视频必须完成当前 pgvector 索引，并具有可核验的 `video_chunks` 和稳定 vector identity。

## 6. Case 分布

整体数据集按以下目标比例构造，允许因整数取整出现少量偏差：

| 类型 | 目标占比 | 主要风险 |
| --- | ---: | --- |
| 专有名词、缩写、精确关键词 | 25% | 向量可能淡化字符串身份 |
| 同义表达和语义改写 | 20% | 关键词检索可能无帮助或产生噪声 |
| 数字、日期、次数、版本号 | 15% | 精确 token 和 ASR 表达变化 |
| 多证据问题 | 15% | 只命中一个片段不足以完整回答 |
| ASR 错词、近音词、口语表达 | 10% | 两类检索都可能失败 |
| 无答案与相似干扰片段 | 10% | 错误接受和错误引用 |
| 普通直接事实 | 5% | 基础检索能力校准 |

问题必须基于视频真实内容编写，但标注者在确定问题、答案要点和证据前不得查看 baseline/candidate 的检索结果。

## 7. 标注合同

每条 case 必须符合现有 strict schema，并至少包含：

- 唯一 `case_id`；
- `video_id`、`source_group` 和 split；
- question、category、difficulty 和 answerable；
- answerable case 的 required answer point；
- 正确 evidence group 和 relevance；
- 无答案 case 的相似 negative confusers；
- 标注说明和争议处理记录。

当前 ASR 语料不使用推测出来的时间戳。证据优先绑定 `video_chunks.vector_id` 或严格 snapshot 导出的稳定 context identity，并通过 chunk index、内容哈希和原始转写人工核验。

至少 20% case 由第二位人工标注者独立复核，优先覆盖 hard、无答案、多证据和精确实体 case。争议必须回看原视频或原始转写裁决，不能以任一候选系统输出为答案。

## 8. 预注册指标与门槛

### 8.1 主指标

主指标预注册为 MRR@5。原因是旧小样本的 Recall@5 已接近饱和，而本实验主要验证正确证据是否更靠前。

当前 registry 的 `minimum_effect` 表示“bootstrap 置信区间下界必须达到的值”。本实验在 registry 中固定 `minimum_effect: 0.0`，用于检验统计方向；同时扩展分析结果，增加独立的 `practical_effect_pass`，用于检验实际意义。两者都作用于 source-group 等权宏平均 MRR@5；由于每个 source group 固定 7 条 answerable case，该口径与当前 `MetricReport.Overall.MRR` 的 answerable case 平均一致。

MRR 主指标明确排除 unanswerable case。paired bootstrap 构造 observation 时必须过滤 `answerable=false`，不能把它们的零值混入 source-group 均值。由于每个 source group 固定 7 条 answerable case，bootstrap observed effect、baseline/candidate macro MRR 和当前 `MetricReport.Overall.MRR` 使用相同分母。unanswerable case 仍进入 Answerability precision/recall/F1、failure rate 和逐 case artifact，不得从整个评测中删除。

候选方案只有同时满足以下条件才判定为具有可写入简历的排序收益：

- source-group 宏平均 MRR@5 点估计绝对提升至少 0.03；
- source-group paired bootstrap 的 95% CI 下界大于或等于 0，且置信区间不能退化为 `[0, 0]`；
- failed case 保留在分母中，不得删除。

实施必须让最终 analysis 同时输出 baseline macro MRR、candidate macro MRR、observed effect、置信区间、`statistical_effect_pass` 和 `practical_effect_pass`。最终 `passed` 状态是两项门槛与全部 guardrail 的合取，不能只依赖当前 `primaryEffectPass`。

### 8.2 Guardrails

- Recall@5 相比 baseline 下降不超过 0.01；
- Context Precision@5 下降不超过 0.01；
- Answerability F1 下降不超过 0.02；
- baseline 和 candidate 的 execution failure rate 都必须为 0；该条件作为 analysis 的硬门禁实现，不伪装成当前 registry 已支持的普通 metric guardrail；
- 任何跨用户、跨 task、跨 embedding model 或跨视频 evidence 命中都直接判定实验无效。

严格 evaluator 不得继续把 case 的期望 `VideoID` 直接写入返回 evidence。它必须根据返回的 `chunk_id/evidence_id` 回查 PostgreSQL `video_chunks`，再关联实际 task 和 asset，生成真实 `user_id/task_id/video_id/embedding_model` provenance。provenance 与请求 scope 不一致时，run 立即失败且不得产出可用指标。

### 8.3 辅助指标

- Recall@5；
- nDCG@5；
- Context Precision@5；
- Complete Evidence Recall；
- Answerability precision/recall/F1；
- 分类别、视频和 source group 指标；
- 首个相关片段排名分布；
- 检索核心延迟和包含 query embedding 的总检索延迟。

简历只能选择预先定义并通过门槛的指标，不允许在 test 后从多个辅助指标中挑最好看的一个冒充主结论。

## 9. Artifact 冻结

在正式 dev experiment 前冻结并记录：

- dataset version、manifest hash 和 split content hash；
- corpus SHA-256；
- chunk manifest SHA-256；
- vector artifact SHA-256；
- baseline/candidate 配置 SHA-256；
- Git commit；
- pgvector 表和 embedding model 身份；
- chunker strategy/version/size/overlap；
- TopK、CandidateK、RRF K；
- Bootstrap iterations、confidence level 和 seed。

推荐 bootstrap 配置为 5000 次、95% 置信水平和固定 seed。正式值写入 experiment registry 后不得更改。

当前 strict evaluator 已支持 dataset、registry、artifact hash、单变量消融和 source-group paired bootstrap。它尚不支持本设计要求的最终 sealed test 执行、failure-rate 硬门禁、真实 retrieval provenance、实际意义门槛和逐 case 延迟证据；这些都是正式实验实施的必需工作，不能用人工检查代替。

## 10. 延迟测量设计

质量评测和延迟评测共享同一数据集与配置，但分别出报告，避免 Provider 网络波动污染检索质量结论。

逐 case 记录：

- query embedding duration；
- pgvector retrieval duration；
- BM25 duration；
- RRF duration；
- retrieval core duration；
- total retrieval duration。

延迟运行要求：

- 先执行固定数量 warm-up，不计入报告；
- baseline/candidate 使用 AB/BA 或按固定 seed 随机交替顺序；
- 使用相同 query、用户、task scope 和 embedding model；
- 同时报告 core latency 与包含 embedding 的 total latency；
- 样本量达到数百次后报告 P50/P95，否则只报告中位数和范围；
- retrieval core P95 额外增加门槛固定为不超过 30ms。

如果严格 runner 的运行顺序仍是“全部 baseline 后全部 candidate”，该结果不可用于延迟结论，因为 candidate 可能系统性受益于数据库缓存。实施必须增加交替顺序或建立独立的配对延迟 runner。

## 11. Sealed final runner 与执行流程

当前 `cmd/rag-eval` 明确禁止执行 sealed test，因此正式实验必须增加一个独立审计的 final-run 路径。该路径可以是单独命令，也可以是与 dev 命令物理隔离的入口，但必须满足以下合同：

1. final runner 不提供调参或 snapshot-only 模式；
2. test 的视频范围通过不含 question、answer point 和 evidence label 的 scope manifest 预先冻结；
3. scope manifest 保存 `video_id/user_id/task_id/embedding_model` 映射，并使用独立 SHA-256 进入 registry；
4. 在不读取 sealed case 文件的情况下，根据 scope manifest 生成 test corpus/chunk/vector artifact，并把 hash 写入 preregistration；
5. final runner 启动后先校验 commit、配置、scope manifest 和 live evidence hash，再使用 token 加载 sealed case；
6. 加载 sealed case 必须追加一次完整 access event；同一个 access event 内完成 baseline、candidate、paired analysis 和 artifact 写入；
7. 中途失败也保留 access event，并将该 dataset version 视为已消费，不能回到 dev 调参；
8. 输出先写入临时目录，全部 artifact 成功写入并校验后再原子发布，避免留下只有一半结果的正式报告；
9. final runner 不打印 sealed question、答案要点、完整 evidence 文本或 token。

严格顺序如下：

1. 选择视频并冻结 source group；
2. 完成 train/dev/test 物理隔离；
3. 人工编写和复核 case；
4. 根据不含问题与答案的 scope manifest 计算 train/dev/test 各自的 corpus、chunk 和 vector artifact hash，不读取 sealed test case；
5. 使用 train 检查标注工具，不得调检索算法；
6. 在 dev 验证 strict runner、指标和 artifact；
7. 写入 experiment registry，冻结主指标、两层门槛、配置、scope manifest hash、evidence hash 和 commit；
8. 运行正式 dev 对照，确认工具链没有错误；
9. 通过独立 final runner 和 sealed token 门禁运行一次 test；
10. 无论通过或失败都保存完整 artifact 和结论。

test 未达门槛后不得修改同一 dataset version 继续运行。后续改进必须创建新的数据集或实验版本，并保留失败实验。

## 12. 实验失败处理

- Sealed access event 追加前发生的 Provider/数据库预检失败：不加载 sealed case、不追加 access event，可以修复预检环境后使用新 run ID 重跑；
- Sealed access event 追加后发生的 Provider、数据库或执行器故障：保留失败 artifact，并将该 dataset version 视为已消费，禁止使用新 run ID 重跑同一 dataset version；必须创建新的 dataset/experiment version；
- access event 之后的单个 case Provider 临时故障：case 保留并按执行失败计零，最终 run 必须失败，不得把失败 case 从分母删除；
- pgvector projection 与 PostgreSQL chunk manifest 不一致：停止实验，先审计或重建 projection；
- 数据集 hash、配置 hash 或 commit 不匹配：拒绝执行；
- 标注争议未解决：case 不得进入 sealed dataset；
- test 结果无显著收益：保留报告，简历继续使用保守的技术实现描述；
- latency 超过 guardrail：不得只报告质量提升而隐藏代价。

## 13. 简历准入条件

量化数字只有在 sealed test 同时通过主指标、guardrail、artifact 校验和权限隔离检查后才能进入简历。

通过时使用以下结构，所有数字由最终报告生成：

> 基于 pgvector 向量检索与 Go 侧 BM25 风格关键词检索构建混合召回，通过 RRF 融合排序；在覆盖【实测视频数】个视频、【实测 case 数】条人工标注问题的严格离线评测中，相比 vector-only 将 MRR@5 从【基线】提升至【候选】，Recall@5 从【基线】提升至【候选】，核心检索 P95 增加【实测值】，并返回可追溯引用片段。

如果只有主指标通过而 Recall@5 基本持平，句式改为：

> ……将 MRR@5 从【基线】提升至【候选】，Recall@5 保持在【实测值】，提升正确引用片段的前排命中能力。

若主指标或 guardrail 未通过，不使用任何“提升”数字，只保留严格离线评测工作流作为工程能力说明。
