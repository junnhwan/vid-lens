# Agent 与视频证据架构调研

核验日期：**2026-08-29（Asia/Shanghai）**

本文只记录公开一手资料中能够复核的内容，并把源码事实、项目文档/论文事实和基于源码的推断分开。三个可解析仓库的代码均按公开仓库当前可见内容核验；能够固定版本的地方同时记录 commit。AGI-saber 项目单独记录为官方入口不可解析的证据边界。本文不摘录或复述原始 Chain-of-Thought，只总结公开的模块、数据结构、工具接口和可观察的状态变化。

## 读法与证据等级

- **[源码事实]**：来自实际打开的源码、schema、测试或仓库文件；包括具体文件名和可复核链接。
- **[论文事实]**：来自论文正文、表格或附录；论文中的实验结果不能直接等同于当前仓库默认配置。
- **[README 事实]**：来自项目 README 的能力描述或 TODO；若与当前代码不一致，以差异本身作为核验结果。
- **[基于源码的推断]**：对 VidLens 的设计建议，或从多个源码事实归纳出的架构含义；不是被调研项目已经实现的能力。

“Agent”“Planner”“Memory”“Reasoning”等通用词不自动代表项目具备长期记忆、可靠推理、语义证明或自主规划能力；下文只在有对应源码或论文证据时使用这些词。

## 结论先行

| 项目 | 本次能确认的核心能力 | 对 VidLens 最有价值的启发 | 不能直接搬用的部分 |
| --- | --- | --- | --- |
| AGI-saber/AGI-saber-go | 用户给出的公开入口无法解析，但本地 checkout `wujingle488-crypto/AGI-saber` 可核验到分层记忆、异步记忆写入和可选图记忆 | 分层 memory provider、有限召回、去重/衰减、异步写入 | 不应隐藏仓库身份差异，也不应把其通用图运行时或工具沙箱直接搬入 VidLens |
| Microsoft/DeepVideoDiscovery | 全局摘要/主体视图 → 片段语义检索 → 原始帧检查的多粒度视频检索 | 为 VidLens 建立逐级收窄、每级留下时间范围的证据漏斗 | 论文配置、模型和 benchmark 结果不能当作当前仓库或 VidLens 的默认保证 |
| mupozg823/timecode-agent | 以时间码为中心的 checkpoint、追加式理解/编辑账本、支持状态和证据校验 | 把 claim、支撑、修订、覆盖度和编辑引用做成一等数据 | 它是本地 Python + 外部编码 Agent harness，不是通用内置 LLM 或密码学防篡改系统 |
| DOVideo-AI | 面向生产任务的 ASR/OCR/关键帧上下文构建、邻帧去重、混合检索、幂等与 checkpoint | 把内容哈希、目标哈希、异步任务和结构化证据约束组合起来 | Java/Spring/Redis/MySQL/MinIO/Qdrant 的重量级栈、MD5 和 substring 校验不适合原样迁移 |

## 1. AGI-saber/AGI-saber-go：长期记忆

### 来源与核验范围

