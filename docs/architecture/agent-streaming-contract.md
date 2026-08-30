# Agent 问答流式契约（前端 UI × 后端实现）

> **状态**：当前实现与后续扩展（2026-08-30）。
> **关联原型**：`/prototype/agent-chat`（UI）、`/prototype/qa-navigation?variant=AB`（问答入口）
> **现有实现**：`POST /api/v1/chat/sessions/:id/messages/stream`（RAG 流式）、`POST .../messages/agent`（实验性 Agent，非流式）、`POST .../messages/agent/stream`（单视频 Agent SSE）

---

## 1. 背景与目标

产品将从「单轮 RAG 问答」演进到 **Agent 式多步执行**（理解 → 检索 → 调工具 → 生成回答）。前端需要：

1. **实时展示**每一步状态（进行中 / 完成 / 失败），而不是等整包 JSON 返回。
2. **区分问答范围**：单视频（`scope=video`）vs 知识库（`scope=knowledge_base`）——见问答入口 A+B 融合方案。
3. **与现有 RAG 流式兼容**：普通 `strict_rag` / `video_assistant` 仍走现有 SSE；Agent 走扩展事件或独立端点。

本文先记录当前已经对外提供的 SSE 事件契约与快照行为，再记录尚未实现的扩展方向。当前事实以 Go/TypeScript 源码和测试为准。

---

## 2. 现有 API（实现真相）

### 2.1 RAG 流式（产品默认）

```
POST /api/v1/chat/sessions/{session_id}/messages/stream
Authorization: Bearer <jwt>
Content-Type: application/json

{ "question": "...", "top_k": 4, "mode": "strict_rag" | "video_assistant" }
```

**SSE 事件（已实现）**：

| event | data 类型 | 说明 |
|-------|-----------|------|
| `answer` | `string` | 回答文本增量（delta） |
| `citations` | `Citation[]` | 引用列表（通常在 answer 之后） |
| `done` | `{ answer?, degraded?, message_id? }` | 结束 |
| `error` | `{ message }` | 失败 |

前端消费：`frontend/lib/api.ts` → `streamAsk()`。

### 2.2 Agent 非流式（实验）

```
POST /api/v1/chat/sessions/{session_id}/messages/agent
{ "question": "...", "top_k": 4, "mode": "" | "research" | "evidence_funnel", "run_id": "optional existing run" }
```

**响应（已实现）** — `VideoAgentResult`：

```json
{
  "message_id": 123,
  "answer": "...",
  "template": "direct_qa",
  "citations": [ /* Citation */ ],
  "trace": [
    { "name": "retrieve", "tool": "search_transcript", "input": {}, "output_ref": "..." }
  ],
  "model": "..."
}
```

- `mode=research`：走 `VideoResearchRunner`（有界 Planner/Tool/Observe），**仅单视频会话**，知识库会话当前返回错误。
- `mode=research` 可携带同一 owner/session/goal 的既有 `run_id`，从 PostgreSQL 的完成 checkpoint 恢复；省略时创建新 Run。终态 Run 不会被该重试覆盖。
- `mode=evidence_funnel`：走服务端固定的“摘要/元数据 → transcript → 时间窗 → 视觉/OCR → Evidence/Claim”单视频漏斗。Planner 只在有限候选 ID 中选择要补的证据缺口或结束；动作顺序、schema、白名单和预算不能由请求或模型修改。
- `evidence_funnel` 每一级动作与两个受限 Planner 决策都写入 Run/Step/ToolCall；命中、耗时、覆盖、原始 evidence refs 和最终引用 refs 可审计。它只读取既有 OCR/视觉索引，不触发开放式视觉浏览。
- 默认 mode：模板化 tool-loop baseline。

非流式接口保留为对照和降级路径。它返回的 `trace` 仍是兼容性的 `VideoAgentStep` 结构；统一的 `step_id/status` 轨迹由 Agent SSE 和 version 1 快照提供。

---

## 3. Agent 流式端点（当前已实现的单视频切片）

### 3.1 端点

```
POST /api/v1/chat/sessions/{session_id}/messages/agent/stream
```

请求体：

```json
{
  "question": "这场发布会提到了哪些新产品？",
  "top_k": 4,
  "mode": "agent",
  "agent_profile": "default"
}
```

| 字段 | 说明 |
|------|------|
| `mode` | 当前只能是 `agent`；省略时服务端默认使用 `agent`，其他值会返回流式接口错误 |
| `agent_profile` | 已保留扩展字段；当前首个流式切片不让它改变安全上限 |

会话 `scope_type` 由 session 决定。当前流式端点只支持单视频会话；知识库会话、`mode=research` 和 `mode=evidence_funnel` 仍由该端点拒绝。证据漏斗只接入上述非流式实验端点，因此没有新增或修改 SSE 事件。

