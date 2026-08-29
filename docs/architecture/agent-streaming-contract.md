# Agent 问答流式契约（前端 UI × 后端实现）

> **状态**：提案（2026-08-29），供 Agent 化改造前后端对齐。  
> **关联原型**：`/prototype/agent-chat`（UI）、`/prototype/qa-navigation?variant=AB`（问答入口）  
> **现有实现**：`POST /api/v1/chat/sessions/:id/messages/stream`（RAG 流式）、`POST .../messages/agent`（实验性 Agent，**非流式**）

---

## 1. 背景与目标

产品将从「单轮 RAG 问答」演进到 **Agent 式多步执行**（理解 → 检索 → 调工具 → 生成回答）。前端需要：

1. **实时展示**每一步状态（进行中 / 完成 / 失败），而不是等整包 JSON 返回。
2. **区分问答范围**：单视频（`scope=video`）vs 知识库（`scope=knowledge_base`）——见问答入口 A+B 融合方案。
3. **与现有 RAG 流式兼容**：普通 `strict_rag` / `video_assistant` 仍走现有 SSE；Agent 走扩展事件或独立端点。

本文定义 **建议的 SSE 事件契约** 与 **REST 快照结构**，后端可按阶段实现。

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
{ "question": "...", "top_k": 4, "mode": "" | "research" }
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
- 默认 mode：模板化 tool-loop baseline。

**缺口**：无 SSE；`trace` 仅在结束后一次性返回，无法驱动原型中的逐步动效。

---

## 3. 建议新增：Agent 流式端点

### 3.1 端点

```
POST /api/v1/chat/sessions/{session_id}/messages/agent/stream
```

请求体与 `/messages/agent` 相同，增加可选字段：

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
| `mode` | `agent`（默认 tool-loop）\| `research`（goal-driven 研究循环） |
| `agent_profile` | 预留：不同步数上限 / 工具白名单 |

会话 `scope_type` 由 session 决定（`video` / `knowledge_base`），**不需要**客户端重复传 scope。

### 3.2 SSE 事件一览

在保留 `answer` / `citations` / `done` / `error` 的前提下，**新增 Agent 步骤事件**：

| event | data | 何时发送 | 前端 UI 映射 |
|-------|------|----------|--------------|
| `run_start` | `{ run_id, mode, scope_type, task_id?, kb_id? }` | 开始执行 | 重置时间线 / 地铁条 |
| `step_start` | `AgentStepEvent` | 某步开始 | 步骤置 `running`，显示动效 |
| `step_update` | `AgentStepEvent` | 步内增量（可选） | 思考文本流式、检索进度 |
| `step_done` | `AgentStepEvent` | 某步完成 | 步骤置 `done` |
| `step_error` | `AgentStepEvent` | 某步失败 | 步骤置 `error`，可 fail-closed |
| `tool_call` | `ToolCallEvent` | 调用工具前 | 工具卡片、终端样式 |
| `tool_result` | `ToolResultEvent` | 工具返回后 | 填充 output |
| `retrieve_hits` | `RetrieveHitsEvent` | 检索完成 | 命中数、来源视频列表 |
| `think` | `{ delta: string }` | 推理摘要流式（可选） | 思考区打字效果 |
| `answer` | `string` | 最终回答增量 | 与现网相同 |
| `citations` | `Citation[]` | 引用 | 与现网相同 |
| `done` | `AgentDoneEvent` | 结束 | 收尾、写 message_id |
| `error` | `{ message, step_id? }` | 致命错误 | 与现网相同 |

### 3.3 数据结构（JSON Schema 级描述）

#### `AgentStepEvent`

