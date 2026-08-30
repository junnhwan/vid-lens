# 视频理解管线改造计划

本文描述 VidLens 从“ASR 文本问答”演进为“带可信时间轴的多模态视频理解”的当前基线、目标模块、实施顺序和验收口径。它只讨论工程实现，不把模型输出本身当作事实。

状态：“来源映射与时间感知 RAG”已按本文契约实施，实施基线为 `38cd22f`（2026-08-30）。重叠 ASR 时间窗、稳定 segment identity、毫秒级 window/core 元数据和确定性 transcript stitcher 继续作为上游事实；ASR 分阶段延迟观测、受控并发、provider 级重试和部分失败复用也已实现。视觉分支解耦与在线视觉核验仍不在当前范围内。为保留改造决策上下文，下方前三节记录更早的历史管线；`38cd22f` 的编码前事实以“来源映射与时间感知 RAG 实施规格”为准。

## 历史改造结论

项目已经具备多模态雏形：场景帧与定时帧抽取、Vision/OCR、`video_visual_frames`、视觉文本入 RAG，以及 evidence funnel 中的视觉证据确认。当时主要问题不是“缺少一个 Vision 接口”，而是 ASR、视觉和 RAG 尚未汇合成同一条可信时间轴：

- ASR 以固定 300 秒、无重叠的音频文件串行调用，结果用空行直接拼接；上游硬切会永久破坏句子边界。
- RAG 的递归句子切片器能保护已有标点边界，却无法恢复 ASR 已截断的语义；ASR 分片间的空行还会被当作强边界。
- `video_chunks` 只保存内容和序号，没有模态、时间范围和来源映射。视觉文本虽然能被嵌入，但进入检索后会退化为带标签的普通文本。
- Evidence Ledger 当前可能用 RAG `chunk_index` 解析同序号的 ASR 分片。两个序号属于不同空间，不能作为稳定映射。
- 视觉索引在 ASR 完成后同步执行、再次下载视频并逐帧调用 Vision/OCR，增加关键路径延迟；`Phash` 字段尚未参与实际去重。

改造的首要目标不是增加更多 Agent 工具，而是建立一个深模块：调用方只提交视频任务，模块产出带时间范围、模态和 provenance 的证据块。ASR 边界修复、视觉采样、语义切片和索引投影都隐藏在该模块内部，RAG、Agent、引用和前端共同消费同一接口。

## 历史实现基线

### 视频处理

当前主路径为：

```text
VideoTask
  → 下载视频
  → FFmpeg 提取 16 kHz / mono / 32 kbps MP3
  → 固定 300 秒切片（无重叠）
  → 逐片 ASR，结果独立写入 video_transcription_chunks
  → 直接以空行拼成 video_transcriptions.content
  → 同步关键帧 Vision/OCR
  → 投递 RAG index job
  → 全文语义切片 + 视觉文本追加
  → video_chunks + pgvector 投影
```

相关实现：

- `internal/mq/consumer_transcribe.go`：转录编排、分片复用和结果拼接。
- `internal/pkg/ffmpeg/ffmpeg.go`：音频提取与固定分片。
- `internal/service/chunk_splitter.go`：句号、分号、逗号、空白、字符级递归切片。
- `internal/service/visual_index.go`：关键帧 Vision/OCR 与视觉文本格式化。
- `internal/service/rag_index_build.go`：把全文切片和视觉文本写入 RAG。

### 已有能力应保留

- PostgreSQL 继续作为转录分片、视觉帧、RAG chunk 和证据账本的事实源。
- pgvector 继续是默认可重建检索投影；不引入第二套向量事实源。
- ASR 分片级完成状态和失败复用继续保留，重试不能重做已完成的昂贵调用。
- 当前递归句子切片器作为降级实现保留；无时间数据或旧数据仍可构建文本索引。
- 视觉处理保持 fail-open：视觉失败不能让已有 ASR 问答完全不可用。
- 默认标准 RAG、显式 research Agent 和 evidence funnel 的产品边界不合并。

## 根因分析

### ASR 断句

