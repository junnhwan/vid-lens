# VidLens 260829–260831 多模态、双连续性与产品闭环审计

状态：现状审计与实施建议；不是已完成验收证明

核验时间：2026-09-02（Asia/Shanghai）

## 1. 结论先行

最近这批改动不是“没有做”，而是已经搭出了不少正确的基础设施，但还没有把用户最关心的效果闭环做完。

- **ASR 连续性：部分实现，效果未验证。** 已有 300 秒 core window、两侧各 5 秒重叠、确定性拼接、受控并发、失败片复用和阶段指标；但 ASR adapter 仍只返回一整段纯文本，拼接只支持规范化后的精确前后缀匹配。相邻窗口把同一句识别成略有差异的文字时，仍可能重复、漏字或断句。
- **RAG/引用连续性：索引侧部分改善，用户侧仍未解决。** 索引切片会优先选择句号、逗号、空白等边界，也保存了来源映射；但公开引用被再次硬裁成最多 160 个 rune，而且可能从句中开始。生产检索又关闭了邻居扩展，前端还丢弃了时间、模态和帧来源，因此用户仍可能看到残句。
- **多模态：已有离线视觉索引，不等于问答时真正看视频。** 当前会抽场景帧和 30 秒间隔帧，执行 OCR/单帧 Vision caption 并参与检索；但 `inspect_visual_window` 只读取已经保存的 OCR/caption 文本，不会按问题重新读取原始像素或视频片段。
- **记忆：后端契约较完整，正式前端没有接入。** 已有部署开关、用户默认偏好、会话 `inherit | enabled | disabled`、异步写入前二次授权、查看/撤回/删除接口；目前只有 mock 原型，没有正式会话开关与治理页面。
- **自动索引：后端已做，正式 UI 仍保留手动按钮。** 转写完成后会自动入队 RAG 构建，但聊天页、任务详情和视频详情仍让普通用户“建立/触发索引”，产品语义冲突。
- **前端：方向比旧管理台更灵动，但还未形成可验收的产品闭环。** 正式聊天已有三种问答模式，Agent Lens 与记忆页有原型；引用时间跳转、记忆控制、自动索引状态、真实浏览器 E2E 和视觉回归仍缺失。

最重要的判断是：**“连续性”不能继续在两处分别用字符串补丁解决。应先建立一份带时间戳、可回放的规范化视频证据时间线，再从同一份时间线派生 ASR 全文、RAG 检索单元、生成上下文和用户引用。**

## 2. 审计范围与证据口径

### 2.1 固定代码范围

- 基线：`014cebf07aa4c969adaf4346c93617e02d5519ad`，2026-08-03，8 月 29 日前最后一个提交。
- 目标：`933c7d93bf7f6f4eb228c8c72be659e318addcf6`，2026-08-31，`feat(memory): add user consent and session policy controls`。
- 当前 HEAD：`0cd5f5e56b836f209585c2dfa39bac601f6042ff`；目标提交之后只有 2026-09-02 的文档提交，没有代码差异。
- 范围内共 37 个提交、396 个文件，约 31,908 行新增、19,313 行删除。这个体量明显大于一个易审查的功能 PR，增加了跨模块契约未闭环的概率。
- GitHub 未检索到 2026-08-29 至 2026-08-31 的对应 PR 记录，提交信息也没有可回溯的 PR 编号。因此本文只能称为**固定提交范围审计**，不能称为“已通过 PR review”。

### 2.2 状态定义

- **已实现**：当前正式代码链路存在，并有与结论相符的自动化验证。
- **部分实现**：有基础实现，但产品链路、关键边界或验收证据仍缺失。
- **未实现**：正式链路中没有该能力；mock/prototype 不计入正式实现。
- **未验证**：代码存在，但没有当前真实视频、真实 provider 或浏览器 E2E 证据证明效果。

## 3. 六个目标的完成度

