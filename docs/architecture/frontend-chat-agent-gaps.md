# 聊天 UI 已落地能力 × 后端现状与待补齐接口

> **状态**：2026-09-02
> **前端**：`frontend/app/chat/`、`frontend/app/kb/[kbId]/`、`frontend/app/qa/`
> **关联**：详细 SSE 事件设计见 [agent-streaming-contract.md](./agent-streaming-contract.md)

---

## 1. 前端已合入正式路由的内容

| 能力 | 落点 | 数据来源（当前） |
|------|------|------------------|
| 方案 D 布局（对话主区 ~72%、可拖拽、右侧可收起） | `/chat/:taskId`、`/kb/:kbId` | 纯前端 |
| 右侧「执行流水线」面板 | 同上 | 单视频 Agent 使用真实 SSE；普通 RAG / 知识库仍根据 RAG SSE 推断 |
| 消息内可折叠「思考与检索轨迹」 | `ChatMessageRow` | Agent 优先读取 `steps[]`；旧消息继续兼容 `trace[]` 或引用条数推断 |
| 流式回答 + 柔和光标 | `streamAsk` → `answer` 事件 | **已实现** |
| 引用卡片完成后淡入 | `citations` 事件后 | **已实现** |
| 问答入口页 `/qa` | 单视频 / 知识库列表 | `GET /media/tasks`、`GET /knowledge-bases` |
| 侧栏「最近可问答」 | `AppShell` | 同上 |
| 单视频 Agent 模式 | `/chat/:taskId` | `streamAgent()` → `/messages/agent/stream` |

原型页仍保留作对比，但已迁入 `frontend/prototype/` 的独立 Next workspace。运行 `npm run dev:prototype` 后才能访问 `/prototype/*`；默认 `npm run build` 不再发现这些路由。

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
| `retrieval_snapshot` | JSON 字符串 → Agent `steps[]`/引用；旧消息降级为 `trace[]` 或「检索 N 条」 |

### 2.3 会话与范围

| API | 用途 |
|-----|------|
| `POST /chat/sessions` + `scope_type: video` + `task_id` | 单视频问答 |
| `POST /chat/sessions` + `scope_type: knowledge_base` + `knowledge_base_id` | 知识库问答 |
| `GET /chat/sessions?task_id=` / `?knowledge_base_id=` | 侧栏会话列表 |

### 2.4 单视频 Agent SSE（当前已实现）

单视频 `/chat/:taskId` 的 Agent 模式调用：

```
POST /api/v1/chat/sessions/{session_id}/messages/agent/stream
{ "question", "top_k", "mode": "agent", "agent_profile"? }
```

当前端点只接受 `mode=agent` 和单视频 session。知识库 session、`mode=research`、`step_update`、`step_delta` 和 `think` 事件均不属于当前流式契约。后端复用已有模板 Video Agent，不输出原始 Chain-of-Thought。

当前成功路径为：

```
run_start
→ step_start + tool_call + tool_result [+ retrieve_hits] + step_done  （工具步）
→ step_start + tool_call + tool_result                              （回答步，等待完成）
→ answer（分片）× N
→ citations
→ step_done（回答步）
→ done
```

前端消费位置：`frontend/lib/api.ts` → `streamAgent()`；轨迹合并位置：`frontend/components/chat/traceTypes.ts`。

## 3. 后端后续缺口

Agent SSE 的第一条纵向切片已经完成，Run/Step/ToolCall/Claim/Evidence 权威持久化也已落地（见 [Agent 总体设计与实施路线](./agent-evolution.md) 与[数据模型](./data-model.md)）。当前右侧流水线的真实事件范围、事件顺序和限制以 [Agent 流式契约](./agent-streaming-contract.md) 为准；以下是后续独立能力，不应和已完成的 SSE 接入混在一起。

### 3.1 知识库范围 Agent

需要将检索工具和证据对象从单 `task_id` 扩展为 session scope-aware，并保留 `task_id`、`video_title`、`evidence_id` 等跨视频定位信息。还需要单独验收权限、空索引、部分视频失败、跨视频引用和成本上限。

