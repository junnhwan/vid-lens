# 聊天 UI 已落地能力 × 后端待补齐接口

> **状态**：2026-08-29  
> **前端**：`frontend/app/chat/`、`frontend/app/kb/[kbId]/`、`frontend/app/qa/`  
> **关联**：详细 SSE 事件设计见 [agent-streaming-contract.md](./agent-streaming-contract.md)

---

## 1. 前端已合入正式路由的内容

| 能力 | 落点 | 数据来源（当前） |
|------|------|------------------|
| 方案 D 布局（对话主区 ~72%、可拖拽、右侧可收起） | `/chat/:taskId`、`/kb/:kbId` | 纯前端 |
| 右侧「执行流水线」面板 | 同上 | **前端根据 RAG SSE 推断**（见 §2.2） |
| 消息内可折叠「思考与检索轨迹」 | `ChatMessageRow` | 历史消息从 `retrieval_snapshot` 条数推断 |
| 流式回答 + 柔和光标 | `streamAsk` → `answer` 事件 | **已实现** |
| 引用卡片完成后淡入 | `citations` 事件后 | **已实现** |
| 问答入口页 `/qa` | 单视频 / 知识库列表 | `GET /media/tasks`、`GET /knowledge-bases` |
| 侧栏「最近可问答」 | `AppShell` | 同上 |

原型页 `/prototype/*` 仍保留作对比，**产品默认路径已是正式路由**。

---

## 2. 当前后端已满足（无需改即可用）

### 2.1 RAG 流式问答（产品默认）

```
POST /api/v1/chat/sessions/{session_id}/messages/stream
{ "question", "top_k", "mode": "strict_rag" | "video_assistant" }
```

| SSE event | 前端用途 |
|-----------|----------|
| `answer` | 正文增量 → 流式打字效果 |
| `citations` | `[C1]` 引用卡片 + 右侧流水线「检索完成」 |
| `done` | 结束流式、标记「生成回答」完成 |
| `error` | 错误气泡 + 流水线失败态 |

前端消费：`frontend/lib/api.ts` → `streamAsk()`。

### 2.2 历史消息

```
GET /api/v1/chat/sessions/{session_id}/messages
```

| 字段 | 前端用途 |
|------|----------|
| `content` | 回答正文 |
| `retrieval_snapshot` | JSON 字符串 → 引用列表 + 降级轨迹（仅「检索 N 条」） |

### 2.3 会话与范围

| API | 用途 |
|-----|------|
| `POST /chat/sessions` + `scope_type: video` + `task_id` | 单视频问答 |
| `POST /chat/sessions` + `scope_type: knowledge_base` + `knowledge_base_id` | 知识库问答 |
| `GET /chat/sessions?task_id=` / `?knowledge_base_id=` | 侧栏会话列表 |

---

## 3. 后端待补齐（才能完整驱动 Agent UI）

以下按**优先级**排列。未实现前，右侧流水线只能显示「检索 / 生成」两步推断，**无法展示真实思考、工具调用、多轮 Agent 步骤**。

### P0 — Agent 流式 SSE（推荐）

**端点（提案）**

```
POST /api/v1/chat/sessions/{session_id}/messages/agent/stream
```

**请求体**

```json
{
  "question": "…",
  "top_k": 4,
  "mode": "" | "research"
}
```

**建议 SSE 事件**（与 `agent-streaming-contract.md` 一致）：

| event | 说明 | 前端映射 |
|-------|------|----------|
| `step_start` | `{ "step_id", "kind": "think"\|"retrieve"\|"tool"\|"answer", "label" }` | 右侧时间线节点 → running |
| `step_delta` | 思考文本或工具输出片段 | 节点详情区增量 |
| `retrieve_hits` | `{ "query", "hits", "sources"[] }` | 检索节点完成 |
| `tool_call` | `{ "tool", "input" }` | 工具节点 |
| `tool_result` | `{ "output" }` | 工具完成 |
| `answer` | 回答增量（与 RAG 相同） | 对话区流式 |
| `citations` | `Citation[]` | 引用卡片 |
| `step_done` | `{ "step_id", "status": "done"\|"error" }` | 节点完成 |
| `done` | `{ "message_id", "degraded?" }` | 整轮结束 |
| `error` | `{ "message" }` | 失败 |