| 编号 | 目标 | 当前判断 | 已完成 | 关键缺口 |
|---|---|---|---|---|
| R1 | 从纯音频升级为多模态视频理解 | 部分实现、未验证 | 离线关键帧、OCR、Vision caption、视觉 chunk、视觉检索工具 | 查询时不读像素；长视频覆盖可能截断；前端不展示视觉来源 |
| R2 | ASR 连续性与延迟 | 部分实现、未验证 | overlap window、精确拼接、并发、重试、片级复用、指标 | 无词/句时间戳；精确匹配脆弱；有空片跨洞拼接缺陷；无真实 P50/P95/RTF |
| R3 | RAG 切片与公开引用连续 | 部分实现；用户侧未达标 | 语义边界切片、完整单元 overlap、source refs | 公开引用 160-rune 裁剪；生产邻居扩展关闭；时间范围粗；前端丢字段 |
| R4 | 记忆可选开关与管理页 | 后端已实现、正式前端未实现 | 三层授权、版本控制、治理 API、异步写前复核 | 正式 composer 无开关；无正式治理页；只有 mock |
| R5 | 问答信息架构、视觉与动效升级 | 部分实现、未验证 | 模式选择器、Agent Lens/记忆原型、正式构建通过 | 产品入口仍冲突；引用不可播放；无真实 E2E/视觉回归；原型未合入正式链路 |
| R6 | 转写后自动构建索引 | 后端已实现、UI 未闭环 | 转写成功后异步入队、任务状态与重试 | 三处正式 UI 仍有普通“触发索引”；同内容索引复用存在高风险空洞 |

## 4. 为什么两个“连续性”问题其实是同一个问题

当前链路把多个语义不同的对象压成了一个 `string`：

```text
ASR 窗口纯文本
    ↓ 字符串前后缀拼接
全量 transcript 字符串
    ↓ 再次切片
RAG chunk 字符串
    ↓ 检索后裁成 160 rune
用户引用字符串
```

每一层都丢失了一部分边界与时间信息。上游一旦在句中断开，下游只能猜；下游即使切得较好，最后的 160-rune 裁剪仍会重新制造残句。

目标链路应改为：

```text
VAD 语音区间 / 视觉 observation
    ↓
结构化 ASR words/segments + 帧时间戳
    ↓
不可变 TranscriptAtom / VisualAtom 时间线
    ├── 全文投影
    ├── 小粒度 retrieval child
    ├── 同模态、按时间扩展的 generation parent/window
    └── 精确 anchor + 完整 display context + 可播放时间码
```

需要明确分开保存四种内容：

1. **raw observation**：provider 原始输出，永不静默改写。
2. **canonical transcript**：经过时间归一与确定性装配后的规范化全文。
3. **retrieval representation**：用于 embedding/BM25 的 child，可带额外检索上下文。
4. **public evidence**：面向用户的原文 anchor 与完整上下文，不得用检索增强文本冒充原文。

## 5. ASR 连续性：现状、根因与目标方案

### 5.1 当前实现做对了什么

- `internal/pkg/ffmpeg/ffmpeg.go:15-18,125-203` 使用 300 秒 core window，并在每个 core 两侧各加 5 秒上下文；相邻普通窗口实际共享约 10 秒音频。
- `internal/transcript/stitcher.go:38-108` 忽略空白、大小写与标点，寻找 4–256 rune 的精确 suffix/prefix，保留原始文字。
- `internal/mq/consumer_transcribe.go:243-391` 使用固定 worker pool，按原 index 回填结果，支持已完成片复用、provider retry 与阶段指标。
- `internal/mq/consumer_transcribe.go:99-115` 在落库后自动进入 RAG 阶段；视觉分支也不依赖 ASR 成功才开始。

这些改动能降低“固定硬切 + 直接拼接”的明显风险，也有可能缩短耗时；但它们只证明机制存在，不证明真实视频效果已提升。

### 5.2 仍会失败的原因

1. **provider 契约只有纯文本。** `internal/ai/audio_transcription.go:81-90` 只解析 `text`，没有请求或保存 segment/word timestamps、置信度和语言信息。
2. **精确字符串重叠不是语音对齐。** “我们接下来讨论性能”与“接下来我们讨论一下性能”来自同一段音频，但当前算法匹配不到；反过来，视频里真实重复的短语也可能被误判成 overlap。
3. **没有持久化低置信度边界。** `Boundary.Method` 只区分 append 与 exact overlap，最终 transcript 也没有可供 UI/评测检查的 ambiguity。
4. **空 ASR 片存在真实装配缺陷。** `consumer_transcribe.go:336-378` 先删除空文本，再根据完整 `segments` 判断“所有片相邻重叠”。如果中间某片为空，前后两个实际不相邻的文本会被当作相邻窗口拼接；空文本又没有被标为 completed，后续重试会反复请求。这违反架构文档中“只有相邻 window 元数据证明 overlap 才拼接”的约束。
5. **时间映射过粗。** 当前 source ref 往往把一个很短的 RAG 片段映射到整个 300 秒级 ASR window，无法支持精确播放或精确引用。
6. **本地前处理仍有串行成本。** `SplitAudioWindows` 为每个窗口顺序启动一次 ffmpeg 并重新编码 MP3；provider 并发已经优化，但分片准备仍可能成为长视频瓶颈。

