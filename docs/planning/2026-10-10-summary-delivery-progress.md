# 摘要体验实施进度

更新时间：2026-10-10，综合验收仍在进行。依据[实施指南](2026-10-09-summary-implementation-guide.md)逐项记录实现与证据；工作区实现、fixture、PostgreSQL、真实模型、浏览器和人工质量审核分别判断。范围为 M0、S0–S4、S5A、S6；S5B 后置。

## 基线、提交与隔离

- 基线 `881a13ca0df28be57e0357a64daada58dfec065a`，分支 `codex/summary-experience`。本轮按能力本地提交；没有推送、合并或部署。
- 已核对本地提交：`f4c5c3d` 来源/文档纯契约；`2362e41` B 站身份与字幕适配；`a8847cb` 来源、生成、标签、注释持久模型；`3a77cb0` 事务发布与用户修订；`6f590d7` 统一读取与范围化修订上下文；`6ee60e1` RAG 来源投影与通用时间线修复；`b5d332d` 前端标签/冻结注释；`46a2582` worker/生成与视觉接力；`c57af01` HTTP 生成读取及用户分类契约；`f37ae04` 新摘要受理/视觉开关；`e11ac73` 视觉事实字段规范化及有界格式修复；`e458138` 摘要阅读工作区与按用户/任务/会话隔离的跨路由问答状态；`e0ade11` VLM parser 窄修。`4ebe508` 显式预算的视觉单独重试；`17b1d72` 旧引用返回的确切文档版本保护；`131ddd3` 修订输出预算、实际用量与绝对时长；`1d992a9` 有界冻结词表及自动标签移除后的恢复建议。视觉重试已有自动和隔离数据库验证，真实 task 4 单次重试已经通过（见下），仍保留图描述质量边界。
- 原有 `docker-compose.yml`、`frontend/package.json`、`frontend/package-lock.json` WIP 保留；不擅自覆盖或整体纳入能力提交。
- PostgreSQL 回归使用 `vidlens_summary_test`，每个测试创建、删除自己的 schema。真实应用验收另用独立数据库 `vidlens_summary_acceptance_20261010_86c475`、独立 Redis 容器、RabbitMQ vhost 与 MinIO bucket，没有修改原应用任务或设置。
- 实际 `cmd/server` 已使用复制的私有临时配置完成迁移与启动，监听 `127.0.0.1:18080`，指标 `127.0.0.1:18081`。配置、登录凭据和 token 在权限为 600 的临时文件中，报告不包含凭据。

## 当前实现与退出条件

| 工作包 | 当前实现 | 已有证据与尚缺条件 |
|---|---|---|
| M0 | 基线、WIP、主线、API/模型接点与样本边界已核对 | 公开规划和实现指南可读；交付记录不把模拟原型算成业务成果 |
| S0 | 不可变文字来源/cue、同一 v2 摘要文档与 Markdown/hash 投影、来源冻结、旧缓存双向隔离、typed 修订/选块范围、预览/应用/撤销/导出兼容 | SQLite 与独立 PostgreSQL 约束/事务回归通过；新来源不混用旧 ASR。真实 ASR cue 冒号 ID 误拒已修复。真实浏览器的修订应用/撤销/导出仍待验 |
| S1 | 新 B 站自动导入选项、请求幂等、最初 generation 受理回执、冻结 profile/偏好/预算；下载→来源→字幕或 ASR→摘要原子接力、补投/lease/fallback；本地上传仍走 ASR | fixture 覆盖可用字幕零 ASR、无 ASR 配置的字幕路径、重复投递/回滚/旧 lease。此前匿名样本无可用轨；登录态实测已取得 74 cue 中文字幕，尚未完成下载/自动摘要接力或证明零 ASR；完整自动 URL 接力仍待真实验收 |
| S2 | strict JSON 主生成、完整来源的短/长上下文分支和检查点、有界修复；无需 RAG/Embedding；先发布 text_ready；同一 generation 的实际步骤与有序事件；手动再生成重新冻结当前来源/同一显式 profile，并保留旧结果/用户 head | 生成/恢复/CAS/预算和游标权限回归通过。真实 22 秒 ASR 来源已完成一次模型生成与持久发布。完整首次导入业务链、刷新不重复生成、1440/1024px 专注布局恢复仍需浏览器实测 |
| S3 | 同一 run 的真实视觉 hook、有限窗口与全篇预算、抽帧/VLM 检查/选择、受控截图引用和授权读取；失败保留文字并给明确 fallback；正文/目录/导图同一文档，阅读切换不发模型请求 | fixture 覆盖共享预算、重投、来源替换、截图权限和生命周期。task 3/4 保留实际视觉失败证据；task 5 已真实图文发布，3 次 LLM＋2 次 VLM，1 图、generated version 2，授权读取与浏览器正文/图/同内容三节点结构已观察。VLM parser 窄修已提交、两份真实响应离线重验通过，旧记录未重写；cue 覆盖和 supports 语气仍有质量边界，回放动作未验。视觉单独重试已本地提交：新显式预算、同 key 受理复用、视觉检查点与原稿 CAS；自动/race/隔离 PG 通过，真实 task 4 重试已通过，图描述仍有小误差 |
| S4 | 用户词表/别名/CAS/合并/拒绝与保护；最多五个候选；摘要同事务冻结 tag intent，随后按同一任务 lease 消费；失败不使正文回退；全库 AND/OR 筛选/分页与 UI | 独立 PostgreSQL 并发、唯一约束、人工冲突、跨 generation supersede、租约/取消、过滤计数回归通过；标签 UI 自动测试通过。隔离账号标准 HTTP API 已实测创建/别名/重命名/合并/手动分配、CAS、AND/OR 全库过滤和分页，保留现有自动标签及摘要。真实分类质量和认证浏览器拒绝/合并/筛选恢复仍待验，HTTP 不代替 GUI |
| S5A | 服务端选段版本/hash/Unicode 范围与权限验证，问题/注释分离、执行和历史冻结；普通问答不改摘要；显式选块修订走既有预览/应用/撤销；独立视频问答页与详情共用有界临时态，按用户/任务/会话隔离，注销清理 | 后端与跨路由/账号/会话前端回归通过，相关前端已本地提交 `e458138`。task 2 真实模型 HTTP＋隔离 PostgreSQL 已验证附选段问答、冻结历史、普通问答不改摘要和单块 preview/apply/undo；原生成稿整行未变。回答仍有超出原文明示的解释。浏览器问答 delta/活动并行、修订动作和完整专注恢复仍待验，HTTP 不代替 GUI |
| S5B | 后续知识库标签范围、全文/hybrid 择路 | 不在本轮完成条件，不把已有 RAG 视为该能力已完成 |
| S6 | 已有后端/数据库/前端自动测试、隔离真实服务与有限浏览器/真实模型证据 | 完整桌面验收和人工质量审核未完成；用户已要求恢复继续，工具当前仍返回 paused（无恢复接口）；不能宣告整项完成或发布就绪 |