**验收**：前端可删除 `streamTraceReducer` 的推断逻辑，改为直接消费 SSE。

### P1 — Agent 非流式增强（过渡方案）

在现有 `POST .../messages/agent` 响应中扩展 `trace` 结构，使一次性返回也能渲染右侧栏（无 Live 动效）：

```json
{
  "answer": "…",
  "citations": [],
  "trace": [
    {
      "step_id": "s1",
      "kind": "think",
      "label": "理解问题",
      "status": "done",
      "detail": "…",
      "started_at": "…",
      "ended_at": "…"
    },
    {
      "step_id": "s2",
      "kind": "retrieve",
      "label": "检索转写",
      "status": "done",
      "query": "…",
      "hits": 6,
      "sources": ["视频标题"]
    },
    {
      "step_id": "s3",
      "kind": "tool",
      "label": "调用工具",
      "status": "done",
      "tool": "summarize_segments",
      "input": "…",
      "output": "…"
    }
  ]
}
```

**额外要求**：

- 知识库会话支持 Agent（当前 `mode=research` 对 KB 会话报错）
- `trace` 持久化到 `chat_messages` 或独立表，历史消息可回放流水线

### P2 — RAG 流式中间事件（可选，减轻推断）

在现有 `messages/stream` 上**可选**增加事件（不破坏旧客户端）：

| event | 说明 |
|-------|------|
| `retrieve_start` | 开始向量/BM25 检索 |
| `retrieve_done` | `{ "hits", "query_rewrite?" }` |
| `generate_start` | 开始 LLM 生成 |

前端收到后即可替换当前的「先发问题 → 猜检索 running」推断。

### P3 — 问答入口聚合 API（性能优化，非必须）

当前 `/qa` 调用 `listTasks` + `listKBs` 两次请求。若视频量大，可增加：

```
GET /api/v1/qa/hub
→ { "recent_videos": [{ "task_id", "title", "indexed", "has_transcription" }], "knowledge_bases": [...] }
```

减少列表过滤与 payload。

---

## 4. 数据模型建议（持久化 trace）

为支持「刷新后会话内流水线可回放」，建议在 assistant 消息上增加其一：

**方案 A** — 扩展 `chat_messages.metadata`（JSONB）：

```json
{
  "agent_trace": [ /* ChatTraceStep[] */ ],
  "agent_mode": "research"
}
```

**方案 B** — 复用并规范 `retrieval_snapshot` 为结构化对象（含 `trace` + `citations`）。

前端 `parseMessages()` 已预留 `trace` 字段，后端落库后只需在 GET messages 时返回即可。

---

## 5. 前端接入清单（后端就绪后）

1. `frontend/lib/api.ts` 增加 `streamAgent()`，解析 §3 P0 事件。
2. `chat/[taskId]/page.tsx`、`kb/[kbId]/page.tsx`：模式切换增加「Agent / 研究」时走 agent 端点。
3. 删除或降级 `streamTraceReducer` 推断逻辑。
4. `AgentTracePanel` 提示文案改为真实步骤来源。
5. （可选）`useTypewriter` 用于 agent 非流式一次性 `answer` 字段。

---

## 6. 相关代码索引

| 区域 | 路径 |
|------|------|
| 聊天 D 布局 | `frontend/components/chat/ChatSplitLayout.tsx` |
| 右侧流水线 | `frontend/components/chat/AgentTracePanel.tsx` |
| 轨迹类型 / 推断 | `frontend/components/chat/traceTypes.ts` |
| 流式消费 | `frontend/lib/api.ts` → `streamAsk` |
| 原型参考（可废弃） | `frontend/components/prototype/agent/VariantD.tsx` |
| Agent 后端（实验） | `internal/handler/chat_agent.go`、`internal/service/` agent runner |
| 路由注册 | `cmd/server/router.go` → `messages/agent` |

---

## 7. 分阶段建议

| 阶段 | 后端 | 前端 | 用户体验 |
|------|------|------|----------|
| **当前** | RAG stream | D 布局 + 推断流水线 | 对话体验完整；右侧为简化两步 |
| **+P1** | agent REST + trace | 接 agent API | 结束后可看完整步骤 |
| **+P0** | agent stream | 接 SSE | Live 流水线 + 与原型 D 一致 |
| **+P2** | RAG 中间事件 | 替换推断 | 严格 RAG 也有真实检索时点 |
