# VidLens 视频摘要体验改造：实现与验收指南

日期：2026-10-09\
状态：待实施。所有标为“拟新增/拟扩展”的表、字段、接口、状态和文件都尚未因本文而实现。\
本次修订：动态活动短标题、真实生命周期、回答增量与活动并行、摘要生成/修订接入缺口及有序恢复契约；统一桌面 Web 原型与公开交接路径。\
产品范围与决策：[产品规划](2026-10-09-summary-product-plan.md)。\
前端原型与交互验收：[原型交接要求](../design/2026-10-09-frontend-prototype-brief.md)。该文件用于独立模拟原型的交互评审，生产接入仍按本文工作包实施。\
仓库：VidLens 仓库根目录；核查基线：`833c570211a5239c92539d5884bf2ff771184fb4`。

## 1. 执行约定与 M0

按 `M0 → S0 → S1 → S2 → S3 / S4 / S5A → S6` 执行。第一批 S0–S2，第二批 S3，第三批 S4/S5A/S6；S3、S4、S5A 在契约稳定后可并行。S5B 是后续知识库增强，单独安排，不作为摘要改造第一轮的完成条件。每个工作包都有退出条件。

新会话首先：

1. 读取产品规划与本文，核对主线是视频摘要、分类与继续学习。B 站是主要输入，字幕优先属于来源策略；本地上传继续 ASR，YouTube 扩展不进入本轮。
2. 读取实际适用的 `AGENTS.md`/技能和当前源码。本次核查未找到 AGENTS.md，未来可能新增。
3. 运行 `git status --short`、`git rev-parse HEAD`，核对 WIP，保存修改前摘要；已有的无关修改不能覆盖、回退或混入本轮交付。
4. 核对数据库/MQ、API 前缀、前端路由、依赖、测试环境和实际 AI 能力。当前 MQ 是 RabbitMQ；遗留字段名或注释出现 Kafka 不代表使用 Kafka。
5. 将当前代码与本文对照。有漂移则更新对应设计和测试范围；已实现项核验后复用，避免再造相同服务。
6. 选定本次交付批次，列出文件、契约、测试与外部验证边界，再实施。普通可逆实现不需要重复征求确认。

本组公开文件包含规划、实现指南与独立模拟原型。业务前后端与数据库实现仍按后续工作包执行，原型中的活动不作为业务实施完成证据。

### 1.1 当前代码入口

| 领域 | 已有入口 | 关键事实 |
|---|---|---|
| URL/下载 | [remoteurl](../../internal/pkg/remoteurl/policy.go)、[yt-dlp](../../internal/pkg/ytdlp/ytdlp.go)、[URL 上传](../../internal/service/media_url_upload.go)、[下载消费者](../../internal/mq/consumer_download.go) | `p` 被清除；开始时 FileMD5 是 URL 临时键；下载后未自动摘要 |
| ASR/来源 | [转写流程](../../internal/mq/transcription_workflow.go)、[ASR 组装](../../internal/transcript/assembly.go)、[来源读取](../../internal/service/task_transcript_source.go)、[来源片段](../../internal/service/transcript_spans.go) | 已有分片与时间来源；字幕不能伪装 ASR 窗口 |
| 摘要调度 | [请求服务](../../internal/service/media_tasks.go)、[summary 作业](../../internal/repository/summary_job.go)、[summary 消费](../../internal/mq/consumer_summary.go)、[恢复调度](../../internal/mq/retry.go) | 已有独立摘要 lease、输入快照与补投 |
| 摘要生成 | [摘要消费者逻辑](../../internal/mq/consumer_analyze.go)、[长摘要](../../internal/mq/summary_pipeline.go)、[提示词](../../internal/ai/summary_preference.go) | 按实际模型上下文预算选择单次/分段；检查点可复用 |
| 文档/修订 | [生成摘要模型](../../internal/model/summary.go)、[修订模型](../../internal/model/summary_revision.go)、[修订仓储](../../internal/repository/summary_revision.go)、[修订服务](../../internal/service/summary_edit.go)、[补丁](../../internal/service/summary_patch.go) | 原稿与用户版本分离；保留预览/应用/撤销和术语规则 |
| 视觉/运行 | [视觉调查](../../internal/service/visual_investigator.go)、[观察记录](../../internal/repository/video_visual_observation.go)、[Agent 模型](../../internal/model/agent_execution.go)、[执行 journal](../../internal/service/agent_execution_journal.go) | 观察截图与 VideoVisualFrame 不同；EnsureRun 当前要求 ChatSession |
| 路由/装配 | [HTTP 路由](../../cmd/server/router.go)、[服务装配](../../cmd/server/wiring.go)、[模型迁移](../../internal/model/model.go) | `/api/v1`；通过现有依赖装配接入，不另起应用 |
| 前端 | [React Router](../../frontend/app/router.tsx)、[API](../../frontend/lib/api.ts)、[导入](../../frontend/components/UploadModal.tsx)、[视频库](../../frontend/app/(main)/library/page.tsx)、[详情](../../frontend/app/(main)/video/[id]/page.tsx) | React Router + Vite，目录名不是 Next.js；摘要目前在弹窗 |
| 摘要 UI/导图 | [SummaryRevisionPanel](../../frontend/components/summary/SummaryRevisionPanel.tsx)、[StudyMap](../../frontend/components/artifacts/StudyMap.tsx)、[导图转换](../../frontend/lib/artifacts/view.ts)、[领域标签解析](../../frontend/lib/markdown.ts) | 学习笔记不是摘要；领域标签尚不是数据库实体 |
| 成果/问答 | [成果 schema](../../frontend/lib/artifacts/schema.ts)、[成果工作区](../../frontend/components/artifacts/ArtifactWorkspace.tsx)、[ChatWorkspace](../../frontend/components/chat/ChatWorkspace.tsx)、[Chat handler](../../internal/handler/chat.go) | 成果 schema 限定 study；已有笔记整块提问会拼入 textarea，尚无摘要选段附件 |
| 实时问答过程 | [Planner](../../internal/service/video_agent_loop_planner.go)、[公开进度](../../internal/service/conversation_progress.go)、[事件类型](../../frontend/lib/conversationStream.ts)、[trace 归并](../../frontend/components/chat/traceTypes.ts)、[ThinkingProcess](../../frontend/components/chat/ThinkingProcess.tsx) | public_summary 已作为 detail；label 多为工具/阶段固定文案。拟增 public_title 并复用现有 progress、step 与工具事件；不另建聊天执行链 |
| 集合检索 | [Chat 准备](../../internal/service/chat_prepare.go)、[检索管线](../../internal/service/rag_pipeline.go)、[集合召回](../../internal/service/collection_retrieval.go)、[Chat 保存](../../internal/repository/chat.go) | 已有 hybrid；少量全文/大量 RAG 自动择路尚未实现，标签范围要贯穿保存校验 |

M0 退出：现状核对、WIP 保护、交付范围、样本清单、API/模型变更表完成；没有把未运行的检查标通过。

## 2. S0：统一文字来源与摘要文档

### 2.1 数据边界

使用现有 PostgreSQL/GORM、RabbitMQ、Redis、MinIO。复用媒体资产、任务作业、用户版本和运行记录；不新增通用工作流平台、第二套笔记文档或新的向量数据库。

为新来源新增一个小型 `textsource` 模块：接收字幕或 ASR 观察、校验与归一、建立不可变快照、返回统一文字及时间引用。来源解析和 ASR 重试/计费分别负责，不混入模型提示词。

**拟新增**数据模型（Go 类型名可沿仓库惯例调整，语义与唯一约束固定）：

| 模型 | 必需字段/约束 | 责任 |
|---|---|---|
| `VideoTextSource` | UUID、user_id、task_id、kind、identity_json、media_fingerprint、language、track_key、subtitle_kind/kind_basis、raw_object_key/raw_hash、canonical_text/canonical_hash、source_digest、parser_version、quality/warnings、created_at | 不可变来源快照；来源属于用户任务，不按 URL MD5 共享 |
| `VideoTextCue` | source_id + cue_id 唯一；order、raw_text、text、start_ms/end_ms 可空、timing_method、raw_refs | 原始与清洗文字、真实声明时间及归并映射；独立表便于分页与大小限制 |
| `SummaryGenerationSnapshot`（逻辑契约） | generation_id、owner/task、source_id/digest、profile/policy/preference/budget、用户要求/表达偏好、recipe_version、base_generated_hash、run_id | 默认落现有 TaskJob 的冻结输入/新增 JSON 字段及 AgentRun；不是强制新增工作流表 |

`VideoTask` 拟加 `active_text_source_id` 与冻结的导入处理意图。`VideoTranscription` 拟加 source_id/kind/hash，保留现有全文响应投影。`TaskJob` 拟加 generation_id/input_source_id 与冻结的生成输入，现有 InputText/InputChunksJSON 兼容旧消费；新 worker 从已冻结统一来源读取。优先复用现有输入快照、幂等与运行记录；只有核实当前记录无法覆盖必需的请求重放/历史后，才补小型不可变请求记录，不再存第二套 stage/status/lease。

ASR 的 `VideoTranscriptionChunk` 继续表示实际 ASR 音频窗口、重试和对齐。字幕不生成假的 audio_object、ASR 模型、forced_alignment 或 usage。ASR 快照只适配真实时间映射；旧全文无时间时用 null/unknown。

所有快照有大小、cue 数量、文本长度和解析时间上限。原始字幕文件由服务端保存并受任务权限控制；日志与普通 API 不返回 signed URL、cookies 或原始供应商敏感响应。

`source_digest` 不是 canonical_text 的 hash：对身份（含 cid/part/媒体修订）、选中轨道/语言/类型依据、parser 版本、规范文字及有序 cue 的文字/时间/归并映射做规范序列化后 SHA-256。排除新 UUID、抓取时间、object key 和临时 URL。相同文字但时间或来源身份变化必须得到不同 digest；相同来源重抓不因新快照 ID 变成“内容更新”。canonical_hash 可只表示文字，用于诊断，不能代替 source_digest。

### 2.2 统一读取与兼容

新增来源读取适配层，输出 `TextSourceSnapshot + TextObservation[]`：字幕 cue、ASR 真实段/窗口、legacy 全文均有显式来源和时间方法。先看任务 active source；旧任务才回退已有转写逻辑。按媒体 MD5 查到的旧结果不能被视为当前用户新字幕的授权或最新内容。

