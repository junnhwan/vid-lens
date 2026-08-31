# 检索与回答链路

## 标准问答路径

标准聊天请求按以下顺序执行：

1. 根据会话绑定的视频或知识库确定检索作用域。
2. `IntentRouter` 先用无副作用的规则层提取时间、实体和句式信号；无法可靠判断时，才调用 LLM 分类或改写能力。
3. `ExecutionPolicy` 把意图映射为检索预算和策略，包括是否改写、作用域、候选数量、是否使用关键词检索、融合、相邻片段回填和 rerank。
4. `RetrievalPipeline` 执行查询改写、关键词/向量召回、跨查询 RRF 融合、上下文扩展和排序，输出带稳定 evidence ID 的候选片段。
5. LLM 只能基于已检索证据生成答案；回答返回引用片段，便于追溯到视频和时间位置。

## 事实源和作用域

- `video_chunks` 是检索内容的关系数据事实源，向量后端只是可重建的检索投影。
- 单视频会话只在当前视频范围内检索；知识库会话按成员视频集合检索。
- 检索、排序和回答生成是分开的职责：召回负责覆盖候选证据，排序负责调整顺序，生成不能自行创造证据。

## 证据约束

回答生成后会校验引用标记是否对应本次检索得到的 evidence ID。发现超范围引用时，最多执行一次补证据检索；仍没有新增支撑证据时，返回受控的无证据结果，不直接交付未经约束的结论。

这条约束只属于标准问答路径的单轮后处理，不扩展为 Planner-Executor-Critic 多轮 ReAct，也不接管实验性的 Video Agent 路径。

## 时间线定位

`timeline_locate` 使用规则层解析“15:00”“第 15 分钟”或显式区间，并只保留与该范围重叠、且 `time_range_status` 为 `exact/coarse` 的候选 chunk。单点按半开区间包含关系匹配，范围按重叠关系匹配；`unknown` 历史数据不会被当作时间命中。向量后端仍是可重建投影，候选命中后由 PostgreSQL `video_chunks` 回填并校验时间、模态和 source refs，再执行时间过滤。

公开 citation 同时返回 `modality`、毫秒范围、时间状态、source mapping 状态和稳定 source refs。`Source` 仍只表示 vector/keyword/hybrid 召回通道。时间过滤会把候选召回预算临时放大到上限后再筛选，且不执行可能跨越请求范围的 chunk-index 邻接扩展；历史索引若没有可靠映射，结果可为空并触发现有的受控降级。

## 模态感知融合

关系事实中的证据模态分为 `transcript`、`visual_ocr` 和 `visual_caption`。检索仍先执行查询改写、vector/keyword 召回、RRF、关系回填和 rerank，然后在候选集上执行低成本模态排序：普通内容问题保持 transcript 优先；字幕、图表、幻灯片、颜色、布局和“画面显示什么”等问题提升视觉证据；询问“画面与解说是否一致”时，若候选允许，最终集合至少保留一条 transcript 和一条视觉证据。

模态排序不把 OCR 或 caption 合并进 transcript，也不把 `Source` 改写成模态。每个候选另外保存 `modality_intent`、`modality_score` 和 `modality_rank`，最终 citation 继续携带原始 `modality`、半开时间范围和稳定 source refs。视觉召回失败时 transcript 可继续回答并明确未取得画面证据；transcript 缺失时 visual-only 索引仍可回答字幕、演示和图表类问题；两者冲突时生成器必须分别引用并保留不确定性。

## 实验性路径

显式调用 `/chat/.../messages/agent` 时，Video Agent 可以使用白名单工具执行有界的研究循环。除 transcript 工具外，`search_visual_evidence` 可限定为视觉模态检索，`inspect_visual_window` 可在当前视频的合法时间范围内读取有上限的已持久化 OCR/Vision observation。该路径有最大步数和重规划次数限制，并通过观察结果绑定证据；它不改变标准聊天接口的默认行为，也不执行查询时在线 VLM 像素分析。