任务卡/详情已补齐来源可用性：已发布 active source 即使没有 ASR 投影也可在空闲时生成；新任务专属生成排队/运行时，上一份正文仍可读，同时本次运行继续显示真实状态。旧共享缓存的生成中隐藏规则保留。

## 已执行验证

### 自动测试和 PostgreSQL

- `internal/textsource`、`internal/summarydoc`、`internal/pkg/ytdlp`、`internal/pkg/remoteurl` 的纯契约/HTTP fixture 测试，以及对应 race/vet 已有通过记录；没有把本地 HTTP fixture 写成真实 B 站。
- 来源、导入、主生成、视觉、typed 修订、注释、标签、MQ 接力、删除和旧缓存相关服务/仓储/handler 回归通过。本次 ASR cue 修复后，`summarydoc/textsource/service/repository/mq` 五模块全测、定向 generation race 和 `summarydoc/service` vet 通过。handler/server 的已执行检查另有记录；最终整仓 `go test ./...` 仍应随最后集成重新检查。
- 独立 PostgreSQL 验证迁移、来源原子发布/回滚、媒体/租约不符、导入并发受理/回执不可变、generation 发布与 tag intent 同事务回滚、初始 dispatch/重试预算与租约竞争，以及截图登记和 typed 修订。
- 最新 `TestPostgresTagIntentSupersedesOlderGenerationsWithoutRewritingHistory` 与 `TestPostgresUserTagIntentLeaseAndCancellation` 通过；旧 generation 不再把过时候选覆盖到新结果。
- RAG 的 source/digest 完成围栏、refresh-during-embed 和迟到外部向量写入顺序隔离回归通过；相关 `service/vector/ragtool` 全测通过。只把它记作投影一致性，不当作真实问答质量。
- 六个后端累计提交候选在独立 `git archive` 副本中逐批编译通过，避免遗漏 helper。worker 快照的完整服务检查另暴露 RAG 映射依赖，实际提交顺序已经调整；旧批次编译记录不是每个后续新快照自动通过的证明。
- 前端跨路由与阅读工作区提交所用的 staged snapshot 检查为 **175 UI tests / 40 files**、**97 unit tests**，typecheck 与生产 build 通过。覆盖跨详情/独立问答路由的草稿、注释、阅读/消息锚点、普通会话切换、账号/视频隔离、注销和窄桌面默认收起；此记录属于该快照，不自动覆盖之后的视觉重试/parser 改动，也不代替浏览器动作验收。
- 后续部署开关的 config/service/MQ/server/handler 定向回归通过：默认启用、YAML/env 覆盖、非法配置、新受理停止、已受理回执重放、旧入口和正文/历史读取、冻结作业继续完成、视觉 hook/规划/VLM/抽帧零支出及 consumer 重投不重复。该记录不等于已在运行中的隔离服务切换过配置。

本机证据日志包括 `/tmp/vidlens-summary-agent-pg-latest.log`、`/tmp/vidlens-summary-screenshot-pg.log`、`/tmp/vidlens-rag-generation-full-final.out`、`/tmp/vidlens-summary-ui-full-final.log`、`/tmp/vidlens-summary-ui-types.log` 和 `/tmp/vidlens-summary-build.log`。临时日志只供本机复核，不保证另一机器存在。

### 实际服务和浏览器

