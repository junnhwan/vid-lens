# VidLens — 长视频理解与智能问答

一个 AI 驱动的长视频内容理解与可追溯问答后端。用户上传或 URL 拉取长视频，经异步链路转写、摘要、索引后，进行带引用的问答。重构聚焦两条线：后端处理链路可靠性，与意图感知的 RAG 检索。

## Language

### 处理链路

**Asset**:
一段被系统处理的视频资源（上传或 URL 拉取），含原始媒体文件与元数据。
_Avoid_: 视频（口语，模糊）、file、resource

**Task**:
用户可见的、有完整生命周期的一次处理请求（如"转写这个视频""为这个视频建索引"），状态机 Pending→Queued→Running→Completed|Failed|Dead。
_Avoid_: job（口语混用，见下）、work

**Job**:
单个 Task 被拆出的一类异步处理单元，由 RabbitMQ 队列投递、consumer 消费（video-download / video-transcribe / video-analyze / video-rag-index）。一个 Task 对应多个 Job。
_Avoid_: task（与上面冲突）、step、stage

**Processing Lease**:
worker 持有 Job 执行权的一段租约，以 (token, version, expiry) 做 DB CAS，过期或被抢占即视为让出。
_Avoid_: lock（不准确，Redis 分布式锁是另一层）、claim

**Transcript Segment**:
长视频被 FFmpeg 切出的、单片可独立重跑的 ASR 单元，每片独立持久化状态与结果，失败恢复只重跑缺失片。
_Avoid_: chunk（与 RAG chunk 冲突）、fragment、piece

**Chunk**:
转写文本被切出的、用于向量检索的最小检索单元，落 PostgreSQL `video_chunks` 为事实源，pgvector 投影为向量。
_Avoid_: segment（与 Transcript Segment 冲突）、piece

### 意图感知 RAG

**Intent**:
一次用户提问被识别出的处理类别，决定后续执行预算（检索是否开、scope、top_k、rewrite、rerank、是否走 LLM）。
_Avoid_: category、type（太泛）

**ExecutionPolicy**:
Intent 到具体检索/生成参数的映射值对象（Retrieve / TopK / Rerank / Rewrite / UseSummary / UseLLM / Scope）。
_Avoid_: budget（口语）、config、plan

**Signal**:
从用户提问中无副作用正则提取的结构化线索（时间戳 / 实体 / 章节 / 比较），用于指代消解与检索过滤。
_Avoid_: entities（wali 用此词，但 Signal 范围更广）、keywords

**Collection**:
用户显式命名的、一组同系列/需跨视频问答的 Asset 集合，会话可绑定之；`topic_compare`/`series_locate` 类 Intent 将检索过滤到集合内 video_ids。
_Avoid_: series（推断味）、group、playlist

**Scope**:
一次问答检索的范围档：单视频（会话当前绑定 Asset）或集合（会话绑定 Collection）。
_Avoid_: range、filter

**RuleIntentClassifier**:
意图分类的规则层，打分维度为关键词命中 + signal 模式（时间戳/比较句式/概览句式/闲聊）+ 历史 intent 加权，confidence 达阈值短路，<1ms。具体权重值由 (A) spec 的 audit trail 写理由。
_Avoid_: rule engine、classifier（太泛）

**LLMIntentClassifier**:
规则层未短路时的 LLM 兜底分类器，独立低温廉价模型，confidence<0.5 回退规则结果。
_Avoid_: LLM router、fallback classifier

### 事实源与投影

**Source of Truth (SoT)**:
RAG 的事实源 = PostgreSQL `video_chunks` 行；pgvector 向量为可重建投影，以稳定 evidence ID 绑定，禁止无主向量写入。
_Avoid_: primary data、master

**Evidence**:
一个 Chunk 的稳定标识，作为关系行与向量投影之间的绑定键。
_Avoid_: id（太泛）、reference

### 可靠性

**Dispatch Intent**:
首次创建 Task→投递 Job 之间消除丢任务窗口的 durable 记录，transactional outbox 或同义机制。
_Avoid_: outbox（实现名，非领域概念）、queue entry

**Tombstone**:
标记长任务待取消的轻量意图记录，worker 在阶段边界检查并释放 lease（候选能力，非默认稿）。
_Avoid_: cancel flag、dead marker

**BYOK Profile**:
用户级 AI 服务凭证配置（ASR/LLM/Embedding 三类），key 以 AES-GCM 加密入库。
_Avoid_: AI config、provider key