以下调用方逐项迁移并加回归：摘要输入/summarySourceChunks、时间线、transcript_spans、学习笔记来源冻结、RAG source refs、详情文字读取。字幕路径不依赖 forced alignment；修改旧 ASR 路径时保留原拼接、重叠和来源映射行为。

active source 切换时标记旧索引过期，但不等待重新索引才摘要。来源更新不静默改写已冻结的生成运行和引用；旧版本保留原来源身份。

### 2.3 规范化文档契约（拟新增 v2）

`SummaryDocument` 是新生成/修订结果的权威内容；Markdown 是同事务派生的兼容投影。目录和导图由 blocks 派生，图片由 references 解析，不能另存一份互不相关的导图正文。

结构会接近已有成果，但第一版保留 `/summary`、AISummary 与 SummaryRevision，复用成果的块阅读、导图、选择、修订预览与运行组件。当前 Go/Zod 成果类型限定 study，不能直接把摘要写入 study API。抽取通用组件和适配器即可，不复制正文到 ArtifactVersion、不新增第二个 head，也不要求先重构整个成果平台。统一列表是可选的只读聚合展示。

以下是有效 JSON 形状示例，ID、hash、参数内容均为示意数据，不是实际视频或已审核事实：

```json
{
  "schema_version": "summary-v2",
  "document_id": "example-document-id",
  "source_id": "example-source-id",
  "source_digest": "example-source-digest",
  "media_revision": "example-media-fingerprint",
  "presentation_mode": "image_text",
  "title": "连接池配置说明",
  "overview": "视频解释连接池配置及其适用条件。",
  "blocks": [
    {
      "id": "block-pool-config",
      "parent_id": null,
      "order": 1,
      "title": "配置与限制",
      "body_markdown": "画面中的最大连接数设置为 20；适用条件需要结合本章说明。",
      "source_refs": [
        {
          "source_id": "example-source-id",
          "cue_ids": ["cue-012", "cue-013"],
          "start_ms": 120000,
          "end_ms": 135000,
          "timing_method": "subtitle_cue"
        }
      ],
      "figures": [
        {
          "id": "figure-pool-config",
          "screenshot_ref": "summary-screenshot-example",
          "capture_ms": 127400,
          "caption": "视频中的连接池配置界面。",
          "alt": "配置界面显示最大连接数为 20。",
          "supports": "配置值及所处设置界面"
        }
      ]
    }
  ]
}
```

契约规则：

- document_id 标识该内容谱系；修订版本由外层 head/version 表示。整篇重新生成可开启新谱系；同一谱系中的改名、移动、补图和修订保留 block ID。
- 用户请求的 output_mode=auto/text/image_text/keyframes，最终文档 presentation_mode 为 text/image_text/keyframes；mindmap_enabled 单独控制视图。auto 由 Agent 按内容/要求选择，选择依据和降级原因在运行投影中可见。
- text/image_text 至少有可读概览或正文；keyframes 允许 overview/body 很少或为空，但至少有一张实际检查过的有效图，必须保留图注/alt/真实时间。纯文字不强制图片；导图组织概念与层级，概念可以作为子 block，与正文共享 ID/来源。
- 最终 image_text 至少有一张有效图；补图中的可读版本 presentation_mode/resolved_mode=text，选图成功才变 image_text。零图结束则 text + fallback_reason，不将纯文字声称为完成的图文结果。
- 同一已发布版本具备哪些内容，就能提供哪些阅读视图；切换文字/图文/关键帧浏览、展开导图不调用模型。output_mode 表示生成时要求，mindmap_enabled 是初始展示偏好；需要新增内容时才明确启动生成或修订。
- 所有 block/figure ID 唯一且长度受限；parent 引用存在、无环；order 明确，服务端排序稳定。
- source_refs 只能指向冻结来源中真实存在的 cue；起止时间来自 cue 或明确时间映射，不按摘要字符位置线性猜测。
- 缺时间用 null，timing_method=unknown；不得把 subtitle_cue 当作声学逐词精确时间。capture_ms 来自工具返回的实际画面位置。
- screenshot_ref 是服务端登记的、不透明的授权资源引用；没有 URL、object key、任意本地路径或模型自造 frame_id。
- 新配图必须绑定 source_id/media_revision/block_id/generation_id。source digest 变化需要新结果；旧图不得挂到新视频段。
- Markdown 正文限制危险链接/HTML，继续沿用现有安全渲染；图片只走受控组件，正文不能注入任意外部资源。
- 校验失败不发布半份文档；有界修复仍失败时记录原因，保留旧结果。完成 S2 的 v2 主路径必须产出合规文档；兼容降级只能标 legacy 格式。

### 2.4 存储、hash、修订与导出

拟扩展 `AISummary` 与 `SummaryRevision`：DocumentJSON 可空、SchemaVersion、SourceID/SourceDigest、ContentDigest/ContentHashKind；用户版本另有 BaseGeneratedHashKind，生成原稿另有单调 GeneratedVersion。DocumentJSON 非空时服务端从它生成 Content，二者同事务提交；不能让前端分别保存图文和 Markdown。

旧行保持 DocumentJSON=null、ContentHashKind=markdown-v1，旧 Markdown hash 语义不变。新 `summary-v2` 使用固定排序后的规范 JSON（包含正文、引用和图注，排除临时访问 URL 与外层时间戳），ContentHashKind=summary-json-v2。比较的单位始终是 `(kind, hash)`；不同 kind 视为内容/原稿发生变化，不比较裸 hash。

用户版本的内容格式与它所接受的生成基准是两回事：ContentHashKind、BaseGeneratedHashKind 分开存；API 的 CurrentGeneratedHash 也带 CurrentGeneratedHashKind。SummaryEditOperation 同样冻结 base_content_hash_kind 与 base_generated_hash_kind。legacy 人工版本遇到新 v2 原稿时进入 needs_merge：keep_revision 保留原 Markdown、document=null 与内容 hash，但更新接受的生成基准 kind/hash；use_generated 同时采用新 v2 文档、Markdown 投影和新基准。编辑 planner 依据当前有效内容格式选择，不能因为基准变成 v2 就把 legacy 内容当成结构化文档。

沿用现有 SummaryRevisionHead/Revision 和 SummaryEditOperation，而非新增第二套用户摘要版本：

1. v2 编辑请求冻结完整 BaseDocumentJSON、版本、hash、来源、术语规则与配置；旧 Markdown 路径仍走原 planner。
2. v2 planner 输出有类型的操作：update_document_title、update_overview，以及块正文/标题更新、插入、删除、移动、图注更新；保留 block ID，来源与截图引用只能从允许集合选择。术语纠错改文档与投影，不修改原始 cue。
3. 应用时校验 expected_revision、base hash、保护的引用/术语规则，原子写入文档及 Markdown；删除块同时移除该块图引用，保留可追溯版本。
4. 预览显示结构差异和受影响图片；撤销恢复整份旧文档，不能只回滚文字。
5. 后台补图只更新对应生成原稿。用户版本已经存在时不修改用户 head，新原稿变化按现有 needs_merge 流程由用户决定；没有人工版本则可直接读取新原稿。
6. 重新生成不先删除 AISummary/用户可读内容；待新原稿通过校验再 CAS 发布。force 的现有删除行为需要在新流程中调整并回归。

保留现有 Markdown 导出接口，取当前有效版本并派生章节/图注/来源时间。第一版不新增图片打包或公开分享：导出不嵌入会过期的私有图 URL；图可用文字图注与源视频时间表示，明确导出为文字版。完整图片离线包作为后续能力，不能输出看似有图但实际不可访问的链接。

S0 退出：新来源/文档校验与持久模型就绪；迁移可重复执行；旧摘要、原稿缺失但有用户版本、预览/应用/撤销/导出均能继续使用；v2 原子文档修订和 hash 测试通过。

## 3. S1：B 站字幕优先与耐久推进

### 3.1 来源身份与 URL 修复

拟新增 `BilibiliIdentity`：platform、bvid、aid（若已知）、part_index、cid、canonical_url、duration_ms、identity_version、resolved_at。解析前不伪造 cid；元数据确定身份后冻结。范围为普通 BV 视频与 b23 短链；兼容 AV 必须元数据转换，合集/番剧/互动视频等不靠 --no-playlist 推断支持。

修复 remoteurl.Sanitize：仅保留已验证的正整数 p，清除追踪参数；未传 p 为第一 P。p=0/负数/小数/冲突重复参数/超范围返回错误，不静默改第一 P。不同 P 有不同身份/去重键，长短链接解析后归一。

元数据解析在媒体下载前确认所选身份；第一版下载完成之后才进行字幕探测/发布与摘要。下载适配器返回并核对实际 cid/part 和媒体指纹；不能靠正文标题或 URL 相似判断一致。

短链解析、平台 API 和字幕下载都使用受约束客户端：检查每跳 scheme/host/port/public IP，限制跳数、响应大小、超时，连接地址与校验地址一致；API/CDN 目标策略由服务端维护。Cookie 仅发送到匹配域名。继续参数数组 + exec.CommandContext、--ignore-config 与证书验证，不用 shell 拼用户 URL。

现有下载器的入口 DNS 校验不是完整网络沙箱。新增 HTTP 请求必须约束实际连接；yt-dlp 后续出站访问需部署代理/出站策略约束，未完成时如实记录剩余限制，不能给测试套件写“完全防 SSRF 已通过”。

### 3.2 yt-dlp 字幕适配

拟新增小型适配器与 fixture 测试，职责为 ResolveIdentity、ListSubtitleTracks、FetchSelectedSubtitle；使用结构化 metadata，业务层不解析 --list-subs 的展示表格。下载、探测共用配置的 yt-dlp、ffmpeg、proxy、cookie 路径，临时目录独立且始终清理。

已核查的 B 站提取器：读取字幕轨道并转 SRT；弹幕也在 subtitles 中；没有可直接据以区分人工/自动的 automatic_captions 集合。实现要用 --write-subs，显式排除 danmaku；轨道数组仅有弹幕视为无可用字幕。硬字幕仍走 ASR，本轮不增加字幕 OCR。

拟新增轨道字段：track_key、provider_track_id 可空、language、display_name、format、subtitle_kind=manual/automatic/unknown、kind_basis。kind_basis 记录确证的元数据依据；仅出现在 subtitles 不构成人工证明。源字幕语言与摘要输出语言分开。