### 3.2 研究模式流式化

`mode=research` 目前仍是非流式的有界 Planner/Tool/Observe 实验路径；它与当前模板 Agent SSE 不绑定在同一个改动中。后续需要为 planner、工具执行、观察和停止原因设计安全的流式摘要，但不能输出原始思维链。

### 3.3 RAG 流式中间事件（可选）

在现有 `messages/stream` 上**可选**增加事件（不破坏旧客户端）：

| event | 说明 |
|-------|------|
| `retrieve_start` | 开始向量/BM25 检索 |
| `retrieve_done` | `{ "hits", "query_rewrite?" }` |
| `generate_start` | 开始 LLM 生成 |

前端收到后即可替换当前的「先发问题 → 猜检索 running」推断。

### 3.4 问答入口聚合 API（性能优化，非必须）

当前 `/qa` 调用 `listTasks` + `listKBs` 两次请求。若视频量大，可增加：

```
GET /api/v1/qa/hub
→ { "recent_videos": [{ "task_id", "title", "indexed", "has_transcription" }], "knowledge_bases": [...] }
```

减少列表过滤与 payload。

---

## 4. 数据模型现状与目标

当前已采用复用 `retrieval_snapshot` 的 version 1 Agent envelope：

```json
{
  "version": 1,
  "run_id": "uuid",
  "mode": "agent",
  "steps": [ /* AgentStepEvent 终态数组 */ ],
  "citations": [ /* 与现网 Citation 相同 */ ]
}
```

这解决了当前历史回放问题，但不替代已经落地的独立 Run/Step/Claim/Evidence 权威模型。`parseMessages()` 通过 `snapshotTraceAdapter.ts` 优先读取 `steps[]`，并兼容旧 `trace[]` 和纯 `Citation[]` 快照。

---

## 5. 前端当前接入结果

1. `frontend/lib/api.ts` 已提供 `streamAgent()`；`SSEStreamDecoder` 处理任意 chunk 边界并支持 `AbortSignal`。
2. `useConversationSession` 统一正式视频/知识库页的会话加载、消息 patch、流式取消和终态；页面只保留各自的 scope 与展示策略。
3. `frontend/app/chat/[taskId]/page.tsx` 的单视频 Agent 模式调用 Agent SSE；普通 RAG 仍调用 `streamAsk()`。
4. `frontend/app/kb/[kbId]/page.tsx` 暂不接入 Agent，继续使用 RAG 和推断轨迹。
5. `agentTraceReducer` 按 `run_id + step_id` 幂等合并；历史 `steps[]`、旧 `trace[]` 与 bare citations 只经快照适配器回放。

---

## 6. 相关代码索引

| 区域 | 路径 |
|------|------|
| 聊天布局与状态 | `frontend/components/chat/ChatShell.tsx`、`useConversationSession.ts` |
| 右侧流水线 | `frontend/components/chat/AgentLensOverlay.tsx`、`frontend/components/chat/chatUtils.ts` |
| 轨迹类型 / 推断 | `frontend/components/chat/traceTypes.ts` |
| 流式消费 | `frontend/lib/api.ts`、`frontend/lib/streamDecoder.ts` |
| 历史兼容 | `frontend/components/chat/snapshotTraceAdapter.ts` |
| 原型 workspace | `frontend/prototype/`、`frontend/components/prototype/` |
| Agent 后端（实验） | `internal/handler/chat.go`、`internal/service/` agent runner |
| 路由注册 | `cmd/server/router.go` → `messages/agent` |

---

## 7. 后续验证顺序

- 先以现有单视频 Agent SSE 为基线，补浏览器联调和断开/取消场景验证。
- 再实现长期记忆的范围隔离、有限召回、异步失败降级和删除语义。
- 随后建立 Claim/Evidence 账本，再扩展多粒度证据漏斗和知识库范围 Agent。
- 每项能力独立提交，commit subject 直接描述实际变更，不使用进度编号或阶段代号。
