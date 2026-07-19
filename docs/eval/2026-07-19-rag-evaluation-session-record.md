# 2026-07-19 RAG 评测会话记录

## 1. 目的

本记录汇总 2026-07-19 会话中 VidLens RAG 检索优化的全部主要实验、口径变化、代码状态和最终简历结论，方便后续复盘。

本次核心问题：原有 vector-only 基线已经较高，BM25 + RRF 出现负优化，需要寻找能够真实提高证据排序质量、且可以量化的方案。

## 2. 评测数据与统一口径

- 数据规模：15 个视频、45 条人工标注问题。
- 类别：semantic、keyword_exact、detail 各 15 条。
- 正确证据：每条问题绑定稳定的 `target_vector_id`。
- 正式命中标准：返回结果中出现目标 `vector_id`，不再使用关键词包含作为正确性标准。
- 当前性质：本地开发集评测，不是 sealed test，也不是线上生产指标。
- Embedding：`text-embedding-3-small`。
- 正式检索参数：CandidateK=30、TopK=5、`min_score=0.35`。

主要指标：

- Hit@1：正确证据是否排在第一位。
- Recall@5：正确证据是否进入前五位。
- MRR@5：正确证据排名倒数的平均值。
- nDCG@5：衡量正确证据在前五位中的排序质量。

## 3. 实验结果汇总

### 3.1 关键词包含口径：结果为正，但不可用于简历

| 方案 | Recall@5 | MRR@5 |
| --- | ---: | ---: |
| Vector-only | 88.89% | 0.591 |
| pgvector + BM25-style + RRF | 100.00% | 0.682 |

表面提升：Recall@5 增加 11.11 个百分点。

问题：该实验通过“返回文本是否包含预期关键词”判断命中，可能把包含关键词但不是正确证据分片的结果计为正确。因此，该数字被废弃，不作为简历证据。

证据文件：`.worktrees/resume-quantification/.logs/rag-resume-v2/retrieval-only.md`。

### 3.2 精确 vector_id：BM25 + 等权 RRF 产生负优化

| 方案 | Recall@5 | MRR@5 | P95 检索延迟 |
| --- | ---: | ---: | ---: |
| Vector-only | 95.56% | 0.845 | 1.57 ms |
| pgvector + BM25-style + RRF | 93.33% | 0.826 | 5.28 ms |

变化：

- Recall@5：下降 2.22 个百分点。
- MRR@5：下降 0.019。
- P95 延迟：增加 3.72 ms。

主要原因：中文问题被切成大量 2～4 字 n-gram；等权 RRF 会优先提升同时出现在 vector 和 BM25 列表中的片段，弱词法噪声可能把原本排名靠前的正确向量结果挤出 Top5。

结论：当前语料和单视频小候选空间下，不再把 BM25 + RRF 作为正式检索主链路。

证据文件：`.worktrees/resume-quantification/.logs/rag-resume-v2/manual-exact-result.md`。

### 3.3 探索实验：Vector Candidate@20 + Qwen3 Reranker

该实验未使用生产 `min_score=0.35`，属于探索结果：

| 方案 | Hit@1 | Recall@5 | MRR@5 | nDCG@5 |
| --- | ---: | ---: | ---: | ---: |
| Vector-only | 75.56% | 95.56% | 0.845 | 0.873 |
| Vector + rerank | 88.89% | 97.78% | 0.933 | 0.945 |

由于参数与正式运行配置不完全一致，该组结果不作为最终简历数字。

### 3.4 生产参数匹配：Vector Candidate@30 + Qwen3 Reranker

配置：

- Vector-only，不使用 BM25/RRF。
- CandidateK=30，TopK=5。
- `min_score=0.35`。
- Reranker：`Qwen/Qwen3-Reranker-4B`。
- Rerank endpoint 从用户 Embedding endpoint 推导，复用 Embedding API Key。

| 方案 | Hit@1 | Recall@5 | MRR@5 | nDCG@5 |
| --- | ---: | ---: | ---: | ---: |
| Vector-only | 75.56% | 93.33% | 0.841 | 0.865 |
| Vector + rerank | 86.67% | 95.56% | 0.907 | 0.920 |

变化：

- Hit@1：增加 11.11 个百分点。
- Recall@5：增加 2.22 个百分点。
- MRR@5：增加 0.066，约提升 7.9%。
- nDCG@5：增加 0.055。
- Win/Tie/Loss：9/32/4。

稳定性：相同配置完整运行两次，45 条 case 的排名差异为 0，指标完全一致。

延迟：

- 两次运行的 P50 约 1.16～1.27 秒。
- P95 受中转站网络波动影响，约 2.41～5.69 秒。

证据文件：

- `.logs/rag-resume-v2/vector-rerank-result.md`
- `.logs/rag-resume-v2/vector-rerank-result.json`
- `.logs/rag-resume-v2/vector-rerank-result-run1.json`