- 官方仓库入口：[AGI-saber/AGI-saber-go](https://github.com/AGI-saber/AGI-saber-go)。
- 官方组织入口：[AGI-saber](https://github.com/AGI-saber)。
- GitHub 官方仓库搜索 API：[`AGI-saber-go` 搜索](https://api.github.com/search/repositories?q=AGI-saber-go)。
- 核验时间：**2026-08-29（Asia/Shanghai）**。

### 实际核验结果

- **[源码事实]** 上述仓库入口和组织入口在核验时返回 GitHub 404；官方搜索 API 返回 `total_count: 0`、`items: []`。因此无法进入该仓库的 README、commit、目录树、论文或具体模块。
- **[本地源码事实]** `D:\dev\agent-learn\other\AGI-saber-go` 的 `origin` 实际为 [`wujingle488-crypto/AGI-saber`](https://github.com/wujingle488-crypto/AGI-saber)，checkout 为 `f85a1da776de76dafbf9302d147a18ad0ea0bdaf`，提交时间为 2026-06-28；它是 Python 实现，目录名和用户给出的 `AGI-saber-go` 公共地址并不一致。
- **[本地源码事实]** 该 checkout 的 `internal/memory/memory.py` 提供 `ShortTerm` 滑动窗口、`LongTerm` 的 embedding/importance 召回、Jaccard 去重、decay/TTL 和 `Preference`；`memory_writer.py` 以异步队列和单 worker 写入，并从回复抽取偏好/事实；`graph_memory.py` 是可选 Neo4j 关系扩展；`restore.py` 恢复上下文；`planner.py`、`graph_runtime.py` 还包括工具计划、依赖/竞态组、并行、重试和快照钩子。
- **[核验边界]** 因此可以使用本地 checkout 说明其实现，但不能把它重新标注为已确认的 `AGI-saber/AGI-saber-go` Go 官方仓库；公开身份和版本仍需上游恢复后重新确认。

### 真正解决的问题与实现方式

- **[本地源码事实]** 这个 checkout 实际解决的是 Agent 运行期间的上下文分层和持久化召回：短期会话保留有限窗口，长期项按相似度/重要性召回，偏好单独保存；新记忆通过异步写入与合并降低主请求耦合。
- **[本地源码事实]** `internal/memory/memory.py` 的 `LongTerm` 使用可选 embedding、cosine similarity 与 `0.7/0.3` 的相似度/重要性组合，带阈值、top-k、tokenized Jaccard 去重、importance decay、TTL/min-importance 清理和 graph hook；`MemoryManager` 组合 short/long/preference，并可做一跳 graph 扩展。
- **[本地源码事实]** `internal/agent/memory_writer.py` 用 `AsyncMemoryWriter` 队列和单 worker 串行化写入；从回复抽取偏好/事实并按规则分类，`restore.py` 在 Agent 启动时尽力恢复用户偏好、长期记忆、短期聊天和 RAG 上下文。`graph_memory.py` 的 Neo4j 关系层是可选能力。

### 对 VidLens 的借鉴

- **[基于本地源码的推断]** VidLens 可借鉴分层接口、异步 best-effort 写入、有限召回、来源/重要性/TTL/去重治理和运行前 memory snapshot。具体采用 PostgreSQL 权威表与 pgvector 投影，是结合 VidLens 现有数据边界的推断，不是该项目本身的实现。
- **[核验建议]** 若上游公开入口恢复，应再次核验 Go 版本、持久化对象、记忆写入触发条件、召回边界、遗忘/修订策略和测试，不应只根据项目名称判断能力。

### 不适合直接照搬的部分

- **[基于核验结果的推断]** 不应把本地 Python checkout 直接当作用户指定的 Go 官方仓库，也不应照搬其 Neo4j 图记忆、通用 graph planner、工具沙箱或记忆项模型。其数据库同步存在 best-effort/占位边界，VidLens 需要自行定义 scope、source_ref、consent、删除和冲突修订。

## 2. Microsoft/DeepVideoDiscovery：视频证据检索与多粒度证据漏斗

### 来源、版本与实际核验对象

- 官方仓库：[microsoft/DeepVideoDiscovery](https://github.com/microsoft/DeepVideoDiscovery)。
- 本次代码快照：commit [`64414b2f35d26809a39740a5a319889f46e29b94`](https://github.com/microsoft/DeepVideoDiscovery/tree/64414b2f35d26809a39740a5a319889f46e29b94)，仓库显示时间为 2025-11-03。
- 官方论文：[arXiv abstract 2505.18079](https://arxiv.org/abs/2505.18079)；[论文 HTML 正文](https://arxiv.org/html/2505.18079)。论文当前页面显示为 v4，日期 2025-11-03。
- 核验时间：**2026-08-29（Asia/Shanghai）**。
- 实际打开的仓库文件：[`README.md`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/README.md)、[`dvd/config.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/config.py)、[`dvd/frame_caption.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/frame_caption.py)、[`dvd/build_database.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/build_database.py)、[`dvd/dvd_core.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/dvd_core.py)、[`dvd/video_utils.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/video_utils.py)、[`mcp_server.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/mcp_server.py)。

### 真正解决的问题

- **[README/论文事实]** 项目针对长视频问答中“整段视频太大、原始帧检查昂贵、用户问题往往只涉及少数时刻”的定位问题，先把视频切成可探索的 clip，再根据问题逐步缩小范围。
- **[论文事实]** 论文将数据库分为全局视频层、clip 文本/向量层和原始 frame 层。工具对应 Global Browse、Clip Search、Frame Inspect；这三层共同构成本文所称的“多粒度证据漏斗”。“证据漏斗”是对论文和源码结构的归纳，不是仓库中名为 `EvidenceFunnel` 的独立组件。
- **[论文事实]** 论文报告的 LVBench 成绩为 74.2，加入 transcript 为 76.0；这些是论文实验结果，不是当前仓库在 VidLens 数据上的承诺。

### 核心实现方式

#### 离线视频表示

- **[源码事实]** `dvd/frame_caption.py` 把视频按固定 `CLIP_SECS` 分段，在 `VIDEO_FPS` 采样帧，合并重叠字幕；每个 clip 的结构化 caption 包含起止时间、主体注册表和 clip 描述。非 lite 路径用视觉模型生成 JSON caption，并把 transcript 合并到描述中；处理过程中按 clip checkpoint 写入 caption JSON，最后再合并主体注册表。
- **[源码事实]** `dvd/build_database.py` 的 `init_single_video_db` 对 caption 做 embedding，写入 NanoVectorDB；记录 `time_start_secs`、`time_end_secs`、caption，并保存视频长度、文件根目录、FPS 和主体注册表等 metadata。
- **[源码事实]** 当前仓库 [`dvd/config.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/config.py) 的默认值是 `VIDEO_RESOLUTION="360"`、`VIDEO_FPS=2`、`CLIP_SECS=10`、`GLOBAL_BROWSE_TOPK=300`、`AOAI_TOOL_VLM_MAX_FRAME_NUM=50`、`MAX_ITERATIONS=3`，并且 `LITE_MODE=True`。这与论文实验中描述的 5 秒 clip、不同模型和更高 reasoning step 上限并不完全相同。

#### 三个检索/检查工具

- **[源码事实]** `clip_search_tool` 用事件描述做 embedding，默认从 NanoVectorDB 取 `top_k=16`，按时间排序，返回带时间的 caption 片段。
- **[源码事实]** `global_browse_tool` 从最多 300 个相关 caption 中构造全局浏览输入，再由视觉语言模型返回主体注册表和问题相关事件答案。README 还特别说明全局浏览使用多个 clip 的文字描述，而不是把原始视频像素一次性送入模型。
- **[源码事实]** `frame_inspect_tool` 接受一个或多个时间范围，校验范围后均匀采样原始帧；总帧数上限为 50，再把选择的帧交给视觉语言模型检查。当前 `DVDCoreAgent` 在 `LITE_MODE` 下会移除 frame inspection 工具。
- **[论文事实]** 论文把三种工具描述为按需选择的自适应循环，而不是固定的“先 A 后 B 后 C”流水线。论文中的工具消融显示，在带 transcript 的表格设置下，去掉 Global Browse、Clip Search、Frame Inspect 的准确率分别为 69.0、59.6、63.5，相对全工具 71.9 的损失不同；这说明工具角色互补，但不能推出每个视频都必须执行全部工具。

#### 循环和当前实现边界

- **[源码事实]** `dvd/dvd_core.py` 注册 frame inspect、clip search、global browse 和 finish；每轮调用模型生成工具调用，执行工具并将结果放回消息，最多运行 `MAX_ITERATIONS`，最后一轮强制进入 finish。`stream_run` 只把 assistant、tool call 和 tool message 作为 UI 流输出。
- **[源码事实]** 仓库中实际存在 [`mcp_server.py`](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/mcp_server.py)，能够串起下载、解码、caption、建库和查询；但当前 README TODO 仍把 MCP implementation 标为未完成。这说明 README 与代码状态并不完全同步，不能只引用 TODO 判断代码能力。
- **[源码事实]** README 所称的“总结/反思”在本次核验中可落到工具调用后的消息观察和有限迭代；没有证据表明仓库实现了长期记忆、通用知识图谱或语义级证据证明。

### 适合 VidLens 的借鉴点

- **[基于源码/论文的推断]** 为一个视频问题建立逐级检索：先用低成本的 transcript/全局摘要定位主题，再用 pgvector 召回带时间范围的 clip/chunk，最后只对少量候选时间窗调用视觉检查。每一级都应保留 query、候选 ID、`start/end` 和下一步选择原因，形成可回放的证据链。
- **[基于源码/论文的推断]** 把“找在哪里”和“看清楚发生了什么”分开：语义召回负责覆盖候选时间，原始帧检查负责验证局部视觉事实。这样符合 VidLens 当前“关系数据是事实源、pgvector 是可重建投影”的边界，可将帧检查结果作为附加证据而不是覆盖 `video_chunks`。
- **[基于源码/论文的推断]** Global Browse 的文字化输入是控制 token 和视觉成本的可行手段；VidLens 可以用已有摘要/转写先生成粗粒度视频地图，再把原始帧预算给真正需要视觉确认的 claim。
- **[基于论文的推断]** 需要显式的最大步数、重复工具调用检测、空结果降级和最终停止条件。论文案例中存在重复 Frame Inspect/Clip Search 或被单次视觉检查误导的失败路径，说明工具存在不等于证据已经充分。

### 不适合直接照搬的部分

- **[源码/论文事实]** 论文配置与当前仓库配置有差异；不能把论文的 5 秒分段、模型、15 步上限或 benchmark 成绩直接当作当前 `config.py` 或 VidLens 的默认行为。
- **[源码事实]** 当前仓库使用 NanoVectorDB、Azure/OpenAI 相关调用和 Python 进程内状态；直接迁移会绕开 VidLens 的 PostgreSQL/pgvector 事实源、AI 治理和 RabbitMQ 阶段恢复边界。
- **[源码事实]** `LITE_MODE=True` 时会去掉 frame inspect；因此“多粒度”在不同运行模式下并不总是三层都启用。
- **[基于源码的推断]** 不应把模型的工具选择消息当作审计账本，也不应把工具返回的自由文本直接当成 claim 已被证明。应另建结构化 evidence/claim 记录、时间范围校验和支撑状态。
- **[README/源码事实]** README 的 MCP TODO 和实际 `mcp_server.py` 不一致；对这类快速迭代仓库，应固定 commit 并以源码和可运行测试为准。

## 3. mupozg823/timecode-agent：时间码证据、Claim 状态和证据账本

### 来源、版本与实际核验对象

- 官方仓库：[mupozg823/timecode-agent](https://github.com/mupozg823/timecode-agent)。
- 本次代码快照：commit [`02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03`](https://github.com/mupozg823/timecode-agent/tree/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03)，release subject 为 `v0.4.0`，仓库显示时间为 2026-08-07。
- 实际打开的文件：[`README.md`](https://github.com/mupozg823/timecode-agent/blob/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03/README.md)、[`docs/ARCHITECTURE.md`](https://github.com/mupozg823/timecode-agent/blob/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03/docs/ARCHITECTURE.md)、`checkpoint_schema.py`、`checkpoint_store.py`、`checkpoints.py`、`verification.py`、`status.py`、`corpus_audit.py`、`transcript_evidence.py`、`image_model.py`、`image_store.py`、`sequence_schema.py`、`sequence_store.py`、`sequence_grounding.py`、`corpus_projection.py`、`search.py`、`index.py`、`view.py`、`wiki.py`，以及 README/architecture 中列出的 CLI 入口。
- 核验时间：**2026-08-29（Asia/Shanghai）**。

### 真正解决的问题

- **[README/ARCHITECTURE 事实]** 项目解决的是长视频理解和编辑过程中的可定位、可恢复、可追溯：先以 transcript 为边界，再用场景、音频、OCR、人脸等确定性/感知信号定位候选，只有 claim 需要视觉确认时才抓取少量帧；后续搜索、wiki、EDL/FCPXML/OTIO 都从持久化的 checkpoint/sequence 状态派生。
- **[README 事实]** 当前 package 负责媒体处理、确定性信号和持久化；语义判断由外部 coding-agent harness 的 skill 提供，项目本身没有内置 LLM。README 中的 Kubrick/Kuleshov 是 Agent Skill 中的操作流程，不是已安装的独立 agent 模块。
- **[源码事实]** 它把理解事实和剪辑决定放到两条物理分离的追加式 JSONL ledger：`checkpoints.jsonl` 保存 claim/revision，`sequences.jsonl` 保存 edit decision/revision；索引、view、wiki projection 和 export 都可重建。

### 核心实现方式

#### 从可测量观察到 claim 的分层

- **[ARCHITECTURE/源码事实]** 该项目把能力分成四个层级：层级 0 是时间戳、帧差、音频能量、清晰度等物理/形式测量；层级 1 是 ASR、OCR、人脸、speaker turn 等模型感知结果；层级 2 是身份、动机、事件、因果等需要证据的语义 claim；层级 3 是剪辑决定。前两个层级只能产生候选位置或候选观察，不能自动升级为语义真相。
- **[基于源码的推断]** 这是一条重要的证据提升边界：系统应区分“某时间点检测到文字/人脸”与“某人完成了某行为”这两类数据，而不是把模型 caption 当作同一等级的事实。

#### Claim-like checkpoint 状态和支撑校验

- **[源码事实]** checkpoint schema 要求 `id`、`status`、非空 `hypothesis`、`[start,end]` 时间范围，并校验 confidence 范围、exact tokens 和 typed `visual_observation`；时间范围必须落在媒体 duration 内。
- **[源码事实]** 持久化状态包括 `hypothesized`、`verified`、`corrected`。`validate_transition` 至少阻止同一 checkpoint 从 terminal 状态退回 `hypothesized`；`verified`/`corrected` 是终态，但这不是一个覆盖所有语义关系的通用 claim engine，而是围绕 checkpoint 的小型持久化状态约束。
- **[源码事实]** `verification.py` 只有在 declared support 能解析时才允许 terminal checkpoint 晋级：视觉证据路径必须存在且可解码，image provenance 必须与时间范围一致；transcript segment 必须存在并与 checkpoint 时间范围重叠。单独填一段 opaque `evidence` 字符串不能建立支撑；`corrected` 缺少 correction note 会进入 audit issue。
- **[源码事实]** `transcript_evidence.py` 按稳定 segment ID 建索引，逐条要求声明的 transcript segment 存在且与 claim span 重叠。这是时间码上的可复核约束，不是语义蕴含证明。

#### 追加式账本和可重建投影

- **[源码事实]** `checkpoint_store.py` 对 `checkpoints.jsonl` 做加锁追加写，并绑定 workspace revision；读取时得到每个 ID 的最新投影和完整历史。旧 visual evidence 在省略时继承，显式空值才清除；损坏行或 revision 不匹配的行会跳过并警告，过去的 ledger 行不直接重写。
- **[源码事实]** `image_store.py` 追加 `image-provenance.jsonl`，记录 `image_id`、`cause_id`、`edge_id`、cause type、请求时间、span、role 和评分等。文档明确它表达 capture causality，不表达 semantic entailment；系统也明确不是密码学防篡改账本或 C2PA。
- **[源码事实]** `checkpoints.py` 计算 span union、covered ratio、verified ratio、超过 3 秒的 gaps，并给出 `cover_gaps`、`verify_more` 或 `converged` 建议。该 readiness 是 advisory：例如评分由 coverage、verified ratio 和平均 confidence 组成，不能单独证明内容正确。

#### 编辑决策与理解事实分离

- **[源码事实]** `sequence_grounding.py` 要求 terminal edit 引用的 checkpoint 是 `verified`/`corrected` 且 support 完整、与剪辑时间重叠，并要求 body-wide coverage；仅有局部 overlap 不足以支持剪辑。sequence 会 pin checkpoint content hash，内容漂移时强制重新验证。
- **[源码事实]** edit sequence ledger 不把剪辑决定写回理解 ledger；导出结果来自已 pin 的 checkpoint/sequence，而不是未持久化的模型回复。
- **[源码事实]** `checkpoint observe` 的视觉观察接口只支持一个受 provenance 跟踪、未裁剪的 frame 和 typed `person_presence`；`va ask` 当前只覆盖一个特定的 seated-man screen departure count intent。它不是任意视觉问答服务。

### 适合 VidLens 的借鉴点

- **[基于源码的推断]** 在 VidLens 的 `evidence_id` 之外增加一层明确的 claim contract：`video/task`、`start/end`、模态、原始 observation、claim、支撑引用、workspace/processing revision、状态和 confidence。状态变化与 evidence 支撑应可查询、可审计。
- **[基于源码的推断]** 采用 PostgreSQL 中的 append-only evidence/claim events + latest projection：向量检索仍是 pgvector 投影，不能替代时间码、支撑关系和 revision 历史。这个方向与 VidLens 当前“PostgreSQL 是事实源、pgvector 可重建”的文档边界相容。
- **[基于源码的推断]** 用 typed time-overlap resolver 处理 transcript、OCR、visual frame、summary 的支撑关系；不能只依赖回答正文中的引用编号或一段自由文本。
- **[基于源码的推断]** 将“理解账本”和“编辑/高亮序列”分开。即便 VidLens 暂时没有剪辑导出，也可以先保存问题回答所引用的 claim snapshot，并在 transcript/visual revision 变化时触发重新验证。
- **[基于源码的推断]** readiness 应同时看覆盖、支撑状态、时间范围完整性和 revision 一致性，而不是只看 LLM confidence 或“循环已经结束”。

### 不适合直接照搬的部分

- **[README/源码事实]** 项目是 Python、本地工作区和外部 coding-agent harness 的组合；没有可直接嵌入 VidLens Go 服务的通用 LLM runtime。不能把 README 中的 skill 流程写成项目内置 agent 实现。
- **[源码事实]** JSONL ledger 虽然追加式，但文档明确它不是密码学防篡改/C2PA 证据。若 VidLens 有合规级 provenance 要求，还需要独立的签名、对象完整性和密钥治理设计。
- **[源码事实]** readiness 与 score 是 advisory，`transcript` 的重叠和 visual path 可解码也不能证明语义 claim 正确；不能把这套校验直接命名为“事实证明器”。
- **[源码事实]** 当前视觉观察/问答意图很窄，不能当作通用视频内容分析器，也没有证据表明存在独立的 editcode agent 或 typed handback protocol。
- **[基于源码的推断]** 不应把 `hypothesized → verified` 设计成“模型说确定了就通过”；必须要求可解析的 typed support、时间范围、版本和人工/规则/模型验证策略。

## 4. DOVideo-AI：视频内容分析、去重和工具化思路

### 来源、版本与实际核验对象

- 官方仓库：[Xiaoc7r/DOVideo-AI](https://github.com/Xiaoc7r/DOVideo-AI)。
- 本次代码快照：本地 checkout commit [`caed156914e4cb4fc76e729f8fd79004674a1c75`](https://github.com/Xiaoc7r/DOVideo-AI/tree/caed156914e4cb4fc76e729f8fd79004674a1c75)，提交时间为 2026-07-27。
- 核验时间：**2026-08-29（Asia/Shanghai）**。
- 实际打开的文件：[`README.md`](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/README.md)、[`VideoContextService.java`](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/server/src/main/java/com/example/server/service/VideoContextService.java)、`VideoContext.java`、`VideoChunkingService.java`、`LongVideoContextService.java`、`VideoEvidenceRetrievalService.java`、`QdrantVectorStore.java`、`AgentLoopService.java`、`AgentState.java`、`AnalysisResult.java`、`EvidenceVerificationService.java`、`AgentCheckpointService.java`、`AgentCheckpointRepository.java`、`AnalysisDispatchService.java`、`AnalysisTaskKeys.java`、`MediaIngestService.java`、`MediaService.java`、`ChunkUploadService.java`、`VideoAnalysisConsumer.java`、`TaskEventService.java` 和 `server/src/main/resources/schema.sql`。
- 本次核验范围内未找到该项目对应的论文或独立 benchmark；README 的产品说明和源码行为不替代外部效果评测。

### 真正解决的问题

- **[README/源码事实]** 项目解决的是把长视频分析做成可恢复的异步产品链：分片上传/续传、ASR/OCR/关键帧并行、统一视频上下文、5 分钟语义块、混合检索、有限轮次的结构化分析，以及失败重试和任务进度事件。
- **[源码事实]** “去重”不是单一算法，而是至少三个边界：分片完成重试的幂等、基于内容/目标的分析任务复用，以及相邻关键帧的感知哈希去重。数据库保存 `content_hash`，但这并不等于上传表拒绝重复内容。

### 核心实现方式

#### VideoContext：并行信号、关键帧和邻帧去重

- **[源码事实]** `VideoContextService` 用 60 秒 `VideoSegment` 作为上下文合并单位；ASR 与关键帧/OCR 在独立的 `ThreadPoolTaskExecutor` 分支运行，整体等待上限为 60 分钟。两分支都失败才报错；单分支失败时保留另一分支，并清理失败的 OCR 证据帧。
- **[源码事实]** FFmpeg 关键帧过滤使用首帧、scene threshold `0.35` 和至少 30 秒的 fallback 间隔，再通过 `showinfo` 取得时间戳。这是一种“场景变化 + 时间兜底”的候选定位启发式。
- **[源码事实]** 对候选帧转灰度后 resize 到 9×8，计算 difference hash；相邻帧 Hamming distance `<=5` 时跳过后者。保留下来的帧进行 OCR，并尝试上传 MinIO；上传失败时保存 `videoPath#timestampMs=...` 形式的 fallback reference。
- **[源码事实]** `VideoContext`/`VideoSegment` 把 ASR transcript、OCR 文本、关键帧引用和 `startMs/endMs` 合并成统一上下文；时间范围有非空和起止校验。

#### 五分钟语义块与混合检索

- **[源码事实]** `VideoChunkingService` 按 5 分钟切 semantic chunk；优先用模型生成 summary/keywords，失败时退回截断的原始 transcript + OCR；再生成 embedding。`VideoChunk` 同时保留 summary/keywords 和原始 segment，命中后可以回到时间段证据。
- **[源码事实]** `VideoEvidenceRetrievalService` 先生成 `semanticQuery`、keywords 和 visualKeywords；Qdrant 取候选后按 semantic 0.60、keyword 0.25、visual/OCR 0.15 合并 chunk score，再按 chunk 0.55、transcript 0.25、visual 0.20 得到 segment score。默认选 3 个 chunk、最多 8 个用户 hits，snippet 上限 180 字符；Qdrant 或 embedding 出错时保留本地排序/降级路径。
- **[源码事实]** Qdrant collection 是 `video_chunks`，payload 包含 mediaId/startMs/endMs，按 mediaId 过滤；point ID 由 mediaId 和时间范围确定性生成。当前源码体现的是向量召回 + 服务层本地融合，不应把它描述成一个已经验证的通用 hybrid search 产品。

#### 有界分析循环与结构化证据

- **[源码事实]** `AgentLoopService` 的状态明确包含 goal、plan、result、critique 和 round；默认最大轮次为 2，并同时校验 duration、estimated tokens、cost 等预算。规划任务最多 5 个。
- **[源码事实]** executor 结果是 `AnalysisResult`，固定包含 title、conclusions、evidence、suggestions、sections；每条 evidence 有 timestamp、source、content、claim。结果持久化前，`enforceEvidenceBounds` 检查 evidence 是否被底层 context 支撑，并要求每个 conclusion 能对应 claim/evidence；失败会进入 critic feedback 和所需时间戳修复路径。
- **[源码事实]** `EvidenceVerificationService` 要求 timestamp 落在半开区间 segment 内、source 含 ASR 或 OCR，且 normalized evidence content 必须出现在同一时间点的 ASR/OCR 文本中；`supportsClaim` 还要求 claim 与 evidence.claim 规范化后相等。测试覆盖了“逐字 OCR 证据接受”和“相似但不来自源文本的内容拒绝”。
- **[实现边界]** 这套验证是时间范围 + 字符串包含 + claim equality，不是自然语言语义蕴含、视觉事实识别或人工事实审查。

#### 上传、任务和 checkpoint 可靠性

- **[源码事实]** `ChunkUploadService` 使用 5 MB 分片、Redis session、已上传分片记录、MD5 merge 和 Redisson merge lock；完成请求重试会返回同一 media mapping。这解决的是上传续传/完成幂等。
- **[源码事实]** `AnalysisDispatchService` 用 `contentHash + goalDigest` 组成 active key，设置 6 小时 TTL，并实施用户/全局速率限制；RocketMQ 异步消费。`VideoAnalysisConsumer` 处理 delivery attempt、stage、retry/dead-letter 和 completed result reuse，最终清理 active/attempt key。
- **[源码事实]** `AgentCheckpointRepository` 把 MySQL 作为恢复事实源，Redis 作为 after-commit hot cache；`AgentCheckpointService` 以 media ID 和 goal/mode digest 区分 plan/result/critic，保存 context/chunks/critic/result/stage，并用 pending/applied revision 标记阶段替换。
- **[源码事实]** `MediaFile.content_hash` 有数据库索引，但 `MediaService.saveUploadedMedia` 仍会插入新的 MediaFile；因此源码能证明的是任务/分析级复用和索引化哈希，不是“同内容上传只保留一个 canonical media row”。

### 适合 VidLens 的借鉴点

- **[基于源码的推断]** 将 VideoContext 的“信号并行、时间窗口合并、原始证据回指”映射到 VidLens 的异步阶段：ASR、OCR/视觉、摘要和 pgvector projection 可以独立重试，最终以统一的 video/task + time span 证据模型汇合。
- **[基于源码的推断]** 采用“粗粒度摘要/关键词召回 → 原始 transcript/OCR segment 回填 → 只对命中时间窗做视觉确认”的成本路径，并把返回的 `start/end`、模态和原文保留给引用层。
- **[基于源码的推断]** 借鉴 `content_hash + goal_digest` 的幂等键形状，但把 PostgreSQL 作为 source of truth、把 pgvector 当 projection；Redis/RabbitMQ 只负责短期协调和异步调度，符合 VidLens 现有边界。
- **[基于源码的推断]** 让分析结果固定包含 conclusions、evidence、suggestions 等结构，验证失败时返回“缺少哪一段时间/哪一类证据”，而不是用自由文本遮蔽支撑不足。
- **[基于源码的推断]** 感知哈希可以减少连续近似帧的 OCR/视觉成本，但去重决策应记录被跳过帧、保留帧、阈值和版本，便于回放与重新处理。

### 不适合直接照搬的部分

- **[源码事实]** DOVideo-AI 采用 Java 21/Spring Boot、MySQL、Redis、MinIO、RocketMQ、Qdrant、FFmpeg、Tesseract 等重量级组合；原样迁移会与 VidLens Go + PostgreSQL/pgvector + RabbitMQ 的现有运行边界冲突。
- **[源码事实]** MD5 在这里主要承担内容/任务幂等键用途，不能当作证据真实性、来源身份或防篡改承诺；数据库也没有以 content hash 约束唯一媒体行。
- **[源码事实]** dHash 的相邻帧 Hamming `<=5` 是经验阈值，可能丢掉画面变化很小但文字/动作关键的帧；scene change + 30 秒 fallback 也只是定位启发式，不是完整镜头切分或内容覆盖证明。
- **[源码事实]** `videoPath#timestampMs` 的 fallback reference 不保证是可直接访问的 HTTP 证据 URL；VidLens 应区分对象存储 URL、可解码本地引用和仅供内部定位的 locator。
- **[源码事实]** Agent checkpoint 是按 key 的状态保存/替换，并非 timecode-agent 那样的完整追加式 claim history；不能直接把 MySQL/Redis checkpoint 称作不可变证据账本。
- **[源码事实]** evidence 校验使用源文本 substring 和 exact normalized claim；它比完全无约束的 LLM 结果严格，但仍不等于语义蕴含或视觉事实核验。VidLens 若照搬会对改写、同义表达和 OCR 误识别过于脆弱。
- **[基于源码的推断]** Planner/Executor/Critic 的三段名字不应成为产品能力宣传；真正可迁移的是显式状态、预算、最多两轮和失败反馈，而不是把循环轮数增加就称为更可靠。

## 5. 结合 VidLens 的架构推断（不是现有实现变更）

本节是基于上面源码事实与 VidLens 当前文档的设计推断，不表示本次已经修改业务代码。VidLens 当前事实边界可参见[架构总览](overview.md)、[检索与回答链路](retrieval.md)、[可靠性与幂等](reliability.md)和[数据模型](data-model.md)。

### 5.1 建议的证据对象边界

**[基于源码的推断]** 对每个视频问题，最小可追溯证据对象应至少有：

1. `task/video identity` 与处理 revision；
2. `[start, end)` 时间范围；
3. modality（transcript、OCR、frame、summary、model observation）；
4. 原始 observation 或稳定引用（`evidence_id`/对象路径/segment ID）；
5. 被支持的 claim 及其支撑关系；
6. `hypothesized`、`verified`、`corrected` 等可审计状态；
7. confidence、校验方式、校验时间和修订原因。

这会把 DeepVideoDiscovery 的时间窗、timecode-agent 的 checkpoint 账本、DOVideo-AI 的结构化 evidence 统一到 VidLens 的 PostgreSQL 事实层；pgvector 只保存可重建的检索投影。

### 5.2 建议的检索漏斗

**[基于源码的推断]** 一次查询可以按如下预算运行：

```text
问题/意图
  → 全局摘要或 transcript 粗召回（低成本、宽覆盖）
  → pgvector + 关键词的时间片段召回（稳定 evidence_id）
  → 相邻时间窗/跨模态回填（ASR/OCR/summary）
  → 仅对需要视觉确认的时间窗做 frame inspection
  → 形成带 claim-support 关系的回答
```

每一步都应写入候选、时间范围、命中模态、过滤原因和预算消耗。这样 UI 可以展示真实步骤，回答也不会把未执行的视觉检查伪装成已执行；当前 Agent 流式契约中的步骤事件设计可继续作为传输层，而不是证据事实层。

### 5.3 建议的状态和可靠性约束

**[基于源码的推断]** 推荐组合以下约束：

- 用 PostgreSQL 追加事件保存 claim/evidence revision，用 latest projection 服务在线读取；不要让 Redis 或 vector index 成为唯一事实。
- 用 typed time overlap、稳定 segment ID、对象可解码性和 revision/content hash 校验支撑；不要只验证回答中的 `[C1]` 或模型自报 confidence。
- 用 `content_hash + normalized goal/mode digest` 做分析任务幂等键，同时明确“同内容、不同问题”不能共享同一结论。
- 为 Agent 工具循环设置 max steps、时间/调用预算、重复调用检测、空结果处理和 fail-closed 终态；把每次工具调用的输入、输出引用和时间窗落入 trace/evidence snapshot。
- 把 coverage/readiness 作为继续检索的建议，而不是正确性证明；terminal claim 必须有可解析、版本一致的支撑。

### 5.4 明确的非目标

**[基于源码的推断]** 以上四个项目的证据不足以支持以下承诺，VidLens 也不应仅凭引入这些术语就宣称已经具备：

- 通用长期记忆或自动遗忘；
- 对任意自然语言 claim 的语义真值证明；
- 密码学防篡改或 C2PA 级 provenance；
- 只依赖“Planner/Executor/Critic”名称就获得可靠自主规划；
- 只靠 frame/scene/dHash 采样就获得完整视频内容覆盖。

## 6. VidLens 当前实现（2026-08-30）

VidLens 已落地证据账本的最小纵向切片：

- `agent_claims` 保存回答事实、`hypothesized`、`verified`、`corrected`、`unsupported`、`uncertain` 状态，以及不覆盖历史的 root/revision 链；
- `agent_evidence` 保存当前 Agent 检索结果的稳定 `EvidenceID`、task/document 定位、引用原文、SHA-256、来源 revision 和时间区间状态；
- `agent_claim_evidence` 保存事实与证据的显式支持关系及绑定核验结果；
- 模板 Agent 和非流式 research Agent 都在原回答保存后写入账本；写入失败只记录服务端错误，不改变回答、引用或既有 SSE 事件；
- 事实后的 `[C#]` 仅在服务端作为绑定标记使用，展示前仍按原逻辑移除；未绑定事实不会被删除，而是标为 `unsupported` 或 `uncertain`；
- ASR 分段或视觉帧能够定位引用时保存可重放时间范围；不能可靠定位时保存 `0/0 + time_range_status=unknown` 并降级 Claim，禁止根据语义 chunk 序号伪造时间码；
- `GET /api/v1/agent/evidence-ledgers/:run_id` 按 owner 查询账本，`POST /api/v1/agent/evidence-ledgers/claims/:claim_id/corrections` 追加人工更正；demo 用户保持只读。

当前没有实现自然语言蕴含证明、视觉补检漏斗、密码学防篡改或独立 Run/Step 恢复。账本不持久化 prompt、Planner 草稿或原始 Chain-of-Thought。

## 7. 来源索引

### AGI-saber

- [AGI-saber/AGI-saber-go（官方入口，核验时 404）](https://github.com/AGI-saber/AGI-saber-go)
- [AGI-saber（官方组织入口，核验时 404）](https://github.com/AGI-saber)
- [GitHub 官方仓库搜索 API](https://api.github.com/search/repositories?q=AGI-saber-go)

### DeepVideoDiscovery

- [官方仓库](https://github.com/microsoft/DeepVideoDiscovery)
- [固定 commit 的 README](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/README.md)
- [固定 commit 的数据库构建](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/build_database.py)
- [固定 commit 的 Agent 核心](https://github.com/microsoft/DeepVideoDiscovery/blob/64414b2f35d26809a39740a5a319889f46e29b94/dvd/dvd_core.py)
- [论文 abstract](https://arxiv.org/abs/2505.18079)
- [论文 HTML](https://arxiv.org/html/2505.18079)

### timecode-agent

- [官方仓库](https://github.com/mupozg823/timecode-agent)
- [固定 commit 的 README](https://github.com/mupozg823/timecode-agent/blob/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03/README.md)
- [固定 commit 的架构文档](https://github.com/mupozg823/timecode-agent/blob/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03/docs/ARCHITECTURE.md)
- [当前仓库代码目录](https://github.com/mupozg823/timecode-agent/tree/02f7c5a9ce1c09b4ba49177d2a4dc8e9ee1bbc03)

### DOVideo-AI

- [官方仓库](https://github.com/Xiaoc7r/DOVideo-AI)
- [固定 commit 的 README](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/README.md)
- [固定 commit 的 VideoContextService](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/server/src/main/java/com/example/server/service/VideoContextService.java)
- [固定 commit 的 VideoEvidenceRetrievalService](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/server/src/main/java/com/example/server/service/VideoEvidenceRetrievalService.java)
- [固定 commit 的 AgentLoopService](https://github.com/Xiaoc7r/DOVideo-AI/blob/caed156914e4cb4fc76e729f8fd79004674a1c75/server/src/main/java/com/example/server/service/AgentLoopService.java)