### 5.3 推荐的数据契约

把窄接口从 `Transcribe(...) (string, error)` 升级为结构化结果，同时保留纯文本 provider 的兼容 adapter：

```go
type TranscriptWord struct {
    Text       string
    StartMS    int64
    EndMS      int64
    Confidence *float64
}

type TranscriptSegment struct {
    Text       string
    StartMS    int64
    EndMS      int64
    Words      []TranscriptWord
}

type TranscriptionObservation struct {
    RawText       string
    Language      string
    Segments      []TranscriptSegment
    SegmentKey    string
    WindowStartMS int64
    WindowEndMS   int64
    CoreStartMS   int64
    CoreEndMS     int64
    TimingQuality string // word_exact | segment | coarse | unknown
}
```

落库后再构建不可变的 `TranscriptAtom`（优先 utterance，必要时 word），包含稳定 ID、原文、精确/粗略时间、来源 observation、装配版本和边界置信度。`video_transcriptions.content` 只是一份可重建投影，不再是唯一事实源。

### 5.4 推荐的装配算法

优先级如下：

1. **VAD-aware 切分**：先检测语音区间，尽量在静音处断开，再按 provider 文件大小/时长上限打包；重叠是上下文 padding，不是最终归属依据。
2. **timestamp + core ownership**：有 word timestamp 时，把 word midpoint 落在当前 core 的词归给该 core。overlap 只帮助模型理解，不再依赖字符串猜测删除哪一份。
3. **segment timestamp fallback**：只有 segment 时间时，优先保留完整 utterance，并对跨 core 的 segment 标记 boundary ambiguity。
4. **纯文本 fallback**：只在音频窗口确实相邻时，对 overlap 候选区做 token/字符级局部序列对齐，可使用编辑距离或 Smith–Waterman 一类局部对齐；达到冻结置信度阈值才去重。
5. **歧义时保守**：保存两侧原始 observation 与 `low_confidence` 边界，不能让 LLM 静默重写原文。面向用户可显示“此处转写边界不确定”，评测也能定位。

不要直接把“模糊匹配”替换成新的唯一真相。它只能是无 timestamp provider 的降级层；主路径应是 VAD + 结构化时间戳 + core ownership。

### 5.5 延迟优化顺序

1. 先记录同一真实数据集的 `audio_extract`、`segment_prepare`、provider request、retry wait、stitch、persist、index 各阶段 P50/P95 与实时因子 `RTF = 处理秒数 / 视频秒数`。
2. 保留当前片级缓存与有限 worker pool；按 provider/model 建全局 admission，避免单任务并发挤爆所有请求。
3. 对分片准备做单次 ffmpeg 产物或受控并行实验，比较 CPU、磁盘与总时延；未测前不声称一定更快。
4. provider 支持 segment timestamps 时先启用，因为 OpenAI 官方接口说明 segment timestamp 不增加额外延迟；word timestamp 可能增加延迟，应独立消融。
5. P2 再考虑 finalized utterance batch 的增量索引和原子 generation swap，以降低 time-to-first-searchable；先不要让用户查询半成品索引。

## 6. RAG 与公开引用连续性

### 6.1 当前实现的关键事实

- `internal/service/chunk_splitter.go:20-39,49-85` 会按强标点、从句标点、空白、最后才按 rune 硬切；overlap 尽量复用完整语义单元。
- `internal/service/rag_source_builder.go:11-61` 会从 ASR observation 重放 transcript，并将保留内容映射回 source ref；无法重放一致时会降级为 unmapped。
- 生产配置 `cmd/server/wiring.go:87-102` 明确设置 `NeighborRadius=0`、`MaxContextChars=0`，因此正式标准 RAG 并没有使用已实现的邻居扩展。
- `internal/service/rag_evidence.go:509-596` 明确不把扩展上下文放进公开 citation，并将 anchor 再裁成默认 160 rune。
- `relevantEvidenceWindows` 可从命中词两侧直接截取固定窗口，只尝试把结尾延到标点，没有保证开头位于句界。因此“公开引用是原文”成立，“公开引用是完整语义片段”不成立。
- `frontend/lib/types.ts:221-236` 与 `frontend/components/Citation.tsx:8-64` 没有接收或展示后端已有的 modality、start/end、time status 与 source refs。

所以当前最主要的问题已经不是单纯的“chunker 不够聪明”，而是**检索单元、LLM 上下文和用户引用共用/丢失了错误的字段**。

### 6.2 推荐的 child–parent–evidence 模型

