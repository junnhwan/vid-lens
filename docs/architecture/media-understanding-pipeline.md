# 视频理解管线改造计划

本文描述 VidLens 从“ASR 文本问答”演进为“带可信时间轴的多模态视频理解”的当前基线、目标模块、实施顺序和验收口径。它只讨论工程实现，不把模型输出本身当作事实。

状态：设计已确认，按可回滚切片逐步实施。基线提交为 `063efff`（2026-08-30）。重叠 ASR 时间窗、稳定 segment identity、毫秒级 window/core 元数据和确定性 transcript stitcher 已实现；RAG source mapping 与视觉分支解耦尚未实施。

## 结论

项目已经具备多模态雏形：场景帧与定时帧抽取、Vision/OCR、`video_visual_frames`、视觉文本入 RAG，以及 evidence funnel 中的视觉证据确认。当前主要问题不是“缺少一个 Vision 接口”，而是 ASR、视觉和 RAG 尚未汇合成同一条可信时间轴：

- ASR 以固定 300 秒、无重叠的音频文件串行调用，结果用空行直接拼接；上游硬切会永久破坏句子边界。
- RAG 的递归句子切片器能保护已有标点边界，却无法恢复 ASR 已截断的语义；ASR 分片间的空行还会被当作强边界。
- `video_chunks` 只保存内容和序号，没有模态、时间范围和来源映射。视觉文本虽然能被嵌入，但进入检索后会退化为带标签的普通文本。
- Evidence Ledger 当前可能用 RAG `chunk_index` 解析同序号的 ASR 分片。两个序号属于不同空间，不能作为稳定映射。
- 视觉索引在 ASR 完成后同步执行、再次下载视频并逐帧调用 Vision/OCR，增加关键路径延迟；`Phash` 字段尚未参与实际去重。

改造的首要目标不是增加更多 Agent 工具，而是建立一个深模块：调用方只提交视频任务，模块产出带时间范围、模态和 provenance 的证据块。ASR 边界修复、视觉采样、语义切片和索引投影都隐藏在该模块内部，RAG、Agent、引用和前端共同消费同一接口。

## 当前实现基线

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

当前耗时包含本地转码、固定切片、N 次串行外部 ASR、同步视觉处理和第二次视频下载。优化顺序应先减少不必要等待，再增加并发：

- 记录 `audio_extract_ms`、`segment_prepare_ms`、每片 `asr_provider_ms`、`stitch_ms`、`visual_index_ms` 和端到端耗时。
- ASR 分片使用有界并发，默认并发度必须受 provider admission、用户配额和 MQ worker 容量共同约束。
- 已完成分片继续复用；并发失败只重试缺失片。
- ASR 与视觉成为独立可恢复分支，复用同一下载资产；RAG 文本索引不必等待高成本 Vision 全量 caption。
- OCR 可先离线覆盖，Vision 优先用于 RAG 命中的有限时间窗，而不是默认逐帧调用。

增加并发前必须先有延迟直方图和 provider 429/5xx 指标；否则吞吐提升可能只是把等待转移到限流与重试。

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

## 数据契约

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

## 发布、回滚与迁移

- 新策略必须带版本，索引行记录构建策略；回滚只切换新任务策略，不删除旧源数据。
- schema 先做可空/有默认值扩展，再写新字段，最后 backfill；不能要求一次部署同时重建全部历史视频。
- 新旧索引并存期间，以任务的 RAG index manifest 决定读取哪一版，不按“最新行看起来存在”猜测。
- backfill 使用现有 `rag-reindex` 的 checkpoint 思路，限制用户、task、模型和批量大小。
- 任何 stitcher 失败都保留原始分片，允许用旧拼接方式重建；不能只保存被合并后的不可逆文本。

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
