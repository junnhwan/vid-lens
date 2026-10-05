# 评测资料

本目录保存可复现的评测规范、配置模板、schema 和示例数据。评测结果不能替代代码和当前运行配置，正式结果需要记录数据集版本、配置和产物哈希。

## 内容说明

- `dataset-schema.yaml`：严格（strict）数据集与 split 的 JSON Schema 结构定义，只描述 `--strict` 输入
- `annotation-guide.md`：严格数据集的 split 隔离、标注口径、证据定位与预注册规则
- `rag-cases.example.md`：live/legacy 案例列表格式示例，即非 `--strict` 模式下 `--cases` 读取的格式
- `ablation-configs/`：检索消融配置，六档变体 `vector_only`、`bm25_hybrid`、`rrf_fusion`、`model_rerank`、`rrf_rerank`、`rrf_model_rerank`。其中 `vector_only`、`bm25_hybrid`、`rrf_fusion`、`model_rerank` 由 `cmd/rag-eval/ablation_configs_test.go` 加载校验，并强制四档冻结同一组 k/chunker 参数；`model_rerank` 与 `rrf_rerank` 都是 deterministic 代理档，不代表真实模型重排收益
- `product-feedback.md`：回答反馈导出与产品回归候选流程

## 运行评测

命令在仓库根目录执行，且需要本地已按 `config.yaml` 配好 PostgreSQL/pgvector、案例中的 `task_id` 在本地库中真实存在。只校验案例与检索投影、不做 embedding 或 LLM 调用：

```powershell
go run ./cmd/rag-eval --config config.yaml --cases cmd/rag-eval/testdata/legacy-cases.yaml --output artifacts/eval/rag-results.md --preflight-only
```

去掉 `--preflight-only` 会执行完整评测：四种检索模式（Vector only、Vector + BM25 + RRF、Rewrite + MultiQuery + RRF、Rewrite + MultiQuery + RRF + Window + Rerank）以及普通 RAG 与 Agent 回答对比，会产生 embedding 与 LLM 调用。

live/legacy 模式的必填与默认项：

- `--cases`：必须显式给出。默认值指向本地私有评测目录，公开检出中不存在该文件。
- `--config`：默认 `config.yaml`，从仓库根目录运行可直接使用；`rag.enabled` 必须为 `true`，否则命令以 `RAG is disabled in config` 失败。
- `--output`：默认 `artifacts/eval/rag-results.md`，父目录由命令自动创建，`artifacts/` 已被 Git 忽略。
- 可选：`--top-k`、`--candidate-k`、`--timeout`、`--environment`、`--commit`、`--progress`（进度写 stderr）。
- `--rerank-model` 与 `--rerank-endpoint` 只属于 live/legacy 实验：只有显式给出 `--rerank-model` 才会追加模型重排档；`--rerank-endpoint` 可以省略而由 embedding 端点推导，但给了 endpoint 就必须给 model。两者都不能与 `--strict` 同时使用。

严格模式（`--strict`）另有必填项：

- `--dataset-version` 必填。`--manifest` 可选：给出时指向 split manifest，`--cases` 必须指向所选的那个物理 split 文件；省略 `--manifest` 时 `--cases` 必须是 combined dataset，且这种输入不能用于 `test`。
- `--experiment-registry` 默认值同样指向本地私有登记文件，公开检出必须显式覆盖。
- 执行实验要求 `--experiment-id`、`--variant-id`、三个冻结证据哈希 `--corpus-hash`、`--chunk-manifest-hash`、`--vector-artifact-hash`，以及 `--retrieval-config` 与 `--baseline-retrieval-config`；`--commit` 和 `--config-hash` 必须与预登记值一致，否则登记绑定失败。
- `--split` 取 `train`、`dev`、`test`；`--validate-only` 只校验数据集与实验登记，不执行检索。
- `--snapshot-only` 必须与 `--strict`、`--retrieval-config` 一起使用；`--preflight-only` 不能与 `--strict` 一起使用。
- 该命令不执行 sealed test：`--split test` 必须提供 `--manifest` 与正确的 `--sealed-test-token`，只能配合 `--validate-only` 做校验，正式 test 执行走单独审计的最终运行流程。
- `--sealed-access-registry` 默认 `artifacts/eval/sealed-access-registry.jsonl`。加载 test 会向它追加访问事件，train/dev 运行前会检查同一登记：已经访问过 test 的 `dataset_version` 不允许继续调参。

## 当前评测约束

- 数据集按版本和 split 管理，开发集与封存测试集保持隔离。
- 检索实验使用单变量消融，不能把多个机制变化归因到同一个结果。
- 实验登记记录数据集版本、代码提交、配置哈希和冻结证据哈希，结果报告和诊断信息写入 `artifacts/`。
- 真实评测输入和登记文件只存在于本地 `docs-private/eval/`，不作为公开工程文档提交。

真实评测数据集和实验登记可能包含本地视频内容或运行环境信息，统一放在被忽略的 `docs-private/eval/`。需要执行受保护评测时，使用当前本地数据集及本地访问登记文件。

评测运行产生的报告、日志和快照放入被忽略的 `artifacts/`，不要回写到 `docs/`。

## 使用本地已有资料执行集合产品评测

`rag-eval product`仍默认通过授权HTTP入口执行。显式传`--local-config config.yaml`时可读取本地数据库中指定用户已保存的AI配置，复用ConversationExecution、预算、journal及持久消息路径；不创建凭据，不重置会话。使用私有dev数据集、独立评测会话和包含代码/未提交源码指纹、资产、模型、索引版本及配置指纹的identity文件，结果文件不得已存在。

本地执行器不包含完整HTTP server wiring的provider admission、长期记忆和query-time视觉investigator。它可以验证真实模型/数据库对话，不能替代认证、流式代理或浏览器回放验收。结果同时保留失败与有限回答，`semantic_success`在独立源证据审核前保持null。P50/P95包含所有计划用例耗时；小样本探索结果不能证明提升或大集合SLO。普通Chat token缺少完整计量时保留unknown，不能当作零成本。
