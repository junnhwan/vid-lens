# Chat 与自主 Agent

产品只有 `chat` 与 `agent` 两种执行模式。知识库是检索范围，支持 Chat 与 Agent；Agent 可以在单视频或当前授权的知识库集合内执行。

知识库 Agent 在 policy snapshot 中冻结 member_task_ids，在规划、工具执行与最终保存边界检查当前成员和索引就绪状态。成员集合变化后，当前 Run 停止并要求重新提问。检索工具默认覆盖集合，也可以指定 task_id；窗口和按需视觉工具必须指定授权成员的视频标识。会话消息用于展示，模型上下文按当前成员集合加载。

## 执行边界

`ConversationExecution` 解析模式、准备当前用户 AI profile/client，并传递取消上下文。Chat 使用摘要、近期对话和一轮检索管线自然回答；Agent 的同步接口和 SSE 接口均调用 `VideoAgentService.RunAgent`。

`video_agent_loop.go` 实现有界 Planner / Tool / Observe。默认最多 32 个工具步骤、8 次重规划，最后一个工具位置预留给最终回答。对话最多 65 次模型调用（包含 Planner 和最终回答），学习笔记最多 32 次模型调用。默认总时长 1200 秒、累计输入 262144 token、累计输出 65536 token；视觉帧默认仍为 8。自定义上限为 64 个工具步骤、1800 秒、累计输入 1048576 token、累计输出 131072 token。服务端配置或 Profile 可以覆盖预算，现有运行使用冻结值。

默认新运行冻结 `convergence_version=1`：连续三次成功取证没有新增来源或扩展同一来源正文时，跳过下一次模型规划，使用正常的 `build_cited_answer` 整理已有证据并说明缺口；模型选择重复动作或已覆盖的转写窗口时，同样转入收尾。最终成功的停止原因为 `evidence_stalled`，不表示所有细节已确认。证据持续增长时仍可使用完整预算。恢复从已保存的 observation 重建进展；没有该版本字段的历史运行保留旧合并与循环规则。

规划上下文保留最近步骤的有界检索词、片段与窗口参数。Planner 使用 1024 token 的结构化 JSON 调用；现有 Qwen 3.5/3.6/3.7 适配器为这类调用关闭思考输出，最终回答保持原有调用模式。工具与引用校验仍在服务端执行。

模型上下文窗口限制单次请求容量，任务 Token 预算限制所有调用的累计用量，两者独立；增加上下文容量不会自动增加任务预算。最终回答的调用、Token 和时间预留计入总额。模型预算先耗尽时，不再调用模型，事务保存已有证据摘录和缺口说明，并保留 budget_exhausted 终态。最终回答成功后立即结束，不再调用 Planner。总超时与模型、工具、检索、视觉预算冻结在 run 中，journal 对每个持久动作执行预算和 lease/CAS 校验。

## 工具

| 工具 | 能力 |
| --- | --- |
| `search_transcript` | 检索当前视频文本 |
| `get_transcript_window` | 加载转写命中附近的上下文 |
| `search_visual_evidence` | 检索已索引的 OCR / 画面描述 |
| `inspect_visual_window` | 读取已有时间窗视觉证据 |
| `investigate_visual` | 按 seed windows 获取源视频、抽帧、VLM 观察并保存或复用结果 |
| `build_cited_answer` | 根据服务端确认的已观察证据生成最终答案 |

`investigate_visual` 仅在服务端装配后进入白名单。源视频或视觉模型不可用时，利用文本证据并说明限制。它仍需要已有时间定位，不是无索引全片视觉搜索。总结、对比和批判使用同一个循环与最终回答工具，没有专用中间生成器。

## 发布、记忆与恢复

最终回答基于本轮已观察并通过校验的 evidence 生成。工具参数严格校验，引用在执行与观察边界根据已观察证据 canonicalize，拒绝授权范围外或未观察引用。公开引用保留真实来源、模态和时间范围；没有依据时不生成引用。

`saveAgentRunExchange` 用 `CreateAgentRunExchange` 在事务中保存一对消息，按 run 去重。成功后刷新近期消息，并按有效记忆策略提取明确用户偏好。SourceRef 指向真实 user message。同一完成 run 重放不再调用模型、重复保存或提取。

[长期记忆](agent-memory.md) 保留服务端能力、用户偏好和会话覆盖；默认不自动开启。在 owner-scoped run 确认后召回有限可信 snapshot，进入 Planner 和最终生成。记忆低于当前证据，持久化执行记录只保存 identity/version/IDs。

Run/Step/ToolCall 是执行恢复权威记录。当前循环冻结 `engine_version=2`；未完成 Run 通过重新提问创建新的执行上下文。完成消息及引用由会话快照和执行记录提供回放。Agent 执行由 HTTP 同步或 SSE 请求驱动。

## 知识库工作区与运行诊断

知识库工作区提供来源卡片、实时命中高亮、按视频分组的引用和带毫秒参数的原视频回放。`GET /api/v1/chat/sessions/:session_id/runs/:run_id` 返回 owner-scoped 运行状态、工具预算、模型/检索次数、用量来源与安全步骤概要，不暴露内部检查点。
