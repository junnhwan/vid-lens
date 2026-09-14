# 在线协议与执行边界

本文说明当前在线请求、会话快照和 Agent 执行记录之间的职责边界。

## 请求协议

- 会话执行模式为 `chat` 或 `agent`。
- 单视频会话通过 `scope_type=video` 绑定一个视频任务。
- 知识库会话通过 `scope_type=knowledge_base` 与 `knowledge_base_id` 绑定服务端授权的成员任务集合。
- Agent 同步和 SSE 接口使用同一套 Planner/Tool/Observe 循环；请求中的 `run_id` 只允许匹配当前 owner、session 和 goal 的执行记录。
- 工具参数必须符合服务端 schema；视频、知识库、时间窗、source refs 和 memory context 均由服务端确认。

## 权威数据

Run、Step、ToolCall 是 Agent 执行的权威数据，保存作用域、冻结策略、预算、lease、checkpoint、工具结果、引用和终态。PostgreSQL 负责保存这些执行事实。

`chat_messages.retrieval_snapshot` 是会话展示和引用回放使用的派生快照，包含版本、run、mode、steps、citations、memory identity 和 policy。它不承担 lease、checkpoint、预算或执行恢复职责。

前端实时轨迹由 SSE 事件更新；刷新或断流后，通过运行查询接口和已保存消息读取执行状态。未保存的正文增量和 reasoning 不作为成功消息。

## Agent 执行版本

当前 Agent policy 使用 `engine_version=2`。未完成 Run 通过重新提问创建新的执行上下文；完成 Run 按 run 幂等保存最终消息，并可根据 message_id 绑定反馈。

Agent 执行受到工具步骤、规划、模型、检索、视觉、抽帧、token、费用和总时长预算限制。只读检索步骤可由 CAS 接管；不可安全重放的 LLM/VLM 调用在 provider 返回与终态提交之间中断时进入 `ambiguous`，需要显式新 attempt。

## 知识库范围

知识库 Agent 在 Run 创建时冻结 `member_task_ids`。检索工具默认覆盖成员集合，也可以指定其中一个 `task_id`；转写窗口、视觉窗口和查询时视觉调查都必须指向授权成员视频。成员集合、索引就绪状态和来源权限在规划、工具执行及最终保存边界再次校验。

## 前端展示

前端展示层可以合并消息、快照、SSE 步骤和引用，但不能根据 UI 推断执行事实。引用详情以服务端返回的 modality、时间范围、source mapping 状态和 source refs 为准；播放跳转使用真实的毫秒时间和站内流地址。

## 原型边界

原型使用本地演示数据和模拟事件；正式页面使用 HTTP/SSE 返回的任务、会话、Agent 运行状态和 citation。原型交互不改变服务端作用域、工具白名单、预算、租约和状态机。