- **retrieval child**：1–3 个完整 utterance/句子，保留 atom IDs 与精确时间；目标是检索精准。
- **generation parent/window**：命中 child 后，按同 task、同 modality、时间相邻关系扩展到完整段落/主题窗；目标是给模型充分语境。
- **anchor evidence**：真正命中并支持 claim 的最小原文范围；目标是审计和高亮。
- **display context**：包含 anchor 的完整句/utterance 窗口；目标是让用户读懂，可比 anchor 更长。

推荐公开 DTO：

```json
{
  "citation_id": "C1",
  "modality": "transcript",
  "anchor_quote": "真正支持回答的最小原文",
  "display_context": "包含前后完整句的可读上下文。",
  "anchor_start_ms": 123400,
  "anchor_end_ms": 128900,
  "context_start_ms": 118000,
  "context_end_ms": 136000,
  "time_range_status": "exact",
  "source_refs": [],
  "truncated": false
}
```

必须保持这些不变量：

- `anchor_quote` 与 `display_context` 都是 source 原文子串；检索时添加的说明文字不能冒充引用。
- anchor 必须包含于 display context，且 UI 高亮 anchor。
- display context 只能按 atom/utterance 边界截断；若因预算不得不硬截，必须返回 `truncated=true`，不能静默显示成完整片段。
- 时间扩展只在同 task、同 modality 内进行，优先按时间和 atom 邻接；不能再用全局 `chunk_index ± radius`。
- LLM 可以使用 generation parent，但 Evidence Ledger 仍绑定 anchor；两者不再争用一个 `Content` 字段。

### 6.3 需要修复的潜在跨模态问题

`rag_index_build.go:127-155` 先追加 transcript chunks，再追加 visual chunks，最后重新赋全局 index；`rag_expand.go:42-66` 按这个全局 index 取邻居。一旦启用 radius，最后一个 transcript chunk 可能把第一个 visual chunk 当作文本邻居，反之亦然；扩展后的 `Content` 还沿用 anchor 的公开 provenance。

生产配置目前关闭 expansion，暂时避免了线上触发，但这不是正确实现。启用 sentence window 前应先改为 modality/time-scoped adjacency，并加“绝不跨模态扩窗”的契约测试。

### 6.4 可选的检索实验，而不是直接照搬

- Sentence Window：用小句检索，命中后替换为周围完整句窗口，契合 VidLens 的 child/parent 分离。
- Contextual Retrieval：embedding/BM25 前给 child 添加简短的来源上下文，可帮助孤立片段被检索；该上下文只能存在于 retrieval representation。
- Late Chunking：先编码较长上下文，再对 token 表示池化成 chunk embedding，可作为长上下文 embedding provider 的独立候选。

三者都必须在 VidLens 固定数据集上做单变量对比。外部论文或厂商实验结果不能直接当成本项目收益。

## 7. 多模态：从“视觉文本索引”到“按问题看画面”

### 7.1 当前能力边界

`internal/service/visual_index.go:37-47,112-229` 已实现：

- scene threshold + 30 秒 interval 的离线采样；
- OCR 与 Vision caption 分开保存，避免一种结果覆盖另一种；
- 帧对象上传、稳定时间、视觉 chunk 与失败状态；
- 视觉分支可与 ASR 并行，并允许 visual-only 降级。

但当前仍是“先把少量画面变成文字，再做文本检索”。`video_agent_tools.go:209-262` 的视觉检查只查数据库 observation；这对 PPT/OCR 问题有帮助，但无法可靠回答动作、物体关系、细节变化、声画冲突等需要重新看像素/片段的问题。

### 7.2 长视频覆盖缺陷

`internal/pkg/ffmpeg/keyframes.go:168-195` 将场景帧与间隔帧按时间排序、2 秒内去重，然后达到 `MaxFrames=120` 就停止。它会优先保留视频前部，不保证全时间轴覆盖；场景切换越密集，后半段越可能没有任何帧。`VideoVisualFrame.Phash` 字段也没有参与实际视觉去重。

应改为有覆盖约束的预算分配：

1. 先按视频总时长分桶，为每个时间桶保留最少 uniform coverage。
2. 剩余预算再分配给 scene change、OCR 变化和高信息密度区间。
3. 使用 pHash/视觉 embedding 去重，而不只是“时间相差 2 秒”。
4. 保存 sampling policy/version/coverage，评测能看到哪些时间段根本没观察。

### 7.3 推荐的两阶段视觉架构

1. **离线廉价概览**：ASR、OCR、caption、场景/时间特征用于定位候选时间窗。
2. **查询时主动取证**：问题分类后，从候选时间窗物化少量原始帧或短 clip，使用针对问题的 prompt 调 VLM；若证据不足，可在固定预算内调整时间范围/采样密度再看一次。