```typescript
interface AgentStepEvent {
  step_id: string          // 稳定 ID，如 "s1" / uuid
  kind: 'think' | 'retrieve' | 'tool' | 'plan' | 'observe' | 'answer'
  label: string            // 展示用，如 "检索转写片段"
  status: 'running' | 'done' | 'error'
  // kind 扩展字段（done 时尽量填满）
  detail?: string          // think
  query?: string           // retrieve
  hits?: number
  sources?: string[]       // 视频标题或 kb 成员名
  tool?: string
  input?: string | object
  output?: string
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

一次典型单视频 Agent 问答：

```
run_start
step_start  { kind: think, ... }
think       { delta: "用户在问..." }     // 可选，多次
step_done   { kind: think, ... }
step_start  { kind: retrieve, ... }
retrieve_hits { hits: 6, ... }
step_done   { kind: retrieve, ... }
step_start  { kind: tool, tool: summarize_segments }
tool_call   { ... }
tool_result { ... }
step_done   { kind: tool, ... }
step_start  { kind: answer }
answer      "根据转写..."
answer      "，发布会..."
citations   [ ... ]
step_done   { kind: answer }
done        { message_id: 456 }
```

**约束**：

- 每个 `step_start` 应有且仅有一个 terminal 事件：`step_done` 或 `step_error`。
- `answer` 增量必须在 `step_start(kind=answer)` 之后。
- 客户端应能 **幂等处理** 重复 `step_id`（重连 / 重放）。

---

## 5. 持久化与历史消息

建议 assistant 消息的 `retrieval_snapshot` 扩展（或新增 `agent_snapshot` JSON 列）：

```json
{
  "run_id": "uuid",
  "mode": "agent",
  "steps": [ /* AgentStepEvent 终态数组 */ ],
  "citations": [ /* 与现网 Citation 相同 */ ]
}
```

前端加载历史时：

- 有 `agent_snapshot` → 渲染完整步骤时间线（无需重放 SSE）。
- 仅有 `retrieval_snapshot` → 保持现网 [C1] 引用行为。

---

## 6. 范围与权限（单视频 vs 知识库）

| scope | session 创建 | Agent `research` mode | 建议 Agent stream |
|-------|-------------|----------------------|-------------------|
| `video` | `{ task_id, scope_type: "video" }` | 已支持（实验） | 支持 |
| `knowledge_base` | `{ knowledge_base_id, scope_type: "knowledge_base" }` | 现网拒绝 | 需扩展 runner / 工具注册 |

知识库 Agent 需在检索工具中携带 `task_id` / `video_title`（与现网 `Citation.video_title` 一致），供跨视频引用 UI 使用。

---

## 7. 前端实现阶段（给后端排期参考）

| 阶段 | 后端 | 前端 |
|------|------|------|
| P0 | 保持 `/messages/stream` 不变 | 问答入口 A+B 合并进 `AppShell` |
| P1 | `agent/stream` 仅发 `step_*` + `answer` + `done`（可先 mock think/retrieve） | `streamAgent()` 消费新事件；融合 UI D |
| P2 | 真实 `tool_call` / `retrieve_hits`；`trace` 写入 `agent_snapshot` | 历史消息恢复步骤 UI |
| P3 | KB scope Agent；`research` mode 流式化 | 地铁条方案 E 作为窄屏降级 |

---

## 8. 前端消费伪代码

```typescript
// 建议新增 frontend/lib/api.ts
export async function streamAgent(sid: number, question: string, opts: AgentStreamOptions, h: AgentSSEHandlers, signal?: AbortSignal) {
  // POST .../messages/agent/stream
  // parse SSE; switch event:
  //   step_start / step_done / tool_call / retrieve_hits / answer / citations / done / error
}
```

`AgentSSEHandlers` 应映射到原型组件 state（见 `components/prototype/agent/useAgentDemo.ts`，后续替换为真实 SSE reducer）。

---

## 9. 错误与取消

- 客户端 `AbortSignal` 中断 → 服务端应尽快停止 LLM/检索，发送 `error` + 关闭流。
- 步级错误：`step_error` 后是否继续由 `mode` / policy 决定；前端在步骤上显示错误，不一定整轮失败。
- Demo 账号：与现网一致，只读；Agent 写工具应 403 或 no-op。

---

## 10. 验收清单（后端）

- [ ] `POST .../messages/agent/stream` 返回 `text/event-stream`
- [ ] 至少发出 `run_start` → 2×`step_start/step_done` → `answer`* → `citations` → `done`
- [ ] `VideoAgentResult.trace` 字段与 `step_done` 事件内容一致（便于对比调试）
- [ ] 单视频 session 端到端：前端融合 UI D 可仅改 `useAgentDemo` → 真实 SSE 即跑通
- [ ] 集成测试：参考 `internal/handler/chat_agent_test.go` 增加 stream 用例

---

## 11. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-08-29 | 初版：基于原型 Agent UI A/B/C 与融合 D/E；对齐现有 `VideoAgentResult` / RAG SSE |