候选选择顺序：用户明确指定且可用的轨道优先（第一版服务端可保留此字段，轨道选择 UI 可后置）；否则在匹配语言的可用轨道中，按有依据的人工、自动、未知排序。不得为优先人工而选错误语言。平台缺少语言/类型证据时保留未知，不让 LLM 填成已确认事实。

### 3.3 字幕解析与质量检查

拟新增字幕解析模块，覆盖 UTF-8/BOM、CRLF、多行、HTML 转义和有限样式标记。保存原始文本指纹与清洗投影，时间为整数毫秒。检查空文本、损坏 cue、负时间、end<=start、明显越界、语言不符和明显截断。

质量结果分 usable / unusable，并保留 warnings。已知时长下的轻微平台时间误差可作为显式警告并保留原时间；不应静默裁掉大段坏行后说全文完整。判断规则与阈值配置化，并有边界 fixture；字幕覆盖率低或中间静音不是单独的不可用证据。

合法重叠 cue 保留。仅归并相邻、时间重叠、可证明是滚动重复的文字，并存原始 cue 映射；后面再次说出同一句必须保留。不让 LLM 纠错覆盖来源文字。第一版一份主要来源，不混合字幕与 ASR；局部补缺后续另定契约。

### 3.4 自动意图、作业与原子接力

拟扩展 `POST /api/v1/media/upload-url` 请求：auto_summary（缺省 false）、text_source_policy=prefer_platform/force_asr、preferred_language（可空）、summary_visual_enabled（缺省 false）、output_mode（新入口缺省 auto）、mindmap_enabled、summary_instruction、auto_tags_enabled（新入口缺省 true），并接收 Idempotency-Key。summary_instruction 是可选用户处理要求，最多 2000 个 Unicode 字符，与来源文字分开冻结；不授予额外工具或越权范围。本地 merge-chunks 同样提交自动意图与幂等键，但文字来源固定 ASR。新前端为一次用户动作创建稳定键，网络重试复用；旧无键客户端保持原有处理行为，不自动启动新增分类。

summary_visual_enabled 映射用户明确的视觉许可；选关键帧/图文时同时展示所需能力，不能用 output_mode 绕过关闭设置。第一批仅文字基础，未接入的模式先禁用并说明原因，第二批才开放。用户要求影响重点/结构/形式，不能覆盖来源真实性和权限。

继续读取 /media/import-options 的 url_import_enabled，执行演示账号和资源归属检查。能力按实际动作判断：字幕获取不需要 ASR；摘要只需 LLM 和完整文字；补图需 Vision/FFmpeg/媒体和用户选择；不要求 Embedding/索引。

拟新增 text_source 作业类型，复用现有 TaskJob 的耐久 dispatch/retry 机制，明确注册生产、消费、补投与终止分支，不靠默认分支接收。服务端冻结处理策略，页面离开不丢任务。

固定接力：

```text
导入意图 → 身份确认与媒体下载
下载完成 + text_source 作业待投递意图（同事务）
→ 探测/获取/验证字幕
→ 可用字幕，或按冻结策略进入现有 ASR 作业
→ 完整来源发布 + active pointer + 兼容全文 + 索引失效
   + 自动 summary 的来源快照/待投递意图（同事务）
→ 独立 summary 执行与文字发布
→ 可选补图、标签处理
```

新增事务仓储方法承担接力；网络发布在事务后执行，失败恢复 dispatch，崩溃由 scheduler 补投。禁止“publish 来源成功后直接 RequestAnalysis”留崩溃空档，也不依赖前端轮询后启动下一步。

fallback 接力同样原子：text_source 作业记录“已委托 ASR”并终结自己的执行占用，与 ASR 待投递意图同事务提交，不能持 lease 等另一个 worker，也不直接调用会拒绝父任务 Running 的旧 RequestTranscribe。ASR 完成后由其发布事务建立统一来源并接力摘要，保留既有分片恢复。当前 SummarySourceReady 会拒绝 downloading/transcribing 等阶段；v2 改为按授权的完整冻结来源、当前 generation 与冲突作业判断，仍拒绝未发布来源和已取消任务。来源发布事务先终结来源作业并设置真实状态，再建立摘要快照；不能让新的 text_source 阶段一直挡住摘要，也不能绕过旧 ASR 冲突校验。RAG/视觉等辅助阶段不代替来源完整性事实；legacy 模式保留原准入规则。

请求幂等与结果缓存分开。HTTP 幂等记录以 `(user_id, action, Idempotency-Key)` 唯一，请求 hash 只使用首次提交的稳定请求内容（已知的目标 task/上传会话/URL、策略、语言、视觉选项、显式 profile ID 等）；创建 task 前先查记录。重复相同请求返回原任务/运行，不因执行中来源、默认配置或凭据变化创建新任务；同键不同请求内容返回 409。首次接收时冻结实际配置/偏好，worker 恢复读快照。

自动接力使用持久 import/processing intent ID + source_digest + recipe/options fingerprint 的稳定键；scheduler 重投不能每次新建生成身份。run 重试复用原 generation ID；用户明确重生成才建立新运行。

当前已有独立 summary lease；只需扩展输入与生成身份，不把 summary 绑回父 task 的 ASR/RAG/visual token。同一个 TaskJob 只关联一个当前 generation；重试保留 run 身份，明确重生成建立新身份。任务取消、删除、来源更新、旧 claim token 与 late worker 在提交前都做 CAS 和权限/存续检查。

### 3.5 字幕失败、刷新、缓存

| 结果 | 拟新增原因码示例 | 第一版行为 |
|---|---|---|
| 无轨道/仅弹幕 | subtitle_not_available | 有 ASR 能力则降级，否则明确待配置 |
| 无合适语言 | subtitle_language_unavailable | 按策略 ASR；不伪造选中语言 |
| 需登录 | subtitle_login_required | 说明未获取；按策略降级，不能标“无字幕” |
| 风控/暂时失败 | platform_rate_limited、subtitle_fetch_timeout | 有界退避后降级或明确失败 |
| 解析损坏/质量差 | subtitle_parse_failed、subtitle_unusable | 不发布坏来源；按策略降级 |
| 身份错误/安全错误 | source_identity_mismatch、subtitle_url_unsafe | 拒绝并终止，不通过 ASR 掩盖错误媒体身份 |

暂时错误的总超时、最大尝试数和退避进入现有重试预算，不增加无界网络循环。缺 ASR/LLM 等配置属于明确阶段失败/待配置状态，不自动反复重试耗尽预算；配置后由用户继续。ASR 预算只在 fallback 实际执行时占用；字幕成功必须证明 ASR 调用为 0。

拟新增 `POST /media/task/:id/text-source/refresh`：Idempotency-Key，策略字段同上；构建候选成功后才替换 active source。现有 transcribe?force=1 仍是明确重跑 ASR，align=1 仍是主动对齐。失败保留旧文字，状态说明此次刷新失败；source digest 未变化且配置/选项未变化不重复摘要。

共享媒体仍按真实 MD5 去重。新来源与 v2 摘要第一版按用户任务隔离；不再以 URL 临时 MD5 或 FileMD5 单项命中新摘要。来源就绪后的生成结果 fingerprint 包括 source digest、来源/输出语言、recipe/prompt/schema、模型/profile/偏好、summary_instruction、output_mode、视觉选项与预算/策略版本；它不是 HTTP 幂等 hash。导图显示/折叠等阅读偏好不改变内容缓存，分类单独判断 auto_tags_enabled 与词表/拒绝决定版本。用户修订、标签、术语规则和私有图引用不能进入跨用户共享缓存。

逐项检查 Summary.FindByMD5、taskTranscriptSource、lookupContentDedup、prepareSummaryDispatch 等短路。新模式可显式跳过 legacy 摘要缓存；命中旧正文不能假装它是新的字幕摘要。媒体去重和正确的既有 ASR 复用保留；来源归属和类型必须可证。

隔离必须双向：旧 FindByMD5/上传去重/旧客户端查询也必须排除用户任务专属的 v2 摘要与平台字幕行，不能让 legacy 查询取到其他用户的新增结果。共享旧 ASR 结果要明确 eligible 类型与原始媒体身份；v2 读取的 owner/task 条件和 legacy 读取的 cache eligibility 都有测试。

S1 退出：正确分 P/短链；字幕选择/解析/原因码；本地 ASR 无回归；无 ASR 配置但有字幕可生成；原子接力、补投、重复消息与旧 lease 测试通过。

## 4. S2：结构化摘要、发布与入口前置

### 4.1 v2 生成配方

新增 recipe `summary-v2`，复用现有 AIProfile、上下文预算、调用观测/重试/限额和 summary_parts。按实际模型 token 容量判断：完整文字、cue 标识、系统规则和预留输出能容纳时一次请求；超出才分段。不要依据模型名字假定容量。

输出目标为 title/overview/blocks/source_refs 和表达选择，可附领域标签候选供 S4 自动处理。Agent 同时组织章节、关键概念和层级，供导图渲染。系统规则要求关键结论、步骤/推导、条件和限制；用户处理要求可影响重点和表达，不能指示编造。字幕/画面是待分析内容，其中嵌入的“忽略规则”等指令不能变成工具授权。

长摘要的叶子与合并结果都带可验证来源引用，检查点包含 source digest/recipe/profile/prompt 等身份。合并不能杜撰时间；旧按字符估算时间的路径不用于截图窗口或精确回放。修复重试有界且算入预算。

生成正文的模型可以指出需要视觉解释的块和理由，但不能直接注册图片引用。文字/图文模式的首次完整文档通过校验后 CAS 发布 text_ready；关键帧模式的候选计划只属内部进度，实际看图/选图完成后才发布该形式。草稿或单个 leaf 不冒充可读完成结果。

### 4.2 状态与 progressive publish

区分执行状态与结果可用性，拟扩展读取投影：

- generation.status：queued/running/completed/failed/cancelled，沿现有命名风格。
- generation.stage：text_source/text_summary/visual_enrichment/finalizing。
- text_state：missing/ready；已发布完整文字才 ready。
- result_state：missing/ready；有符合实际表达形式的可用摘要才 ready。
- requested_mode/resolved_mode/fallback_reason：区分用户形式偏好、最终表达和降级。
- visual_state：not_requested/pending/running/complete/skipped/failed，并有 reason_code。
- tag_state：not_requested/pending/complete/failed（S4 后启用）。

text_ready 后 visual_pending 是可用文字摘要；视觉失败或预算耗尽可明确降级为文字。关键帧模式需要实际选图才达目标；降级时显示“关键帧未完成，提供文字结果”，不能冒充按要求完成。新生成运行失败时，旧已发布结果仍可读；API 同时返回 result source/version 与本次 run 状态，避免把旧结果展示为本次成功。

