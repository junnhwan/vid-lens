# Agent 流式契约

## 请求

- Chat：`POST /api/v1/chat/sessions/:session_id/messages/stream`，请求 `{ "question": "...", "top_k": 4, "mode": "chat" }`。
- Agent：`POST /api/v1/chat/sessions/:session_id/messages/agent/stream`，请求 `{ "question": "...", "top_k": 4, "mode": "agent" }`，支持单视频和已授权知识库成员集合。
- 同步 Agent：`POST /api/v1/chat/sessions/:session_id/messages/agent`，使用相同循环，额外接受 `run_id` 进行 owner/session/goal 匹配的重放。

省略 mode 使用对应端点默认值。Chat 与 Agent 均支持单视频和已授权知识库成员集合，服务端在每个执行边界校验作用域。

## SSE 事件

| 事件 | 范围 | 语义 |
| --- | --- | --- |
| `run_start` | 仅 Agent | run_id、mode、scope_type（附 task_id 或 kb_id）、有效记忆策略 |
| `progress` | Chat 与 Agent | 上下文准备、检索、规划与保存阶段；包含 id、kind、label、status、ts，`detail`/`duration_ms` 在对应阶段有值时出现；Chat 另带执行快照的 input_summary/output_summary；run_id、plan_id、简要决策说明、所选 tool、replan 标记与已有 evidence_refs 仅 Agent |
| `reasoning` | Chat 与 Agent | 模型接口提供的 reasoning 增量；按 call_id 归属（Agent 为 plan-N 或 answer，Chat 固定 answer），独立于正文；run_id 仅 Agent 填充 |
| `step_start` / `tool_call` | 仅 Agent | 实际工具开始执行 |
| `tool_result` / `retrieve_hits` | 仅 Agent | 工具结果摘要与检索命中 |
| `step_done` / `step_error` | 仅 Agent | 步骤终态 |
| `answer` | Chat 与 Agent | provider 生成增量；不支持流式时完成后一次发送 |
| `answer_reset` | 仅 Chat | 已生成部分正文后发生降级，清空暂存正文再接收替代答案 |
| `citations` | Chat 与 Agent | 最终允许显示的引用集 |
| `done` | Chat 与 Agent，字段集不同 | 保存成功。Chat 载荷：message_id、answer、model、profile_id、execution_duration_ms、degraded、degradation_reason、diagnostic_id、memory_policy——没有 run_id，也没有步骤统计。Agent 载荷：message_id、run_id、answer、memory_policy、model、profile_id、execution_duration_ms、degraded、stop_reason、budget_notice、budget 与 trace_summary（steps/tools/retrievals）——没有 degradation_reason 或 diagnostic_id |
| `error` | Chat 与 Agent，字段集不同 | 失败或无法完成；不能视为成功保存。Chat 载荷只有 message；Agent 载荷为 run_id、message、step_id |

`run_start`、`step_*`、`tool_*`、`retrieve_hits` 只来自 Agent 的工具 observer 与 run 身份；Chat 端点没有 run 身份，也就没有这些事件。反之 `answer_reset` 只在 Chat 的档2降级路径发出，Agent 降级通过 `done.degraded` 与 `stop_reason` 表达。

步骤事件只有工具 observer 一个发布位置。Journal 管持久化，不重复发步骤。恢复完成 checkpoint 不重新调用工具。

规划在调用模型之前发送 running，完整 JSON 解析、白名单校验后发送终态。Planner 接收每个工具的 input_schema；检索规范参数为 question，query 作为兼容输入，但两者不允许冲突，其他未知字段继续拒绝。public_summary 是独立的用户可读说明，最多 240 字符，存入 checkpoint；不保存内部 reason 草稿。

模型流通过 ai.StreamResponse 保留 admission/retry/observed 包装器，兼容 delta.reasoning_content、delta.reasoning 和跨块 think 标签。没有 reasoning 的上游只展示执行进度。Planner 的正文 JSON 不进入回答区；仅完成后解析，不能根据部分参数执行工具。

预算快照将最终回答的时间预留落实为 Planner 子上下文截止时间。规划达到该截止或 provider 返回 length 时，服务端丢弃未完成的决策，保留该次用量，以持久检查点转入同一 journal 内的受限收尾。最终 writer 仍受整轮剩余额度和期限约束；不重放规划或成功工具。调用方取消、内容过滤及其他错误保持各自终态。

`answer` 在保存之前发送，因此不是已提交消息。`done.answer` 是数据库中保存的权威最终文本，前端覆盖此前增量，将有效引用规范化为独立的 `[Cn]` 链接标记、清除未知编号并避免答案重复追加。保存失败不会发送成功 done。最终回答工具的 step_done 在发布成功后发送。

取消上下文贯穿 Planner、检索、视觉下载/抽帧/VLM 和生成；取消后不启动下一步。前端保留已收到的部分文本并标识终态。

Handler 是响应的唯一写入者，使用有背压的事件传递并在首事件之后每 10 秒发注释心跳。响应包含 Cache-Control: no-cache, no-transform 和 X-Accel-Buffering: no。生产前端由 `frontend/server.mjs` 转发 API，使用 Node 响应流直接传递上游数据，并向上游请求 identity 编码。上游缺完成标记、前端 EOF 缺 done/error 都按中断处理。

先在 `frontend` 执行 `npm run build`，再执行 `npm run test:stream` 验证实际 Node 代理：上游只有在客户端收到第一段后才能发送剩余内容。测试还会检查 API Range、构建后静态视频的 Range、SPA 深链接与缺失资源/开发预览的 404，因此必须先生成 `dist`。GitHub 自动部署在打包前执行这些成品检查。

## 运行状态、快照与重连

`retrieval_snapshot` 保存版本、run/mode、步骤、引用、记忆 identity 与 policy，供会话展示和引用回放；完整执行恢复依赖 Run/Step/ToolCall journal，不持久化完整记忆正文。

Agent 快照版本为 2，在工具步骤之间保存公开规划摘要。模型 reasoning 仅保存在当前页面内存，不写入快照或对话正文。前端消息内时间轴与右栏共用执行状态；完成后默认折叠，用户可重新展开。

GET /api/v1/chat/sessions/:session_id/runs 返回最近运行的公开元数据（包括 pending/running），检查 session 和 run 的 owner。前端与已保存消息按时间合并、按 run_id 去重；刷新可看到活跃状态、失败原因和执行步骤，但不恢复未持久化的部分正文或 reasoning。接口不返回 checkpoint、原始工具参数或 profile 快照。

断流后前端在有界时间内只读运行详情与消息列表，取回已保存回答或显示真实运行状态，不自动重发 POST。`done.message_id` 与已保存消息 ID 用于绑定回答反馈；仅有流式文字而无保存身份时不提供反馈入口。

预算从同次读取的默认 AI Profile 解析并冻结到 run。探索准入保留最终回答的调用、输入、输出与时间额度，预留仍计入总预算；受限收尾经过原 journal。`budget_notice` 与 `stop_reason` 同时用于实时、快照和运行查询。provider 实测 usage 与估算值分别标记，不能将 completion tokens 解释为精确内部推理预算。

工具参数在调用前校验失败可纠正一次，失败尝试仍写 journal 并计入预算；权限与证据完整性错误不能靠重试绕过。持久化收尾使用独立短超时，写失败不发送 done。