查询时工具需要真正返回 frame/clip observation，而不是只返回旧 caption：

- 输入由服务端绑定 task、候选时间窗、最大帧数/时长，不接受任意 URL/path。
- 输出保存 exact frame timestamps、对象 key、model/prompt version、原始 observation 和失败原因。
- 回答引用可打开帧或从 `context_start_ms` 播放视频。
- 未调用像素检查时，UI 和回答不得声称“已查看画面”。

这与长视频研究中的共同方向一致：先低成本定位，再对与当前问题相关的少量 frame/token 做细看；并不意味着要把整段视频一次性喂给大模型。

## 8. 记忆：应该可选，而且后端已经为三态控制做好准备

用户提出的“对话框里可选择是否开启记忆”是正确方向，但正式 UI 不应只做一个无法解释的 boolean switch，因为后端已经有三层策略。

### 8.1 建议交互

- composer 放一个紧凑的“记忆”控制，提供：`跟随默认 | 本会话开启 | 本会话关闭`。
- 同时显示服务端返回的 effective state 和 reason；部署能力关闭或策略读取失败时，必须显示“当前不可用”，不能伪装成用户主动关闭。
- 首次开启前用一次性说明解释会保存什么、不保存什么、如何撤回/删除。
- 关闭只阻止未来召回和自动写入，不删除历史；删除必须是独立、明确操作。
- 处理 409 optimistic concurrency：重新 GET 后让用户确认，不自动覆盖另一端的新选择。

### 8.2 正式治理页

- 按 scope、kind、status、来源筛选；展示内容、来源、时间、冲突组和当前状态。
- 支持撤回、删除、查看冲突；危险动作有清晰反馈。
- 区分“默认是否使用记忆”和“已有记忆数据”；关闭前者不能让数据在界面中消失。
- prototype 可作为视觉探索，但数据层必须接 `GET/PATCH preferences`、session policy 与 memories governance API，不能继续使用本地 `useState` mock 冒充完成。

## 9. 前端信息架构与动效建议

### 9.1 推荐主工作区

桌面端围绕一个清晰的三栏/两栏自适应工作区：

- 左侧：视频/知识库与处理状态，弱化为导航，不与对话入口竞争。
- 中间：播放器、时间轴和当前问题相关的帧/字幕高亮。
- 右侧：对话、Agent 过程与可折叠证据抽屉；小屏改为 tabs/bottom sheet。

composer 保留现有“引用问答 / Agent / 助手”模式选择，并加入会话记忆三态。引用卡片显示模态、完整 display context、时间范围、置信/映射状态；点击即可跳转视频，anchor 在上下文中高亮。

### 9.2 动效原则

- 动效服务于状态变化：上传→转写→视觉索引→RAG ready、检索命中到证据出现、点击引用到时间轴定位。
- Agent Lens 可以保留环境感，但不要让装饰运动比答案与证据更抢眼。
- 当前 `frontend/app/globals.css:228-235` 的 Card/Pill 切换使用 scale 关键帧，与 `plans/002-lens-mount-only-entrance.md:33-38,95-99` 中“切换只用可中断 opacity transition，不添加 keyframes 缩放”的约束冲突；应改为淡入或共享布局运动。
- `AgentLensOverlay.tsx` 与 `AgentChatBubble.tsx` 存在重复 `CrossfadeText`，应抽为一个有 reduced-motion 支持的组件。
- 遵循 `prefers-reduced-motion`，关闭非必要动画且不影响状态可理解性。

### 9.3 自动索引的产品状态

普通用户不再看到“建立索引”。统一显示：

```text
上传中 → 转写/视觉处理中 → 索引构建中 → 可问答
                              ↘ 失败：重试
```

“重建索引”只放在失败恢复或高级设置中，并说明原因，例如 chunker/model 版本变化。现有三个普通入口应移除或改成状态/重试：

- `frontend/app/chat/[taskId]/page.tsx`
- `frontend/components/TaskDetailPanel.tsx`
- `frontend/components/library/VideoDetailModal.tsx`

### 9.4 自动索引的额外高风险点

`internal/mq/consumer_rag.go:90-96` 按 `(file_md5, embedding_model, versions)` 找到任意已有成功索引后直接返回；但关系 chunk 与 pgvector 查询均以 `user_id + task_id` 隔离，当前路径未看到为新 task 建立别名或复制投影。结果可能是新 task 被跳过构建，却无法按自身 task ID 检索旧索引。