- 真实独立服务 `/readyz` 返回 200；实际注册、认证 `/user/profile`、`/media/list`、`/tags` 返回 200。不存在的任务 generation/events 返回 404；未认证 profile/generation 返回 401。
- 浏览器已真实登录该独立服务，从 `/library` 打开 `/video/1` 的默认摘要工作区，看到实际失败活动和实际 22 秒播放器来源；已查看导入 modal 的默认高级选项和 B 站 `p=2` 提示。
- 后续浏览器已在成功 task 5 通过真实 DOM 和截图看到 canonical 正文、授权图片及来自同一文档内容的三节点结构。观察仍为浅色；交互确认框阻塞后续动作，不能声称深色、专注切换、回放动作、问答或修订 GUI 已通过。
- 后续实际 API 已读取 task 3 的 summary、generation、task、timeline，均返回 200；文字 ready、视觉 failed 的 fallback 持久状态可供浏览器验收。API 成功不代替浏览器恢复、阅读与交互验收。

### S4 标准 HTTP API 免费验收

- 使用同一隔离服务 owner 2 的标准 `/tags`、`/media/task/:id/tags`、`/media/list` API，新建四个明确带“验收”前缀的测试标签，完成别名设置、重命名和显式 C→B 合并。旧 canonical 名称和新别名保留，别名搜索命中正确标签；通过已有别名创建按契约复用同一个 canonical 标签（200），将另一标签改成该别名被拒绝（409）。过期标签版本和过期任务标签集合版本均返回 409。
- task 2 手动增加 A/B，task 3 手动增加 A/C，均基于读取的 expected version；原有自动/手动关系、建议和分类状态保持一致。合并请求及同 key 重放均 200，结果一致；C 的关系归并到 B，旧 C ID 仍能解析到 B，B/C 同时筛选不会重复计数。
- 合并前选择 A/B，**AND 仅命中 task 2、OR 命中 task 3/2**；合并后 AND/OR 均命中 task 3/2。全库结果与 **page_size=1** 的逐页 ID/顺序及 total 一致，末页为空但 total 保持正确；单 B 标签筛选在合并前仍准确返回 task 2，不受较新无匹配任务占页影响。无分配的测试标签和无匹配 keyword 组合均返回 total 0 / 空列表，没有回退到全部任务。
- 完整词表原有 7 项逐项保持不变；四项测试标签合并后净增三个 active 标签，总数 10，合并历史及测试数据保留，未删除原数据。A/B 的实际 video_count 均为 2，空标签为 0；task 2/3 的有效 summary/document/digest/version/ref 与测试前完全相同，现有人工修订也未改变。
- 全程没有调用模型或 provider 端点，没有新增付费调用。请求/响应及 `evidence.json` 私存在本机 `/tmp/vidlens-summary-acceptance-20261010_86c475/s4-http/`，JSON 权限均为 600，临时执行脚本已删除。该记录是 **标准 API HTTP 验收，不是 GUI 验收**，不补称建议拒绝或标签质量已完成真实验收。

### 真实 S5A HTTP 与数据库验收

- 在同一隔离服务，owner 2 / task 2 / session 1 / default profile 1（`qwen3.6-flash`）先读取服务端 canonical block context，再用确切 Unicode 范围、quote、generated version 1 和 document/block digest 提交 **一次普通问答请求**。HTTP 200，约 **36.92 秒**；数据库持久保存两条消息，历史注释中的 quote、版本、来源 ID/digest、来源标题、章节标题、task/session 均与接受时一致。普通问答前后有效摘要和原生成稿未变。
- 问答的实际 `ai_call_logs` 为 **2 次 LLM＋3 次 embedding**，均 success；LLM 时长 16013ms / 20262ms。prompt/completion/total tokens 与费用字段均为 null，实际用量和费用记为 **unknown**，不把缺失用量记为零。
- 同一生成稿上仅提交 **一次显式选中 `block-1` 的 preview edit-run**，携带 expected revision、content digest 和 version ref。服务端首个模型 patch 未通过 `invalid_patch` 校验，已有运行内的有界第二次规划修复成功，没有重新提交运行。实际 preview→apply→undo 均成功，修订号 **0→1→2**；正文变为三条要点并保留对最终效果的疑问，没有新增教程步骤。章节 ID/标题、来源引用与其它文档区域保持一致；preview 不写 head，应用后的 JSON、Markdown、digest 与预览一致，撤销完整恢复原文档/Markdown/digest。
- 直接查询独立 PostgreSQL 核对 operation 的冻结 base/scope、两个完整 v2 revision、当前 head 和 undo revision；问答历史在修订及撤销后仍保持接受时快照。原 `ai_summaries` 完整行（含 document JSON）前后相同，SHA256 为 `f7b6d0a2b048de922c366dc1e46463bf849e69062930aa45a489be93ef77619d`。任务读取接口的 `task.summary` 会投影人工修订，原生成稿是否变化以实际数据库行核对。
- 编辑共 **2 次 LLM**，实际时长 27539ms / 29833ms；调用记录的实际 tokens 为 unknown。run 上 prompt 3038 是 **estimated** 预算计数，completion 0 不是已确认的实际零用量，费用仍 unknown。
- 人工质量边界：回答正确区分“能做出软件”和“对效果有疑问”，但进一步解释为功能完整性、代码结构、稳定性、用户体验，以及“重结果验证、轻指令形式”，这些并未在 22 秒原文明示。不能把链路通过写成完全符合原文的回答质量。初次临时 harness 的 `mode=natural` 在模型准备前被拒绝，确认无模型调用后改成标准 `mode=chat`；免费前置错误诊断保留，没有付费失败循环。
- 私有响应、数据库快照和摘要证据保存在本机 `/tmp/vidlens-summary-acceptance-20261010_86c475/s5a-http/`，临时可运行脚本已删除；没有操作原应用数据库、账号或配置。本次是 **真实模型 HTTP＋数据库验收，不是 GUI 验收**。

