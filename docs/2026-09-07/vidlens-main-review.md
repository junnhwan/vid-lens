# VidLens main 升级验收与收尾建议

日期：2026-09-07。审查对象：本地 main `f003dab73cb0946b6d551f005c17f7a0d1bbf89d`，历史对照 `933c7d9...HEAD`。本地 origin/main 与 main 一致，未 fetch 验证远端是否有新提交。

对照会话：「挖掘项目升级方向」「分析视觉感知迭代方向」「调研并完善视频多模态流程」。本次读取了三个指定会话的原始用户诉求和最终建议。历史建议不是当前实现证据。

## 结论

已经完成实质升级，值得进入“修复关键缺陷 + 真实效果验收 + 可复现演示”的收尾阶段。还需要完善，但不建议再次整批执行所有路线图或扩张新功能。现有主线足够承载后端与 Agent 项目展示，当前缺的是稳定性、效果证据与能力边界的一致表述。

## 已实现与仍未完成

| 能力 | 当前源码结论 | 下一步 |
| --- | --- | --- |
| 在线视觉取证 | research 工具已重新取原始帧并调用 VLM，保存 query observation | 修复失败缓存及 sufficient 判定；用真实视频测定位和补看效果 |
| 独立证据核验 | 重新读取来源、反证搜索、像素核验、发布前检查已接线 | 修复无关候选导致过度拒答的问题 |
| 引用与播放 | Citation 有 anchor_quote/display_context、模态、时间和来源，新版 UI 有证据展示与播放跳转 | 浏览器验收文字连续性、画面与时间跳转一致性 |
| 记忆与设置 | 新版前端有 BYOK profile 与 memory governance | 验收开关、删除/撤回和后续召回一致性 |
| Agent 模式 | 前端已接入普通 Agent、research、evidence_funnel | 主动取原图工具目前在 research；不能把所有模式都称为主动视觉调查 |
| ASR 连续性 | ASR 接口仍返回 string，消费端仍依靠 overlap + 文本 stitch | 尚不是 VAD/词级时间戳/TranscriptAtom 完整方案；先测真实边界错误，再按失败类型补 |
| 后台持久执行 | 有 Run/Step/checkpoint、ResumeResearch，但 HTTP 请求仍驱动执行；未发现 Last-Event-ID 续传 | 不应宣称完整后台 durable runtime；是否建设取决于实际长任务/断网痛点 |
| 跨视频研究 | KB 工作区与跨视频检索可用；研究 Agent 仍单视频 scope | KB RAG 不等于多跳跨视频 Research Agent；暂不必扩展 |

引用展开已不只是旧版的 160 字：`rag_evidence.go:543–545` 同时返回短 anchor 与完整 anchor 的 display_context。不过 display_context 仍是当前 anchor，不是保证句子完整的相邻原文时间窗。旧结论“所有 UI 引用最多 160 字”已不适用；“完整连续性已解决”同样不能成立。

## Standards

以下为实现可靠性问题，非风格建议。

1. **P1：并发 MQ 消费者故障通知存在数据竞争和阻塞。** `internal/mq/consumer_lifecycle.go:97–100` 在 worker 写 firstErr、主循环无同步读；select 没有错误通知分支。ACK 失败且没有后续 delivery 时，循环可能不退出重建 reader。违反 `docs/architecture/reliability.md` 的基础设施异常重投恢复约定及函数自身重建契约。建议用错误 channel/context 唤醒消费循环，统一取消和等待在途任务。
   - 临时 overlay 回归实际产生 `WARNING: DATA RACE`，读写定位 97/100，并失败于 `ACK failed but idle consumer did not exit to rebuild reader`。不是只凭静态猜测。
2. **P1：停止研究后，旧回答可能覆盖新会话。** `frontend/components/chat/useConversationSession.ts:178–190` 的 askAgent 没接 AbortSignal；232–234 的 stop 立即释放 UI；迟到结果仍 agent_replay。`conversationSession.ts:119` 覆盖当前最后一条 assistant。建议传递取消信号，并按 request/session generation 过滤迟到结果和 finally。
   - 当前 reducer 的确定性状态回放复现：旧研究开始 → 停止 → 新会话 → 新问题 → 旧结果返回，显示新问题搭配 OLD ANSWER、old-run，并提前结束 loading。未做浏览器网络复现。

本轴 2 项，最严重的是 MQ 故障恢复与跨会话结果污染。

## Spec

目标来自指定会话：按用户问题定位、观察、按缺口补看、独立核验，并在证据充分或预算用尽时正确停止。

1. **P1：无关的未引用图片也会阻断正确主张。** `internal/service/evidence_inspector.go:96–105,232–243` 要求所有视觉候选都 pixel support，包含反证检索和其他已观察候选。某张无关图为 insufficient，会让已有充分支持的主张无法发布。引用必须支持；未引用候选应区分无关、未知和真实冲突，不应一律否决。
   - 临时 overlay 直接调用 applyInspectionVerdict：相同支持字幕 + 无关文字候选通过；只把无关候选改成视觉来源就失败，实际 got=insufficient。
   - 同处先像素核验再 source_ref 去重，会重复消耗预算，也应一起处理。