固定时长切音频本身不是错误；错误在于切片没有上下文重叠，且拼接器不知道相邻片段的时间范围和重叠内容。只在下游按标点重新切文本无法恢复丢失的语音上下文。

目标处理方式：

1. 每个逻辑 300 秒 core window 在两侧带少量音频重叠，例如 5 秒；相邻 ASR 请求因此共享边界语音。
2. 持久化 `window_start/end` 与 `core_start/end`，重试身份由切片策略版本和时间范围共同决定，不能只靠数组序号。
3. 使用确定性的 transcript stitcher 对相邻输出做规范化 suffix/prefix 对齐，去掉重叠文本并保留完整标点。
4. 精确对齐失败时不让 LLM 静默改写原文；保留两个原始分片及低置信度边界，使用安全连接策略，并记录指标供后续评估。
5. provider 若能返回 utterance/word timestamps，则由 provider adapter 提供精确时间；纯文本 provider 只能给出 coarse window，不能按字符比例伪造精确时间。

### ASR 延迟

ASR 关键路径已先建立分阶段观测，再把逐片 provider 调用改为固定 worker pool。视觉处理与第二次下载仍是后续独立优化项。

- Prometheus 使用 `vidlens_asr_stage_duration_seconds{stage,status}` 记录 `audio_extract`、`segment_prepare`、`provider_request`、`retry_wait`、`stitch`、`persistence`。同一测量同时输出 `asr stage measured` 结构化日志及 `duration_ms`；task ID 只进日志，不进入指标 label。
- `vidlens_asr_provider_inflight` 暴露当前 provider 请求数；原有 `vidlens_asr_chunk_duration_seconds` 继续表示一个逻辑分片包含重试等待在内的总耗时。
- 单任务 worker 数由 `mq.asr_concurrency` 控制，默认 3、配置上限 16；只创建固定数量 worker，不按分片数量创建 goroutine。RabbitMQ transcribe consumer 的任务级并发、provider admission、用户/operation/provider/model 配额仍构成外层约束。
- 每个 provider attempt 都独立计时。429、5xx、网络错误和超时只有在 provider adapter 明确标记可重试时才重试；429 和本地 admission rejection 优先采用 `Retry-After`，否则使用 `mq.asr_retry_backoff_ms`。默认最多额外请求 2 次，配置硬上限为 5；设为 0 可关闭单次任务内的 provider retry，任务级恢复仍由 RetryScheduler 负责。
- retry attempt 使用稳定的 `task-{taskID}-asr-chunk-{index}:provider-retry:{n}` 身份；当 task job 绑定了 retry budget 时，额外请求继续消费同一持久化预算，正常首请求不消费额外重试额度。
- worker 完成顺序不影响输出：结果写回原始 chunk index，最终仍按时间窗顺序交给同一个确定性 stitcher。
- 单片最终失败不会取消其他片。成功片先写入 `video_transcription_chunks`，失败片记录失败和 retry count；本次任务返回最低失败索引的稳定错误。重复消费或任务级重试复用已完成且 segment key 匹配的片，只请求失败/缺失片。
- 上游 context 取消会终止排队 worker、provider 请求和 retry wait；取消本身不增加分片失败次数。processing lease 和消息幂等门仍负责拒绝过期/重复消费者。

因此当前并发优化不会改变 overlap window、原始分片内容或 stitcher 算法；相同分片结果集合必须得到与串行实现相同的 transcript。

### RAG 语义不连续

当前句子优先切片已经比固定字符切片更安全，但仍存在三个信息损失点：

- 输入全文已经包含 ASR 硬边界。
- `TextChunk` 只有 `Index/Content`，切片后无法回指原始转录分片与时间范围。
- transcript 与 visual 使用同一 `video_chunks` 行形状，却没有 modality/provenance 字段。

目标 chunk 不是“800 字符字符串”，而是可重放证据：

```go
type EvidenceChunk struct {
    Content         string
    Modality        string // transcript | visual_ocr | visual_caption | fused
    StartMS         int64
    EndMS           int64
    TimeRangeStatus string // exact | coarse | unknown
    SourceRefs      []SourceRef
}
```