### 真实 22 秒来源和模型

- 样本为现有真实媒体的 **0–22 秒派生片段**，原媒体 MD5 与既有真实来源记录核对；新片段有自身媒体指纹并上传至独立 bucket。它不是完整原视频，不用于声称全片摘要成功。
- ASR 来源为实际识别/对齐记录适配出的 7 个 cue，约 152 字，`timing_method=forced_alignment`，最晚结束 21920ms，来源警告为空。不是新造字幕或字符比例推算时间；本次摘要复验复用该真实 ASR 来源，未证明本轮重新执行 ASR/fallback。
- 前两轮真实主生成失败：结构校验错误地给 `asr-window-0:start:end` cue ID 套用 block ID 正则。没有发布半份文档，也没有修改模型候选冒充成功。修复后，cue ID 作为受限 opaque 值保留；仍须存在于冻结来源，block/figure ID 规则没有放宽。原候选离线重验通过只是诊断证据。
- 修复后的一轮完整 22 秒文字复验已经通过真实 `SummaryGenerationService.Generate`、规范校验、原子文档发布及同 lease 的作业完成：隔离 owner 2 / task 2 / profile 1，source `d0b3f0ae-f1ee-4179-a81f-94b4487f9040`，generation `9424a825-fa6b-4d3c-a909-a23eed8742dd`。上一失败 task 1 保留原失败证据。
- 一次真实 `qwen3.6-flash` 调用约 **8.237 秒**，记录 prompt/completion tokens **1018 / 1164**；7/7 cue 引用通过冻结来源校验，作业 completed 且 lease 已清。该计数证明引用合法与覆盖，不代替摘要关键点/条件/编造的人工质量评审。
- 实际 profile 的 LLM/ASR/Embedding/Vision 配置已在隔离账号下重新加密；配置可解析不代表所有供应商动作都已执行。本轮已执行真实 LLM、Embedding 和 VLM，仍未以该记录证明重新 ASR 或浏览器问答/修订动作。
- 另一次有界的真实 image_text 请求使用新的隔离 task 3，source `c9004d42-d891-453c-aaf2-35d1f3321f60`、generation `1f793e4f-f7d3-46e2-820b-527481c5cdfa`。文字和标签接力已完成，job completed/lease 清；视觉 planner 严格 JSON 解析返回 `invalid_visual_response`，结果诚实降为 text、`visual_state=failed`。记录 2 次 LLM、input/output tokens 1868/1720、约 12.753 秒，0 截图/0 VLM。模型标签候选总计五个：同 task/generation/version 的 suggestion 历史中四个 accepted 并成为当前 assignment、一个 pending；不是九个候选。task 2 的原冻结文档和任务保持一致，没有修改 provider 候选冒充图文成功。
- 随后的真实 task 4 显式打开导图/视觉/标签，但视觉 leaf 的 `field_type` 响应仍未通过格式校验，保留失败事实，不能写成图文成功。
- 真实 task 5 已完成图文生成：**3 次 LLM＋2 次 VLM、约 32.779 秒**，发布 **1 张图、generated version 2**。受控截图认证读取 200、跨 task 读取 404、未认证 401；浏览器已看到该正文与图片。该成功不消除 task 3/4 的失败记录。
- task 5 的引用覆盖为 **5/7 cue**；约 **20 个字符的反问**未被引用，不把合法引用计数当成完整覆盖或全片质量通过。真实画面显示 **Claude Code**，ASR 是 **Cloud Code**，应保留来源差异，不能静默改写 ASR 充当一致证据。部分 `supports` 用语加强了原意，仍需质量审查。实际 VLM 输出的 parser 缺口已窄修并本地提交 `e0ade11`；两份原始真实响应分别离线解析得到 **4 facts / 3 gaps**，旧运行记录未重写。离线重验不等同于修复后重新完成一次真实视觉运行。

## 待补证据和回退边界