### 3.5 完整策略：Query Rewrite + Multi-Query Vector + Rerank

配置：

- 保留原始问题。
- LLM 额外生成查询，总计最多 3 路查询。
- 每路执行向量候选召回。
- 跨查询融合候选。
- 使用 `Qwen/Qwen3-Reranker-4B` rerank。
- CandidateK=30、TopK=5、`min_score=0.35`。
- 不使用 BM25、RRF 关键词混合检索和邻居扩展。

| 方案 | Hit@1 | Recall@5 | MRR@5 | nDCG@5 |
| --- | ---: | ---: | ---: | ---: |
| Vector + rerank | 86.67% | 95.56% | 0.907 | 0.920 |
| Query Rewrite + Multi-Query Vector + rerank | 88.89% | 97.78% | 0.930 | 0.942 |

相对 Vector-only 最终变化：

- Hit@1：75.56% → 88.89%，增加 13.33 个百分点。
- Recall@5：93.33% → 97.78%，增加 4.45 个百分点。
- MRR@5：0.841 → 0.930，增加 0.089。
- nDCG@5：0.865 → 0.942，增加 0.077。

相对“Vector + rerank”：1 胜、44 平、0 负。其中一条原本未进入 Top5 的问题被 Query Rewrite 找回并由 reranker 排到第一位。

Fallback：

- 45 条中有 3 条 Query Rewrite 调用失败或未产生有效改写，自动保留原问题回退。
- Rerank fallback 为 0。

延迟：

- 完整成功运行的平均/P50/P95：9.39/8.34/16.84 秒。
- 第二次完整复跑时中转站出现单请求 77～92 秒延迟，因耗时过高中止；因此 Query Rewrite 的质量增益已观察到，但延迟和完整复跑稳定性仍需继续验证。

证据文件：

- `.logs/rag-resume-v2/rewrite-vector-rerank-result.md`
- `.logs/rag-resume-v2/rewrite-vector-rerank-result.json`
- `.logs/rag-resume-v2/rewrite-vector-rerank-result-run1.json`

## 4. 统计解释

按 15 个视频/source group 做 20,000 次 cluster bootstrap，生产参数下 Vector + rerank 相对 Vector-only 的 95% 区间为：

- Hit@1 delta：[-2.22, 24.44] 个百分点。
- Recall@5 delta：[0, 6.67] 个百分点。
- MRR delta：[-0.007, 0.137]。

由于只有 15 个 source group，Hit@1 和 MRR 的区间仍覆盖 0。本次结果适合表述为“本地 45 条人工标注评测中的提升”，不能表述为已证明线上显著收益。

## 5. 当前代码状态

当前工作区已完成但尚未提交、尚未部署：

- 正式检索关闭 BM25，使用 pgvector 向量候选召回。
- 支持 `rag.rerank_model`，当前配置为 `Qwen/Qwen3-Reranker-4B`。
- 支持 `rag.rewrite_queries`，当前配置为 3。
- Query Rewrite 始终保留原始问题；失败时回退原问题。
- 模型 rerank 失败时保留原向量候选顺序。
- 普通 RAG Chat 和 Video Agent 共用该检索链路。

主要修改路径：

- `cmd/server/wiring.go`
- `internal/config/config.go`
- `internal/config/loader.go`
- `internal/service/chat.go`
- `internal/service/chat_prepare.go`
- `internal/service/rag_eval_config.go`
- `internal/service/video_agent.go`
- `config.yaml`

验证：

- `go test ./...`：通过。
- `go build ./cmd/server`：通过。
- `git diff --check`：通过。

## 6. 最终简历建议

推荐表述：

> 基于 **pgvector** 构建视频转写 RAG，结合 **Query Rewrite、多路向量召回、rerank** 与引用片段回传优化检索链路，使检索首位命中率提升 **13.33 个百分点**，增强视频问答的证据定位能力与结果可追溯性。

使用边界：

- “13.33 个百分点”来自本地 15 个视频、45 条人工标注问题。
- 不写成线上提升、生产收益或统计显著结论。
- 面试追问时说明：Vector-only 基线已较高，因此最终主要观察 Hit@1、MRR 和 nDCG，而不是只看 Recall@5。
- 面试追问时主动说明 Query Rewrite 的延迟代价和 fallback 设计。

## 7. 后续工作

1. 将数据集扩展到更多独立视频/source group，并建立真正隔离的 holdout 或 sealed test。
2. 对 Query Rewrite 做第二次完整稳定复跑，记录中转站延迟分布。
3. 评估是否按问题复杂度选择性启用 Query Rewrite，避免所有请求都承担 8～17 秒额外延迟。
4. 分别消融 Query Rewrite、Multi-Query 和 rerank，避免把组合收益错误归因给单一组件。
5. 在本地代码提交和部署前，再执行一次完整测试、配置检查和线上 smoke test。