切片策略按以下优先级选择边界：provider utterance/sentence → 强标点 → 从句标点 → 安全硬切。overlap 只复用完整语义单元；每个输出块保留覆盖到的 source refs。检索相邻上下文优先按时间相邻和同模态扩展，旧数据才按 chunk index 降级。

### 多模态不是“把 OCR 字符串追加到全文”

画面理解分为三层，不能互相冒充：

- 采样事实：帧时间、scene score、图像对象、采样策略与感知哈希。
- 感知结果：OCR 原文、Vision caption、模型与 prompt 版本、失败状态。
- 语义证据：与问题相关的时间窗、引用帧和经过约束的 claim。

离线索引负责低成本的候选定位；在线 Agent 只对命中的少量时间窗做视觉检查。标准 RAG 可以召回视觉 OCR/caption，但 citation 必须显示模态、时间和帧来源，不能继续仅返回 `source=vector/hybrid`。

## 目标模块与 seam

目标外部接口保持小而稳定：

```go
type TimelineBuilder interface {
    Build(context.Context, BuildTimelineRequest) (TimelineArtifact, error)
}
```

`TimelineArtifact` 是一次处理版本的结构化产物，包含 transcript observations、visual observations、evidence chunks、降级状态和指标摘要。调用方不需要知道 FFmpeg 切片参数、ASR overlap 对齐算法、OCR/Vision 选择或 chunk packing 细节。

模块内部存在真实 seam：

- `ASR adapter`：生产环境的 OpenAI-compatible/MiMo provider 与测试 scripted adapter。
- `Vision adapter`：生产 Vision provider 与本地 OCR/测试 adapter。
- `Evidence projection adapter`：PostgreSQL 源行与 pgvector/Milvus 投影。

FFmpeg 音频窗口生成、transcript stitcher 和语义 packing 是进程内实现细节，不对 handler、Agent 或前端暴露。

## 来源映射与时间感知 RAG 实施规格

### 编码前现状核对

以下结论来自 `38cd22f` 的实际实现，而不是目标设计推断：

- `video_transcriptions.content` 是问答与索引读取的拼接文本；`video_transcription_chunks.content` 保存各 ASR window 的原始 observation。新 observation 已有 `segment_key`、window/core 毫秒范围，旧行可能只有数据库 ID、秒级范围或完全没有时间。
- `internal/service/chunk_splitter.go` 已按强标点、从句标点、空白、字符硬切递归选择边界，并只复用完整语义单元；但 `TextChunk` 只有 `Index/Content`，上游来源在切片时全部丢失，`chunk_size` 与 `token_count` 实际仍按 rune 数处理。
- `video_chunks` 只有内容、序号、embedding 与 vector identity，没有 modality、时间范围、映射状态或 source refs。pgvector/Milvus 投影也只返回检索元数据和内容。
- 视觉帧拥有真实 `time_ms`、数据库 ID、caption method 和对象 key，但 `FormatOCRChunksForIndex` 只把这些信息格式化进文本标签，没有保存结构化来源。
- 向量检索结果、BM25 结果、公开 `Citation` 均不携带模态、时间或 source refs；`Source` 仅表示 vector/keyword/hybrid 召回通道，不应继续兼任 modality。
- Evidence Ledger 会尝试把 RAG `chunk_index`、`chunk_id` 或 evidence ID 解析回一个 RAG chunk index，再读取同序号 `video_transcription_chunks`。这把两个独立序号空间错误地当成同一身份，chunker 改变后会产生错误时间和错误文档定位。
- `video_rag_indexes` 已保存 chunker strategy/version、参数、manifest hash 和 build version，但在线去重只按 `file_md5 + embedding_model + indexed` 判断，未校验当前 source-mapping/build 版本，旧索引可能错误地阻止重建。
- PostgreSQL schema 由 GORM `AutoMigrate` 扩展；MySQL 只用于离线迁移。新增字段必须允许旧行保留并可读取，不能要求部署时同步全量重建。

### 本次目标数据模型

`video_chunks` 新增以下可空或带安全默认值的事实字段：

