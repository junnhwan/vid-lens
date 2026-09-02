# VidLens：视频 Agent 创新功能与后端新增模块

> 调研日期：2026-09-02  
> 目标：只回答“下一步值得新增什么”，不重复已有项目全量审计。  
> 边界：文中的论文和开源项目是设计灵感；所有“建议实现”均是 VidLens 的候选方案，不代表当前项目已经具备。

## 一句话结论

VidLens 最值得形成的差异化，不是再包装一个通用聊天 Agent，也不是增加更多角色名称，而是成为：

> 一个会主动寻找视频证据、独立核验证据、跨视频串联证据，并把结论回放成可审计片段的视频研究 Agent 平台。

如果只做三件事，建议按这个顺序：

1. **自适应取证 Agent**：根据问题决定先看哪些章节、镜头和原始帧，并在证据足够时停止。
2. **独立证据审计器**：将“答案可能正确”和“答案确实由所看证据支持”分开验证。
3. **跨视频多跳研究 + 证据短片**：在多个视频间组装完整证据链，并自动生成可回放的引用片段。

## 1. 为什么不能只做普通 RAG 或多 Agent

当前 VidLens 已经有视频处理、多模态产物、RAG 索引版本、Evidence Funnel，以及 AgentRun / Step / ToolCall 的预算与检查点模型。这意味着继续新增一个普通“Planner + Tools + Answer”循环，简历增量有限。

真正体现视频业务特点的问题是：

- 长视频不可能把所有帧都交给模型，Agent 必须决定**看哪里、看多细、何时停止**。
- 文本命中不等于画面支持，正确答案也可能引用了错误片段，必须验证**语义支持与时间定位**。
- 多视频研究不是把 Top-K 片段拼进 Prompt，而是要确认多个必要证据是否都已经找到。
- 最终结果不应只有文字，而应能回放到原视频，甚至直接生成一段“证据短片”。

## 2. Agent 创新功能：推荐优先级

### P0：自适应取证 Agent（最推荐）

#### 用户看到的功能

用户提出问题后，Agent 不再固定执行同一套检索流程，而是：

1. 判断问题需要字幕、OCR、视觉动作、人物关系还是时序比较。
2. 先在章节/场景级粗查，再下钻到镜头和帧。
3. 当已有索引无法支持结论时，才按需读取原始帧或调用 VLM。
4. 每轮判断“证据是否足够”；若不足则继续搜索，足够或预算耗尽则停止。
5. 返回答案、时间戳、实际查看路径、未解决的不确定性和成本。

#### 为什么适合 VidLens

它能直接复用现有 Evidence Funnel、检索、预算和 ToolCall 记录，但把“固定顺序的工具链”升级为“查询驱动的主动感知策略”，既有 AI 创新，也有后端调度和成本治理。