这需要先补一个跨 task、最好再含跨 user 的端到端复现测试。正确方案不是复制所有 embedding，而是明确选择一种资产级索引模型：

- 让 index 属于 asset/content identity，task 只做 owner-scoped alias；或
- 为新 task 原子复制/重绑定关系与向量投影，并保持租户边界。

在该测试通过前，不能把“内容级索引复用”当成已经可靠的性能收益。

## 10. 推荐实施顺序

每个阶段应拆成独立、小范围改动，不再把前端、ASR、RAG、Agent runtime 和记忆治理塞进同一批 396 文件变更。

### P0：先修正确性和可测性

1. 修复 ASR 空片未完成与跨洞拼接；增加 `[非空, 空, 非空]`、真实重复短语、非相邻窗口测试。
2. 修复当前全量 Go 测试中的 Windows `ffprobe` companion path 失败。
3. 建立 10–20 个可人工标注的双连续性最小真实视频集，并冻结 provider/model/参数、gold transcript、句界与时间范围。
4. 把公开 citation 拆成 anchor/display context；前端接完整 provenance 和时间跳转。
5. 补跨 task 索引复用 E2E，确认新 task 确实可检索。

### P1：建立统一时间线

1. 引入结构化 ASR observation 与 provider adapter 能力探测。
2. 增加 VAD-aware segmentation、word/segment timestamp 归一和 core ownership。
3. 落地 `TranscriptAtom`/时间线投影与版本化重建。
4. RAG 改为 utterance child + 同模态时间 parent/window；消除全局 chunk index 邻接。
5. 自动索引 UI 完成状态闭环，移除普通手动入口。

### P2：完成产品与多模态闭环

1. 正式 composer 接会话记忆三态，设置页接用户默认，治理页接真实 API。
2. 长视频关键帧改为全时间轴覆盖预算与视觉去重。
3. 新增受限的 query-time frame/clip materialization + VLM 工具。
4. 正式工作区整合播放器、时间轴、对话、证据与 Agent Lens；补 Playwright E2E、截图回归、键盘与 reduced-motion 验收。

### P3：在证据支持后再优化

- 对 Contextual Retrieval、Late Chunking、hybrid/BM25、增量索引分别做单变量实验。
- 根据真实瓶颈再决定本地 ffmpeg 并行、provider batching、模型替换或缓存策略。
- 多视频/知识库 Agent 建立在单视频时间线与 citation 契约稳定之后。

## 11. 评测与验收设计

### 11.1 数据集分组

至少覆盖：

- 句子恰好跨 ASR 边界；边界前后 0.5/2/5 秒；
- 长静音、快速讲话、中英混合、专有名词、同一句真实重复；
- 无标点长句、快速字幕、静态 PPT、密集切页；
- audio-only、visual-only、audio+visual、声画冲突四组问题；
- 短、中、长视频，以及超过 120 帧预算的长视频。

所有候选必须使用同一视频、provider/model、温度、并发上限和网络环境；报告保存 commit、配置、数据集版本和产物 hash。

### 11.2 ASR 指标

- 全文 WER/CER；
- boundary duplication rate、boundary deletion rate；
- sentence/utterance completeness；
- word/utterance timestamp MAE 与 P95 drift；
- segment prepare、provider、总转写 P50/P95；
- RTF、失败率、重试率、缓存复用率和单位视频分钟成本。

### 11.3 RAG 与引用指标

- Recall@K、MRR、nDCG、complete evidence recall；
- moment recall@K@IoU（检索时间窗与 gold 时间窗重叠）；
- anchor 原文一致率与 source ref 可回放率；
- display context 完整句率、未标记硬截断率；
- citation time/modality/source accuracy；
- 回答正确率与 evidence grounding 分开评分。

### 11.4 多模态指标

- visual-only/audio-only/both/conflict 分组准确率；
- 时间覆盖率、关键帧命中率、冗余帧率；
- query-time visual call 数、帧数、时延和成本；
- 答案正确但证据帧错误的比例；
- 未调用视觉工具时的虚假“看过画面”率必须为 0。

### 11.5 首轮发布门槛

数值阈值应在首轮人工标注基线后冻结，不能事后调整。可先固定以下不依赖业务分布的不变量：

- 公开 anchor 100% 可在原始 observation 中回放；
- 未标 `truncated` 的 display context 100% 从完整 atom/句界开始并结束；
- 所有精确时间范围满足 `0 <= start < end <= video duration`；
- 相邻扩窗 0 次跨 task/跨 modality；
- ASR 空片不会被留在 running，也不会导致跨洞 overlap；
- 候选方案的 WER/CER 不劣于基线，连续性指标有改善后才上线；
- 延迟收益只能用真实 P50/P95/RTF 报告，不以 worker 数推算。