1. 真实 B 站可用字幕样本：正确分 P/身份、字幕选轨、无需 ASR 配置、ASR 调用为零，以及完整导入→来源→主生成自动接力；登录/风控/无字幕的真实原因分别记录。此前无 cookies 未取得可用轨；用户补充登录态后首个样本已取得真实中文字幕，完整 URL 导入接力仍待验。
2. 在成功 task 2/5 上继续认证浏览器回放动作、刷新、专注恢复和失败后重新生成；已有正文/图/结构观察不代表这些动作完成，继续核对新旧 result generation。
3. 完成 task 5 的 cue 缺失、画面/ASR 名称差异和 supports 语气审核，以及已提交 VLM parser 修复后的实际运行、已实现的视觉单独重试真实验收。进一步覆盖图表/代码/设置界面与纯讲述样本，记录 usage、预算、fallback、相关性、重复和可读性；不由单图成功外推所有样本。
4. 真实 HTTP 的选段问答、冻结历史、preview/apply/undo 已通过；仍需对应浏览器交互、delta/活动并行和跨路由恢复，以及实际回答的原文边界质量审核。
5. 1440px/1024px、浅深色、键盘/嵌套 Esc/焦点和 reduced-motion；摘要/问答专注切换保留草稿、选段、滚动、播放和在途请求。测试不能只覆盖 DOM 断言。
6. 最后集成后的全量后端/前端验证、认证浏览器标签操作/建议拒绝与分类质量、质量对照和人工审核。标签创建/别名/重命名/合并/手动分配及过滤分页已有标准 HTTP 实测，不再列为未执行；尚无质量提升统计结论。
7. 指南 9.4 的新 v2 受理与视觉部署开关已实现、定向回归通过并本地提交 `f37ae04`，见[启用与回退说明](../operations/summary-rollout.md)；实际部署切换尚未验收。兼容默认启用；关闭新受理不影响旧/新结果读取，同 key 已受理回执可重放，冻结作业允许继续完成；关闭视觉保留文字并明确 skipped。混合版本 worker、实际启用/停止新请求/回退实操尚未验收；不删表、来源或用户版本，不自动批量重算旧任务。

## 隔离服务的安全重编译与同实例重启

隔离服务已在前序模型调用结束后按同实例流程重编译、重启，并完成后续 task 3/5 和 S5A 实际读取及模型验收；旧 PID 不作为当前进程身份。下面步骤用于后续再次集成后的安全重启，须先等待当时的模型/业务验收结束，并确认检查通过。保持同一数据库、vhost、Redis、bucket 与登录状态，不重新创建资源或改原应用配置。

1. 在仓库根目录先编译新二进制；编译失败不停止旧服务：

```sh
go build -o /tmp/vidlens-summary-acceptance-server.next ./cmd/server
```

2. 再执行下面脚本。它先校验旧 PID 的完整命令，向该隔离进程发送 SIGTERM，等待退出，原子替换二进制并用原来的私有启动脚本重启。PID 不符或停机超时即中止，不扩大杀进程范围。

```sh
python3 - <<'PY'
from pathlib import Path
import os, signal, subprocess, time, urllib.request

runtime = Path(Path('/tmp/vidlens-summary-acceptance-current').read_text().strip())
config_path = runtime / 'config.yaml'
binary = Path('/tmp/vidlens-summary-acceptance-server')
next_binary = Path('/tmp/vidlens-summary-acceptance-server.next')
if not next_binary.is_file():
    raise SystemExit('Compile the candidate binary first')
pid = int((runtime / 'server.pid').read_text())
command = subprocess.check_output(['ps', '-p', str(pid), '-o', 'command='], text=True).strip()
expected = f'{binary} --config {config_path}'
if command != expected:
    raise SystemExit('PID command differs; do not stop this process')
os.kill(pid, signal.SIGTERM)
for _ in range(90):
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        break
    time.sleep(0.5)
else:
    raise SystemExit('Graceful shutdown timed out; do not replace or force-kill')
os.replace(next_binary, binary)
process = subprocess.Popen(
    ['python3', str(runtime / 'start.py')],
    cwd='/Users/jacob.z/misc/vid-lens',
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    start_new_session=True,
)
(runtime / 'server.pid').write_text(str(process.pid))
os.chmod(runtime / 'server.pid', 0o600)
for _ in range(45):
    try:
        with urllib.request.urlopen('http://127.0.0.1:18080/readyz', timeout=2) as response:
            if response.status == 200:
                print('Isolated backend ready; PID', process.pid)
                break
    except Exception:
        pass
    time.sleep(0.5)
else:
    raise SystemExit('Not ready; inspect the private server.log without printing credentials')
PY
```

`start.py` 清除继承的 `VIDLENS_*` 环境覆盖，再设置 loopback 指标地址，使用同一私有 `config.yaml` 并将日志追加到权限为 600 的 `server.log`。该隔离脚本故意不接受外部环境覆盖；测试部署开关需使用单独核对后的隔离配置，不能误把真实环境连接信息注入。重新启动后验证 `/readyz`、认证列表、task 2 文档/事件与 owner fence，再继续浏览器验收。不会因重启自动制造一次新 generation。

## 退出审计的后续窄修

