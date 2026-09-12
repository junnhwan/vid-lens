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
| `step_start` / `tool_call` | 实际工具开始执行 |
| `tool_result` / `retrieve_hits` | 工具结果摘要与检索命中 |
| `step_done` / `step_error` | 步骤终态 |
| `answer` | provider 生成增量；不支持流式时完成后一次发送 |
| `citations` | 最终允许显示的引用集 |
| `done` | 保存成功，含 message_id、run_id、answer、memory_policy 和步骤统计 |
| `error` | 失败或无法完成；不能视为成功保存 |

步骤事件只有工具 observer 一个发布位置。Journal 管持久化，不重复发步骤。恢复完成 checkpoint 不重新调用工具。

`answer` 在保存之前发送，因此不是已提交消息。`done.answer` 是数据库中保存的权威最终文本，前端覆盖此前增量，以去除内部引用标记并避免答案重复追加。保存失败不会发送成功 done。最终回答工具的 step_done 在发布成功后发送。

取消上下文贯穿 Planner、检索、视觉下载/抽帧/VLM 和生成；取消后不启动下一步。前端保留已收到的部分文本并标识终态。

## 历史

`retrieval_snapshot` 保存版本、run/mode、步骤、引用、记忆 identity 与 policy。`snapshotTraceAdapter` 继续读取旧模板、research/funnel 与 bare citations；这些数据只供历史显示，不驱动在线执行恢复，也不持久化完整记忆正文。
