# VidLens 主动视觉感知：现状审计、方案调研与效果优先路线

> 日期：2026-09-02  
> 文档性质：项目专项调研与候选设计，不是“当前已经实现”的能力说明  
> 核心问题：Agent 如何知道该看哪里、如何判断看准了没有、证据不足时如何继续调整  
> 结论范围：单视频问答中的主动视觉感知；跨视频 Research、MCP、长期记忆不是本文重点

## 0. 阅读约定

本文严格区分四类内容：

- **[项目源码事实]**：由当前代码直接支持的行为。
- **[外部来源事实]**：来自论文或官方实现的一手资料。
- **[设计建议]**：面向 VidLens 的候选方案，尚未落地。
- **[尚未验证]**：需要本项目真实数据、模型与运行环境才能确认的效果。

同目录已有材料仅作为背景，不作为执行指令：

- [项目双主线与演进路线](./project-mainlines-and-evolution-roadmap.md)
- [视频 Agent 创新功能与后端新增模块](./video-agent-and-backend-innovation-research.md)

判断当前行为时，以源码和本次测试为准；项目文档中写到的目标状态不能自动视为已经实现。

## 1. 结论先行

**[项目源码事实]** 当前 VidLens 已经具备“视觉索引”和“视觉证据路由”的骨架，但还没有真正的查询时主动视觉感知。更准确地说，当前链路是：

```text
视频处理时
  固定规则取帧
    -> OCR + 通用单帧描述
    -> JPEG 存 MinIO，文本观察存 PostgreSQL/RAG

用户提问时
  检索已有 OCR/描述文本
    -> 在已有观察中选择时间窗或帧
    -> 生成带引用答案
```

当前 `inspect_visual_window` 名字像“看画面”，实际读取的是已经持久化的视觉文本 chunk；Evidence Funnel 的“确认视觉”也只是把已有 OCR/Vision 文本转成 Evidence。它们都不会在提问时重新截取原始视频帧，也不会把像素发送给 VLM。

因此，用户关心的三个问题目前分别是：

| 问题 | 当前答案 | 应达到的目标 |
| --- | --- | --- |
| 图片怎么获取 | 视频入库时按场景变化和每 30 秒抽帧 | 根据问题先定位候选时间，再按需从原视频精确取帧、裁剪或短片 |
| Agent 怎么知道截哪里 | 依赖离线 OCR/通用 caption 和 transcript 检索 | 用问题类型、文本锚点、图文相似度、时间覆盖和相邻帧关系共同选点 |
| 怎么知道截准并调整 | 当前只能确认引用可回放，不能证明像素支持结论 | 把定位、像素观察、语义支持拆开核验；按结构化证据缺口扩窗、加密采样、裁剪、看前后帧或拒答 |

**[设计建议]** 下一阶段最值得优先做的不是再增加一个 Agent 名称，而是把“查询时按需 VLM”从远期可选项提前为 AI 主线 P0：先建真实评测基线，再落地一个受约束的 `VisualInvestigator` 纵向切片，最后才根据失败数据增加自适应采样、裁剪和独立核验。

这条路线符合“为了效果增加能力，而不是为了能力数量增加功能”：任何新增机制只有在冻结测试集上提升任务成功率或证据质量，并且成本、延迟和 unsupported claim 不越界时才保留。

## 2. 当前项目到底怎样处理视觉

### 2.1 离线图片从哪里来

