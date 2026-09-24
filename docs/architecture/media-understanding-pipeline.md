# 视频理解管线

本文说明 VidLens 当前的视频处理、检索、Agent 与可靠性实现。模型输出属于候选观察或生成内容，系统通过来源、时间范围、模态和执行状态记录其上下文。

## 系统职责

VidLens 面向单视频和授权知识库提供长视频理解、时间定位、引用回放与 Agent 问答能力。PostgreSQL 保存业务事实和执行状态，MinIO 保存视频及抽取帧，Redis 保存短期状态与限流数据，RabbitMQ 承载长耗时处理任务，pgvector 保存可重建的向量投影。

正式会话入口为 Chat 与 Agent：

- Chat 使用标准检索链路生成带来源的回答。
- Agent 在服务端冻结的作用域、工具白名单和预算内执行 Planner/Tool/Observe 循环。
- 单视频会话使用一个视频任务；知识库会话使用经过服务端授权的成员任务集合。

## 视频处理流程

```text
上传或接入视频
  → PostgreSQL 创建任务、处理作业、重试预算与 dispatch lease
  → RabbitMQ 投递下载 / 转写 / RAG index 作业
  → 下载源视频并并行启动 ASR 与视觉处理
  → 保存带时间范围的转写 observation 与视觉 observation
  → 构建带模态、时间和 source refs 的 RAG chunk
  → 生成 pgvector 投影并更新任务状态
```

任务创建和消息投递分别受数据库事务、Publisher Confirm、消息幂等门和处理租约保护。任务状态、分片状态、视觉状态和索引状态独立保存，便于按阶段重试和复用已完成结果。

## ASR 转写

- 音频由 FFmpeg 提取为 ASR provider 使用的格式。
- 每个逻辑分片采用 300 秒 core window，并在相邻分片之间保留 5 秒 overlap。
- `window_start/end_ms` 记录实际送入 provider 的范围，`core_start/end_ms` 记录该分片负责的非重叠范围。
- 固定 worker pool 并行执行分片；每片保存 `segment_key`、provider/model、状态、retry count 和原始输出。
- 确定性 stitcher 按时间顺序合并相邻结果，去除 overlap 重复文本；重试和任务恢复只请求缺失或失败分片。
- provider 只能提供粗粒度时间时，结果标记为 `coarse`；没有可靠时间事实时使用 `unknown`，不按字符比例伪造精确时间。

## 离线视觉索引

视觉处理由每个视频的设置决定是否在转写时启动；关闭时也阻止问答中的按需抽帧，不删除已有证据。当前采样策略为 `scene-interval-v3`：结合场景变化帧与 30 秒定时帧，按全片时间范围在共享的 120 帧预算中选帧，图像缩放上限为 960。已保存帧更换后，旧检索索引提示重建。

每个稳定帧保存时间点、采样策略、对象定位、感知哈希和处理状态。OCR 与 Vision caption 分开调用、分开保存，分别形成 `visual_ocr` 与 `visual_caption` 证据；一侧失败不会覆盖另一侧成功结果。

## 时间感知 RAG

RAG 索引从 PostgreSQL 的转写和视觉事实构建 chunk。每个 chunk 携带：

- `modality`：`transcript`、`visual_ocr`、`visual_caption` 或 `unknown`；
- `start_ms/end_ms` 与 `time_range_status`：`exact`、`coarse` 或 `unknown`；
- `source_mapping_status`：`mapped`、`partial` 或 `unmapped`；
- 稳定 `source_refs`，指向转写 segment 或视觉帧；
- `chunker_strategy/version` 与索引 manifest 信息。

转写 chunk 采用段落、完整句子、从句、空白的递归边界，超出预算时才安全硬切；overlap 只复用完整语义单元。视觉 OCR 与 caption 保持独立模态，不与转写文本跨模态拼接成无法定位的单一事实。

当前索引版本为 build `3`、source mapping `source-map-v2`、chunker `recursive-sentence-source-v2`。pgvector 只保存可重建投影，检索命中后由 PostgreSQL 回填模态、时间和 source refs。

