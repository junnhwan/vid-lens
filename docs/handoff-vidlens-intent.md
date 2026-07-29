# Handoff: vid-lens 意图识别子系统嫁接

> 一次会话的工作交接。下个 session 的目标:**在 Go 项目 vid-lens 里落地一个借鉴自 xfg-wali(小傅哥 bugstack.cn)的 AI Agent 项目的意图识别子系统,作为简历亮点**。

## 用户背景(已存于长期记忆,见 MEMORY.md)

- **身份**:大二升大三,Go/Java 后端方向,正在投简历找实习。
- **项目**:VidLens("映知")—— Go + Gin + GORM + Kafka 异步 + PostgreSQL/pgvector 的长视频内容理解 AI 后端。路径 `D:\dev\my_proj\go\vid-lens`。
- **学习风格**:vibe coding，有自用 skill 仓库 `D:\dev\my_proj\skills`(主力 learn-vibe-coded-project / project-interview-grill)。
- 用 pwsh,bash profile 在 `D:\wendang\PowerShell\`。

## 本会话已经完成的调研

### 1. 评估了 `D:\dev\agent-learn\other\xfg-wali` 仓库四个子项目

| 项目 | 语言 | 类型 | 结论 |
|------|------|------|------|
| walicode-client | Tauri+React+Rust | Agent 类前端壳 | 别复刻(非 Go) |
| walicode-server | Java/Spring Boot | Agent 类(编码助手) | 思想来源之一 |
| walissh-client | Tauri+React+Rust | Agent 类前端壳 | 别复刻 |
| walissh-server | Java/Spring Boot | Agent 类(SSH 运维) | 思想来源 |

**结论**:四个项目**全是 Agent 类**(LLM 驱动业务),不是业务 CRUD。真正 Java 的只有两个 server。作者是小傅哥,`cn.bugstack.ai`,教学样板级,可当 maven archetype 生成脚手架。

**对实习简历的判断**:1:1 Go 平移 wali-server(只做后端,手写 ReAct 引擎+MCP+SSH tool)竞争力 ⭐⭐⭐⭐;但用户最终选择**不整体复刻,只借鉴其中"意图识别"亮点**嫁接到自己已有的 vid-lens,这样更自然、不撞车。

### 2. 精读了 wali 的意图识别设计(关键产出)

源:`walicode-server/docs/intent-recognition-enhancement-design.md` 与 `walissh-server` 下同名(两份字节级相同),实现代码在 `walicode-server-domain/.../service/intent/`。

**意图识别的本质**:LLM 调用前的一道**廉价位闸门**。两件事:
- **intent** → 定资源配额(wali 里是 ReAct 步数/工具调用数/token 预算)
- **signal/entities** → 指代消解 + 检索上下文注入 prompt

**核心机制**:
- 两层级联,不是并行投票:**规则分类器(<1ms,confidence≥0.8 短路)→ LLM 兜底分类(100-500ms)→ LLM<0.5 回退规则结果(不 UNKNOWN 空转)**。
- 规则打分三段式:`keyword×0.2(≤0.6) + regex×0.2 + context×0.1`,封顶 1.0 但**永远到不了**(0.9 上限),强制短路需多维证据。
- LLM 分类器用**独立低温度(0.1)廉价模型实例**,和主对话模型隔离配置。
- Prompt 强约束 JSON-only + 正则抠取 `{...}` + 解析失败兜底 UNKNOWN。
- Signal 是**纯函数无副作用正则提取**,只提取不决策。六维:filePaths/symbolNames/errorPatterns/commandHints/apiHints/opsKeywords。
- **指代消解**:entity 回写到 `ConversationContextVO.lastEntities` → 检测代词(它/这个/that)→ 替换 → **二次跑 signal 提取**(闭环)。这是最值钱、最便宜的讲故事点。
- 配置优先于意图分级:YAML `reactBudget` 覆盖 intent 默认。
- 全程 session 级 LRU+TTL 缓存(SHA-256 键)、fallback 标志位、结构化日志。

详细代码层面研读已在上一轮 agent 报告里,这里只摘要。

### 3. 摸清了 vid-lens 现状和已有意图识别雏形

**AI 在 vid-lens 的 6 个落点**:分段 ASR 转写、LLM 摘要、标题生成、关键帧 Vision/OCR、Embedding 索引、RAG 问答(LLM rewrite + 模型 rerank)。

**整体 pipeline**:Kafka 多阶段异步——`文件落 MinIO → analyze(ffmpeg切分→ASR分段→转写落库→视觉索引→投递→LLM摘要) → rag_index(切chunk→embedding→pgvector) → 同步RAG问答`。全程 Redis 分布式锁 + processing lease + 退避重试。

**关键:vid-lens 已经有意图识别的散落雏形,是改造的最佳切入点**:
1. `internal/service/video_agent.go` 的 `ClassifyVideoAgentTemplate` —— 已按关键词分 4 类 intent(direct_qa/summarize_topic/compare_topics/critique_topic),标注是实验路径。
2. `internal/service/chat_prepare.go` 的 `isVideoOverviewQuestion` —— 硬编码关键词判定"是否概览问题"。
3. `prepareChatByMode` 的 StrictRAG vs VideoAssistant 分流 —— 已是 intent 路由雏形。

**接入点关键文件**(下个 agent 要改的):
- `internal/service/chat_ask.go` — `AskWithMode`(插 IntentService.Classify 的入口)
- `internal/service/chat_prepare.go` — `prepareChatByMode` / `isVideoOverviewQuestion`(被替换的硬编码)
- `internal/service/rag_pipeline.go` — `RetrievalPipeline.Retrieve`(接收 intent 定的预算 top_k/rerank/rewrite 开关)
- `internal/service/video_agent.go` — 已有 `ClassifyVideoAgentTemplate`(可作为 RuleIntentClassifier 的基础)
- `internal/ai/` — 有 `Strategy`/`Factory`/`Chat`/`CompositeStrategy`,LLM 兜底分类用现成 chat client(开个 temperature=0.1 的廉价配置)
- `internal/model/chat.go` — 会话表(带 `RetrievalSnapshot`),`ContextTracker` 可挂这上面支持指代消解

## 下个 session 要做的事(已与用户对齐的范围)

**核心**:在 vid-lens 里新建 `internal/service/intent/` 子包,把散落的三处判定收口成统一的前置路由层。按优先级:

| 顺序 | 内容 | 代码量 | 简历价值 |
|------|------|--------|---------|
| 1 | `IntentService` + `RuleIntentClassifier`(三段式打分,0.8 短路) | ~200行 | ⭐⭐⭐ |
| 2 | `SignalExtractor`(视频域四维:时间戳/实体/章节/比较,纯函数正则)+ 接入 prepareChatByMode | ~150行 | ⭐⭐⭐⭐ |
| 3 | `ContextTracker` + 指代消解(实体回写→代词替换→二次提取闭环) | ~100行 | ⭐⭐⭐⭐⭐(最高性价比讲故事点) |
| 4 | `LLMIntentClassifier` 兜底 + LRU+TTL 缓存(SHA-256 键) | ~150行 | ⭐⭐⭐ |
| 5 | 可观测日志 + fallback 标志位透传 | ~50行 | ⭐⭐ |

**intent 类型**(直接套 vid-lens 已有领域语义,不要用 wali 的 DIAGNOSE/CONFIGURE):
- `video_overview`(概览,复用摘要零检索)、`direct_qa`(标准 RAG)、`topic_compare`(放大 top_k 强制 rerank)、`topic_summarize`、`timeline_locate`(跳过 embedding 走转写时间戳)、`small_talk`(零检索零 LLM 兜底)。

**intent → 执行预算**:不是 ReAct 步数(wali 那套),而是 vid-lens 已有的检索参数 — `Retrieve` bool / `TopK` / `Rerank` / `Rewrite` / `UseSummary` / `UseTranscript` / `UseLLM`。一个 switch 消掉 `prepareChatByMode` 硬编码。

## 简历卖相(本会话与用户对齐的话术)

> **意图识别前置路由层(借鉴 LLM Agent 架构)**
> - 级联意图识别:规则分类器(<1ms,关键词+正则+历史意图加权,0.8 置信短路)→ LLM 兜底(独立低温度廉价模型,0.5 阈值,失败回退规则),平均 70%+ 请求规则层短路,省一次 LLM 调用
> - 信号提取纯函数(时间戳/实体/章节/比较四维正则)+ 指代消解(实体回写→代词替换→二次提取闭环),支持多轮对话上下文记忆
> - intent → 检索资源配额路由:概览复用摘要零检索、时间定位走转写跳过 embedding、对比放大 top_k 强制 rerank、闲聊零成本兜底,降低无效 RAG 开销

价值点:有算法(级联/阈值/回退)、有工程(纯函数/缓存/降级/可观测)、有真实业务价值(省 LLM 调用、省检索成本)、Go 后端八股能 hook(strategy 模式/接口隔离/纯函数并发安全)。且与现有架构吻合(不是另起炉灶)。

## 注意事项 / 风险

- **不要复刻 wali 的 xfg-wrench 策略树框架**和 Java 三大 AI 框架(Spring AI/ADK/LangChain4j),Go 侧没有对等物,用标准 interface + 工厂替代,反而更干净,面试不会被怀疑套现成轮子。
- **不要碰 wali 的 Tauri/Rust 客户端**——用户投 Go 后端,复刻前端壳对简历是负分。只做后端。
- 实现**必须真懂 wali 的 ReAct engine 和 armory 装配源码逻辑才能在面试 explain**,建议动手前先深读 `walicode-server-domain/.../service/intent/` 和 `cases/react/node/` 的意图消费点(RootNode/AiCallNode)。
- vid-lens 的 `video_agent.go` 标注是**实验路径**,产品默认走 `ChatService`,新写的 intent 层要和默认路径对接好,别造两条互不认的路。
- wali 是小傅哥公开教程,Go 移植天然洗白(包名/结构/栈全变),但核心难点得能讲清楚,否则一问就露馅。

## Suggested skills(下个 agent 应优先调用)

1. **codebase-design** — 在动手写 intent 子包前,用它把 vid-lens 现有 architecture 梳理清楚并设计新模块的接入方式(避免破坏现有 RAG pipeline)。
2. **domain-modeling** — intent类型 / signal / entity / ExecutionPolicy 这些值对象建模,正好是它的用武之地(借鉴 wali 的 IntentTypeEnumVO/IntentResultVO/IntentRuleVO/ConversationContextVO 思路转 Go)。
3. **resume-project-interview-prep** — 实现阶段落定后,用它把"意图识别前置路由层"这一段落到简历项目卡片 + 面试 grill 文档里(用户自用 skill 仓库 `D:\dev\my_proj\skills`,主力就是这个)。
4. **learn-vibe-coded-project** — 用户的自用学习 skill,可用它反向把 wali 的源码吃透(intent 消费点/ReAct/指代消解),确保面试能 explain 每一行。
5. **tdd** — intent 分类和 signal 提取是纯函数 + 阈值判定,天然适合先写 case(正例/反例/边界/0.7vs0.8 短路),推荐 TDD 推进阶段 1-2。
6. **code-review** — 阶段接进 chat_ask.go 改动 prepareChatByMode 后,用它审接入点是否破坏现有问答路径。

(用户的项目面试/学习类 skill 装在 `~/.claude/skills`,详见 MEMORY.md 的 skills-repo / skills-environment 两条记忆。)

## 下一步首选动作

进入 worktree 或直接在 vid-lens,先跑 **codebase-design** skill 梳理 `internal/service/chat_ask.go` / `chat_prepare.go` / `rag_pipeline.go` / `video_agent.go` 四个文件的现有控制流,设计 `internal/service/intent/` 接口与最小接入点,再按上面 1→5 顺序落地。阶段 1 完成(规则分类器跑通短路)即可与用户对齐一次,再继续 2-5。