拟扩展现有 `GET /media/task/:id/summary`，保留 content/revision/revision_id/source_status/base_generated_hash/current_generated_hash 等字段，增加 document（legacy 为 null）、format、source、generation、generated_version/content_digest、content_hash_kind、base_generated_hash_kind、current_generated_hash_kind。读取当前有效用户版本，与 task.summary 原稿字段不同。

详情准备阶段可复用现有 GET 进度读取与 React Query 轮询。当前摘要读取没有本文要求的动态活动流，修订 UI 也主要轮询操作状态；不能把这两者标为已支持实时短标题。S2 接入真实准备/生成活动的快照与统一展示投影，S3 完成摘要 Agent 的有序活动与选帧预览，S5A 接入问答和修订。旧路径暂时只有粗粒度事实时如实展示该事实，不虚构更细步骤。

### 4.2.1 动态活动的公开契约

这是拟扩展的展示契约，不是新的编排平台或第二套执行状态机。执行仍由 TaskJob/AgentRun/AgentStep/journal 掌握；前端仅消费其安全投影。已有 Chat 的 `progress`、`run_start`、`step_start`、`step_done`、`step_error`、`tool_call`、`tool_result` 与历史快照分别适配到共用 ActivityView。下表给出目标语义，实际生产 wire 字段可沿已有 `id`/`label`/`status` 命名扩展，不为命名一致破坏旧客户端。

| 字段 | 目标语义与来源 |
|---|---|
| run_id、subject_kind/subject_id | 当前运行与归属：摘要生成、单轮问答或摘要修订；task/session/message/generation/edit_operation 的合法关联由服务端确认 |
| activity_id、attempt | 稳定业务活动标识及真实尝试；可映射现有 step_id/ID。重试和多次同工具执行不能按标题或 tool 名折叠 |
| seq | 该 run 单调递增的事件游标；重放同一事件保留 seq。不能用墙钟时间排序替代游标 |
| kind | prepare/plan/tool/answer/save/revision 等公开分类，表示实际活动来源 |
| state | 展示投影 running/done/error/cancelled；已接受未开始的 run 用 run 状态 queued。确有接受记录的活动可兼容 pending，禁止生成预设的未来 pending 列表 |
| title | 面向用户的短行动标题；原型字段 title，生产可沿用 label。Planner 拟增 public_title，失败时工具映射回退 |
| detail | 可选安全结果/决策摘要，复用 public_summary 等公开说明，不承载供应商 reasoning 或内部思考草稿 |
| tool、started_at、finished_at、duration_ms | 实际白名单工具名与服务端生命周期时间；未知时间为 null，不用动画运行时长伪造执行耗时 |
| references | 可选 block_id/cue/time/授权截图预览等受控指针；只有真实资源登记成功后出现，不返回任意 URL/路径/凭据 |
| run_status、connection_status | 运行终态与浏览器连接状态分开保存/展示；断线不是后端 failed 或 completed |

语义示例（拟扩展 JSON，仅表示一个实际工具动作已开始）：

```json
{
  "run_id": "example-run",
  "subject_kind": "chat_message",
  "subject_id": "example-message",
  "activity_id": "inspect-window-2",
  "attempt": 1,
  "seq": 7,
  "kind": "tool",
  "state": "running",
  "title": "查看 06:12 附近的配置画面",
  "detail": "字幕只说明按画面设置，需要读取具体参数。",
  "tool": "inspect_visual_window",
  "started_at": "2026-10-09T04:12:00Z",
  "finished_at": null,
  "references": [{ "time_ms": 372000 }]
}
```

`public_title` 随 Planner 的已有结构化决策返回，无需单独模型请求或按秒总结。建议 8–24 个字，最多 40 个 Unicode 字符；`public_summary` 继续限为 120 字的公开说明，服务端按统一上限校验。当前提示词限制 public_summary 120 字，但持久化路径 trim 为 240 字，落地时应对齐生产字段和迁移兼容。标题与 tool/目的不符、为空、过长或含敏感信息时，按真实工具和安全定位信息映射成“检索相关字幕”“查看候选画面”，不因此阻塞工具执行。服务端确认动作已开始后才发布 running；只在收到真实结果后发 done/error，模型不决定完成状态。固定“获取视频”“读取字幕”可由系统生成，但同样必须关联真实调用。

Agent 活动是用户可读的动作说明。供应商 reasoning 若已有独立按需查看能力，仍沿原路径保存/展示，不能通过截取推理文本来制造标题，也不能把全部 planner/tool 原始输入输出公开为 detail。

### 4.2.2 归并、恢复与流式结果

1. 前端按 run_id、activity_id、attempt 幂等归并，按 run 内 seq 排序。同 seq 重放不增加一行；相同活动的旧 running 不能覆盖新 done/error，旧 attempt 不能覆盖重试结果。仅收到终态更新时可据其真实信息补建活动，不伪造遗漏的中间历史。
2. 所有事件先校验用户、视频、会话和当前运行归属。切换视频/账号/新 run 后，迟到响应不能回填当前卡片。取消请求在后端确认前显示“正在请求停止”；收起/放大/离开视图不改变执行状态。
3. 复用现有持久运行、步骤和事件游标/快照能力；Chat 缺少所需游标时最小扩展既有事件 envelope，成果 study 事件接口不能直接用作摘要接口。重连从最近已确认游标补读；检测游标缺口时恢复授权快照，再合并其后的事件。游标推进到连续已确认位置，不能因为先收到 seq=9 而永久漏掉 seq=8。
4. 回答文本 delta 与活动事件独立处理。回答已出现后仍保留过程区，后续核对/整理/保存活动继续更新；活动 UI 不能阻塞 token 输出或清空已收到的正文。answer 结束、保存完成、run 终态分别处理，EOF 不当作保存完成证据。
5. 摘要发布、补图完成、整个 run 结束分别处理；旧结果可读不代表新运行成功。修订预览就绪后停止执行动效，显示等待用户应用；应用事件只影响明确的 edit operation，不把问答记录改成摘要生成记录。
6. 默认展示当前活动与最近 3 条，完整历史按需展开。初始状态只展示“等待开始”或真实已开始活动，不列未来步骤。无新事件时保留最后已知状态、真实耗时与连接提示，不定时轮播新标题。总量未知不显示估算百分比；历史数量/资源加载有界并可分页。

公开原型通过本地 ActivityView 和模拟运行事件验证上述体验；计时器只重放 fixture 的实际模拟事件，不能移植到生产以推测活动完成。具体 SSE/轮询读取路由在工作包中登记，并覆盖归属授权、重复、乱序、断线与快照恢复。

### 4.3 API 变更登记

下表均为拟扩展/拟新增；实际 handler 落地后在仓库已有 docs/contracts 约定位置补公开契约。前缀都是 `/api/v1`。

| 接口 | 类型 | 目标行为 |
|---|---|---|
| POST /media/upload-url | 扩展 | 持久化自动摘要/来源/视觉选项；缺省兼容旧行为 |
| POST /media/merge-chunks | 扩展 | 本地上传可显式自动摘要，ASR 路径不变 |
| POST /media/analyze/:id | 扩展 | 无来源时按明确 generate 意图准备来源；有来源直接独立摘要；冻结处理要求/表达/分类选项，Idempotency-Key，返回 generation 身份 |
| GET /media/task/:id/summary | 扩展 | 有效版本 + document + 来源 + 分阶段状态 |
| 摘要运行活动/事件读取，路径在 S2/S3 登记 | 拟新增或扩展已有读取 | 授权快照、动态短标题与有序游标恢复；映射 4.2.1，不调用 study 专用接口 |
| Chat progress/step、修订活动读取 | 最小扩展既有事件/读取 | Chat 公开 title/label，修订细粒度活动与预览/应用分离，回答 delta 独立 |
| GET /media/list | 扩展 | 有效摘要短预览/更新时间/状态；S4 增加标签过滤 |
| POST /media/task/:id/text-source/refresh | 新增 | 显式刷新文字来源，保留旧来源与结果 |
| POST .../summary/edit-runs、apply、undo、resolve-base | 扩展内部契约 | 保留已有行为，v2 原子操作文档与投影 |
| GET .../summary/export | 扩展投影 | 有效版本文字导出；不写过期图片 URL |

analyze 旧请求没有完整自动意图时保留现有语义；不能默默把“只摘要”变成“下载/ASR/视觉全开”。新入口显式选择流程，服务端按 source readiness 准入和冻结选项；409 冲突返回可恢复状态。相同幂等键 + 相同内容返回同一次请求，不同内容返回冲突。

### 4.4 前端第一批

1. UploadModal 主动作“导入并生成摘要”，可选处理要求；高级项为仅导入、形式偏好、补图/自动分类与配置提示。新提交显式传意图；去重命中显示阅读已有摘要。保留 url_import_enabled 与 demo 禁用。
2. Library 卡片主动作 read/generate/view_progress；短预览来自当前有效用户版本。卡片不加载整篇文档/图片，也不通过浏览器启动下一阶段。
3. `/video/:id` 默认摘要视图，直接复用/拆出 SummaryRevisionPanel 的读取、状态、修订与导出能力。播放器与问答使用同一可收起辅助区并互相切换；字幕、笔记和完整运行详情是次级入口。S5A 前问答沿用既有入口，不提前放出无业务接入的按钮。
4. 旧 Markdown 页面仍可读、修改、撤销、导出；没有结构时从标题派生文字目录，不伪造 cue、不自动调用模型迁移。
5. 文本、播放器和图片读取独立；有用户版本时不能因 task.summary 缺失阻止展示。原稿与人工版本冲突仍提供既有 resolve-base 决策。
6. 摘要标题栏提供“放大阅读/恢复布局”，以页面内 focus mode 调整面板占用；保留摘要组件实例、有效版本、滚动锚点与播放器状态。正文保持行宽上限，图片与导图可使用更宽区域；嵌套浮层优先处理 Esc，退出恢复触发按钮焦点。
7. 运行卡按 4.2.1/4.2.2 动态展示当前短标题、真实耗时与最近 3 条记录，不列预设待办；支持完整记录展开；本次文字发布且 visual_state=pending/running 时使用“文字摘要已就绪，正在补充画面”，其他情况按真实状态展示。用户手动折叠决定保持，已读正文不被自动滚动打断。详细运行展示按 4.2 的投影接入，不要求先完成补图才提供反馈。
8. 复用 tokens.css 的 --bg-0/1/2/3、--tx-*、--acc-* 与主题切换；摘要、成果阅读面和导图画布在深色模式中统一用主题表面，检查现有固定 --paper 的消费位置并做受影响范围内的适配。原视频/图片保持原色，现有显式浅色模式继续可读。

