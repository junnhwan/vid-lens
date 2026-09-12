# Chat 与自主 Agent

产品只有 `chat` 与 `agent` 两种执行模式。知识库是检索范围，支持 Chat 与 Agent；Agent 可以在单视频或当前授权的知识库集合内执行。原模板、research 和固定漏斗不再对应独立在线引擎。

知识库 Agent 在 policy snapshot 中冻结 member_task_ids，在恢复、规划、工具执行与最终保存边界检查当前成员和索引就绪状态。成员变化后拒绝继续旧 Run，需重新提问。检索工具默认覆盖集合，也可以指定 task_id；窗口和按需视觉工具必须指定授权成员的视频标识。知识库旧消息继续仅供展示，不向模型注入可能包含已移除视频的历史正文。

## 执行边界

`ConversationExecution` 解析模式、准备当前用户 AI profile/client，并传递取消上下文。Chat 使用摘要、近期对话和一轮检索管线自然回答；Agent 的同步接口和 SSE 接口均调用 `VideoAgentService.RunAgent`。

`video_agent_loop.go` 实现有界 Planner / Tool / Observe。默认最多 8 个工具步骤、2 次重规划，最后一个工具位置预留给最终回答。模型预算先耗尽时，不再调用模型，事务保存已有证据摘录和缺口说明，并保留 budget_exhausted 终态。最终回答成功后立即结束，不再调用 Planner。总超时与模型、工具、检索、视觉预算冻结在 run 中，journal 对每个持久动作执行预算和 lease/CAS 校验。

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

最终回答不再经过 Inspector 或 Claim 账本。工具参数严格校验，引用在执行与观察边界根据已观察证据 canonicalize，拒绝授权范围外或未观察引用。公开引用保留真实来源、模态和时间范围；没有依据时不生成引用。

`saveAgentRunExchange` 用 `CreateAgentRunExchange` 在事务中保存一对消息，按 run 去重。成功后刷新近期历史，并按有效记忆策略提取明确用户偏好。SourceRef 指向真实 user message。同一完成 run 重放不再调用模型、重复保存或提取。

[长期记忆](agent-memory.md) 保留服务端能力、用户偏好和会话覆盖；默认不自动开启。在 owner-scoped run 确认后召回有限可信 snapshot，进入 Planner 和最终生成。记忆低于当前证据，历史仅保存 identity/version/IDs。

Run/Step/ToolCall 是执行恢复权威记录。新循环冻结 `engine_version=2`；旧未完成 run 明确拒绝续跑，用户需重新提问。旧完成消息及引用继续由历史适配器读取。没有新增后台消费者来执行旧引擎。

## 研究工作区与运行诊断

知识库工作区提供来源卡片、实时命中高亮、按视频分组的引用和带毫秒参数的原视频回放。`GET /api/v1/chat/sessions/:session_id/runs/:run_id` 返回 owner-scoped 运行状态、工具预算、模型/检索次数、用量来源与安全步骤概要，不暴露内部检查点。