- `modality`：`transcript | visual_ocr | visual_caption | unknown`。`unknown` 只用于旧行或无法证明来源的降级数据。
- `start_ms` / `end_ms`：来源覆盖的毫秒范围。只有 `end_ms > start_ms` 且来源事实支持时才可对外宣称时间范围。
- `time_range_status`：`exact | coarse | unknown`。ASR window/core 没有 utterance/word timestamp 时只能是 `coarse`；视觉关键帧的 observation timestamp 可表示为精确的点范围；旧 `0/0` 保持 `unknown`。
- `source_mapping_status`：`mapped | partial | unmapped`。它描述内容到 source refs 的映射完整性，不能由“有时间”间接推断。
- `source_refs`：稳定 JSON 数组。每项至少包含 source type、stable observation ID、可用的 `segment_key`、事实表行 ID、该 observation 的时间范围和时间状态。ASR 优先以 `segment_key` 为 stable ID；没有 segment key 的历史 observation 只能使用持久化行 ID，且不得因此补造时间。
- `chunker_strategy` / `chunker_version`：写入 chunk 行本身，使关系事实源可独立审计，不必只依赖索引状态行。

`video_rag_indexes` 的 build/source-mapping version 与 chunk manifest 一并升级。manifest 必须覆盖 modality、时间状态、映射状态、source refs 和 chunker provenance；这些字段任一变化都应改变 manifest。在线内容去重只有在 build version、chunker version 和 source-mapping version 与当前实现一致时才可跳过索引。

向量库继续只做可重建投影。检索命中后以 `chunk_id/evidence_id + user/task/model scope` 从 PostgreSQL 回填 provenance；不能信任旧向量 payload 伪装成来源事实。这样 pgvector 和兼容 Milvus 不需要在同一次部署中同步迁移 provenance schema。

### 切片与来源映射算法

1. 从已完成 ASR observation 按持久化顺序重放拼接：只有相邻 window 元数据证明存在 overlap 时才使用确定性 stitcher，否则使用旧的安全连接方式。
2. 重放结果必须与 `video_transcriptions.content` 一致；不一致时整份 transcript 仍可索引，但标记 `unmapped + unknown`，不得猜测字符属于哪个 ASR 分片。
3. stitcher 同时返回每个保留输出区间对应的原始 observation。被 overlap 去重的右侧前缀不再重复占有输出文本；保留下来的字符仍能回到至少一个真实 observation。
4. 在带 source span 的文本上依次按段落/换行、完整句子、从句、空白选择边界；只有单个语义单元本身超过预算时才硬切。overlap 只复用完整单元，并合并这些单元的 source refs。
5. `chunk_size`/`chunk_overlap` 在新 chunker version 中解释为保守 token 预算，不再把 rune count 写成 token count。实现使用确定、可测试的本地上界估算，避免依赖 embedding provider 的私有 tokenizer；任何输出 chunk 都不得超过配置预算，超长单元必须继续细分。
6. transcript 与单个视觉 observation 不跨模态混装。视觉 OCR/caption chunk 直接引用 `video_visual_frames` 的稳定行 ID、真实 `time_ms`、caption method 和对象定位。

### 检索、引用与 Evidence Ledger 契约

- `RetrievedChunk` 和公开 `Citation` 传递 `modality`、`start_ms/end_ms`、`time_range_status`、`source_mapping_status` 和 `source_refs`。现有 `Source` 字段继续只表示召回通道。
- BM25 从关系行直接填充 provenance；向量命中必须通过关系行批量回填。回填失败时结果只能降级为 `unknown/unmapped`，不能沿用 chunk index 猜来源。
- 邻接上下文仍可扩大给 LLM，但公开 citation 始终指向 anchor chunk，并只公开 anchor 的时间和 source refs；不能把邻居的范围冒充为 anchor 范围。
- Evidence Ledger 仅通过 citation 的 `chunk_id/evidence_id` 定位真实 `video_chunks` 行，再消费该行持久化的 source refs 和时间范围。删除 RAG index → ASR index 的所有映射逻辑，也不再用重复文本或 chunk index 选择一个 ASR observation。
- 映射完整且时间可重放的 evidence 才可形成 `known` ledger range；历史 `unmapped/unknown` evidence 仍可作为稳定文本引用，但 Claim 必须保持 `uncertain`，不能升级为 `verified`。