S2 退出：首次导入一次启动到可读摘要；短/长预算分支正确；无需 Embedding/RAG；浏览器刷新不重复生成；重新生成保留旧结果；1440px/1024px 桌面专注阅读/恢复与真实活动投影可用，无假步骤；旧版与 v2 修订、权限和任务状态都通过相关测试。

### 4.5 运行反馈动效

复用 ThinkingProcess.tsx / ThinkingProcess.module.css 的步骤时间线与已有 1.6 秒 breathe，以及 tokens.css 的 --ease、--motion-press、--motion-feedback、--motion-panel。抽出可接收公开活动投影的共用 ActivityView 展示层，摘要生成、回答期间和修订共用，按 run 隔离。组件名称/旧聊天语义保留兼容，摘要默认标题“Agent 运行过程”。RunProgress 当前学习笔记阶段和文案需要适配，不能按固定时间自动推进。

- 当前 running 节点与小状态点轻微透明度呼吸；仅这类持续执行标识循环。连接中断改成“连接中断，等待恢复状态”，状态点停止呼吸以避免冒充最新服务端状态。完成、失败、取消确认后全部停止循环。
- 新真实记录用已有反馈/面板时长柔和进入；完成转勾、失败转语义色。变化不搬动正在阅读的正文，不用不断闪动的全文骨架代替进度。卡片内层动画使用 opacity/transform，避免动画化大面积布局与阴影。
- 点击展开/收起与专注切换复用面板动效 token；键盘触发直接切换，快速重复操作可打断且不留退出遮罩。
- prefers-reduced-motion 下停止循环与位移动效，以静态状态点、文字/勾选和温和颜色变化保留反馈。aria-live=polite 仅播报新活动和重要状态变化，不逐秒播报耗时或每条流式 token。
- 实际选帧成功后才显示该帧预览与时间；授权读取/图片加载失败保留动作记录和替代说明，避免模型自造 URL。

可见性降低或过程卡收起时停止无意义的循环动画；后台执行由服务端继续。完整事件历史与恢复信息不因 UI 动效状态改变而丢失。

## 5. S3：Agent 按需配图与摘要导图

### 5.1 Agent 的职责与接入方式

按受控配方执行“确定需用画面表达的内容 → 调查时间窗 → 看候选图 → 选图/说明信息 → 组织关键帧或修订对应正文 → 校验发布”。关键帧模式挑选能代表重要步骤/概念的几张图；图文模式在相关正文旁配图。不是每隔固定秒数截图再全部塞进摘要。自动模式发现画面无帮助时可用纯文字；显式关键帧请求无法完成时给出明确降级。

复用 VisualInvestigator 的提取、观察、缓存与验证；在 wiring 中新增摘要生成依赖。当前默认导入 VisualModeOff，工具要求允许 caption 与可用 Vision；必须让新摘要图文选项与已有视频视觉策略有清楚映射。用户已明确关闭的视觉能力不被后台改开；OCR-only 不等于可做视觉语义判断。

AgentRun 已有 SubjectKind/SubjectID、recipe 和无 Session 的生成运行结构，但 AgentExecutionJournal.EnsureRun 当前要求 ChatSession。新增 summary_generation subject 与显式创建/恢复适配，复用持久步记录/预算/lease；不要为摘要创建假的聊天会话。模型/仓储中的 subject 白名单和迁移约束一起扩展并回归旧 chat/artifact/summary_edit。

SummaryGenerationSnapshot 保存冻结业务输入，TaskJob 承担队列 lease，AgentRun 承担运行/步骤日志；以同一 generation ID 关联，定义单一提交授权。默认复用已有存储，不新增复制运行状态的表。

### 5.2 工具和窗口约束

Agent 只能使用当前任务授权工具，输入为 block_id、goal、required_facts、来自冻结 cue 的窗口与剩余预算。窗口必须位于同一媒体的真实时间范围；无真实时间则不按字符比例猜图，可标“缺可定位来源，未补图”。

候选调查提供实际画面、capture time、observation/frame 引用和缺口。Agent 实际看到图后才能选图和更新正文；工具仅成功抽帧而模型未看图不算已验证。模型不能传任意远程 URL/路径、其他 task ID 或主动突破预算。

复用既有候选选择、图像质量/相似检测；不足的地方实现清晰度、相邻重复和相关性检查。相关性无法完全由阈值保证，仍需人工验收。图注描述画面及其帮助，不写无图支持的推断。补写事实区分字幕与画面依据；不覆盖原字幕。

### 5.3 全篇预算与恢复

第一版可配置默认全篇上限：3 个调查窗口、8 张候选帧、8 次 Vision 调用；每窗最多 2 分钟、累计 6 分钟。另设选中图上限 5 张。借鉴已有调查默认值，**所有窗口/重试共享这一次运行的总预算**，不能每个章节重新获得 8 次配额。

这些值是初始限制而非效果目标；长视频可按显式预算调整。planner/修复/描述的 LLM 调用、tokens、墙钟时间与费用纳入现有治理；供应商未知 usage 不写零。工具结果、已看/已选图与检查点持久化，重试复用已完成安全步骤而不重复 Vision 调用。

没有帮助/缺能力/预算耗尽记 skipped 或明确 stop_reason；工具失败记 visual failed，文字结果保持 ready。不能在来源变化、人工编辑或旧 lease 后无条件覆盖 head。

### 5.4 截图读取是必补接点

当前 visual-frame/:frame_id 读取 VideoVisualFrame。按需调查保存的是 VideoVisualObservation，二者 ID 不是同一空间；不能把 observation ID 拼进现有 frame URL。

第一版新增受控摘要图片引用表/映射 `SummaryScreenshotRef`：opaque ID、user/task、generation ID、source/media revision、block ID、observation ID、object key、capture_ms、status。发布事务登记真实引用，文档只存 opaque ID。

拟新增 `GET /media/task/:id/summary/screenshots/:ref`：复用现有受限播放/图片凭据思路，重新验证 owner、task 存续、ref 所属、版本与可读性，再访问对象。短时访问凭据可刷新；不返回长期公开对象 URL。旧版本读取只访问自己对应媒体修订；资源已删除返回明确错误。

图片加载失败保留图注与真实回放入口。删除任务的清理链路覆盖来源、截图映射和实际拥有的对象；共享媒体/其他用户对象不能被误删。模型输出与日志不打印 object key 或签名凭据。

回放也核对文档 media_revision 与实际播放器媒体。第一版无需新增历史媒体播放器；若某旧文档对应媒体已不可用或修订不一致，保留正文/图注并禁用其回放、说明原因，不能把旧时间跳到当前不同视频。正常当前版本必须能回到同一分 P 的对应位置。

### 5.5 补图发布与并发

文字/图文模式在 text_ready 时冻结 generated hash/version，视觉步骤从该文档开始；关键帧模式从冻结候选计划开始。仅修改目标块和图引用；验证完成后按 generation/source/hash/lease 做 CAS 更新原稿与 Markdown。

发现新生成版本、来源变更或任务删除时停止发布，旧运行转 cancelled，原因码为 generation_stale/source_changed/task_deleted，不另加未定义的 stale 状态；不把旧图附到新章节。已有用户修订时保留用户 head，新原稿通过 needs_merge 提示；不自动把图插入人工文档。用户采用新原稿后才显示其图文；用户选择保留仍可继续阅读自己的版本。

### 5.6 导图和阅读 UI

抽出通用 `{id,parent_id,title}` 到 Markmap 的适配器，StudyMap 保留包装。SummaryMap 从当前有效文档 blocks 中 Agent 组织的章节/概念层级派生，使用已安装 markmap-view；不创建 StudyArtifact、不维护第二份导图内容。需要改进层级时使用同一摘要的结构修订，而非禁止 Agent 调整导图所需内容。

概览之后放可展开导图/目录，章节正文在下面。节点定位 `summary-block-${id}`，使用安全 ID/转义与键盘文字目录；改名保留 ID，删除清除选择。布局请求绑定文档版本，旧结果不得覆盖新图。超过 50 节点默认折叠两层，节点内容不丢失；懒加载，失败退回目录。

桌面正文主栏，侧栏播放器保持可回放且不挤压阅读。桌面默认紧凑章节概览，完整导图按需展开/放大；图片可放大，独立“回放此处”跳实际 capture/cue 时间并展开播放器。没有媒体就绪或来源时间时说明原因，不虚构播放成功。

S3 退出：文字/关键帧/图文表达与形式降级正确；真实工具动作动态形成活动、短标题回退与有序恢复正确；Agent 真正看图、全篇预算与恢复有效；截图可授权读取；用户冲突正常；回放身份一致；目录/正文/导图同版本；真实画面样本经过人工审核或明确标为待审核。

## 6. S4：用户标签与分类检索

### 6.1 数据模型与归一

拟新增四个核心实体和候选记录：

| 模型 | 字段/唯一约束 |
|---|---|
| UserTag | ID、user_id、display_name、creation_origin=manual/agent、protected_by_user、version、active/merged、merged_into_id |
| UserTagName | user_id + normalized_key 唯一；tag_id、canonical/alias。规范名与别名同一命名空间 |
| VideoTagAssignment | user_id + task_id + tag_id 唯一；manual/auto、summary generation/version、created_at |
| VideoTagDecision | 用户任务 + canonical tag ID 或 candidate normalized key；accepted/rejected、version/time |
| VideoTagSuggestion | ID、用户任务、summary version、candidate、已有 tag ID 可空、理由、pending/accepted/rejected |

视频标签集增加 CAS 版本（可独立 head）。服务端 NFKC、Trim、连续空白折叠、Unicode casefold，保留 C++/C#/.NET 的标点；展示大小写保留用户名称。名称归一阶段不做翻译、拼音、词干或仅凭模型相似度自动归并；实体合并遵循 6.3 的等价依据、用户保护和事务条件。

规范名和已确认别名自动去重；Agent 优先选择已有 tag ID，不必为了不同表达另建标签。名称/别名冲突返回 409。相似主题不等于同义，“数据库”和“PostgreSQL”不能自动合并。任务/标签归属每次验证，不能用其他账号 ID、名字或计数推断其数据。