## 12. 本次验证结果

| 检查 | 结果 | 能证明什么 | 不能证明什么 |
|---|---|---|---|
| `go test ./...` | **失败** | 暴露跨平台 ffprobe companion path 回归 | 不能称 Go 全量通过 |
| 单测 `TestCompanionFFprobePathUsesFFmpegDirectoryAndExtension` | **失败**：Windows 期望路径，macOS 实际得到 `ffprobe.exe` | 失败可稳定复现 | 与真实 ASR 质量无关 |
| `go test -count=1 ./internal/transcript ./internal/service ./internal/mq` | **通过** | 相关单元/服务/MQ 测试通过 | 没有真实 provider/视频质量证据 |
| `go test -race -count=1 ./internal/transcript ./internal/service ./internal/mq` | **通过** | 相关范围未触发 Go race detector | 不证明分布式/数据库生产竞争不存在 |
| `go vet ./...` | **通过** | Go 静态检查通过 | 不证明业务正确性 |
| `npm test` | **通过**，8/8 | 当前 lib/chat 单测通过 | 覆盖面很小，无浏览器 E2E |
| `npm run typecheck` | **通过** | TypeScript 类型检查通过 | 前端类型本身还缺 provenance 字段 |
| `npm run lint` | **通过** | 当前 lint 无告警 | 不代表 UX/可访问性合格 |
| `npm run build` | **通过** | 正式前端可生产构建 | 后端未启动，未验证真实聊天 |
| `npm run build:prototype` | **通过** | prototype 可构建 | prototype 不等于正式功能 |
| 本地浏览器快照 | **部分通过** | 正式模式选择器、Agent Lens 与记忆 mock 可打开 | 后端 8080 未运行；无真实数据/E2E/视觉回归 |
| `git diff --check baseline..target` | **失败** | `plans/README.md:5` 有 trailing whitespace | 不影响核心效果，但说明范围未完全收口 |
| `npm audit --omit=dev` | **失败**：2 high、1 critical package | 直接依赖 Next 14.2.5 及间接依赖需升级后复测 | 不能仅按数量判断实际可利用性 |

仓库公开目录只有评测规范、示例/fixture 和测试代码；`docs/eval/README.md:16-18` 说明真实数据与结果在被忽略的本地目录，本次工作区没有可核验的真实 ASR 连续率、P95、多模态回答或浏览器验收报告。因此本文将这些项目统一标为**未验证**，不猜测结果。

## 13. Code review：Standards 轴

**总严重度：P1。** 主要原因是存在可破坏 transcript/source continuity 的数据正确性问题，以及后端 provenance 在正式前端被系统性丢弃。

1. **P1：空片过滤后可能跨洞拼接。** `consumer_transcribe.go:336-378` 删除空片后，把 compact 列表交给 stitcher，却用原始完整 segment 列表判断 overlap；前后非相邻文本可能被误去重，空片也未完成落库。
2. **P1：按全局 chunk index 扩邻可跨模态。** `rag_index_build.go:127-155` 将 transcript/visual 共序，`rag_pipeline.go:196-203` 与 `rag_expand.go` 按 index window 扩展；这违反架构要求的 same-modality/time adjacency。生产 radius=0 暂时避免触发，不消除缺陷。
3. **P1：正式引用组件丢弃来源事实。** `Citation.tsx:8-37,57-64` 只保留文本、score、rank 等字段，后端的 modality/time/source refs 无法到达用户，违反现有媒体与检索架构文档的公开 citation 契约。
4. **P2：视图切换动效违反仓库计划约束。** `frontend/app/globals.css:228-235` 使用 scale keyframes，而 `plans/002-lens-mount-only-entrance.md:33-38,95-99` 要求 Card/Pill 切换使用可中断的 opacity transition，且不得添加 keyframes 缩放。
5. **P3：重复实现。** `AgentLensOverlay.tsx` 与 `AgentChatBubble.tsx` 分别定义相近的 `CrossfadeText`；抽公共组件才能一致处理时序、取消与 reduced motion。

## 14. Code review：Spec 轴

**总严重度：P1。** 六项目标均有相关改动，但 R1、R3、R4、R6 没有形成用户可用的完整链路，因此不能判定需求完成。