### 3.2 SSE 事件一览

当前实际发送的事件如下：

| event | data | 何时发送 | 前端 UI 映射 |
|-------|------|----------|--------------|
| `run_start` | `{ run_id, mode, scope_type, task_id?, kb_id? }` | 开始执行 | 重置时间线 / 地铁条 |
| `step_start` | `AgentStepEvent` | 某步开始 | 步骤置 `running`，显示动效 |
| `step_done` | `AgentStepEvent` | 某步完成 | 步骤置 `done` |
| `step_error` | `AgentStepEvent` | 某步失败 | 步骤置 `error`，可 fail-closed |
| `tool_call` | `ToolCallEvent` | 调用工具前 | 工具卡片、终端样式 |
| `tool_result` | `ToolResultEvent` | 工具返回后 | 填充 output |
| `retrieve_hits` | `RetrieveHitsEvent` | 检索完成 | 命中数、来源视频列表 |
| `answer` | `string` | 最终回答增量 | 与现网相同 |
| `citations` | `Citation[]` | 引用 | 与现网相同 |
| `done` | `AgentDoneEvent` | 结束 | 收尾、写 message_id |
| `error` | `{ message, step_id? }` | 致命错误 | 与现网相同 |

当前不会发送 `step_update` 或 `think`，也不会输出原始 Chain-of-Thought。若未来增加步内更新，只能用于安全的执行摘要或进度信息，并需要单独扩展契约。

### 3.3 数据结构（JSON Schema 级描述）

#### `AgentStepEvent`

```typescript
interface AgentStepEvent {
  step_id: string          // 稳定 ID，如 "s1" / uuid
  // 当前实现实际使用 retrieve / tool / answer；其他 kind 属于后续扩展
  kind: 'retrieve' | 'tool' | 'answer' | string
  label: string            // 展示用，如 "检索转写片段"
  status: 'running' | 'done' | 'error'
  // kind 扩展字段（done 时尽量填满）
  detail?: string          // 安全执行摘要；当前通常为空
  query?: string           // retrieve
  hits?: number
  sources?: string[]       // 视频标题或 kb 成员名
  tool?: string
  input?: string | object
  output?: string          // 当前主要是 output_ref，如 "citations:3"
  error?: string
  ts?: string              // ISO8601，可选
}
```

与现有 `VideoAgentStep` 对齐映射：

| 现有 `VideoAgentStep` | 建议 `AgentStepEvent` |
|----------------------|------------------------|
| `name` | `label` + `kind` |
| `tool` | `tool` |
| `input` | `input` |
| `output_ref` | `output`（或 `tool_result` 事件单独带全文） |
| `error` | `error` |

#### `ToolCallEvent` / `ToolResultEvent`

```json
// tool_call
{ "step_id": "s3", "tool": "summarize_segments", "input": { "chunk_ids": [12, 18] } }

// tool_result
{ "step_id": "s3", "output": "提取到 3 款产品…", "duration_ms": 420 }
```

#### `RetrieveHitsEvent`

```json
{
  "step_id": "s2",
  "query": "发布会 新产品",
  "hits": 6,
  "chunks_preview": [
    { "chunk_index": 12, "score": 0.89, "video_title": "2024 产品发布会", "task_id": 101 }
  ],
  "cross_video_count": 2
}
```

#### `AgentDoneEvent`

```json
{
  "message_id": 456,
  "degraded": false,
  "trace_summary": { "steps": 4, "tools": 1, "retrievals": 1 }
}
```

---

## 4. 事件顺序（参考时序）

当前模板 Agent 的典型单视频问答：

```
run_start
step_start    { kind: retrieve, tool: search_transcript, ... }
tool_call     { ... }
tool_result   { ... }
retrieve_hits { hits: 6, ... }
step_done     { kind: retrieve, ... }
step_start    { kind: answer, tool: build_cited_answer, ... }
tool_call     { ... }
tool_result   { ... }
answer        "根据转写..."
answer        "，发布会..."
citations     [ ... ]
step_done     { kind: answer }
done          { message_id: 456 }
```

**约束**：

- 每个 `step_start` 应有且仅有一个 terminal 事件：`step_done` 或 `step_error`。
- `answer` 增量必须在 `step_start(kind=answer)` 之后。
- 当前回答步骤的 `step_done` 在 `citations` 之后；回答内容是在完整生成后按 80 个字符分片发送，并非 Provider token 级流式。
- 客户端应能 **幂等处理** 重复 `step_id`（重连 / 重放）。

---

## 5. 持久化与历史消息

当前 assistant 消息的 `retrieval_snapshot` 已扩展为 version 1 Agent envelope：

```json
{
  "run_id": "uuid",
  "mode": "agent",
  "steps": [ /* AgentStepEvent 终态数组 */ ],
  "citations": [ /* 与现网 Citation 相同 */ ]
}
```