2. **P2：失败/仅 captured 的观察缓存阻断重试。** `internal/service/visual_investigator.go:507–512` 对任何缓存状态直接返回；532–550 的暂时 VLM 错误、预算不足也持久化。相同缓存键下，Provider 恢复或新请求有预算仍无法重新观察。应只复用成功观察，失败设重试策略，captured 允许继续 VLM。
3. **P2：sufficient 判定未检查语义缺口。** `internal/service/visual_investigator.go:326–332` 只检查技术失败等条件，未检查 UnresolvedGaps 和必要事实覆盖。“看不清”也可能 sufficient。独立 Inspector 仍可能阻断发布，但调查阶段给 Planner 的停止信号不可靠。

本轴 3 项，最严重的是无关候选造成过度拒答。

另有尚未达到原规划的能力：Investigator 在观察前选完帧，没有内部按 gap 自动改变时间窗/密度/ROI 的迭代；外层 Planner 可再次调用，但不能据此声称完整自适应观察已验收。应先修停止信号，用失败案例决定是否需要 ROI、连续帧或扩大窗口。

## 历史可靠性缺口复核

- Redis 消息去重仍在业务前 SETNX，占位即视为已处理；业务前崩溃的重投仍可能被 ACK 跳过。`consumer_lifecycle.go:232` 起，`idempotency.go:57` 起。
- Producer MessageId 仍是 jobType:taskID，未含 dispatch generation：`producer.go:274`。新派发可能受旧去重键影响。任务状态/lease 调度可能后续恢复，所以本次不直接宣称永久丢任务；需要真实 Redis/RabbitMQ + kill worker 故障演练确定恢复窗口。
- 内容复用仍主要按 MD5 找既有结果，`content_dedup.go` 的锁在找到结果以后才获取，未实现首次 cache miss 的唯一计算所有权。不同模型/语言/版本复用及并发重复费用仍值得单独处理。

上述是旧缺口仍存在，不是本轮新增回归。建议先闭合当前 MQ 可靠性，再决定是否需要完整 Inbox/Outbox；避免为了名词重写所有任务链路。

## 实际验证

| 检查 | 结果与范围 |
| --- | --- |
| 当前工作区 go test ./... | 通过，输出多数命中缓存 |
| mq/service/transcript/ffmpeg 定向 -count=1 | 通过，重新执行 |
| 当前已有 MQ -race -count=1 | 通过；说明已有用例未覆盖新发现故障路径 |
| 新增临时 MQ 故障回归 -race | 失败，数据竞争 + ACK 失败后无法及时退出 |
| 新增临时 Inspector 回归 | 失败，无关视觉候选导致 insufficient |
| 前端 npm test | 25/25 通过 |
| 前端 typecheck | 通过 |
| 前端 lint | 通过，有 1 个 aria-selected/role button warning |
| main 原版 FFmpeg 单测（overlay） | 失败，TestCompanionFFprobePathUsesFFmpegDirectoryAndExtension |
| git diff --check | 通过 |
| 真实视频/真实 VLM 效果、浏览器全链路、生产 | 本次未执行；不能声称通过 |

工作区原有 docker-compose.yml 与 internal/pkg/ffmpeg/ffmpeg.go 未提交修改已保留。本地 FFmpeg 修改修正了测试路径问题，所以“工作区测试通过”不等于“main 原版全部通过”。未修改业务代码、暂存、提交、推送或调用真实 AI Provider。新增回归通过仓库外临时测试及 Go overlay 运行。

当前 checkout 没有找到 docs-private/eval 或 artifacts 的真实评测输入/报告；公开 docs/eval 保存的是规范、schema 和示例。这个结论只针对本机当前 checkout，不代表其他电脑从未评测。

## 建议只推进三步

1. **修复五个新增缺陷，并闭合原有 MQ 去重窗口。** 验收停止/切换会话不串回答；ACK 故障可退出并重建；有效主张不被无关帧挡掉；失败帧可以重试；有未解决事实时不返回 sufficient。对 main 的平台测试保持绿色。
2. **冻结一个小型真实视频问题集。** 起步 30–50 问，包含字幕可答、仅画面可答、小字/PPT、短暂画面、跨分片句子、声画冲突、无证据应拒答。每题标注答案要点、时间窗、决定性帧和应否拒答。记录人工任务成功、引用/时间准确、错误拒答、无支撑结论、P50/P95、实际 VLM 调用/帧数与可得费用。
3. **同一数据集比较三条路径并录可复现演示。** 文本检索 → 离线 OCR/caption → research 在线取证；固定模型/配置和数据版本，逐个解释成功与失败。若新能力无可测收益，先调整定位/核验，不再堆新工具。跑一次重复投递、worker kill、Provider 超时/429 和浏览器断网演练。

达到以下条件即可阶段性收尾：关键回归转绿；真实小数据集有可核对结果；从上传、问答、证据回放到停止/失败恢复的演示稳定；能清楚讲出模块设计、数据边界与失败案例。无需等待跨视频 Claim Graph、Evidence Reel、MCP 或更多 Agent 模式全部做完。