1. **P1 R4 缺失。** `docs/architecture/agent-memory.md:77-81` 明确写“本次只实现后端”；正式前端没有记忆开关/治理页，`prototype/memory` 只是 mock。
2. **P1 R3 未达用户目标。** LLM 内部上下文与公开 citation 被刻意分开，公开引用仍固定裁成 160 rune；现有测试还固化了该上限，无法保证用户看到完整片段。
3. **P1 R1 只完成离线文本化视觉。** 架构文档明确查询时在线 VLM 读原始像素不在当前范围；前端也没有把视觉时间/帧证据展示给用户。
4. **P1 R6 产品集成冲突。** 后端已自动入队，正式 UI 仍要求普通用户触发索引。
5. **P2 R2 只有启发式改进。** 精确 overlap stitcher、并发和指标存在，但没有 timestamp 主路径或真实连续性/延迟报告。
6. **P2 R5 尚未验收。** 代码与原型有明显改造，测试脚本只覆盖 8 个 lib/chat 测试；没有带后端的浏览器流程、响应式、可访问性和截图回归证据。
7. **P2 范围扩散。** 同一固定范围还引入大量 Agent evidence ledger、claim correction、持久化 run/step/recovery 和旧 Vue 删除；它们并非 R1–R6 的必要最小改动，显著扩大审查面。

## 15. 一手来源与适用边界

### ASR

- [OpenAI 转写 API](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create)：结构化输出可请求 segment/word timestamp；segment 与 word 的延迟代价不同。适合定义 adapter capability，不代表当前兼容 provider 都支持。
- [OpenAI Whisper `transcribe.py`](https://github.com/openai/whisper/blob/main/whisper/transcribe.py)：官方实现暴露 `word_timestamps` 与 `condition_on_previous_text`；前文条件可提高窗口一致性，也有 repetition/timestamp 失步权衡。
- [WhisperX](https://arxiv.org/abs/2303.00747)：VAD cut/merge 与 forced alignment 用于长音频 word timestamp；论文报告的质量/速度结果只属于其实验环境。
- [faster-whisper](https://github.com/SYSTRAN/faster-whisper/blob/master/faster_whisper/transcribe.py)：提供 word timestamp、VAD 与 batch 的可参考实现。是否替换 provider 应由 VidLens 同数据集实验决定。

### RAG

- [LlamaIndex Sentence Window 实现](https://github.com/run-llama/llama_index/blob/main/llama-index-core/llama_index/core/node_parser/text/sentence_window.py)：小句检索、周围句窗口供生成，直接支持 child/parent 分离思路。
- [Anthropic Contextual Retrieval](https://www.anthropic.com/engineering/contextual-retrieval)：embedding/BM25 前给 chunk 增加简短上下文；该上下文应与公开原文隔离。
- [Late Chunking](https://arxiv.org/abs/2409.04701)：先获得长上下文 token 表示再池化 chunk，适合作为独立检索实验，不是默认答案。

### 长视频与多模态

- [Video-RAG](https://arxiv.org/abs/2411.13093)：联合 ASR、OCR、对象等视觉对齐辅助信息与视频帧，说明“文本化视觉 + 原始视觉”应互补。
- [APVR](https://arxiv.org/abs/2506.04953)：长视频采用分层、查询自适应的 pivot frame/token 选择，支持“先定位、再细看”的方向。
- [Moment-DETR / QVHighlights](https://github.com/jayleicn/moment_detr)：可参考时间片段检索与标注结构。
- [Video-MME](https://arxiv.org/abs/2405.21075)：可参考长短视频与模态能力分组；VidLens 仍需自己的真实任务集。

### UI 与记忆控制

- [OpenAI Memory FAQ](https://help.openai.com/en/articles/8590148-memory-faq/)：可参考“使用偏好”和“管理/删除记忆”分开的用户心智，不作为 VidLens 后端契约来源。
- [W3C WCAG：Animation from Interactions](https://www.w3.org/WAI/WCAG21/Understanding/animation-from-interactions.html)：非必要交互动效应允许关闭，正式前端应支持 reduced motion。

## 16. 最终决策

下一步不应该继续单独调 `overlap=多少秒` 或 `chunk_size=多少字符`。优先完成两个纵向切片：

1. **Transcript Timeline**：VAD/结构化 timestamp → core ownership → immutable atoms → 可量化 seam 质量。
2. **Playable Evidence**：child retrieval → same-modality temporal parent → anchor/display context → 前端时间跳转。

这两项完成后，多模态 Agent、记忆开关、自动索引状态和灵动 UI 才有稳定的数据接口可依赖。当前代码可以作为基础，但截至本次核验，最准确的表述是：**机制已部分落地，真实连续性、多模态理解与最终用户体验尚未完成验收。**