它仍是聊天历史的兼容快照，不是可恢复的 Agent Run/Step 数据源。模板 Agent 的实际工具动作，以及 research Agent 的 Planner/工具动作，另行写入 `agent_runs`、`agent_steps`、`agent_tool_calls`；进程恢复只读取这些 PostgreSQL 权威记录。前端加载历史时：

- 有 `agent_snapshot` → 渲染完整步骤时间线（无需重放 SSE）。
- 仅有 `retrieval_snapshot` → 保持现网 [C1] 引用行为。

Agent 回答完成后还会把 Claim、Evidence 和 Claim-Evidence 关系写入独立 PostgreSQL 账本。该副作用不新增 SSE event，不改变本文事件顺序；账本写入失败不会把已生成的普通 Agent 回答改成流式错误。账本只保存可见事实和稳定证据引用，不保存原始 Chain-of-Thought。

执行持久化同样不新增 SSE event，也不改变 `run_start → step_* → done/error` 顺序。数据库只保存安全输入摘要、validated arguments/call digest、输出引用、可恢复的安全工具结果和错误分类；不保存或发送 provider prompt、Planner 草稿、API key 或 Chain-of-Thought。

---

## 6. 范围与权限（单视频 vs 知识库）

| scope | session 创建 | Agent `research` mode | Agent stream 当前状态 |
|-------|-------------|----------------------|-------------------|
| `video` | `{ task_id, scope_type: "video" }` | 已支持（实验） | 支持 |
| `knowledge_base` | `{ knowledge_base_id, scope_type: "knowledge_base" }` | 流式端点拒绝 | 需扩展 runner / 工具注册 |

知识库 Agent 需在检索工具中携带 `task_id` / `video_title`（与现网 `Citation.video_title` 一致），供跨视频引用 UI 使用。

---

## 7. 当前前端消费与后续扩展

当前 `frontend/lib/api.ts` 的 `streamAgent()` 已消费真实的 `run_start`、`step_*`、`tool_*`、`retrieve_hits`、`answer`、`citations`、`done` 和 `error` 事件；聊天页单视频 Agent 模式直接使用该流，普通 RAG 和知识库问答保持原路径。

后续扩展应分别验收：

- 知识库范围的 Agent 工具和跨视频引用；
- `mode=research` 的流式观察；
- Provider token 级答案流式化；
- 仅包含安全执行摘要的可选步内更新，不输出原始思维链。

---

## 8. 前端消费索引

```typescript
// frontend/lib/api.ts 中的现有实现
export async function streamAgent(sid: number, question: string, opts: AgentStreamOptions, h: AgentSSEHandlers, signal?: AbortSignal) {
  // POST .../messages/agent/stream
  // parse SSE; switch event:
  //   run_start / step_start / step_done / step_error /
  //   tool_call / tool_result / retrieve_hits / answer / citations / done / error
}
```

`AgentSSEHandlers` 当前映射到正式聊天页的 `agentTraceReducer`；原型组件仅作视觉参考。

---

## 9. 错误与取消

- 客户端 `AbortSignal` 中断 → context 取消会传入检索和模型调用，服务端停止继续执行并关闭流；客户端主动断开时不保证还能收到 `error` 事件。
- 当前模板流遇到执行错误会结束本轮，并在仍可写时发送 `step_error`/`error`；未来研究循环可由 policy 决定步级错误后是否继续。
- Demo 账号：与现网一致，只读；Agent 写工具应 403 或 no-op。

---

## 10. 当前验收结果

- [x] `POST .../messages/agent/stream` 返回 `text/event-stream`。
- [x] 单视频成功路径覆盖 `run_start`、工具步骤、答案分片、`citations` 和 `done`。
- [x] 每个已开始步骤只有一个 `step_done` 或 `step_error`；答案步骤在引用事件后结束。
- [x] `streamAgent()` 已直接消费真实 Agent 事件；普通 RAG 路径不变。
- [x] 后端单测覆盖事件顺序、取消、错误和知识库/模式拒绝。

尚未实现的 Run/Step 独立持久化、知识库 Agent、研究模式流式化和 Provider token 级流式不属于当前端点的验收范围。

---

## 11. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-08-29 | 初版：基于原型 Agent UI A/B/C 与融合 D/E；对齐现有 `VideoAgentResult` / RAG SSE |
| 2026-08-30 | 根据实际 Go/TypeScript 实现更新单视频 Agent SSE、事件顺序、范围限制和历史快照说明；移除将 `step_update`、`think` 当作当前事件的表述。 |
| 2026-08-30 | 记录 Agent 证据账本作为回答后的兼容副作用；明确不新增 SSE 事件、不改变事件顺序。 |