### 历史数据兼容与重建

- 新 schema 对旧 `video_chunks` 使用 `modality=unknown`、`time_range_status=unknown`、`source_mapping_status=unmapped`、空 source refs。读取旧行不会失败，也不会出现伪造的 `0ms` 精确引用。
- 已有 transcription observation 若能重放出完全一致的 transcript，可在显式重建时生成可靠 source refs；只有秒级持久化范围时标记 `coarse`，`0/0` 仍为 `unknown`。
- 旧 RAG index 的 build/source-mapping version 不满足当前契约时，状态接口和去重判断应把它识别为需要重建；不会在服务启动时自动删除或重写历史向量。
- `rag-reindex` 只重建已有关系 chunk 的向量投影，不能凭空补 provenance。要升级旧的语义切片和来源映射，必须重新执行 task RAG index build；两种操作在文档和状态中保持区分。
- 回滚只切回旧读取行为或停止新 build；新增关系字段和原始 ASR/视觉 observation 保留，不做破坏性降级。

### 本次验收标准

- 中英文句子、段落和长从句测试证明：有可用边界时 chunk 不在句中截断；超长单元严格受 token 预算约束；overlap 不制造只含重复内容的尾块。
- 索引集成测试证明：一个跨多个 ASR observation 的 RAG chunk 保存全部真实 source refs，重建后即使 chunk 数量或 index 改变，来源 stable ID 与时间范围不变。
- 视觉索引测试证明：OCR 与 caption chunk 的 modality、frame stable ID 和 `time_ms` 被结构化持久化，而不是只存在于显示文本。
- 存储测试覆盖新增字段、JSON source refs、时间范围查询/回填以及旧默认值读取。
- 检索与 citation 测试覆盖向量和 BM25 provenance 传播，并证明 `Source` 与 modality 不混用。
- Evidence Ledger 测试证明不再按 RAG chunk index 查 ASR；mapped citation 使用真实来源，unmapped 历史 citation 保持 unknown/uncertain。
- index manifest/version 测试证明 provenance 或 chunker 版本改变会触发 rebuild 识别，旧 indexed 行不会被错误复用。
- 不修改 `frontend/`；完成后运行 `go test ./...`、`go vet ./...` 和四个规定的 Go build 目标。

### 实施结果

- 新 RAG chunker 以段落/完整句子/从句/空白为递归边界，只对仍超预算的单元做安全硬切；overlap 只复用完整语义单元。RAG 路径的 `token_count` 使用 UTF-8 byte-level 保守上界，与展示引用的历史字符预算分离。
- `video_chunks` 由 GORM 迁移增加 modality、毫秒范围、时间/映射状态、source refs 和 chunker provenance；旧行的数据库默认值为 `unknown/unmapped/[]`。
- ASR 索引构建只在 observation 重放结果与权威 transcript 完全一致时写入 stable refs；不一致就整份降级为 `unmapped/unknown`。视觉 chunk 则直接保留 frame ID、`time_ms`、object key 和 caption method。
- 向量命中以关系 `chunk_id` 回填 PostgreSQL provenance，BM25 直接携带同一关系事实；`timeline_locate` 只保留真实范围重叠的 mapped chunk。Citation 和 Evidence Ledger 不再从 RAG `chunk_index` 推导 ASR 身份。
- 索引 build version 升级为 `2`，source mapping version 为 `source-map-v1`，chunker version 为 `recursive-sentence-source-v2`。manifest 纳入来源字段，状态接口将旧 indexed 行显式返回 `needs_rebuild`，内容去重也不再复用旧版本。

### 转录分片

`video_transcription_chunks` 逐步增加并使用：