- 视觉单独重试能力提交 `4ebe508` 的完整 index archive 执行 `go test ./...` 通过。该快照的前端 41 文件 / 178 UI tests 与 97 unit tests、typecheck / production build 通过。后端专项 race / vet、认证 handler 和独立 PostgreSQL 7 项验证覆盖同 key 并发唯一受理、旧记录不可变、来源/删除/取消/旧 lease、失败重试检查点，以及截图登记和原稿 CAS 的同事务发布与回滚。真实 HTTP 模型重试未计入此验证。
- “返回原段”现在同时核对 task、正文 digest 和确切 generated/revision 版本，且只在当前阅读容器内定位。文字相同但撤销形成新 revision 时，旧引用不会跳到新正文；引用和问题草稿保留。该提交的独立 index archive 为 41 文件 / 181 UI tests 与 97 unit tests，typecheck / production build 通过，仍不代替 GUI 动作验收。
- 修订预算窄修提交 `131ddd3`：provider 请求限制剩余总输出，实际 usage 回调在成功/无效 patch/截断失败中持久化；无 usage 时保持 estimated/unknown。同步与 worker 使用受理时 CreatedAt 加冻结总时长的绝对截止，续租不延长，过期运行零 provider 调用。27 个定向测试及 race 通过、1 个真实模型 opt-in 跳过，vet 与完整 index archive `go test ./...` 通过。没有新付费调用、没有重写旧用量记录。
- 标签词表与恢复提交 `1d992a9`：14 路径，完整 index archive Go 测试、41 文件 / 182 UI tests 与 97 unit tests、typecheck / build 通过；专项 race/vet、隔离 PostgreSQL 7 项通过。来源 refresh/补配置继续和 v2 force ASR 新受理接点正在修复，尚未计为完成。

### 后续真实 visual-only retry 与登录态字幕检查

- task 4 标准 HTTP 提交一次显式视觉预算，生成 `6e8ac9e4-828b-4ffc-aeef-0dadb4db918e`；同 Idempotency-Key 重放仍 202 且 generation 相同。26.740 秒完成，90 秒 / 2 帧上限内，2 次视觉规划 LLM（input/output 3642 / 649 actual）与 2 次 VLM；VLM tokens 和费用 unknown。文字摘要 LLM、ASR、分类调用均为 0。
- 图文发布 version 1→2：4.560s / 14.480s 两图。旧 run/steps/calls 完整行不变，标题/overview/blocks（除 figures）/order/sourceRefs、user head、tag assignments/suggestions 均不变。owner/task4 图片 200，同 owner 错 task 404，无认证 401。真实页面已观察同文档正文、两图和三节点结构，仍不是回放/缩放 GUI 动作通过。
- 当前 VLM 结构化 facts/gaps 分别 4/3、3/4，不再将整段 JSON 当作 fact。图描述仍有事实边界：第二张实帧只有三个倾斜彩色框，底部普通字幕被 caption 算作第四框；alt 的“推荐”与部分 supports 语气缺乏来源依据。旧图文不回写；后续质量提示已收紧，真实改善不能由提示回归直接宣称。
- 用户补充 B 站登录态后，使用私有 cookies 文件检查首个既有样本 `BV15E411p7Da?p=1` 成功：官方 aid 94226970 / cid 370690024，45 parts，本 part 312000ms；3 轨中选 zh-CN:1，真实 SRT 6852 bytes，74 cues、1625 canonical 字符，6.660–309.500s，usable / 0 warnings。字幕类型保持 unknown，不按 ai 名称猜测人工类型。未在该探测步骤下载视频、验证媒体指纹、调用模型或导入任务，完整业务链另验。
- cookies 只存本机权限 600 的隔离临时文件，不进入仓库、报告或模型输入；隔离配置的 CookiesPath 已更新，下次安全重启才读取。原应用配置保持原状。

### 真实样本人工质量审核与后续提示修复

- task 2/5 的主题、工具和三个软件例子、承认能做软件、对效果的疑问都有保留，但“你可能会想”被加强为“普遍看法/人们通常认为”；task 2“持保留和怀疑态度”比原文疑问更强。
- S5A 回答把未定义的“效果”解释为功能完整性、代码结构、稳定性、用户体验，并概括“重结果验证、轻指令形式”的立场，原文没有明示。普通解释和生成不得把这些当作原文事实。
- 152 字来源对应 task 2 正文 186 + overview 89 字、task 5 正文 156 + overview 53 字；选块修改 186→165 字，约减少 11%，更接近整理而非显著压缩。已有图是工具/任务举例，不能直接证明效果疑问；ASR Cloud Code 与画面 Claude Code 写法不同。
- 后续提示修复提交 `446d8d8` 收紧原文条件/疑问语气、短来源重复、明确标记推断，并约束图 caption/alt/supports 的观察边界与来源差异。完整 service、定向 race/vet 和完整 index archive Go 测试通过；旧记录未改。提示回归只证明规则接入真实路径，不证明真实输出已改善；后续新样本另评。

## 用户真实首个导入反馈后的修复（2026-10-10）

用户要求继续处理七项问题，并优先完成阅读布局和完整生成链路，以真实 `BV1cAp46iEfA` 验收。原 task 6 的失败基线已保留：46 个有时间的 ASR cue、两章平铺、正文 45 个裸露内部标记、零结构化引用、零图片。此前“来源没有真实时间”的文案不能解释这一例；直接阻断配图的是摘要缺少画面来源关联。首次自动生成的实际输出达到固定 2048 上限而截断，手动再生成也未得到合格正文。