初始输入上限配置化：名称 80 个 Unicode 字符、每标签别名 20 个、一次筛选 ID 50 个、标签列表每页最多 200 个；空名称和超限返回 400。数量限制在规范化后检查，不靠前端截断或数据库静默裁切。

### 6.2 Agent 候选与人工决定

优先从当前用户词表选择内容相关标签，已有词表很大时按确定性名称/别名候选收窄并限制 prompt 大小；不因为标签功能引入新 Embedding 依赖。候选最多 5 个、有效关联最多 20 个，配置化且服务端硬校验。

新摘要入口默认开启自动领域分类，可关闭。Agent 根据冻结的完整视频文字来源与用户处理要求提出少量领域候选及依据，不只读取可能很短的最终摘要。服务端验证已有 ID/名称/别名、拒绝记录和数量；命中则复用，新的合适领域词在事务中 create-or-get 并关联为 auto，不要求逐条接受。并发创建由用户命名空间唯一约束兜底，不靠模型保证无重复。

不确定的新词或合并关系保留建议；用户可选择手动接受。不能推断“待读/已学会/面试重点”等个人意图，不能通过候选文字授权全库改名或删除。自动创建的标签标 creation_origin=agent，手工创建/改名/别名调整或人工使用后设 protected_by_user=true。

人工来源优先：手动保留自动标签转 manual，手动删除形成 rejected；重新摘要不重添。人工拒绝候选按规范 key 记录，接受后由 tag ID 归一。背景生成只替换自己维护的 auto 关系，不删 manual；人工决定的版本与后台发布并发时使用 CAS 重算。

明确恢复入口：PATCH 的 add_ids 表示人工添加并撤销对应拒绝，keep_auto_ids 表示将已有自动关系转为人工，remove_ids 表示移除并记录拒绝；同一请求同一 ID 的矛盾操作返回 400。候选 decision=accept 创建/复用标签、撤销拒绝并作为人工关联；reject 记录拒绝；restore 只撤销拒绝并恢复为 pending，不立即创建/关联。拒绝项在详情的可展开列表可恢复，不能只存数据库却让用户无法撤销。

候选可由摘要配方返回，标签结果独立持久化。发布摘要 + tag pending intent 同事务，后台处理幂等恢复；标签失败只是 tag_state failed，不让正文失效。auto_tags_enabled 与处理意图冻结，关闭时不新建标签或关系。补图发布新 generated version 后，持久推进其标签状态；相同来源可复用候选，不让旧 worker 取消后留下永远 pending 的分类。

后台标签提交必须比较 source_digest、generation ID/generated version 与 tag-set version。人工修改造成的版本冲突可重新读取决定后重算；来源/生成过期直接取消，旧 worker 不替换新 auto 关系。来源更新时旧 pending 建议标为过期；接受过期建议返回可读的 409 并刷新，不套用到新内容。

### 6.3 合并与重命名

合并采用事务：固定 ID 顺序锁源/目标，验证同一用户和双方 expected_version；搬迁关系并去重，manual 优先；迁移决定与候选，拒绝不被合并抹掉；规范名和别名迁移到目标命名空间；源登记 merged_into。名称碰撞第三个 tag 时整体回滚，不静默连锁合并。

同一次 merge 幂等返回原结果，旧版本返回 409。旧 tag ID 在用户范围内解析到目标，用于旧筛选 URL；禁止循环合并。重命名默认将旧规范名保留为别名，冲突时提示显式处理。已登记候选拒绝 key 在名称成为 alias 后仍解析到同一 tag，不能换个别名绕过拒绝；merge 的目标与别名决定一起归一。人工现有关联优先保留；拒绝记录约束后续自动关联，不因 merge 被清除，只有用户 add/accept/restore 可解除。

Agent 自动合并仅限双方都是 agent 创建、未被用户保护/人工使用，并有明确同义/同一概念依据的实体；名称相同或确认别名是确定性依据，模型仅“觉得相似”不够。服务端再次校验来源、等价依据、保护状态和版本，再调用同一 merge 事务，记录自动操作与理由。不明确等价或涉及人工标签时显示建议；可以继续复用已有 ID，而不修改它的名字/别名。自动合并也可由用户检查和纠正，不承诺语义分类永远正确。

### 6.4 拟新增 API

| 接口（/api/v1 前缀） | 契约 |
|---|---|
| GET /tags | 当前用户标签、别名、版本、有效视频计数；搜索/分页有上限 |
| POST /tags | 创建规范名；确定性重复返回已有实体 |
| PATCH /tags/:id | 重命名，expected_version，旧名保留别名 |
| PUT /tags/:id/aliases | 替换显式别名集合，expected_version |
| POST /tags/:id/merge | target_id、双方版本、Idempotency-Key |
| GET /media/task/:id/tags | 有效关联、候选、标签集版本 |
| PATCH /media/task/:id/tags | add_ids/remove_ids/keep_auto_ids、expected_version，原子更新决定与关系 |
| POST /media/task/:id/tag-suggestions/:sid/decision | accept/reject/restore；接受新词时事务创建/复用后人工关联；幂等 |
| GET /media/list?tag_ids=...&tag_match=all | 扩展全库筛选；all=AND，any=OR，缺省 all |

关键词、状态和标签条件之间 AND。先将旧/合并 ID 解析为用户范围内目标并再次去重，再计算 all 的目标数量；两个旧 ID 合到一个目标不能导致 AND 永远无结果。使用 EXISTS 或 DISTINCT/HAVING 计数，避免 JOIN 造成视频重复和总数失真。未知模式返回 400；跨账号 ID 按现有资源拒绝契约处理；计数仅包含可读有效任务。

### 6.5 UI

视频库增加标签筛选、管理面板和清空筛选；关键词/状态/标签/视图/页码进入 URL，筛选变更重置页码。桌面默认显示最多 6 个常用标签并始终显示已选标签，其余从可搜索的“全部标签”面板选择；结果来自服务端全库。

详情概览附近显示当前有效标签与候选，不将旧 Markdown 领域标签自动导入用户词表；旧文本可继续展示为正文中的标签。手动增删、接受/拒绝、改名和合并都有错误/冲突反馈，演示账号继续只读。

S4 退出：自动创建并发去重、关闭开关、已有词复用、自动/人工合并边界、拒绝保持、人工并发、AND/OR 计数分页、多用户隔离与 UI 筛选恢复通过；PostgreSQL 约束/锁得到真实集成验证。统一标签筛选规格可被后续知识库复用，不自动改变成员或重建 embedding。

## 7. S5A：摘要选段问答与修订衔接

### 7.1 当前能力与复用方式

已有学习笔记支持整块“围绕这段提问”，通过 artifacts/:id/blocks/:block_id/context 和 chat/v/:task 的 artifact/version/block 参数取内容，再拼进 textarea；这不是摘要划词、可折叠注释或结构化消息上下文。当前 ChatMessage 与普通/stream/agent-stream 请求没有选段快照字段。

沿用现有会话、请求逻辑、消息渲染和输入框，在视频详情接入可展开侧栏问答及注释卡片，形成统一工作区；独立视频聊天页保留并复用同一能力，不能嵌套两套完整 shell 或同时挂载两个发送实例。从详情选择整块或单块内文字后在当前侧栏提问，也可进入独立问答路由；保持问题草稿，不自动发送。在问答页也能打开摘要选段。输入框上方展示“摘要选段”：来源标题、节选、版本、展开/回到原段/移除。第一版最多 3 个选段，总计 3000 个 Unicode 字符，独立于现有 question 长度限制，不静默截断。

问答标题栏增加“放大问答/恢复布局”，与摘要专注视图互斥；保留同一 ChatWorkspace 会话与请求实例、输入草稿、已选注释、消息滚动锚点及流式输出。已有问题/上下文侧栏折叠可复用，进入专注后摘要按需打开；退出恢复之前的分栏与焦点。不得通过重新挂载组件重发请求。1024px 窄桌面默认收起辅助区，需要时在摘要旁打开问答；输入框留在可视区，切换不自动发送。
专注模式切换保留组件实例；实际跨路由时用按 user_id/task_id/session_id 隔离的共享视图状态保存草稿、注释及锚点，沿用现有请求/恢复逻辑，不把执行生命周期交给视图状态。切账号/视频不得混用草稿与订阅；退出账号清理临时状态。S2 未接入侧栏问答时沿用既有路由，不提前展示新增入口为可用。

桌面提供整段选入/选择按钮；首版不支持跨章节或复杂图形区域选区。关键帧可先选所属图注块，图片附件作为可后置扩展；不要把这一功能升级为任意文件上传平台。

### 7.2 拟新增/扩展契约

- GET /media/task/:id/summary/blocks/:block_id/context?version_ref=...：授权后返回该版本的块、规范可选纯文本、block digest、摘要版本引用与原始 source refs。legacy 按 Markdown 确定性投影临时段落 ID + digest，不调用模型迁移。
- 概览用保留逻辑块 ID `summary-overview`，标题可用 `summary-title`；生成的 blocks 不占用这些 ID。它们同样可选入问答；编辑时映射到 update_overview/update_document_title，而非伪造来源 cue。
- messages、messages/stream、messages/agent/stream 的请求增加可选 context_refs，每项 kind=summary_selection、task_id、version_ref、document_digest、block_id、block_digest、text_start/text_end、quote。version_ref 显式区分 revision_id 与 generated_version，不能用标题作身份。
- text_start/text_end 按服务端约定的规范可选文本 Unicode code point 计数，不按 UTF-16 单元；前端转换和 Markdown 纯文本投影有往返测试。quote 只用于校验，服务端从真实保存文档截取实际文字。
- ChatMessage 增加可空的 ContextAnnotationsJSON，保存服务端已验证节选、版本/hash、块、关联来源和引用类型；复用当前消息与执行快照，不新增另一套消息存储。

普通和流式提交在首次 SSE 输出前验证会话、视频/摘要归属、scope、版本、范围和 quote。引用的视频必须在该次问答明确的授权范围内，不能因为附件偷偷扩展范围。保存的 revision 可按权限读取；旧 generated 版本未归档且当前 digest 已变时返回 409 要求重选，不替换为最新正文。限额、错误码与字段在实际 API 契约登记。

请求接受后冻结实际注释到本次执行和用户消息；恢复/重投继续用它，历史卡片读快照，不回读最新摘要。源视频删除/权限变化遵循现有会话与资源访问规则；回放和图片继续授权，不能靠历史附件绕过。