标准 Chat 检索组合向量召回、BM25、RRF、上下文扩展和重排；查询规则可根据请求配置 query rewrite 或多查询。Agent 使用 Planner 生成的检索问题，复用同一向量、关键词、融合和来源回填能力。

## Agent 执行

Agent 由 `VideoAgentService` 负责单一有界执行循环，当前工具包括：

- `search_transcript`：按问题检索转写和多模态索引；
- `get_transcript_window`：读取指定视频时间窗的转写；
- `search_visual_evidence`：检索已经完成的视觉索引；
- `inspect_visual_window`：读取指定时间窗的视觉证据；
- `investigate_visual`：在服务端确认的 seed windows 内按预算执行查询时视觉调查；
- `build_cited_answer`：基于已观察证据生成带引用回答。

Run 创建时冻结 owner、会话、视频/知识库范围、目标、AI profile、工具白名单和执行预算。Run、Step、ToolCall、checkpoint 与最终消息均由 PostgreSQL 持久化；lease token、version CAS、幂等事务和有限重试用于故障恢复。

当前策略限制工具数量、模型调用次数、检索次数、视觉调用次数、抽帧数量、token、费用和总时长。工具参数经过服务端 schema、owner、视频成员范围和证据引用校验；模型不能通过参数自行扩大知识库范围或注入 memory context。

Agent 支持同步和 SSE 流式请求。流式接口发送运行状态、公开规划摘要、工具步骤、回答和 citations；运行状态与消息接口支持断流后的只读查询，成功回答按 run 幂等保存。

## 查询时视觉调查

`investigate_visual` 与离线视觉索引职责分离：

1. 服务端先校验请求视频归属、知识库成员范围和 seed windows 是否覆盖已确认的转写或视觉时间线。
2. 调查器在限定时间窗内下载源视频，使用 FFmpeg 抽取少量原始帧并上传 MinIO。
3. 配置了 Vision provider 时，对受预算约束的帧调用 VLM；未满足调用条件时保留确定性的帧与时间事实。
4. 结果以 `video_visual_observations` 追加保存，包含视频 revision、帧哈希、FFmpeg 参数、对象 key、模型、prompt/capture 版本、结构化事实、信息缺口、原始响应哈希和状态。

相同视频 revision、查询和窗口可复用缓存。调查结果可以是 `sufficient`、`uncertain` 或 `budget_exhausted`，其作用是补充已定位窗口的观察，不代表对整部视频完成无界视觉搜索，也不把模型判断自动升级为事实。

## 存储与可靠性

- RabbitMQ 使用持久消息、Publisher Confirm、手动 ack、worker pool、prefetch、每队列 DLX/DLQ 和错误重投递。
- Redis 使用 `SETNX` 消息去重门、上传分片状态与 TTL、合并锁、MD5 去重、AI token 多桶 Lua 限流和用量缓存。
- PostgreSQL 保存任务、作业、处理 lease、ASR/视觉 observation、RAG chunk、Agent 执行记录和 AI usage ledger。
- processing lease 通过 token、版本、心跳和到期时间围住外部调用与副作用；系统保证幂等和故障隔离，不承诺第三方调用的绝对 exactly-once。
- AI 用量以 PostgreSQL ledger 为权威，Redis 缓存用于快速限制和补偿；实际 provider usage 与估算值分开记录。

## 观测与边界

系统通过 Prometheus 指标和结构化日志记录 ASR 阶段耗时、provider 并发、任务状态、消息处理、重试、限流、RAG 检索和 Agent 工具调用。关键边界包括：

- 视觉调查必须拥有服务端确认的 seed windows；系统不接受无范围约束的全片视觉调查。
- Agent 由 HTTP 同步或 SSE 请求驱动，执行恢复依赖持久化 Run/Step/ToolCall 状态。
- 引用只公开系统实际持有的时间、模态和 source refs；unknown/unmapped 证据不会被包装成精确定位。
- PostgreSQL 是事实源，Redis、RabbitMQ 和 pgvector 分别承担短期状态、异步传输和可重建投影职责。