| Before | After | Why |
|---|---|---|
| 正文前出现执行记录、配图重试、版本说明与多组操作 | 标题压缩；执行记录和重试进入“生成详情”；次级动作进入“更多”；版本说明移至文末 | 首屏直接开始阅读，仍可查询真实运行和重试 |
| 导图默认折叠，原结果仅两章标题 | 默认展示可点击预览，完整导图共享当前文档层级；生成质量由真实结果另行验收 | 可发现结构，避免仅凭导图组件存在判定成功 |
| 自动标签后跟保留、删除和建议正文 | 阅读时只展示分类名称；建议与改动进入“管理分类”弹窗 | 减少阅读干扰，并保留显式版本检查 |
| 引用/修改/回放按钮抢占章节标题 | 章节操作进入“•••”，截图仍保留直接引用与回放 | 保留内容导航，降低重复工具对正文的影响 |

已本地提交来源刷新 `cee06de`、B站 `ai-zh` 语言匹配 `c862d6d`、阅读布局 `5773c73`。语言修复保留真实 provider 语言、轨道键和 unknown 类型，不据名称伪造字幕类型。真实指定视频已只读取到 `ai-zh:1` 的 328 个 cue、4306 字、1.160–902.960 秒、usable/零解析警告；这一步尚不等于完整导入成功。

新版阅读布局的 42 文件/189 UI 回归、类型检查和生产构建通过。真实浏览器已看到正文首屏、分类名称和默认导图，展开生成详情后真实运行记录位于浮层；“更多→放大问答”执行后菜单关闭且专注视图正常。后续实际视频重新生成与图片、分类质量须继续记录，不由这些检查推断成功。

生成修复与视觉 provider 已本地提交 `2046941`、`12162b5`。新机器生成正文必须包含实质内容、合法结构化引用且不得混入裸 cue 标记；旧原稿与用户修订保持通用契约。标题跟随正文在同一 lease 事务中按空值 CAS 写入，用户标题及主动清空受保护。输出额度按正文/JSON引用需求分配，文字组织、视觉组织及 VLM 均扣同 run 的实际剩余额度；缺失 VLM usage 按申请上限保守计入 estimated，不能记作实际零用量。给定实际 328 cue、1M context、24000 输入/8192 输出/90秒/2帧的 fixture，正常整篇只需一轮文字调用，首轮限额6144，并保留2048给后续；真实模型完成情况另记。

最终独立 index 快照的 `go test ./...`、97 unit/189 UI、类型检查和生产 build 通过；相关生成/视觉/rules race、vet、隔离 PostgreSQL 标题并发 CAS 及发布回滚通过。验收服务保持原隔离资源，已安全重编译并重启，`/readyz` 200。旧任务没有在途作业；真实指定BV的一次标准新导入已授权执行，仍须检查实际内容与图片。


### 指定 BV 的首次完整导入：失败仍按失败记录

标准链接导入 task 7 / generation `b8e898fd-8872-4b2f-91f2-7b7472df7ab9` 已执行一次：download 和 text_source 完成，平台字幕身份、媒体指纹及 328 条真实 cue 均匹配，ASR job 为零。文字 LLM 实际 17260 输入 / 5173 输出 / 31.897 秒，随后未通过结构或来源校验，未发布摘要、分类或图片。现有 checkpoint 只记录 invalid，不能还原首轮具体字段错误；不能把该次失败归因于未经观测的模型字段。修复步骤在调用 provider 前因剩余输入不足停止，服务日志明确 `budget_exhausted`。

这次实际运行证明先前 fixture 的正常路径不能代表真实模型成功。下一步在不扩大冻结预算的前提下压缩字幕重复字段，为失败诊断保存安全校验代码，并优先完成合法正文。原 task 6 的 13 类记录、既有 owner2 任务与原配置均经全行只读对比保持不变。页面真实来源和失败阶段文案窄修提交 `e01301b`，针对该次实际状态的 UI 回归与类型检查通过，浏览器已显示“平台字幕”和“摘要未完成”。

用户已明确要求恢复并继续 Goal，授权工作持续执行；本次 get_goal 仍返回 paused，而现有工具没有 active/resume 操作。不得把工具状态称为已恢复，或为绕开它创建重复未完成 Goal；其余退出条件仍须实际验收。


阅读布局后续 GUI 验证：用户原 task 6 的正常页面执行“放大阅读→恢复布局”，主导航和侧栏隐藏/恢复；1024×768 的实际 Chrome override 经 DOM 核对生效、无整页横向溢出。窄宽度刷新首次进入时侧栏默认收起，真实截图中概览、分类、可见导图与章节起点可阅读；已 reset viewport 并恢复原尺寸。该验证仍使用旧生成内容，仅证明布局，不作为新的摘要质量通过。

视觉规划重复输入窄修 `6f3460b`：同章节关联多条来源时，标题和正文只发送一次，每个合法 cue 身份和精确时序保留；重复同块 cue 去重。定向真实生成 fixture 与对应 race 通过，不由此宣称已经产生真实配图。


### 两次后续候选：技术发布与内容验收分开记录

