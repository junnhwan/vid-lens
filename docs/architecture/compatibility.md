# 在线协议与执行边界

本文说明当前在线请求、会话快照和 Agent 执行记录之间的职责边界。

## 请求协议

- 会话执行模式为 `chat` 或 `agent`。
- 单视频会话通过 `scope_type=video` 绑定一个视频任务。
- 视频库会话通过 `scope_type=video_library` 不绑定单个视频或知识库；检索范围是服务端按当前用户和当前向量模型解析出的已建索引视频集合，且该范围的成员集合只在标准 Chat 路径解析，Agent 的成员集合只对 `knowledge_base` 会话计算。
- 知识库会话通过 `scope_type=knowledge_base` 与 `knowledge_base_id` 绑定服务端授权的成员任务集合。
- 三种范围与其 ID 组合由 `chat_sessions` 上的 `chk_chat_sessions_scope` 约束限定；创建会话时服务端按范围校验请求参数，`video_library` 不接受 `task_id` 或 `knowledge_base_id`。
- Agent 同步和 SSE 接口使用同一套 Planner/Tool/Observe 循环；请求中的 `run_id` 只允许匹配当前 owner 下 session、scope、goal、mode、agent profile 和 profile 快照全部一致的记录，任何一项不符即拒绝复用。
- 工具参数必须符合服务端 schema；视频、知识库、时间窗、source refs 和 memory context 均由服务端确认。

## 权威数据

Run、Step、ToolCall 是 Agent 执行的权威数据，保存作用域、冻结策略、预算、lease、checkpoint、工具结果、引用和终态。PostgreSQL 负责保存这些执行事实。

`agent_runs` 是问答与笔记子系统共用的执行表：`subject_kind`、`subject_id`、`execution_kind` 和 `recipe_version` 决定这条 Run 属于聊天会话、笔记生成请求、笔记编辑请求还是摘要修订操作。聊天主体由服务端在创建时固定为 `chat_session`，`subject_id` 即 session ID；三类 artifact 主体的 `session_id` 为空并指向自己的不可变请求行，配方版本随请求一起冻结。主体归属由 `chk_agent_run_subject` 约束在数据库层限定。

`chat_messages.retrieval_snapshot` 是会话展示和引用回放使用的派生快照，包含版本、run、mode、steps、citations、memory identity 和 policy。它不承担 lease、checkpoint、预算或执行恢复职责。

前端实时轨迹由 SSE 事件更新；刷新或断流后，通过运行查询接口和已保存消息读取执行状态。未保存的正文增量和 reasoning 不作为成功消息。

## Agent 执行版本

当前 Agent policy 使用 `engine_version=2`。未完成 Run 通过重新提问创建新的执行上下文；完成 Run 按 run 幂等保存最终消息，并可根据 message_id 绑定反馈。

Agent 执行受到工具步骤、规划、模型、检索、视觉、抽帧、token、费用和总时长预算限制。只读检索步骤可由 CAS 接管；不可安全重放的 LLM/VLM 调用在 provider 返回与终态提交之间中断时进入 `ambiguous`，需要显式新 attempt。

## 知识库范围

知识库 Agent 在 Run 创建时冻结 `member_task_ids`。检索工具默认覆盖成员集合，也可以指定其中一个 `task_id`；转写窗口、视觉窗口和查询时视觉调查都必须指向授权成员视频。成员集合、索引就绪状态和来源权限在规划、工具执行及最终保存边界再次校验。

## 笔记与摘要修订执行

三类 artifact 执行都落在 `agent_runs`、`agent_steps` 和 `agent_tool_calls`：笔记编辑和摘要修订复用问答同一条 `AgentExecutionJournal` 路径（摘要修订同样记录 plan 步骤和 Planner LLM 调用），笔记生成用自己的调用记账写入同一套 Step/ToolCall 表。三者共享 Run 的冻结策略、预算、lease 和终态语义，但走各自的请求路由、outbox 表和队列：生成在 `vidlens.artifact.generate.v1`，笔记编辑在 `vidlens.artifact.edit.v1`，摘要修订在 `vidlens.summary.edit.v1`。分表分队列是兼容边界的一部分——投递方和消费方都会比对持久化的 Run 主体与消息所属队列，主体不符的消息直接丢弃，因此只理解生成队列的旧消费者不会确认它不能执行的编辑或摘要任务。

提交携带 owner 级幂等键和请求哈希；同键不同内容按冲突拒绝，笔记重试同样受幂等键约束并在非终态 Run 上拒绝。笔记的取消、恢复、重试和事件读取都要求目标 Run 的 `subject_kind` 与服务端预期一致。执行由后端 worker 驱动，客户端断开只停止观察：笔记的进度和终态可从持久 `run_events` 序列按序号续传，SSE 与轮询读取的是同一份服务端状态；摘要修订没有事件流接口，只能按 owner 读取操作与运行状态。人工保存、编辑应用与撤销都按 `head_version` CAS 写入新修订，基于过期版本的写入返回版本冲突，而不是覆盖式落库。

## 前端展示

前端展示层可以合并消息、快照、SSE 步骤和引用，但不能根据 UI 推断执行事实。引用详情以服务端返回的 modality、时间范围、source mapping 状态和 source refs 为准；播放跳转使用真实的毫秒时间和站内流地址。

## 原型边界

原型使用本地演示数据和模拟事件；正式页面使用 HTTP/SSE 返回的任务、会话、Agent 运行状态和 citation。原型交互不改变服务端作用域、工具白名单、预算、租约和状态机。
