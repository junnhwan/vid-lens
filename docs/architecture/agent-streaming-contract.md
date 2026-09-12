# Agent 流式契约

## 请求

- Chat：`POST /api/v1/chat/sessions/:session_id/messages/stream`，请求 `{ "question": "...", "top_k": 4, "mode": "chat" }`。
- Agent：`POST /api/v1/chat/sessions/:session_id/messages/agent/stream`，请求 `{ "question": "...", "top_k": 4, "mode": "agent" }`，只支持单视频。
- 同步 Agent：`POST /api/v1/chat/sessions/:session_id/messages/agent`，使用相同循环，额外接受 `run_id` 进行 owner/session/goal 匹配的重放。

省略 mode 使用对应端点默认值。`strict_rag`、`video_assistant`、`research`、`evidence_funnel` 新请求被拒绝，不静默转入旧执行器。知识库 Agent 被拒绝；知识库 Chat 保持当前成员授权。

## SSE 事件

| 事件 | 语义 |
| --- | --- |
| `run_start` | run_id、scope、有效记忆策略 |
| `progress` | 真实规划、上下文准备、检索和保存阶段；包含 id、kind、status、detail、ts；Agent 规划包含 plan_id、简要决策说明及已有 evidence_refs |
| `reasoning` | 模型接口提供的 reasoning 增量；按 call_id（plan-N 或 answer）与 run_id 归属，独立于正文 |
| `step_start` / `tool_call` | 实际工具开始执行 |
| `tool_result` / `retrieve_hits` | 工具结果摘要与检索命中 |
| `step_done` / `step_error` | 步骤终态 |
| `answer` | provider 生成增量；不支持流式时完成后一次发送 |
| `answer_reset` | 已生成部分正文后发生降级，清空暂存正文再接收替代答案 |
| `citations` | 最终允许显示的引用集 |
| `done` | 保存成功，含 message_id、run_id、answer、memory_policy 和步骤统计 |
| `error` | 失败或无法完成；不能视为成功保存 |

步骤事件只有工具 observer 一个发布位置。Journal 管持久化，不重复发步骤。恢复完成 checkpoint 不重新调用工具。

规划在调用模型之前发送 running，完整 JSON 解析、白名单校验后发送终态。Planner 接收每个工具的 input_schema；检索规范参数为 question，兼容 query，但两者不允许冲突，其他未知字段继续拒绝。public_summary 是独立的用户可读说明，最多 240 字符，存入 checkpoint；不保存内部 reason 草稿。旧 checkpoint 缺少公开摘要时，展示基于动作的说明。

模型流通过 ai.StreamResponse 保留 admission/retry/observed 包装器，兼容 delta.reasoning_content、delta.reasoning 和跨块 think 标签。没有 reasoning 的上游只展示执行进度。Planner 的正文 JSON 不进入回答区；仅完成后解析，不能根据部分参数执行工具。

`answer` 在保存之前发送，因此不是已提交消息。`done.answer` 是数据库中保存的权威最终文本，前端覆盖此前增量，以去除内部引用标记并避免答案重复追加。保存失败不会发送成功 done。最终回答工具的 step_done 在发布成功后发送。

取消上下文贯穿 Planner、检索、视觉下载/抽帧/VLM 和生成；取消后不启动下一步。前端保留已收到的部分文本并标识终态。

Handler 是响应的唯一写入者，使用有背压的事件传递并在首事件之后每 10 秒发注释心跳。响应包含 Cache-Control: no-cache, no-transform 和 X-Accel-Buffering: no。Next 必须保留 compress: false：其默认压缩会缓冲 rewrite 转发的小块 SSE。上游缺完成标记、前端 EOF 缺 done/error 都按中断处理。

执行 `cd frontend; npm run test:stream` 验证真实 Next rewrite：上游只有在客户端收到第一段后才能发送剩余内容。它使用独立 .next-stream-test 输出目录；测试失败意味着分段传输被缓冲。

## 历史

`retrieval_snapshot` 保存版本、run/mode、步骤、引用、记忆 identity 与 policy。`snapshotTraceAdapter` 继续读取旧模板、research/funnel 与 bare citations；这些数据只供历史显示，不驱动在线执行恢复，也不持久化完整记忆正文。

新 Agent 快照版本为 2，在工具步骤之间保存公开规划摘要。模型 reasoning 仅保存在当前页面内存，不写入历史快照或对话正文。前端消息内时间轴与右栏共用执行状态；完成后默认折叠，用户可重新展开。

GET /api/v1/chat/sessions/:session_id/runs 返回最近 20 条失败、取消或预算终止运行的公开元数据，检查 session 和 run 的 owner。前端与已保存消息按时间合并、按 run_id 去重；刷新可看到未保存运行的失败原因和执行步骤，但不恢复未持久化的部分正文或 reasoning。接口不返回 checkpoint、原始工具参数或 profile 快照。