字幕表压缩提交 `f7ce97e` 后，第二次标准新导入 task 8 / generation `eb4ba94e-bacd-4aeb-b632-c7a22e0d7843` 的两轮文字输出均截断：实际分别12425/6144、12482/2048输入/输出，共24907输入/8192输出/54.515秒。未发布正文、分类或图片。实际输入超过24000冻结限额的记录按原数保留，后续修复增加完成后实际用量检查；不能只凭估算允许下一步或发布。未扩大冻结预算。

语义生成契约提交 `7f81c22`：模型只返回标题、概览、层级正文及cue_ids；输入仅含全部cue ID与原文字，服务器从冻结来源补齐身份与精确时间。历史canonical输出仍须原严格验证；重复JSON key、未知字段、未知/重复cue均拒绝。新增328cue、本地HTTP provider持久化及实际单次/累计超限回归通过；独立HEAD归档完整Go检查通过，隔离服务使用同一配置重启。导入初期状态修复提交 `4257f38`，typecheck及9个workspace UI回归通过。

第三次标准新导入 task 9 / generation `9aeee4ad-f69d-4016-9923-6e69174c5ba5` 发布了文字和标题：完整328平台cue、ASR零调用、3根块/0子块/25精确引用、两项自动分类。文字实际6633输入/1253输出，视觉规划1712输入/349输出，总16.505秒。视觉规划返回3目标超过配置允许的2帧，尚无实际VLM或图片；原任务6和已有失败记录保持不变。

**task9内容验收未通过**：结构层级藏在Markdown而非子块；引用常选过渡句，未支持块内全部论断；漏掉一周总结再一周配置的成本、Skill检索准确的条件、末尾LLM意图识别/状态机执行边界；把不同例子的百分比混接成90→60，并把评测/示例回流增强为训练或自动更新。成功发布不能代替真实内容通过。后续机器新稿加入所选证据百分比检查与命名小节层级检查，仍只能发现明确窄错误，不能证明所有因果论断成立；新的真实结果须继续逐项验收。


## 指定真实视频：第四轮与发布前审核修复

同一 `BV1cAp46iEfA` 的第四轮 task 10 已发布2个主题、7个子概念、46条合法字幕引用。实际链路进入抽帧及2次VLM看图，但在图片选择/发布前耗尽该隔离测试配置的输出额度；实际用量18238输入、8876输出、73238ms，超过冻结8192输出额度的事实保留，未伪造图片或覆盖旧失败记录。该轮不是配图验收通过。独立来源审核仍发现遗漏关键条件及把数据回流加强为“自动提升”的失真，因此也不是语义质量通过。

`01e909c`、`5d42693`、`8e419bf` 加入发布前独立校对：长且完整可容纳的来源，审核同时收到完整原文和语义草稿；小上下文按原文分段逐段审核，并在每次归并后再次对全部已审分段做保真校对，明确不把分段摘要冒称原始全文。所有步骤使用同一持久日志和冻结预算，审核失败不发布草稿、不改自动标题。每次调用及检查点重放只能引用本次提供的cue或参与归并的引用并集。`6f65258` 的回归及定向race通过，4096上下文和原预算未提高。

`5d70010` 调整默认导图预览，丰富结构给予更多画布空间并缩短横向间距；图仍与正文共用真实父子节点。前端类型检查及摘要页15项回归通过。以上仍需要原账号视频6的真实刷新、生成、图片显示及回放验收；不将这些自动验证写成实片效果通过。


## 原账号视频6：基本功能真实验收

从浏览器标准“刷新文字来源 → 文字就绪后继续生成摘要”单次操作启动，沿用原账号user3已有profile5，没有调整额度或手工写入摘要。generation `8ba949e7-f348-4f00-998e-e87d97554488` 完成：新来源 `552926a3-9772-46f1-859c-bd3949c70042` 是指定B站p1的328条平台字幕；新增ASR作业为零。实际4次LLM（正文、独立全文审核、画面计划、画面选择）及3次VLM、3帧，25477输入/9100输出/84076ms，在原冻结65536输入/24576输出/480秒内。

发布6个正文块，其中3个子块；导图默认预览、展开大图及点击节点跳正文已在真实页面操作。3张配图都已实际加载为960×720像素，分别捕获于15.950秒、437.860秒和719.580秒。配图放大与关闭、点击“回放7:17”定位播放器437.860秒已观察；图、截图注册、观测记录、当前来源、媒体、任务及generation一致。原账号自动标题已更新到摘要页、面包屑及播放器。图下保留简短标题，完整画面说明及支持边界收进可展开详情。预览禁用独立平移/缩放，避免阅读滚轮把图移出画布；大图保留缩放与拖动。

只读前后快照核对：user3及owner2配置逐字段不变、两条人工标签及标签决定保留、owner2受保护任务不变。浏览器截图及私有只读证据保存在忽略目录，报告不含cookie、token、密钥或DSN。独立提交快照的后端全量测试与构建、生成定向race、前端类型检查及摘要页15项回归通过。

**上述只证明指定视频的基本导图、配图和阅读链路跑通，不表示7项整体完成。** 独立全文校对后的正文仍把回流写成Agent训练/提示词优化及持续自我改进，且遗漏检索准确、Skill编写条件和完整反馈子步骤；语义质量未通过。用户最新要求先确认基本功能，本轮清楚区分该验收与尚未通过的内容质量。