- `window_start_ms` / `window_end_ms`：实际送入 ASR 的含重叠音频范围。
- `core_start_ms` / `core_end_ms`：该分片对最终 transcript 负责的非重叠范围。
- `segment_key`：由 task、时间范围和 segmenter version 生成的稳定身份。
- `raw_content`：provider 原始输出；现有 `content` 作为 stitch 后可消费文本的兼容投影。
- `timing_status` 与可选 utterance 明细：明确 exact/coarse/unknown。
- `segmenter_version` / `stitcher_version` / provider/model provenance。

首个兼容切片可继续写现有秒级 `start_second/end_second`，但新逻辑内部统一使用毫秒，避免视觉帧毫秒时间退化。

### RAG chunk

`video_chunks` 逐步增加：

- `modality`。
- `start_ms` / `end_ms` / `time_range_status`。
- `source_refs` 或规范化映射表，用于从语义 chunk 回到一个或多个 ASR segment / visual frame。
- `chunker_strategy` / `chunker_version`，并参与索引 manifest 和重建判定。

`chunk_index` 只表示某个 embedding model 下的展示/邻接顺序，不再承担 ASR 身份或时间身份。

### 引用

内部 `RetrievedChunk` 与公开 `Citation` 增加 modality、时间范围和可展示 locator。`Source` 继续表示 vector/keyword/hybrid 的召回来源，不再与证据模态混用。

## 实施顺序

### 重叠 ASR 与确定性拼接

- 引入带时间元数据的 audio window。
- 默认使用小范围 overlap，保留旧 fixed segmenter 作为兼容降级。
- 实现纯函数 transcript stitcher，覆盖中文、英文、标点漂移、无匹配和空分片。
- 写入真实 ASR window 时间范围，修复当前全部 `0/0 + unknown` 的新任务。
- 保持逐片重试复用和输出顺序确定性。

验收：跨 300 秒边界的一句话不重复、不插入强制段落；重试只调用缺失片；所有新完成分片都有合法时间范围；旧行仍可读取。

### 来源映射与时间感知 RAG

- 语义切片产出 source refs，不再从 RAG index 猜 ASR index。
- `video_chunks` 持久化 modality/time/provenance。
- Evidence Ledger 直接消费 source mapping；删除“同序号等价”的兼容推断。
- 相邻扩展和 `timeline_locate` 使用时间过滤；unknown 数据明确降级。
- reindex manifest 纳入 chunker 和来源版本，提供可恢复 backfill。

验收：任意 citation 可回指真实 ASR segment 或 visual frame；RAG chunk 数量变化不会改变证据时间；时间问题只检索重叠窗口。

### 视觉分支解耦与成本控制

- ASR 与视觉分支共享本地/对象资产并独立记录 job 状态。
- 实现实际 perceptual hash 邻帧去重，记录 kept/skipped 决策和版本。
- OCR 作为低成本离线候选；Vision caption 支持批量/有界并发和失败复用。
- 文本 RAG 可先完成；视觉产物完成后做增量视觉 projection，而不是重做全部 transcript embedding。

验收：视觉 provider 不可用时 ASR/RAG 可用；重复帧不重复付费；视觉完成不会覆盖 transcript 源事实。

### 多模态检索与 Agent 视觉核验

- 标准 RAG 对 transcript、visual OCR/caption 分模态召回并融合，保留各自得分。
- Agent 新增受限的 `search_visual_evidence` 与 `inspect_visual_window`；服务端注入 task/time scope。
- 原始帧检查只允许来自已召回时间窗，受 `max_visual_calls/max_frames/max_duration` 约束。
- 前端 citation 显示时间、模态，并可跳转视频位置/查看证据帧。

验收：纯画面问题能召回 visual evidence；语音与画面冲突时答案展示不确定性和各自来源；未调用视觉检查时 UI 不宣称“已看画面”。

### 延迟与质量评估闭环

- 构建边界句、长静音、快速字幕、静态 PPT、场景切换和语音/画面冲突数据集。
- 记录 boundary duplication/deletion rate、sentence completeness、Recall@K、MRR、citation source accuracy、视觉覆盖率、端到端 P50/P95 和单分钟成本。
- `rag-eval` 增加 chunker/stitcher/visual policy 版本快照，变更必须进行可复现对比。

