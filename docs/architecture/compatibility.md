# 兼容边界清单

兼容逻辑必须停留在协议或派生数据边界，不能反向进入 Agent 执行、实时 trace 或数据库事实模型。下表是当前仍有调用方的兼容面；没有列出的旧实现不应保留为永久双轨。

| 兼容面 | 当前替代路径 | Owner | 删除条件 |
|---|---|---|---|
| 创建视频会话时仅传 `task_id`，省略 `scope_type` | 新调用方显式传 `scope_type=video`；知识库传 `scope_type=knowledge_base` 与 `knowledge_base_id` | 后端 HTTP/Chat service | 已支持客户端全部显式发送 scope，且一个发布周期的访问记录无旧请求后删除默认推断 |
| 非流式 Agent 响应中的旧 `trace[]` | version 1 `retrieval_snapshot.steps[]` 与 Agent SSE 的 `step_*` | Agent API | 外部非流式消费者完成迁移并经过一个兼容窗口后移除；此前不得用于执行恢复 |
| `retrieval_snapshot` 中 bare `Citation[]` | version 1 envelope `{run_id, mode, steps, citations}` | 聊天历史 API | 现存历史数据完成迁移或超过产品保留期，且回放抽样无 bare 数组后移除 |
| `retrieval_snapshot` 中旧 Agent `trace[]` | version 1 `steps[]` | 前端历史回放 | 与旧消息保留期一致；删除前保留 fixture 行为测试 |
| 历史快照解析 adapter | `frontend/components/chat/snapshotTraceAdapter.ts` | 前端聊天模块 | 上述两种旧快照均可删除时整体删除；实时事件不得引用该 adapter |
| `RetrievalRequest.TaskID` 单视频字段 | `TaskIDs` 集合字段 | 向量 adapter | pgvector 的所有生产调用方都传 `TaskIDs`，且单视频兼容测试与调用统计允许移除后删除 |
| prototype UI 组件 | 正式 `frontend/app/` 与 `frontend/components/chat/` | 前端设计 workspace | 原型决策完成并归档后删除；不得从正式产品模块导入 prototype 组件 |

Run/Step/ToolCall 的权威数据只来自 PostgreSQL 执行表。聊天 `retrieval_snapshot`、旧 `trace[]` 和 UI 推断步骤始终是展示兼容层，不能提供 lease、checkpoint、预算或终态事实。
