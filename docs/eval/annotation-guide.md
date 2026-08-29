# VidLens 严格 RAG 评测标注指南

## 1. 目的与边界

该数据集用于离线比较 VidLens 的检索方案，不用于训练模型，也不允许从 sealed test 反向调参。确定性检索指标是主证据；LLM Judge 只能作为辅助诊断，不能替代人工证据与答案要点。

## 2. 文件隔离与数据冻结

严格评测使用四个彼此独立的文件：manifest、train cases、dev cases、sealed test cases。

- manifest 只保存 `source_group`、`video_id`、split 归属与摘要，不得包含问题、答案要点或证据内容。
- train/dev 通过 `LoadSplitDataset(manifestRaw, splitRaw, ...)` 分别加载；日常 loader 不接收、也不读取 test 文件。
- 每个独立 split 文件都必须在 manifest 中登记 `content_sha256`。该值由 `ComputeSplitContentSHA256` 对规范化内容计算，加载时重新计算并严格比对。
- test 还必须设置 `sealed: true` 与 `access_token_sha256`。token 明文不得写入仓库、日志、示例命令或评测产物。
- `schema_version` 固定为 `"1"`；所有 SHA-256 必须是 64 位小写十六进制字符串。

先按内容来源定义 `source_group`，再以 source group 为最小单位划分 train/dev/test。同一 `source_group` 或 `video_id` 不得重复登记，也不得跨 split。划分冻结后计算 `manifest.sha256`；生成问题、查看答案或调参不得改变归属关系。

## 3. 问题类型与难度

建议覆盖事实定位、关键词精确匹配、同义表达、跨片段组合、时间顺序、否定/无答案，以及后续 OCR 场景。每条 case 必须填写非空 `category`，不要在一个类别中混合不同判定口径。

- `easy`：答案由一个直接片段给出，问题和原文表述接近。
- `medium`：需要同义理解、局部上下文或两个相邻证据点。
- `hard`：需要多个非相邻证据组、容易受干扰片段误导，或必须正确拒答。

`difficulty` 只能取 `easy`、`medium`、`hard`，并按取证要求而非标注者主观感觉标注。

## 4. Answerability 与答案要点

- `answerable: true`：视频中存在充分证据；至少有一个正证据范围和一个 `required: true` 的答案要点。
- `answerable: false`：视频无法支持问题结论，不得引入视频外知识补齐答案。
- 每个 `answer_point` 只表达一个可独立核验的事实，`id` 和 `text` 必填；同一 case 内的 answer point ID 不得重复。
- 无答案样例应尽量加入语义相近但不足以作答的 `negative_confusers`，用于检验错误接受。

## 5. 证据定位与唯一性

每条证据必须有非空且在 case 内唯一的 `id`。正证据还必须填写 `group_id` 和 relevance；正证据与 `negative_confusers` 之间也不能复用同一个 evidence ID。

证据至少使用一种稳定定位方式：

1. 时间范围：毫秒级 `[start_ms, end_ms)`，并保证 `end_ms > start_ms`；或
2. `context_ids`：引用检索语料中的稳定 context/chunk identity，值不能为空且不可重复。

如果同时提供时间范围与 `context_ids`，任一定位命中即可作为候选证据匹配，但仍须满足来源和视频隔离规则。`context_ids` 适用于没有可靠时间戳但有稳定 chunk identity 的语料。 VidLens 当前 ASR baseline 必须使用 `video_chunks.vector_id`，并通过 `ListEvidenceManifest` 导出的 chunk index、内容哈希和原文核验。索引重建若改变分块或内容，必须重新导出 manifest 并升级数据集哈希；在 ASR 真正保存可靠时间范围前，不得由字符位置估算时间戳。

来源取值为 `asr`、`ocr` 或 `both`：

- relevance `3`：直接、充分支持答案要点。
- relevance `2`：支持答案，但需要与其他上下文组合。
- relevance `1`：有帮助的背景，单独不足以回答。

`group_id` 表示完整证据结构：同一 group 下的多个范围互为可替代证据，命中任意一个即可覆盖该组；不同 group 是完整回答所需的不同证据组。Complete Evidence Recall 只有在全部组被覆盖时才为 1。

## 6. 预注册的命中规则

每次实验必须在 registry/配置中冻结 `k`、`boundary_tolerance_ms`、`max_chunk_duration_ms` 与 `min_evidence_coverage`，运行后不得按结果修改口径。

一次命中必须同时满足：

- `RetrievedContext.VideoID == Case.VideoID`，其他视频中内容或时间范围相同也不计命中；
- stable context identity 或预注册的时间覆盖规则成立；
- 来源兼容，ASR 结果不能冒充仅 OCR 证据，反之亦然，`both` 与两类来源兼容；
- 时间定位场景下，检索块未超过预注册的最大时长。

## 7. 失败样例与报告口径

executor 失败的 answerable case 不能从统计中删除。它必须进入 Recall@K、MRR、nDCG、Context Precision 和 Complete Evidence Recall 的分母，并在这些检索指标上按 0 计；否则系统性失败会被错误包装成检索提升。

JSON、CSV 和 Markdown 报告都必须展示 `failure_rate`，逐 case artifact 必须保留失败 stage、code 和 message。无答案指标仍按其独立口径计算。

比较候选方案时，minimum effect 不是只看点估计越过阈值：higher-is-better 指标要求置信区间下界 `>= minimum_effect`；lower-is-better 指标要求置信区间上界 `<= -minimum_effect`。

## 8. Sealed test 真实门禁

sealed test 只能通过 `LoadSplitDataset` 加载，并同时提供：

- 正确的 sealed token；
- access registry 路径；
- 含时间、experiment、run、commit 等审计身份的完整 `SealedAccessEvent`。

loader 只有在 token 验证成功、test 内容摘要匹配且 access event 成功追加到只增不改的 registry 后，才返回可执行的 test Dataset。`Runner.Run(..., SplitTest, ...)` 会在调用 executor 前检查这项私有登记状态；直接拼装 Dataset、只验证 token 或登记失败都不能执行 test。

一次 test 访问后，不得继续使用同一 `dataset_version` 调参。未达门槛时如实记录结果，后续实验创建新的 dataset version。

## 9. Artifact 摘要与可复现性

语料、chunk manifest、向量 artifact、配置和 prompt 的摘要必须绑定实际文件，而不是信任调用方手填。使用 `BindArtifactFileDigests` 从文件字节计算 SHA-256 写入 `RunMetadata`，产出前或复核时使用 `VerifyArtifactFileDigests` 重新计算；文件被替换或修改后必须失败。

报告还应保留代码 commit、数据集版本、manifest/content 摘要、模型身份、Milvus collection/index/metric 参数和逐 case 结果。所有摘要均执行严格的小写 SHA-256 校验。

## 10. 复核与争议处理

- 至少 20% 样例由第二位标注者独立复核，优先抽查 hard、无答案和跨片段问题。
- 复核 source group 防泄漏、答案要点、evidence/context identity、时间边界、group、relevance 与 answerability。
- 争议样例必须回看原视频或原始转写后裁决，并在 `notes` 记录原因；不能用候选系统输出反推标注。
- 无法可靠确定时间且没有稳定 context identity 的样例，不得进入 strict 数据集。