后续轮次构建 conversation_context 时，已冻结注释与对应问题一起纳入独立、有界的上下文，携带“摘要选段”来源标签并重查当前范围/访问规则。不能只在 UI 历史显示而让模型丢失选段；用户追问“那这个怎么设置”仍能理解所指。历史/注释/输出共同受总 token 预算约束，超限或来源不可访问明确说明，不暗中读当前摘要替代旧快照。

### 7.3 提示词、来源与编辑意图

选段作为“用户提供的摘要上下文”进入提示词，问题保持独立。它是衍生内容：可以解释该说法，但不能把节选包装为原视频原话或凭空生成 [Cn] 视频引用。事实核查沿真实字幕/音频/画面证据；没有这些依据时说明边界。注释内容不授予工具权限。

读取摘要时使用当前有效用户版本；修复 chat_prepare 现有 raw Summary/MD5 回退，使新 v2 不跨用户复用，也不忽略人工修订。单视频选段问答复用现有 LLM/视频上下文路径，无必要索引时不强行新建 RAG；完整集合全文择路属于 S5B。模型能力不足的路径明确给原因。

“围绕这段提问”只发送上下文与问题；“让 Agent 修改这段/全文”提交现有摘要 edit-runs，拟增加可选 selected_block_ids，冻结编辑范围。preview/apply/undo 的正文、图注、结构和来源变更一起展示；应用后各视图同版本。不能把普通回答直接写回摘要。

### 7.4 问答与修订的动态活动接入

Chat 复用现有 progress/step/tool 事件与 trace reducer。Planner 增加公开短标题后投影到 label，旧事件没有标题时按真实工具回退；ThinkingProcess 的公开 detail 继续按需展开。单轮回答开始输出后过程仍保留，保存/引用等后续事件实时更新；有能力直接回答时只显示实际生成/保存动作，不强制产生检索或截图步骤。普通回答和 Agent 工具模式复用展示层，各自只提供自己真实发生的活动。

摘要修订目前通过 SummaryEditOperation 与 AgentRun 持久状态、SummaryRevisionPanel 状态轮询恢复，尚没有本设计的实时活动标题流。扩展实际 planner/取证/校验/预览发布接点并提供安全活动投影；只调用一次 planner 时就展示这一次，不补出“定位画面”“对照字幕”等未执行动作。选段范围和用户要求影响实际活动；预览 ready 与用户 apply/undo 分离，等待应用停止呼吸。编辑 run 的快照/事件权限沿现有归属执行，并复用 4.2.1/4.2.2，不给修订增加假的 ChatSession。

S5A 退出：折叠/展开/移除/返回、摘要与问答专注切换/恢复、草稿及消息位置保留、单块划词与中文/emoji 范围正确；节选不占问题限额；发送前过期/越权拒绝；历史/恢复快照稳定；普通问答不改摘要，明确编辑可预览/应用/撤销；问答 delta 与动态活动并行，修订过程真实且预览待应用无运行假象；旧学习笔记提问入口无回归。

## 8. S5B：后续知识库标签范围与全文/检索择路

该工作包为后续设计，首轮 S6 完成不依赖它。先复用已落地标签实体/筛选规格和现有知识库/视频库 ChatSession，不创建新的标签知识库类型或第二套检索系统。

当前向量 + 请求内 BM25 并发召回、RRF、多查询、重排、邻段扩展均已存在。KB/video_library 目前统一走 RAG；单视频概览有 8000 字符来源预算，不能说已经支持自动全文。关键词通道不是 Elasticsearch；集合成本先测量再考虑索引升级。

### 8.1 统一 selector 与授权成员

拟扩展 ChatSession/请求的持久化 selector：tag_ids、tag_match=all/any，可含显式选择 task_ids。video_library 在用户拥有视频内过滤；knowledge_base 在既有 KB 成员内过滤；显式 task_ids 再取交集。标签不自动新增 KB 成员、授权、索引或模型选择。

每次提问解析并冻结 selector、基础授权成员、过滤后的有效成员、就绪来源与版本。准备、Agent policy、恢复、历史提示词和 CreateExchange 最终事务使用同一解析器。当前保存逻辑会与整库/完整 KB 成员比较；只把子集传进 RetrievalPipeline 会被误判为成员变化，必须同步改最终校验。

请求提供过滤但标签删除/归一/交集后零匹配时，返回空范围或明确错误，绝不丢弃过滤回退整库。自动标签合并导致集合增加/变化时，在途运行继续按冻结成员校验并拒绝变化；重新提问才解析新集合。

范围变化后在继续执行/恢复/保存时按既有 fail-closed 约定处理；不要宣称现有普通 SSE 每个 delta 都实时复查权限。流式范围失效的检测与停止位置需要明确契约和测试，首次授权也必须早于输出。

### 8.2 上下文择路

新增 scoped source-context builder/router，使用真实模型窗口、历史/系统规则/输出预留、总来源 token、问题与成本策略判断，不只按视频数量：

- 所有完整授权来源能容纳：按视频边界、身份与来源映射组成全文上下文，可不依赖 Embedding。
- 超预算或需要定点证据：复用当前 RetrievalPipeline，向量与 BM25 都只接收冻结 task IDs。
- 来源不完整/缺能力：明确部分可用或错误；不能截断文字后称为“全文”，也不把最终摘要当完整原文。

当前 KB 加视频要求相关索引完成，且有 50 个上限。无 Embedding 的小集合全文问答需要把成员存在/文字可读与检索投影就绪分开，连同成员管理、能力准入一起调整；不是只改 router。50 个上限先保留，不顺带扩张产品配额。

标签改名/合并改变 SQL 范围，不必重建视频 embedding；文字 source refresh 才使旧内容投影过期。原文摘录和 exact/coarse/unknown 时间保持真实，标签与摘要不能进入原始证据字符串。

S5B 退出：库内/全库标签交集与权限隔离，空过滤不回退整库，合并扩张不扩大在途范围；冻结/恢复/历史/保存成员一致；无 Embedding 小集合完整问答；超预算进入已有 hybrid；范围变化、索引过期、缺来源和时间可信度验证。实际模型回答质量与索引覆盖率另测。

## 9. S6：测试、质量与交付

### 9.1 测试矩阵

| 层级 | 必测场景 | 证据要求 |
|---|---|---|
| URL/字幕单元 | p 合法/非法/重复；短链恶意跳转；弹幕-only；manual/auto/unknown；BOM/CRLF/重叠/重复/越界 | fixture 精确断言，不请求真实平台或收费模型 |
| 来源兼容 | 字幕、ASR、旧全文无时间；active/source freeze；索引失效；跨任务媒体复用 | 来源 refs 不串，unknown 不伪精确，本地 ASR 回归 |
| 文档/修订 | ID/树/引用校验；概览/标题纠错；preview/apply/undo；legacy 遇 v2 的 keep/use、再次编辑/撤销；原稿缺失 | 同事务与 hash kind/CAS 校验，投影/目录同步，有冲突/迟到响应测试 |
| 作业恢复 | DB commit 后 publish 前崩溃；MQ 失败/重复；lease 过期；cancel/delete/source refresh | 恢复有结果，旧 worker 无提交能力；不重复来源/摘要发布 |
| 模型/视觉 | 文字/关键帧/图文同内容；形式降级；Agent 概念层级；无必要截图；实际看图；预算累计；截图授权 | fake 精确观测调用数；真实视觉结果另做人审 |
| 标签集成 | 自动新建并发/开关/复用；人工保护/自动合并依据；别名碰撞；拒绝恢复/auto 转 manual；旧 worker；AND/OR/分页 | PostgreSQL 唯一约束、锁与事务验证；SQLite 不替代此证据 |
| 问答衔接 | 单块划词/emoji 范围；注释折叠/移除；草稿不丢/不自动发送；过期/越权 SSE 前拒绝；历史快照；问答不改摘要 | 问题与注释分别限额；普通/stream/agent-stream/恢复一致 |
| 后续知识库 S5B | 标签交集/成员变化；无 Embedding 全文；token 超预算 hybrid；来源与索引过期 | 独立验收，不把现有 RAG 当已实现自动全文 |
| 浏览器 | 导入→摘要→回放；文字先读；图片/导图失败；legacy 修订；配置缺失；1440px/1024px 桌面/键盘；切账号/视频后的迟到响应 | 正确用户、真实服务与数据；刷新不重复调用；旧摘要/标签/截图不回填新页面 |
| 专注与动效 | 摘要/问答放大和恢复；草稿/选段/滚动/播放不丢；嵌套 Esc/焦点；深色/浅色主题；减少动态效果 | 切换不重发请求；1440px/1024px 桌面可读；循环只在有效运行中存在 |
| 动态活动 | 摘要/问答/修订按实际动作出现；无需截图/追加检索/重试；public_title 缺失或不符；回答 delta 与活动并行；修订预览待应用 | 无预设 future 步骤、无假活动轮播；工具映射回退正确；完成来自生命周期；正文不被阻塞 |
| 活动恢复 | 断线/快照恢复；重复 seq、乱序/游标缺口、done 后迟到 running、旧 run/attempt；失败/取消确认 | 幂等有序、终态不倒退、用户/运行隔离；动效仅活跃，已发布结果可读；EOF 不冒充保存成功 |
| 真实 B 站 | 可用字幕、登录/风控、无字幕、多 P、本地 ASR、图表/代码/纯讲述 | 实际版本/来源/任务/时间与调用记录，凭据不进报告 |
| 质量对照 | 同字幕全文基线；图文增益；遗漏/编造/条件/图文相关/回放 | 原视频人工核查；未审核字段为空，模型不能冒充审核人 |

重点扩展现有 ytdlp/remoteurl/media/summary_independence/summary_pipeline/summary_revision/consumer_download_security/content_dedup/fault_matrix/transcribe_refresh_failure 测试和上传/视频库/详情/SummaryRevisionPanel 前端测试。新 API/模块需要覆盖行为边界，不写只照抄实现的测试。

### 9.2 命令与环境

按工作包先运行相关包，再在第一批与最终完成时运行全量。当前已存在以下脚本；执行前核对实际版本与依赖，不为跑文档检查安装或改写用户依赖。

在仓库根目录执行相关后端检查：

```sh
go test ./internal/pkg/ytdlp ./internal/pkg/remoteurl ./internal/transcript
go test ./internal/model ./internal/repository ./internal/service ./internal/mq ./internal/handler ./internal/ai
go test ./...
```