## 首个代码切片

本文落地后首先实现“重叠 ASR 与确定性拼接”的最小闭环：

- 不改变 HTTP/SSE/前端协议。
- 不修改 Agent 工具注册表。
- 不将粗粒度 ASR window 描述为精确 utterance 时间。
- 不在同一改动中引入 ASR 并发；先用指标验证正确性，再单独调整并发和 admission。
- 新任务使用 overlap window 和 stitcher；旧任务与旧数据库行继续兼容。

完成该切片后，再实施 RAG source mapping。原因是后者必须建立在可信的上游 segment identity 和时间范围上，否则只是把错误映射持久化得更牢。

当前实现位于：

- `internal/pkg/ffmpeg/ffmpeg.go`：`overlap_windows_v1` 音频时间窗与显式临时目录生命周期。
- `internal/transcript/stitcher.go`：忽略空白、大小写和标点差异的确定性 suffix/prefix 拼接。
- `internal/mq/consumer_transcribe.go`：只在元数据证明窗口重叠时启用 stitcher；旧 path-only 分片保持兼容连接。
- `internal/model/transcription_chunk.go`：segment key/version、window/core 毫秒范围；旧秒级范围继续供现有 evidence 路径读取。

## ASR 延迟与受控并发代码切片

本切片只修改后端编排、AI retry、配置、指标和测试，不改变 HTTP/SSE/前端协议，也不改变确定性拼接契约：

```text
提取音频（计时）
  → overlap window 切片（计时）
  → 读取并固定复用 completed chunks
  → 将其余 chunks 标记 running
  → 固定 N 个 worker 并发调用 ASR
       每次 attempt：provider admission → 请求计时
       transient failure：共享 retry budget → Retry-After/退避计时 → 重试
  → coordinator 按完成事件持久化 success/failed
  → 所有结果按 chunk index 归位
  → 原确定性 stitcher（计时）
  → 权威 transcript 持久化（计时）
```

配置位于 `mq`：

```yaml
asr_concurrency: 3
asr_max_retries: 2
asr_retry_backoff_ms: [1000, 3000]
```

实现边界：

- `internal/mq/consumer_transcribe.go`：固定 worker pool、取消传播、稳定归位、部分失败持久化和阶段日志/指标。
- `internal/ai/retry_policy.go`：可重试 ASR strategy、Retry-After、本地 admission rejection、稳定 attempt identity 及 request/retry-wait observation。
- `internal/observability/metrics.go`：低基数阶段延迟直方图和 provider inflight gauge。
- `internal/config/config.go` 与 `cmd/server/wiring.go`：默认值、YAML 解析和生产接线。

验证覆盖并发上限、乱序完成后的稳定 transcript、并行请求确实重叠、context 取消、429 Retry-After、稳定 retry identity、部分失败后只重跑失败片，以及重复消费时 completed chunk 复用。测试使用受控 channel 而不是依赖脆弱的毫秒阈值判断并发收益。

## 发布、回滚与迁移

- 新策略必须带版本，索引行记录构建策略；回滚只切换新任务策略，不删除旧源数据。
- schema 先做可空/有默认值扩展，再写新字段，最后 backfill；不能要求一次部署同时重建全部历史视频。
- 新旧索引并存期间，以任务的 RAG index manifest 决定读取哪一版，不按“最新行看起来存在”猜测。
- backfill 使用现有 `rag-reindex` 的 checkpoint 思路，限制用户、task、模型和批量大小。
- 任何 stitcher 失败都保留原始分片，允许用旧拼接方式重建；不能只保存被合并后的不可逆文本。
- 回滚受控并发只需把 `mq.asr_concurrency` 设为 `1`；原始 chunk 行、segment identity 和最终 transcript schema 不需要迁移或回写。

## 验证命令

每个切片至少运行：

```powershell
go test ./...
go vet ./...
go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit
cd frontend
npm run typecheck
npm run build
```

只改后端且前端契约未变化时，开发过程中可先跑相关 Go package；交付前仍执行仓库规定的完整检查。