**[项目源码事实]** [`ExtractKeyFrames`](../../internal/pkg/ffmpeg/keyframes.go#L22-L90) 同时执行两种采样：

1. FFmpeg scene-change 采样，默认阈值 `0.30`；
2. 固定间隔采样，默认每 `30s` 一帧。

默认最多保留 `120` 帧，宽度缩放为 `960`。两路结果先按时间排序，再删除 2 秒内近邻，最后达到上限便停止，见 [`mergeKeyFrames`](../../internal/pkg/ffmpeg/keyframes.go#L168-L195)。

**[项目源码事实]** 由于实现是“按时间升序后取前 120 个”，候选帧超过上限的长视频会优先保留前半段，不能保证全时间轴覆盖。当前测试只覆盖 scene 优先、2 秒去重和数量上限，没有覆盖“达到上限后仍均衡覆盖整段视频”，见 [`keyframes_test.go`](../../internal/pkg/ffmpeg/keyframes_test.go#L8-L45)。

### 2.2 图片拿到后做什么

**[项目源码事实]** [`VisualIndexService`](../../internal/service/visual_index.go#L112-L229) 会下载任务视频、抽取关键帧，然后对同一帧分别执行：

- 本地 OCR；
- 用户配置存在时的 Vision caption；
- 将 JPEG 上传到 MinIO；
- 将时间、对象键、OCR、caption 和状态保存到 `video_visual_frames`；
- 把 OCR 和 caption 作为不同 modality 写入 RAG chunk。

这是正确的证据分层方向：OCR 与 caption 不互相覆盖，失败状态也可以独立记录。

不过 Vision 接口目前一次只接收一张图片，默认 prompt 是“读可见文字 + 一句话描述场景”，与用户当前问题无关，见 [`VisionClient`](../../internal/ai/vision.go#L16-L26) 和 [`CaptionImage`](../../internal/ai/vision.go#L48-L101)。它适合生成可搜索的通用离线线索，不适合可靠回答“这个图表具体是多少”“动作先后是什么”一类查询。

### 2.3 提问时 Agent 有没有真的看图

**[项目源码事实]** 没有。

- [`SearchVisualEvidence`](../../internal/service/video_agent_tools.go#L183-L206) 搜索的是 `visual_ocr` / `visual_caption` 文本 chunk。
- [`InspectVisualWindow`](../../internal/service/video_agent_tools.go#L209-L263) 只按时间范围读取已有视觉 chunk，最多返回八个稳定帧引用，没有读取 `ObjectKey` 对应图片，也没有调用 VLM。
- Evidence Funnel 的视觉预算中 `MaxVisionCalls` 为 `0`，见 [`video_evidence_funnel.go`](../../internal/service/video_evidence_funnel.go#L62-L78)。
- Funnel 的 `confirmVisualCandidates` 只是将已选的 OCR/caption 内容复制为 Evidence，见 [`video_evidence_funnel.go`](../../internal/service/video_evidence_funnel.go#L543-L606)。
- 默认 Agent 和 research Agent 的 `MaxVisionCalls`、`MaxVisualCalls`、`MaxFrames` 都是 `0`，而且 [`visionAgentAction`](../../internal/service/agent_execution.go#L242-L254) 固定返回 `false`。

所以当前更准确的产品表述是：

> Agent 可以检索视频处理阶段已经生成的视觉文本观察，但不会因为某个具体问题而重新查看原始画面。

### 2.4 当前“verified”验证了什么

**[项目源码事实]** 当前 Evidence Ledger 的 `verified` 表示显式引用具有稳定来源和可回放时间范围；代码直接写明“semantic entailment was not evaluated”，见 [`buildLedgerClaim`](../../internal/service/evidence_ledger.go#L375-L410)。

因此它验证的是：

- 引用 ID 存在；
- 来源稳定；
- 时间范围可回放。

它没有验证：

- 截到的帧是否真正包含目标事实；
- OCR/caption 是否读对；
- 引用是否在语义上支持答案；
- 答案是否是客观真值。

这个边界也已在 [Agent Evidence 架构文档](../architecture/agent-evidence.md#L280-L297) 中写明，但现有状态名容易让产品侧误以为“事实已经验证”。后续建议在 UI 和 API 中把它明确命名为 `binding_verified`，另设 `semantic_support`。

### 2.5 用户现在能否看到“Agent 看了哪张图”

**[项目源码事实]** 目前前端 Citation 类型没有 modality、毫秒时间范围、source refs、对象键或 crop/frame 信息，见 [`frontend/lib/types.ts`](../../frontend/lib/types.ts#L221-L236)；[`CitationCards`](../../frontend/components/Citation.tsx#L50-L67) 也只显示文本、分数、来源和排名。

此外，普通 Agent UI 发送的模式固定为 `mode: 'agent'`，见 [`useConversationSession.ts`](../../frontend/components/chat/useConversationSession.ts#L142-L159)；后端虽然支持 `research` 和 `evidence_funnel`，但它们不是当前正常 UI 路径的可见选择，见 [`conversation_execution.go`](../../internal/service/conversation_execution.go#L86-L110)。

这意味着即使后端已经保存 JPEG，用户也无法从回答卡片直接核对实际帧、时间点和裁剪区域。没有这个反馈界面，自主感知很难形成可诊断的产品闭环。

### 2.6 当前评测能回答什么、不能回答什么

**[项目源码事实]** 当前评测主要覆盖 Recall@K、MRR、nDCG、Context Precision、Complete Evidence Recall 和 answerability 等检索指标，见 [`internal/eval/metrics.go`](../../internal/eval/metrics.go)。数据 schema 的 evidence source 只接受 `asr`、`ocr`、`both`，没有 `vision_caption`、`image`、`crop` 或 `clip`，见 [`dataset-schema.yaml`](../eval/dataset-schema.yaml#L234-L266)；run metadata 也没有 Vision 模型字段，见 [`internal/eval/runner.go`](../../internal/eval/runner.go#L18-L29)。

本次检查中，本地 `docs-private/eval/` 与 `artifacts/` 都不存在。因此：

- **通过**：现有取帧合并、视觉 chunk、视觉窗口约束、模态排序和 Funnel 约束的定向单元测试；
- **阻塞**：无法从当前工作区给出真实视觉问答正确率、关键帧命中率、VLM 成本或延迟基线；
- **不能声称**：当前视觉能力已经提升用户任务成功率。

## 3. 当前链路最影响效果的失败模式

### 3.1 离线固定采样与用户问题无关

“每 30 秒 + 场景变化”能抓 PPT 翻页，却可能漏掉：

- 两次采样之间短暂出现的数值、按钮、人物或动作；
- 场景几乎不变但局部内容变化的 UI、表格和白板；
- 需要连续帧才能判断的动作、顺序和因果；
- 只占画面很小区域的文字或对象。

### 3.2 上限策略可能丢失后半段

达到 120 帧后直接停止不是“有预算的全局覆盖”，而是时间排序后的前缀截断。长视频越长，后半段被完全遗漏的风险越大。

### 3.3 当前去重不是视觉去重

**[项目源码事实]** `VideoVisualFrame` 已声明 `Phash` 字段，见 [`video_visual_frame.go`](../../internal/model/video_visual_frame.go#L12-L39)，但除模型字段外当前代码没有计算或使用它。现在的“去重”只是丢弃 2 秒内的候选，不能识别相隔较远但内容相同的幻灯片，也可能把 2 秒内真正变化的 UI 丢掉。

### 3.4 单帧通用 caption 无法替代针对问题的观察

一个通用描述不可能预先覆盖未来所有问题。比如同一张仪表盘，用户可能问颜色、同比数值、异常点、图例对应关系或按钮状态；离线的一句话通常只会保留其中一小部分。

### 3.5 选择已有文本不等于重新看像素

当 OCR 或通用 caption 首次生成就错了，当前查询链路会检索、排序和再次引用这个错误文本，却没有机制回到原图复核。这会形成“错误观察被稳定引用”的假确定性。

### 3.6 同一个模型自报“我有信心”不能当真值证明

模型可以在证据不足时仍给出高置信答案，也可能答案碰巧正确但引用错帧。停止条件可以使用自评估，但验收必须同时检查客观 provenance、时间/区域定位和 claim-evidence 支持关系。

## 4. 外部一手研究带来的设计启发

### 4.1 Active Video Perception：主动决定看什么、何时看、看哪里

**[外部来源事实]** [Active Video Perception](https://arxiv.org/abs/2512.05774) 直接针对查询无关 caption 带来的计算浪费与时空细节模糊，提出从原始像素主动获取紧凑、问题相关证据。其运行循环是 `plan -> observe -> reflect`：Planner 提议有目标的视频交互，Observer 产出带时间戳的证据，Reflector 判断证据是否充分并决定停止或继续观察。

**[设计启发]** 这与 VidLens 当前缺口最直接对应：离线 caption 应是导航线索，查询时 Observer 才产生问题相关的像素证据；Reflector 只能触发继续观察或候选答案，最终发布仍要经过可回放与语义支持检查。

### 4.2 VideoAgent：先概览，再按缺口迭代找帧

**[外部来源事实]** [VideoAgent](https://arxiv.org/abs/2403.10517) 把长视频理解建模为 state/action/observation 循环：先用少量均匀帧获得概览，自评估证据是否充分，不足时明确缺少什么信息，再在指定 segment 内检索新帧。论文的消融显示，在其 EgoSchema 子集和具体模型配置中，移除自评估后平均帧数从 8.4 增至 11.8，准确率反而从 60.2 降至 59.6；取消 segment selection 使准确率下降 3.6 个百分点。

**[设计启发]** “更多帧”本身不是目标；每轮应回答两个结构化问题：缺什么证据、到哪里找。论文数字只说明该机制在其设置中有效，不是 VidLens 的预期指标。

### 4.3 AKS：相关性和时间覆盖必须同时优化

**[外部来源事实]** CVPR 2025 的 [Adaptive Keyframe Sampling](https://arxiv.org/abs/2502.21271) 将取帧目标写成“问题—帧相关性 + 时间覆盖”，通过递归 judge-and-split 在 TOP 与分桶覆盖之间自适应切换。论文诊断指出，单一时刻问题可能偏向高相关局部帧，而计数、多次事件等问题需要分布在多个时间段的覆盖。

**[设计启发]** VidLens 不应只有一个全局 Top-K，也不应只有均匀采样。问题类型应影响采样策略；当前 `mergeKeyFrames` 的前缀截断尤其应该先修复。

### 4.4 VideoTree：长视频需要查询自适应的粗到细表示

**[外部来源事实]** CVPR 2025 的 [VideoTree](https://openaccess.thecvf.com/content/CVPR2025/html/Wang_VideoTree_Adaptive_Tree-based_Video_Representation_for_LLM_Reasoning_on_Long_CVPR_2025_paper.html) 先利用视觉聚类构建较粗层节点，再对与查询更相关的节点向下展开细粒度内容。论文把计算预算动态分配给查询相关区域，而不是对所有时间段同等加密。

**[设计启发]** VidLens 无需第一版就实现完整树结构，但应该采用同一原则：全局便宜概览，局部昂贵观察；只有候选窗口才提高帧率、分辨率或调用 VLM。

### 4.5 DeepVideoDiscovery：把视频片段当成可探索环境

**[外部来源事实]** 微软官方 [DeepVideoDiscovery](https://github.com/microsoft/DeepVideoDiscovery) 将分段 clip 视为探索环境，由 Agent 选择多粒度工具并反复提取、总结和反思；其官方实现还说明全局浏览优先使用 clip 文本描述以节省成本，细查时再进入更贵的视觉工具。

**[设计启发]** 低成本离线索引并不是应该删掉，而应该从“最终视觉事实”降级为“导航地图”。真正的证据要在查询时从原始帧或短片重新产生。

### 4.6 LongVideoBench：少帧不是普遍真理

**[外部来源事实]** [LongVideoBench](https://arxiv.org/abs/2407.15754) 的实验中，一些长上下文模型在增加输入帧后表现提升，同时仅字幕输入明显弱于包含画面的输入。这个结果与 VideoAgent 的“无目的增加轮次可能下降”并不矛盾：关键变量是帧是否覆盖了问题需要的信息，以及模型是否能处理这些上下文。

**[设计启发]** VidLens 的目标应是“在成本约束下最大化充分且相关的证据”，而不是追求固定的最少帧数。不同问题必须画出自己的质量—帧数曲线。

### 4.7 VideoSEAL：答案正确也可能证据错位

**[外部来源事实]** 2026 年的 [VideoSEAL](https://arxiv.org/abs/2605.12571) 将“答案正确但检索/查看的证据不支持答案”称为 evidence misalignment，并分别衡量 temporal groundedness 与 semantic groundedness；它主张将长程规划与最终回答授权解耦，用像素级检查作为发布答案的门槛。

**[设计启发]** VidLens 已有 Evidence Ledger，下一步不需要为了“多 Agent”增加角色，而应增加独立权限边界：Planner 可以提出候选窗口和答案假设，但不能把自己的置信度写成 `semantic_verified`。

## 5. 目标能力：一个深而窄的 VisualInvestigator

**[设计建议]** 对外只暴露一个稳定能力，而不是把“截图、裁剪、OCR、VLM、验证”全部变成 Planner 可任意拼接的低级工具：

```go
type VisualInvestigator interface {
    Inspect(context.Context, InspectRequest) (Investigation, error)
}

type InspectRequest struct {
    Goal            string
    RequiredFacts   []RequiredFact
    SeedWindows     []TimeRange
    Budget          VisualBudget
    // UserID、TaskID、视频版本与对象位置由服务端运行上下文注入，
    // 不允许 Planner 提交任意 URL、文件路径或跨租户 task。
}

type Investigation struct {
    Observations    []VisualObservation
    ClaimBindings   []ClaimEvidenceBinding
    UnresolvedGaps  []EvidenceGap
    Status          string // sufficient | uncertain | budget_exhausted
    TraceRef        string
}
```

内部可以有 capture、dedupe、OCR、VLM、crop 和 verifier 等 seam，但 Planner 只看到业务语义：

- “检查 12:00 附近图表的同比数值”；
- “确认人物在说完这句话之后做了什么”；
- “核对画面和旁白是否冲突”。

模块内部负责安全地决定取哪些帧、如何裁剪、何时追加相邻帧，并返回不可变的证据引用。这样能同时控制权限、成本、可重放性和策略迭代。

## 6. Agent 怎么知道应该截哪里

答案不是让模型凭空输出一个时间戳，而是把定位做成逐层收缩的候选搜索。

### 6.1 先把问题转成“证据需求”

**[设计建议]** Planner 不直接生成答案，先输出有限枚举：

```json
{
  "question_type": "visible_text",
  "required_facts": ["图表标题", "同比数值", "数值所属系列"],
  "modalities": ["transcript", "ocr", "pixels"],
  "temporal_shape": "single_moment",
  "needs_ordered_frames": false
}
```

关键是 `temporal_shape`：

| 问题形态 | 首选观察策略 |
| --- | --- |
| 单一时刻的文字、颜色、对象 | 高相关候选 + 前后各一帧确认稳定性 |
| 状态变化或动作 | `before / during / after` 有序帧或短 clip |
| “第几次”“一共几次”“先后顺序” | 全时间轴分桶覆盖，再对事件峰值加密 |
| 小字、表格、代码、UI | 原分辨率帧 + ROI 裁剪 + 高分辨率 OCR/VLM |
| 画面与旁白冲突 | 对齐 transcript 时间窗，同时保留两种 modality |
| 问题没有任何时间线索 | 低成本全局浏览后再下钻，禁止直接盲扫高分辨率全片 |

### 6.2 候选时间来自多种锚点

候选窗口按成本从低到高生成：

1. transcript 语义命中及其相邻时间窗；
2. 现有 OCR/caption 命中；
3. 章节、场景边界和元数据；
4. 低成本图像 embedding 与问题的相似度；
5. 没有锚点时的全局分层/分桶覆盖。

离线层建议保存低成本“候选地图”：时间戳、缩略图、场景边界、真实 pHash 或图像 embedding、OCR 和处理版本。昂贵的通用 VLM caption 不必无差别覆盖每个候选帧；它是否保留应由消融结果决定。

### 6.3 选点同时考虑相关、覆盖和去重

**[设计建议]** 第一版可用可解释打分代替复杂强化学习：

```text
candidate_score =
    relevance_to_question
  + anchor_strength
  + temporal_coverage_gain
  + modality_diversity_gain
  - visual_redundancy
  - expected_cost
```

- 单时刻问题提高 relevance 权重；
- 多时刻问题提高 coverage 权重；
- pHash/embedding 相近的帧只保留代表帧；
- 无论如何先保留少量 timeline reservoir，避免 Top-K 全挤在一个局部。

### 6.4 查询时才物化高质量证据

对排名靠前的小窗口，从原视频按精确时间重新取帧：

- 初轮通常用 4–8 张 contact sheet 获取局部概览；
- 文字问题回到原分辨率，不复用 960px 缩略图作为最终 OCR 证据；
- 动作和顺序问题使用有序多帧或受限短 clip，不能用一张静帧冒充时序证据；
- 每张派生图保存 `video_revision + timestamp + ffmpeg args + image hash`。

这里的数字是第一版预算建议，不是效果结论；最终值由质量—成本曲线决定。

## 7. Agent 怎么判断有没有截准

“截准”至少要拆成四层，不能用一个 confidence 混在一起。

### 7.1 物理与来源正确

这些检查应由代码确定性完成：

- 帧是否成功解码；
- 时间戳是否来自当前不可变视频版本；
- crop 是否确实由指定 frame 的合法归一化 bbox 派生；
- 图像哈希、对象键、模型版本和 prompt 版本是否完整；
- owner/task scope 是否匹配；
- 引用是否能重新打开同一帧或同一 crop。

这一层通过后只能叫 `provenance_verified`，不能叫“答案正确”。

### 7.2 时间和空间定位正确

- 单点事实：决定性帧是否落在人工标注容差内；
- 时间段事实：预测窗口与决定性窗口的 IoU/覆盖率；
- 小区域事实：crop 是否命中人工标注 ROI；
- 动作事实：before/during/after 是否保持正确顺序；
- 转场附近：相邻帧是否说明当前帧不是瞬间噪声或错位画面。

### 7.3 感知结果正确

- OCR 用 CER/WER、数字/单位 exact match；
- 颜色、对象、状态、动作等用结构化字段而不是长段自由文本；
- 对关键数字至少让 OCR 与 VLM 独立读取，冲突时不自动选一个；
- 对动作、计数和顺序让 verifier 读取有序帧/clip，而不是只看 Planner 摘要。

### 7.4 证据是否支持 Claim

Verifier 的输入应是规范化 Claim 和原始 frame/crop/clip，不应只读 Planner 已写好的自然语言观察。输出至少包括：

```json
{
  "relation": "supports",
  "temporal_grounding": "supported",
  "semantic_grounding": "supported",
  "missing_facts": [],
  "conflicts": [],
  "publishable": true
}
```

允许值建议为 `supports / contradicts / insufficient`，不要强迫二选一。最困难或高风险的案例仍需人工抽检；同一个模型的二次自问只能作为弱信号，不能独立批准自己的答案。

## 8. 截得不准时如何继续调整

不要把“再试一次”写成自由文本。Planner 只提交结构化缺口，运行时映射到受控动作。

| Evidence Gap | 含义 | 允许动作 |
| --- | --- | --- |
| `location_missing` | 还没找到相关时间段 | 扩大窗口、回到上一层、换锚点、全局分桶 |
| `coverage_insufficient` | 可能漏了其他时刻 | 增加时间分桶、检查弱峰值、比较首尾 |
| `text_unreadable` | 文字过小、模糊或遮挡 | 取原分辨率、ROI 裁剪、邻帧择优、重跑 OCR |
| `spatial_ambiguity` | 不清楚哪个对象/图例对应数值 | 生成带坐标观察、扩大或拆分 crop |
| `temporal_ambiguity` | 单帧无法判断动作或顺序 | 看前后帧、提高局部帧率或读取短 clip |
| `modality_conflict` | 旁白、OCR、画面或多帧冲突 | 保留冲突双方、独立 verifier、降级措辞 |
| `semantic_unsupported` | 看到了相关画面但不足以支持 Claim | 重写较弱 Claim、寻找补充证据或拒答 |

受预算约束的循环可以非常简单：

```text
build evidence requirements
locate coarse candidate windows

while budget remains:
    capture a small batch from the best window
    observe pixels for the current question
    verify provenance, grounding and claim support
    if all required facts are supported and no blocking conflict remains:
        publish with replayable evidence
    classify unresolved evidence gaps
    choose one bounded refinement action

return uncertain / budget_exhausted with the unresolved gaps
```

停止条件必须同时包含：必要事实已覆盖、证据可回放、语义支持通过、没有阻塞冲突。模型自评“充分”只是一项输入。预算耗尽不是失败隐藏机制，而是明确返回 `uncertain` 或拒答。

## 9. 建议的数据与可观测性设计

### 9.1 Observation 只追加，不覆盖

查询时的新观察必须追加成新的 Evidence，不覆盖离线 OCR/caption：

```text
VisualObservation
  id
  task_id / video_revision
  frame_ref / crop_ref / clip_ref
  start_ms / end_ms
  parent_observation_id
  capture_policy_version
  model / prompt version
  structured_facts
  raw_response_hash
  status / error
```

这样才能比较“离线 caption 说了什么”和“针对问题重新看像素后说了什么”，也能定位到底是采样错、OCR 错、VLM 错还是回答器错。

### 9.2 每次调查保存可重放轨迹

建议记录有限状态事件，而不是保存隐藏 Chain-of-Thought：

- `route_decided`：为什么需要/不需要视觉；
- `window_selected`：候选来源、得分与覆盖增益；
- `frame_captured` / `crop_derived`；
- `visual_observed`：结构化事实与模型版本；
- `gap_classified`；
- `refinement_selected`；
- `claim_checked`；
- `stopped`：sufficient、uncertain 或 budget_exhausted。

每步记录帧数、VLM 调用数、耗时、成本、缓存命中和失败类型，之后才能回答“增加这个功能到底改善了什么”。

### 9.3 安全边界

- Planner 不能传任意 URL、磁盘路径、SQL 或跨视频 ID；
- task、owner、视频版本和对象位置由服务端运行上下文注入；
- 时间窗、总时长、帧数、crop 数、像素大小、并发、VLM 调用和费用都有硬上限；
- bbox 使用归一化坐标，服务端验证、裁剪并生成稳定 crop ID；
- 缓存 key 至少包含视频版本、时间、crop、模型和 prompt 版本；
- 任何 fail-open 都必须保留降级标记，不能让 UI 显示“已看原图”。

## 10. 效果优先的评测闭环

### 10.1 先建立 VidLens 自己的最小真实集

**[设计建议]** 第一轮先收集 30–50 个真正代表目标业务的问题，不追求 benchmark 数量。至少覆盖：

1. PPT、字幕、白板、UI 可见文字；
2. 图表、表格、数值、单位与图例对应；
3. 对象、颜色、属性、人物关系；
4. 动作、状态变化、先后顺序；
5. 多次事件、计数与跨时刻汇总；
6. 画面与旁白冲突；
7. 无法回答或画面确实看不清的负样本。

每个案例至少标注：

- `answerable`；
- 必要 answer points；
- 决定性时间窗与允许误差；
- 可接受的 frame/crop/clip；
- 每条 Claim 需要的 modality；
- 是否需要单点、连续动作或多时刻覆盖；
- 容易混淆的负窗口或反证。

开发集用于调阈值和 prompt，封存测试集只用于最终对照。模型、prompt、视频版本和预算必须冻结，否则无法归因。

### 10.2 不要只看最终答案正确率

| 阶段 | 关键指标 | 它回答的问题 |
| --- | --- | --- |
| Route | vision invoke precision/recall | 该看图时有没有看，不该看时是否浪费 |
| Locate | window recall@budget、time IoU | 候选时间窗是否覆盖决定性证据 |
| Capture | decisive-frame hit@N、ROI hit | 真正送给模型的帧/crop 是否正确 |
| Perceive | OCR CER/WER、数字 exact match、结构化事实 precision | 像素有没有读对 |
| Verify | temporal/semantic groundedness、unsupported claim rate | Claim 是否由所引证据支持 |
| Answer | answer-point coverage、task success、abstention precision/recall | 用户目标是否完成 |
| Efficiency | P50/P95、VLM calls、frames、tokens、cost/success | 改善是否值得成本 |
| Trajectory | 平均轮数、gap 类型、重试收益、预算耗尽率 | Agent 调整是否真的有效 |

### 10.3 固定消融矩阵

在相同视频、问题、模型、prompt 和预算上逐项比较：

| 版本 | 唯一新增机制 | 要验证的假设 |
| --- | --- | --- |
| A | 当前固定 scene + 30s OCR/caption | 真实基线 |
| B | A + 时间轴均衡上限 + 真 pHash 去重 | 修复覆盖偏置是否已带来提升 |
| C | B + 问题—图像 embedding 候选排序 | 纯视觉定位是否改善 |
| D | C + 查询时局部重新取帧/VLM | 主动重新看像素是否改善最终效果 |
| E | D + gap 驱动扩窗、加密、crop/短 clip | 自适应迭代是否优于一次观察 |
| F | E + 独立 semantic verifier | 引用支持率是否改善且代价可接受 |

不要同时加入 D、E、F 后把提升全部归因于“Agent”。如果 B 已解决主要问题，就不应为了技术复杂度强行保留更贵机制；如果 D 没提升，先检查定位/标注/模型适配，不直接继续堆角色。

### 10.4 上线门槛

在拿到基线前不虚构具体业务准确率阈值，但可以先固定这些不变量：

- 所有已发布 Evidence 的 provenance 可回放率必须为 100%；合法取帧失败或明确返回 `uncertain` 的运行不计入已发布证据分母；
- 跨 owner/task 取帧为 0；
- 预算越界为 0；
- 新版本的 unsupported claim rate 不得恶化；
- 冻结测试集上的任务成功率和 decisive-frame hit 必须优于 A；
- 成本与 P95 延迟必须在产品预先确定的预算内；
- 对不可回答样本不能以“多看几帧”为由强行作答。

样本较小时应报告逐题 paired wins/losses 和置信区间，不用一个平均分掩盖类别退化。

## 11. 推荐实施顺序

### P0：评测与可观测性先行

1. 扩展 eval schema：加入 `vision_caption / image / crop / clip`、决定性帧/ROI、Vision 模型和视觉策略版本。
2. 建立 30–50 个真实目标问题，产出版本化 A 基线。
3. 修复 `MaxFrames` 的前缀偏置，并实现真正 pHash/embedding 去重。
4. 打通后端 Citation 到前端的 modality、时间和 frame/crop 引用。

完成门槛：能从失败案例明确区分 route、locate、capture、perceive、verify 或 answer 哪一层出了问题。

### P1：最小查询时视觉纵向切片

1. 实现受限 `VisualInvestigator.Inspect`；
2. 只允许检查前序检索定位的当前 task 小窗口；
3. 从原视频物化少量帧，按问题生成结构化观察；
4. 保存新的 append-only Evidence 和完整 provenance；
5. 前端可打开实际帧、时间点和 crop；
6. 达到预算时明确 uncertain/abstain。

第一版先限制为 1–3 个小窗口、每轮最多 8 帧、最多 2–3 轮。窗口不应沿用当前“最长十分钟”作为实际像素检查默认值；十分钟只适合作为输入合法性上界，具体初始值由数据集和成本测量决定。

完成门槛：D 对 A 的 paired comparison 证明任务成功率或证据支持率提升，且成本与延迟可接受。

### P2：根据失败数据增加自适应能力

- route 不准：改问题类型与模态判断；
- locate 不准：加图文 embedding、层次/分桶搜索；
- capture 不准：加邻帧、动态帧率和 ROI；
- perceive 不准：换分辨率、OCR/VLM 或结构化 prompt；
- temporal 不准：引入有序帧或短 clip；
- verify 不准：增加独立 verifier 和反证搜索。

只有失败分布明确需要时才增加对应能力。

### P3：再考虑跨视频 Research、MCP、Memory 和多 Agent

这些能力可以复用成熟的 VisualInvestigator，但不应挡在单视频主动感知之前。建议调整现有大路线：

```text
可信评测基线
  -> 单视频查询时视觉闭环
  -> 证据 UI 与反馈
  -> 独立核验
  -> 跨视频 Research
  -> MCP / Memory / 其他扩展
```

后端可靠性、安全上传和 durable runtime 仍需继续，但 AI 产品功能的优先级应以目标问题成功率为中心。

## 12. 一个完整例子

用户问：

> “12:00 左右的图表显示同比增长多少？”

建议运行轨迹：

1. Planner 将其识别为 `visible_text + single_moment`，必要事实为图表标题、同比数值和所属系列。
2. transcript/OCR 将候选定位到 `11:45–12:20`，服务端确认窗口属于当前视频。
3. Investigator 在窗口内先取 6 张低成本 contact sheet。
4. VLM 只输出结构化观察：“12:07 有目标图表，但数值区域过小”，并给出归一化 ROI。
5. 服务端从 12:07 原分辨率帧生成确定性 crop，OCR 与 VLM 分别读取数值和单位。
6. 再看相邻一帧，确认不是转场残影，并检查图例—数值对应关系。
7. Verifier 直接读取 frame/crop，判断 Claim 是否由像素支持。
8. 若 OCR 与 VLM 冲突，标记 `modality_conflict` 并换邻帧/扩大 crop；预算耗尽仍冲突则回答“无法可靠确认”，展示两份证据。
9. 用户可以点开 12:07 的原帧与 crop，反馈“帧错 / 数值错 / 缺上下文 / 答案错”。

这才是“Agent 自主感知”：不是盲目截图，也不是模型说自己看懂了，而是每轮都有可观察的证据缺口、受控动作和可回放结果。

## 13. 首批建议改动集

为保证每个改动都能单独评测，建议拆成以下小切片：

1. **Eval schema + 基线报告**：只增加视觉标注、指标和运行元数据。
2. **采样覆盖修复**：只改时间轴 reservoir/分桶上限与 pHash 去重，对比 A/B。
3. **Evidence preview**：后端安全读取帧/crop，前端展示 modality、时间和真实图像。
4. **VisualInvestigator vertical slice**：局部取帧、单轮问题相关 VLM、append-only Evidence，对比 C/D。
5. **Gap-driven refinement**：扩窗、加密、邻帧、crop 和短 clip，对比 D/E。
6. **Independent verifier**：semantic/temporal groundedness 与发布门槛，对比 E/F。

每个切片都应附同一冻结集的 before/after、成本、延迟和失败分类。没有效果证据时不要继续往上叠功能。

## 14. 明确不做什么

- 不把整段长视频按高帧率全部送进 VLM；
- 不让 Planner 自由访问 URL、文件路径或任意 task；
- 不把通用离线 caption 当作所有未来问题的最终事实；
- 不把同一个模型的 confidence 当作语义核验；
- 不为了“多 Agent”拆出没有独立权限和评测价值的角色；
- 不把用户一次纠错直接当训练真值，先追加到 ledger 并人工/规则确认；
- 不引用外部 benchmark 数字作为 VidLens 自身效果；
- 不在没有真实基线时宣称“自主感知已经解决”。

## 15. 本次验证快照

本次只做了只读源码审计、外部一手资料调研和定向测试，没有修改业务代码、配置或远端状态。

执行命令：

```bash
go test ./internal/pkg/ffmpeg ./internal/service \
  -run 'Test(MergeKeyFrames|FormatVisualChunks|InspectVisualWindow|RankRetrievedModalities|VideoEvidenceFunnel|EvidenceFunnelDoesNotSelectVisualFrames)' \
  -count=1
```

结果：

```text
ok  vid-lens/internal/pkg/ffmpeg
ok  vid-lens/internal/service
```

这些结果只证明当前 plumbing、上限和约束测试通过，不能证明真实视频上的视觉问答效果。完整测试套件和外部 provider UAT 不在本次验证范围内；真实视觉效果评测因当前工作区没有 `docs-private/eval/` 与 `artifacts/` 而处于**阻塞**状态。

## 16. 一手来源索引

### 项目源码与内部设计材料

- [关键帧抽取与合并](../../internal/pkg/ffmpeg/keyframes.go)
- [视觉索引服务](../../internal/service/visual_index.go)
- [单图 Vision 客户端](../../internal/ai/vision.go)
- [视觉 Agent 工具](../../internal/service/video_agent_tools.go)
- [Evidence Funnel](../../internal/service/video_evidence_funnel.go)
- [Agent 执行预算](../../internal/service/agent_execution.go)
- [Evidence Ledger](../../internal/service/evidence_ledger.go)
- [视觉帧模型](../../internal/model/video_visual_frame.go)
- [前端 Citation 类型](../../frontend/lib/types.ts)
- [前端 Citation 展示](../../frontend/components/Citation.tsx)
- [评测 schema](../eval/dataset-schema.yaml)
- [媒体理解架构文档](../architecture/media-understanding-pipeline.md)
- [Agent Evidence 架构文档](../architecture/agent-evidence.md)

### 外部论文与官方实现

- [VideoAgent: Long-form Video Understanding with Large Language Model as Agent](https://arxiv.org/abs/2403.10517)
- [Active Video Perception: Iterative Evidence Seeking for Agentic Long Video Understanding](https://arxiv.org/abs/2512.05774)
- [Adaptive Keyframe Sampling for Long Video Understanding, CVPR 2025](https://openaccess.thecvf.com/content/CVPR2025/html/Tang_Adaptive_Keyframe_Sampling_for_Long_Video_Understanding_CVPR_2025_paper.html)
- [VideoTree: Adaptive Tree-based Video Representation for LLM Reasoning on Long Videos, CVPR 2025](https://openaccess.thecvf.com/content/CVPR2025/html/Wang_VideoTree_Adaptive_Tree-based_Video_Representation_for_LLM_Reasoning_on_Long_CVPR_2025_paper.html)
- [Microsoft DeepVideoDiscovery official repository](https://github.com/microsoft/DeepVideoDiscovery)
- [LongVideoBench](https://arxiv.org/abs/2407.15754)
- [VideoSEAL: Mitigating Evidence Misalignment in Agentic Long Video Understanding](https://arxiv.org/abs/2605.12571)

## 17. 最终决策建议

如果只做一项产品方向调整：**把查询时主动视觉闭环提前到跨视频 KB Agent 之前。**

如果只做一个代码模块：**做受约束、可评测、可回放的 `VisualInvestigator`，而不是继续扩充通用 Tool 列表。**

如果只做一个管理动作：**先冻结一批真实目标问题，以任务成功、证据命中和 unsupported claim 为主指标；之后每个功能必须通过单变量消融证明自己值得存在。**