在 `frontend/` 执行：

```sh
npm run typecheck
npm test
npm run build
```

新增 UI 测试必须进入实际 vitest 执行范围，当前 test:ui 是显式文件列表；若必要修改 package.json，应基于当前 WIP 做最小追加并报告，不覆盖整个文件。使用已有 markmap/react-query/zod 等依赖；必要的新依赖另给理由和锁文件差异。

PostgreSQL opt-in 集成测试的启用方式先读现有测试 helper，使用隔离测试库。真实模型/平台检查只在用户配置和权限允许时运行，不能默认启用所有 *_real_llm_test；基础测试通过不代表已验证实际模型。

### 9.3 私有样本与报告

建议从少量固定样本开始，覆盖：上传者字幕、平台自动/未知字幕、无字幕/仅弹幕、分 P、长视频、代码/设置界面、图表与纯讲述。保存来源链接（清除凭据）、part/cid、媒体/字幕 hash、模型与 recipe、运行选项、任务 ID、时间、审核状态。

真实样本、原片与包含私有来源的审核记录保存在仓库外或明确配置为忽略的本地评估目录；公开报告只引用可发布的自造 fixture 与脱敏统计；报告不得含 Cookie、API key、完整签名 URL 或私有原片公开地址。初始样本不是统计显著结论，缺少真实样本时交付 fixture、审核表与可运行评估入口并明确待验。

评价包括：

- 必要点击/手工动作；首次文字可读、图文完整、各阶段耗时；失败恢复动作。
- 摘要关键点召回、条件保留、错误/编造；分母与人工清单明确。
- 配图帮助、清晰、相关、重复；零图片的纯讲述视频可正确通过。
- 点击 cue/截图实际回到相同分 P 的对应内容；unknown 不纳入成功定位分母且单独报告数量。
- ASR/LLM/Vision 调用、tokens、成本、缓存/重试；估算/未知标注来源，不能缺 usage 就当零。

### 9.4 启用与回退

新 schema/API 先兼容落地，再切新前端意图。用部署功能开关控制 v2 生成与补图启用；读取旧/新文档不依赖开关。关闭补图时可继续摘要；关闭新生成时已存图文仍可阅读，必要时明确仅停止新生成，不把旧入口暗中改为收费新流程。

迁移以新增表/可空字段为主，数据库迁移和读兼容先于 worker/前端；检查旧 worker 忽略新增作业或错误消费的风险，消费者注册完成后才启用新作业。已有数据不自动批量重新摘要/补图/分类。

回退停止接受新 v2 请求并让在途任务明确完成/取消，保留已发布结果、用户版本和来源；不删表、不清所有缓存、不回滚用户内容。启用前后真实配置/依赖/网络/用户身份分别验收。部署属于另行授权的动作。

S6 退出：S0–S4、S5A 的相关检查与完整摘要体验通过；S5B 独立标为后续/已验收，不假装本轮完成。剩余真实验证/人工审核清楚列出，不能把待验项记完成。缺凭据时功能可实现并完成 fixture/本地验证，但“真实 B 站成功”和质量提升结论保持未确认。

## 10. 工作包交付清单

| 工作包 | 必须交付的内容 | 本文创建时状态 |
|---|---|---|
| M0 | 基线/WIP/能力/契约与样本核对记录 | 未实施 |
| S0 | 来源与文档模型/校验、迁移、版本修订与投影兼容 | 未实施 |
| S1 | 身份/分 P、字幕选择解析、耐久接力、fallback/refresh/cache | 未实施 |
| S2 | v2 摘要配方、发布状态、导入/库/详情、共用活动投影/快照基础 | 未实施 |
| S3 | 关键帧/图文、受控配图、授权图资源、CAS/预算、导图/回放、有序可恢复活动 | 未实施 |
| S4 | 自动领域分类、用户标签/别名/合并/决定、全库筛选 | 未实施 |
| S5A | 选段注释/问答快照/选块修订、共用侧栏专注、问答/修订实时活动 | 未实施 |
| S5B | 后续标签范围、授权成员、全文/hybrid 择路 | 未实施，后续安排 |
| S6 | 自动/集成/浏览器/真实平台/人工质量报告和回退说明 | 未实施 |

每批结束更新本文状态或单独交接记录，但不能回写成“从一开始已实现”。主开发契约落入仓库的对应公开文档；此规划继续记录取舍和进度。

## 11. 可复制的新会话启动提示

### 11.1 第一批：来源与摘要主流程

```text
请在 VidLens 仓库根目录实施“视频摘要体验改造”第一批 M0、S0、S1、S2。

先读：
docs/planning/2026-10-09-summary-product-plan.md
docs/planning/2026-10-09-summary-implementation-guide.md

这些是待实施设计，先核对当前源码、HEAD、适用说明和 WIP。本轮允许实现与必要测试，保持用户原有修改；不要提交、推送或部署。

范围是 B 站输入和本地上传兼容，不扩展 YouTube；字幕优先是来源策略。先完成统一文字来源、轻量结构化摘要及旧修订兼容，接受可选用户处理要求和形式偏好；复用现有成果组件/摘要版本/作业能力，避免复制第二套成果流程。正确保留分 P；可用字幕跳过 ASR；持久自动推进；摘要不等索引；前置导入、视频库和详情入口。新文档与 Markdown 投影一致，预览/应用/撤销可用。本批先文字基础，未实现的关键帧/图文/分类入口不要冒充可用。
按产品规划 7.1/7.2 与实现指南 4.2.1/4.2.2/4.4/4.5 接入统一主题、1440px/1024px 桌面专注视图和动态活动投影，按真实动作出现短标题，无预设等待列表。原型交接文件是交互参考，模拟进度不能进入生产数据路径。

持续做到第一批退出条件，分别报告实现、自动测试、数据库/浏览器验证、真实 B 站测试与未验证部分。必要配置不足时完成可独立验证的工作，明确缺失证据，不伪造实际成功或质量结论。最后写清下一批接点和剩余任务。
```

### 11.2 第二批：关键帧、图文与导图

```text
请继续 VidLens 仓库的视频摘要体验改造，完成实现指南 S3。
先读 2026-10-09-summary-product-plan.md、2026-10-09-summary-implementation-guide.md（均在 docs/planning）及上一批交接记录，重新核验 S0–S2 的代码和证据。

允许本批实现与必要测试，不提交、推送或部署；保持 WIP。复用视觉调查和持久 Agent 记录，按内容与要求调查并实际看图，保存授权截图/图注/回放时间；支持几张关键帧、图文、纯文字表达；全篇共享预算，图文先文字后图，关键帧完成须有实际选图，失败明确降级。用户修订不被覆盖；Agent 组织概念层级，导图由同一摘要结构派生，结构修订同步生效。关闭视觉或缺能力时保留可读文字。摘要工具开始/结果驱动动态短标题和完成状态，public_title 按需回退；复用持久记录/游标完成幂等恢复，不能靠前端计时器制造步骤。

通过 S3 的退出条件后报告真实画面/浏览器/人工审核与模拟测试的不同证据，写下一批交接。
```

### 11.3 第三批：自动分类、问答衔接与综合验收

```text
请继续 VidLens 仓库的视频摘要体验改造，完成实现指南 S4、S5A、S6；S5B 后续另行安排。
先读 docs/planning 下的 2026-10-09-summary-product-plan.md、2026-10-09-summary-implementation-guide.md 和前批交接，核对当前实现与 WIP。

允许实现与必要测试，不提交、推送或部署。Agent 自动创建/复用少量领域标签、去重，建立共用名称/别名唯一空间及拒绝决定；人工修改/拒绝保持，自动合并满足等价与用户保护规则，人工标签语义合并需确认；全库 AND/OR 筛选分页。分类失败不影响摘要。

问答页增加可折叠摘要面板，选段作为上下文注释，问题单独输入；发送前验证版本/权限，接受后保存真实快照；选段不自动发送，保留草稿。普通问答不修改摘要，明确 Agent 修改才走预览/应用/撤销。复用现有 Chat/成果交互，不另建聊天或成果平台。
问答支持放大/恢复布局，与摘要专注切换时保留会话、输入、注释、滚动与在途请求；按 4.2.1/4.2.2/7.4 接入公开短标题，问答 token 输出与活动独立更新，修订按实际 planner/工具/校验展示，不预摆固定步骤。沿用既有事件和快照；1440px/1024px 桌面、统一主题和减少动态效果一并验收，本轮不做手机。

完成相关 PostgreSQL/后端/前端/浏览器验证，并按固定样本记录真实 B 站流程和图文质量对照。缺真实凭据或人工审核时如实列待验，不用模型自评替代人工。最终报告每个工作包的状态、证据、风险和后续事项。
```

### 11.4 后续：知识库标签范围与上下文择路

```text
请在 VidLens 仓库根目录实施视频摘要规划的后续工作包 S5B。
先读 docs/planning 下的 2026-10-09-summary-product-plan.md、2026-10-09-summary-implementation-guide.md，核验统一文字来源、标签及摘要主线已落地。

允许实现与必要测试，不提交、推送或部署，保留 WIP。复用现有知识库/视频库授权集合和向量+BM25/RRF，统一 selector 贯穿准备、Agent policy、恢复、历史与最终保存；标签在授权成员内过滤。按真实 token/模型预算择完整来源上下文或现有 hybrid，不只看视频数量，不把截断内容称全文；无 Embedding 小集合还要调整成员/索引准入。分别报告代码、权限/恢复、真实模型质量与未验证事项。
```

### 11.5 交接记录模板

```markdown
# 视频摘要体验改造交接

- 日期、分支、HEAD、已有 WIP：
- 本批范围与完成工作包：
- 关键实现文件和已落地契约：
- 数据迁移/开关/兼容行为：
- 相关自动检查：命令、结果、环境。
- PostgreSQL、浏览器、真实 B 站、真实模型、人工审核：分别列证据；未做写未验证。
- 现存失败、残留风险及可恢复方式：
- 下一批入口、依赖和明确待办：
- Git/部署状态：未提交/已提交/已推送/已部署，按真实证据填写。
```

公开规划位于 docs/planning/，原型交接位于 docs/design/，可重放原型位于 docs/prototypes/summary-experience-desktop-v2/。新会话直接从仓库读取，生产接入完成后按真实证据更新工作包状态；不把模拟事件或本地浏览器验收写成真实模型/部署完成。