相关研究中，[VideoAgent](https://arxiv.org/abs/2403.10517)使用迭代式帧搜索、视觉模型与自评估来减少无关帧；[VideoTree](https://arxiv.org/abs/2405.19209)采用查询自适应的层次视频表示；微软的 [DeepVideoDiscovery](https://github.com/microsoft/DeepVideoDiscovery)则把分段视频作为可探索环境，让 Agent 规划并使用多粒度工具。

#### 必须做出的工程内容

- 分层时间索引：`video -> chapter -> scene -> shot -> frame`。
- 查询规划器：输出所需模态、时间粒度、最大预算和停止条件。
- 在线取帧工具：按时间范围与采样策略读取原始画面。
- 证据充分性判定：`sufficient / insufficient / conflicting`。
- 去重与缓存：相同视频版本、时间范围、模型和 Prompt 版本只分析一次。

#### 可量化实验

- 固定 Evidence Funnel 与自适应策略的回答正确率对比。
- 平均查看帧数、VLM 调用数、Token/费用、P95 延迟。
- 时间戳命中率和证据支持率。
- 不同预算下的质量—成本曲线。

### P0：独立 Evidence Inspector + 反证搜索（最有可信 AI 辨识度）

#### 用户看到的功能

每条答案先形成 Claim，然后由一个与规划过程解耦的 Inspector 检查：

- 引用片段是否真的包含该事实。
- 时间范围是否准确，而非只在附近出现。
- 是否存在与 Claim 相矛盾的字幕、OCR 或画面。
- 证据只能支持弱结论时，是否把措辞降级为“不确定”或“未充分支持”。

最终每条 Claim 显示为：`支持 / 反驳 / 证据不足 / 存在冲突`，并可以直接回放证据。

#### 为什么这是有意义的“多 Agent”

这里拆角色不是为了显得复杂，而是拆分权限：Planner 可以提出候选结论，但不能批准自己的证据；Inspector 独立读取规范化帧/片段，Answerer 只能发布已通过或明确标注不确定性的 Claim。

[VideoSEAL](https://arxiv.org/abs/2605.12571)专门研究“答案正确但所查看证据并不支持答案”的 evidence misalignment，并采用独立检查机制；[VideoMind](https://github.com/yeliudev/VideoMind)把 Planner、Grounder、Verifier 和 Answerer 分开用于时间定位推理。这两点与 VidLens 已有 Claim/Evidence 账本非常契合。

#### 必须做出的工程内容

- Planner 与 Inspector 使用不同的上下文和发布权限。
- Inspector 只能引用不可变的视频版本、帧或片段 ID。
- 增加 `support / contradict / insufficient` 关系和检查记录。
- 增加反证检索工具，不只检索“支持当前答案”的相似片段。
- 保存验证版本、模型版本、证据摘要和验证理由，允许重放。

#### 可量化实验

- Unsupported Claim Rate。
- Citation Correctness 与时间边界准确率。
- “答案正确但引用错误”的比例。
- 人工构造冲突样本上的反证召回率。

### P1：跨视频多跳 Research Agent + Claim Graph（旗舰业务功能）

#### 用户看到的功能

用户可以对一个知识库提问，例如：

> “三场发布会对同一功能的描述发生了哪些变化？每项结论分别由哪几段视频支持？”

Agent 会：

1. 将问题拆为若干必须回答的子问题。
2. 在不同视频中检索每个必要证据，而非只拿全局 Top-K。
3. 构造 Claim Graph：Claim 与支持、反驳、前置条件、来源视频相连。
4. 暴露知识库覆盖率、未完成索引和仍缺失的证据。
5. 只有必要证据齐全时才输出强结论，否则报告缺口。

[LongVidSearch](https://github.com/yrywill/LongVidSearch)把多跳长视频问题设计成“每个证据片段都必不可少”，并同时评估正确率和工具成本；[VideoRAG](https://arxiv.org/abs/2502.01549)探索了跨视频的图式文本知识与多模态上下文结合。这比简单的“知识库聊天”更有项目辨识度。

#### 必须做出的工程内容

- KB 级检索计划与子问题状态机。
- Claim/Evidence Graph，而不是只保存最终答案引用数组。
- 视频覆盖率、索引版本和部分失败的显式状态。
- 多跳完成条件：哪些证据是必需、可选、缺失或相互冲突。
- 跨视频并发检索的预算分配与背压。

### P1：Evidence Reel——自动生成带引用的证据短片（最适合演示）

#### 用户看到的功能

研究完成后，一键生成一段 30～120 秒的证据视频：

- 每条 Claim 对应精确的原视频区间。
- 自动补充片段前后语境，避免断章取义。
- 叠加来源、时间戳、Claim 卡片和字幕。
- 点击报告中的 Claim 可跳到短片或原视频对应位置。
- 生成规格与所有素材引用可审计、可重复渲染。

这不是做一个庞大的生成式视频编辑器，而是把 VidLens 已有的引用能力变成可交付产物。[UniVA](https://github.com/univa-agent/univa)展示了通过工具工作流统一视频理解、分割和编辑；[Prompt-Driven Agentic Video Editing](https://arxiv.org/abs/2509.16811)则展示了语义索引、时间分段与跨粒度编辑的组合。VidLens 可以只取其中与证据研究最相关的一小块。

#### 必须做出的工程内容

- 规范化的 `RenderSpec`：Claim、证据引用、上下文 padding、布局与字幕版本。
- FFmpeg 渲染任务队列、进度、超时、重试和取消。
- 以 `RenderSpec hash + asset revision` 为键的幂等与缓存。
- 片段级对象存储、生命周期策略和短期签名 URL。

### P2：Video + Web Verification Agent（适合事实核查场景）

#### 用户看到的功能

Agent 先从视频中提取可核查 Claim，再到可信网站或指定资料库查证，最终分栏展示：

- 视频原话与时间戳。
- 外部资料支持或反驳的内容与链接。
- 两类证据的发布时间、来源和冲突点。
- `支持 / 反驳 / 信息不足 / 时效性未知`，而非武断地判断真伪。

[VideoDR-Benchmark](https://github.com/QuantaAlpha/VideoDR-Benchmark)把视频多帧线索与开放网络的多跳检索、证据综合结合起来，说明这是一条正在形成的视频 Deep Research 方向。

#### 实现边界

- 视频证据与 Web 证据必须使用不同的 provenance 域，不能混成一段 Prompt 后丢失来源。
- 默认限定可信域名、记录抓取时间和内容摘要。
- 网页不可访问或信息过期时必须输出“证据不足”。
- 这项功能应排在视频内部证据能力之后，否则容易变成普通 Web Research Agent。

### P2：视频转 SOP / 流程核验 Agent（适合垂直化）

#### 用户看到的功能

- 从教程、培训或操作视频提取步骤、顺序、前置条件、材料和注意事项。
- 每个步骤绑定时间戳与示范片段。
- 上传另一段执行视频后，识别漏步、乱序或关键动作偏差。

[ProcedureVRL](https://github.com/facebookresearch/ProcedureVRL)围绕教学视频中的动作步骤和时间顺序学习展开，说明“程序化视频理解”本身就是区别于普通问答的业务方向。

它很有特色，但会把产品进一步定位到培训、制造、维修或教程，因此建议在确定目标岗位或作品展示主题后再选。

### P3：流式 Watcher Agent（创新高，但工程量最大）

用户为直播或持续增长的视频设置自然语言规则，例如“当讲者开始比较 A/B 方案且给出价格时提醒我”。Agent 持续观察，并自主决定继续等待还是立即告警。[StreamAgent](https://arxiv.org/abs/2508.01875)探索了面向流式视频的主动观察与响应决策。

这会新增直播接入、事件时间、水位线、增量 ASR/OCR、窗口索引、状态检查点和通知去重，已经接近一条新产品线。除非前面三项完成，否则不建议先做。

## 3. 为这些 Agent 新增的后端深模块

### B0：Hierarchical Temporal Index

把当前视频块扩展为可导航的时间树，而不是另建一套相互冲突的真相源：

- 节点类型：chapter、scene、shot、frame。
- 核心字段：半开时间区间、父节点、视频/产物版本、模态摘要、embedding 引用。
- 支持从粗到细遍历，以及由叶节点回溯上下文。
- `video_chunks` 可以继续作为检索投影，时间树负责结构与定位。

它直接支撑自适应取证、跨视频多跳和 SOP 三类功能。

### B0：Frame / Clip Materialization Service

提供统一的领域接口，而不是让每个 Tool 自己调用 FFmpeg：

- `GetFrames(assetRevision, range, samplingPolicy)`。
- `GetClip(assetRevision, range, contextPadding)`。
- `RenderEvidenceReel(renderSpec)`。
- 输出内容哈希、准确时间范围、生成参数和父产物引用。

内部增加内容寻址缓存、临时文件配额、并发限制和短期签名 URL。它既是视觉 Agent 的“眼睛”，也是证据短片的底座。

### B0：Evidence Acquisition Scheduler

把在线取帧/VLM 检查从普通 HTTP 请求中拆出来：

- 按用户、Run、Provider 进行配额和公平调度。
- 以 `(asset revision, range, sampling, model, prompt version)` 去重。
- 支持优先级、背压、超时、取消、熔断和降级。
- 记录每次取证的帧数、成本、耗时和缓存命中。

这比“再接一个模型 API”更能体现 AI 应用后端的工程深度。

### B0：Agent Event Store + 可恢复状态投影

现有 AgentRun / Step / ToolCall 可以作为基础，再新增顺序化事件：

- `RunStarted / ToolScheduled / EvidenceObserved / ClaimProposed / ClaimVerified / RunStopped`。
- 每个 Run 单调递增 sequence，状态表由事件投影得到。
- SSE 支持 `Last-Event-ID` 断线续传。
- Worker 崩溃后从最后检查点恢复；取消和预算耗尽也写成事件。
- 前端看到的是实际执行事件，而不是事后模拟分块的答案。

### B1：Artifact Lineage & Selective Reprocessing

项目已有 RAG manifest、构建版本和来源映射，下一步不是重新发明 provenance，而是把它推广到完整产物链：

`raw video -> audio -> ASR -> OCR/Vision -> temporal nodes -> embeddings -> claims -> report/reel`

每个产物记录父产物、内容哈希、生成器/模型/Prompt 版本、参数和状态。模型或切块策略变化时，只重算受影响的下游节点，并能回答“这条 Claim 会不会受影响”。可借用 [OpenLineage](https://openlineage.io/) 的 dataset/job/run 事件词汇，但个人项目没有必要为了名词完整而部署整套平台。

### B1：Claim Graph / Evidence Ledger Service

把已有 Claim/Evidence 账本深化为独立领域模块：

- Claim 版本、规范化文本和强度。
- `supports / contradicts / contextualizes / derived-from` 边。
- 视频帧、片段、字幕、Web 页面分别使用类型化 EvidenceRef。
- Inspector 结果、验证版本和发布策略。
- 查询某 Claim 的完整证据子图并可重放。

### B1：安全的媒体执行沙箱

项目已有远程 URL/DNS 的准入检查，但准入校验不等于下载器与 FFmpeg 的网络和进程沙箱。建议将 yt-dlp、FFmpeg、OCR 等放进隔离 Worker：

- CPU、内存、磁盘、进程数与执行时间限制。
- 只读根文件系统和有配额的临时目录。
- 解析/转码 Worker 默认无外网；下载 Worker 通过受控出口访问。
- 重定向、DNS 重绑定和最终连接地址再次校验。
- 恶意媒体、压缩炸弹和超大分辨率的提前拒绝。

这项不是最显眼的产品功能，却非常适合后端面试追问。

### B1：Experiment / Replay Registry

为每次策略实验冻结：数据集、视频版本、索引/产物版本、模型、Prompt、工具策略、预算与输出。支持重放既有 Tool Trace，比较固定 Funnel、自适应取证、不同 Inspector 策略。

没有这一层，“效果提高、成本下降”很容易停留在主观演示；有这一层才能在简历中给出可信数字。

### B2：租户隔离的纵深防御

在应用层 owner scope 之外，可为视频、知识库、Run、Evidence 等核心表评估 PostgreSQL Row-Level Security。PostgreSQL 官方文档说明，开启 RLS 且没有适用策略时采用 default-deny，但表所有者和具备 bypass 权限的角色可能绕过，因此必须使用正确的运行时角色并写越权测试，不能只写几条 policy 就声称完成隔离：[PostgreSQL Row Security Policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)。

对象存储还需统一 owner-scoped key、短期签名 URL 与下载授权，避免数据库隔离后媒体对象仍可越权访问。

## 4. 中间件怎么选

### 不建议为简历数量同时堆 RabbitMQ、Kafka 和 Temporal

当前 RabbitMQ + PostgreSQL 足以实现这一版 Agent Runtime，关键是把事件顺序、幂等、检查点、恢复、取消和 SSE 重放做扎实。

[Temporal](https://docs.temporal.io/)适合长时间、故障可恢复的工作流，但若要引入，应做一个小型对比 Spike：用同一个“在线取证—Inspector—渲染”流程比较恢复语义、代码复杂度和运维成本，然后选择它替代一部分自研编排职责。长期同时保留两套职责重叠的执行引擎，通常只会增加架构解释成本。

可新增但必须有明确职责的基础设施只有：

- Redis：仅在确有分布式限流、短期缓存或租约需求时引入，PostgreSQL 仍保存权威状态。
- pgvector：继续作为向量检索投影，不承担时间树或 Claim Graph 的权威模型。
- MinIO/S3：保存不可变原始资产和派生产物；大文件上传可演进为服务端签发的 multipart upload。[S3 Multipart Upload](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)支持分片独立、乱序上传和单片重试，但未完成上传必须显式 complete 或 abort。

## 5. 推荐实施路线

### 第一阶段：把单视频 Agent 做出真正差异（优先）

1. Hierarchical Temporal Index。
2. Frame/Clip Materialization Service。
3. 自适应取证策略与证据充分性停止条件。
4. 独立 Inspector 和反证搜索。
5. 用冻结评测集对比固定 Funnel，拿到质量、帧数、成本和延迟数据。

这是最小但最完整的一条简历故事：**视频特有算法思路 + Agent 工具规划 + 后端任务治理 + 可量化评测**。

### 第二阶段：形成旗舰功能

1. KB 级跨视频 Research Agent。
2. Claim Graph 与多跳完成条件。
3. Evidence Reel 渲染流水线。
4. Agent Event Store、真实 SSE 与断线恢复。

完成后，项目不再只是“视频 RAG”，而是可审计的视频研究工作台。

### 第三阶段：只选一个垂直方向

- 面向内容事实核查：Video + Web Verification。
- 面向教程/培训：视频转 SOP 与执行核验。
- 面向直播监控：Streaming Watcher；工程量最大，最后考虑。

不要同时做三个半成品。

## 6. 完成后可形成的简历表述

以下只能在真实实现并取得指标后使用：

- 设计查询自适应的视频取证 Agent，通过章节—场景—镜头—帧分层检索与证据充分性停止策略，在保持回答质量的同时降低平均 VLM 调用量与推理成本。
- 构建 Planner–Inspector 权限分离的证据审计链路，对 Claim 执行时间定位、语义支持和反证搜索，降低无依据结论与错误引用比例。
- 实现跨视频多跳 Research Agent 与 Claim/Evidence Graph，显式跟踪必要证据、冲突和知识库覆盖率，并生成可回放的带时间戳证据报告。
- 设计内容寻址的帧/片段物化、派生产物血缘与选择性重算机制，支撑 Agent 在线视觉取证、故障恢复和可重复评测。
- 基于 FFmpeg 异步渲染可审计 Evidence Reel，通过 RenderSpec hash 实现任务幂等、缓存复用和产物追溯。

## 最终取舍

如果目标是后端开发 / Agent 开发 / AI 应用开发三类岗位都能讲，最佳组合是：

> **自适应取证 Agent + 独立 Evidence Inspector + 跨视频 Claim Graph**，底层配套 **分层时间索引 + 帧/片段物化 + Agent 事件存储 + 产物血缘与评测回放**。

Evidence Reel 用来强化 Demo；Video + Web Verification 或 SOP Agent 用来做垂直化；Streaming Watcher 留作远期。这个组合比新增一个聊天入口、一个普通知识库问答或一组只有角色名称的多 Agent，更能形成 VidLens 自己的技术叙事。
