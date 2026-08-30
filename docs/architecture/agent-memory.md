# VidLens Agent Memory：长期记忆设计

状态：最小纵向切片已实现并完成安全加固；持久队列与知识库 Agent 接入仍待增强

核验时间：2026-08-30（Asia/Shanghai）

本文从 [agent-evolution.md](agent-evolution.md) 拆出长期记忆的边界和最小实现。长期记忆是 Agent 的上下文基础设施：它可以提供有限的历史信息，但不拥有目标分解、工具选择、验证或停止能力。

## 现状

VidLens 的短期会话上下文仍由 `internal/service/chat.go` 中的 `ChatMemoryStore` 和 `RecentTurns` 管理；它没有被改名或冒充为长期记忆。长期记忆最小切片现已独立落在 `agent_memory_items`、`agent_memory_events`、`MemoryProvider` 和异步 `MemoryWriter` 上，并只在显式启用配置时接入模板 Video Agent。

长期记忆仍不是 Agent：它不选择工具、不验证视频 Claim，也不拥有循环或停止条件。

## 已实现的最小切片

- `internal/model/memory.go` 定义 `user`、`video`、`knowledge_base`、`run` 四类 scope，以及 item/event 权威模型。item 包含 kind、content、source、importance、embedding ref、生命周期时间、status、version 和软删除字段。
- `internal/repository/memory.go` 使用 GORM/PostgreSQL 保存 item 与追加事件；同一 owner/scope/kind 的不同内容会同时保留并标记 `conflicted`，精确重复内容不会静默覆盖原记录。PostgreSQL advisory transaction lock 解决首条记录尚不存在时的并发写竞争，有限查询命中冲突项后会从关系表扩展完整冲突组。
- 删除采用 `deleted` 状态、版本递增、事件和 GORM tombstone；撤回采用 `withdrawn` 状态与事件。两者会在同一事务中移除 pgvector 投影，并且与过期、无 `source_ref` 的记录一样不会进入召回。
- `ScopedMemoryProvider.Snapshot` 在查询前逐一校验用户、视频和知识库所有权，并在查询中再次固定 `user_id + scope_type + scope_id`。Run 尚无独立表，因此当前以 owner `user_id + run_id` 隔离。
- Snapshot 使用稳定 schema/version hash 和确定性 memory id 顺序；召回同时受 top-k、字符和近似 token 上限控制。冲突项只会成组进入，不会只挑一个值冒充确定事实。
- `AsyncMemoryWriter.Enqueue(candidate)` 和异步 extractor queue 都是非阻塞 best-effort side effect。关系 item 先持久化；pgvector embedding 投影失败只计入 `vidlens_memory_background_total`，不回滚 item，也不影响当前回答。
- 默认 extractor 只识别用户明确表达的回答偏好，输出固定的规范化偏好文本，并只写 `user` scope；包含凭据、token、私钥或数据库认证 URL 的输入会被丢弃，不保存用户原始文本。writer 还会拒绝敏感内容、`agent_answer`/`assistant_response`，读取侧也会排除可能由旧版本留下的敏感 item；video/KB scope 只接受 `verified_claim`、`user_confirmation` 或 `manual` 来源。
- `memory.enabled` 默认为 `false`。启用后模板 Video Agent 和非流式 research Agent 在执行前读取 `user + current video + current run` snapshot。最终回答工具只能使用服务端注入的可信 snapshot；planner 参数不包含 `memory_context`，未知字段会被拒绝。聊天历史只持久化 snapshot schema/version/memory ids，不保留内容或 source ref。
- 当当前 AI profile 的 embedding 与 pgvector 投影可用时，snapshot 以请求 `query` 的语义相似度产生候选；embedding、投影或在线向量查询不可用/为空时退回关系排序。普通 RAG、KB RAG 和现有 SSE 行为在关闭 memory 时不变。
- 已提供鉴权后的 `GET /api/v1/memories`、`POST /api/v1/memories/:memory_id/withdraw` 和 `DELETE /api/v1/memories/:memory_id`；服务端始终以 JWT user id 和资源 owner 再次校验，客户端不能指定其他 owner。

## 参考启发与适用边界

[AGI-saber 本地 checkout](https://github.com/wujingle488-crypto/AGI-saber) 的 `internal/memory/memory.py` 将 ShortTerm、LongTerm、Preference 分层，长期项有 embedding、importance、相似度召回、去重和衰减；`memory_writer.py` 通过异步单 worker 将抽取和写入解耦；`restore.py` 在运行前恢复上下文；`graph_memory.py` 可选 Neo4j 图扩展。

这是“接口分层、异步写入、有限召回和治理”的有用参考。它的本地提交为 `f85a1da776de76dafbf9302d147a18ad0ea0bdaf`；用户指定的 [AGI-saber/AGI-saber-go](https://github.com/AGI-saber/AGI-saber-go) 地址在核验时不可访问，目录 remote 实际指向上述仓库。以下是基于本地源码的判断。

不照搬其通用图运行时、Neo4j、工具沙箱或 planner。其 `Item` 模型没有足够的 VidLens source/scope/consent 语义，部分持久化同步是 best-effort/占位；这些实现只能作启发，不能作为生产契约。

## 作用范围

每项记忆必须属于一个明确 scope：

| scope | 例子 | 默认可见性 |
|---|---|---|
| `user` | 用户偏好的回答语言、关注主题 | 该用户的会话；不支持视频事实 |
| `video` | 某视频的稳定别名、用户确认的主题 | 该视频的后续 Run |
| `knowledge_base` | KB 的人工说明、领域术语 | 该 KB；不可越权到其他 KB |
| `run` | 本次 Run 的中间摘要或未决问题 | 只在本次 Run；终态后可转为审计记录而非长期记忆 |

记忆项最少需要：`id`、`scope_type`、`scope_id`、`kind`、`content`、`source_type`、`source_ref`、`importance`、`embedding_ref`、`created_at`、`last_used_at`、`expires_at`、`status`、版本和删除标记。`source_ref` 应能指向消息、Run、Claim 或人工确认，禁止无来源的“模型记得”。

## 读路径

请求开始时按 scope 取最近消息和少量语义记忆，形成不可变 `MemorySnapshot`：

```text
session recent messages
  + user memories
  + video/KB memories for current authorized scope
  → deduplicate → conflict mark → top-k/token cap
  → Agent context
```

召回排序可以结合语义相似度、importance、新鲜度和 scope 优先级，但必须满足：

- 当前视频证据优先于历史记忆；
- 记忆只影响目标理解和检索提示，不直接进入 Claim 的支持集合；
- 每次召回有数量、字符和 token 上限；
- 同一 scope 内的冲突同时保留并标记，不静默挑一个“看起来更像”的值；
- 越权、过期、撤回、低质量或来源不可解析的项不进入 Prompt；
- Snapshot 的版本和 memory ids 写入 Run，便于复现。

## 写路径

记忆写入是策略控制的异步副作用，不是 Agent 的任意工具：

1. 只有明确允许的来源（用户明确偏好、用户确认、稳定视频事实的已验证 Claim）才可产生候选。
2. 规则先过滤敏感信息、凭据、短期闲聊和未验证模型猜测。
3. 需要 LLM 抽取时，输出固定 JSON schema；解析失败丢弃候选，不写入自由文本。
4. 通过去重、冲突、importance、TTL 和 scope 校验后，以事件方式追加。
5. 写入失败不阻塞当前答案；失败计数和原因进入指标。

默认不把所有 assistant 回复自动写成长记忆。尤其不能把 Agent 生成的未核验结论自动提升为 video memory；应先从 Evidence Ledger 的 verified Claim 投影，或经过用户确认。

## 治理和隐私

- 用户可查看、删除或撤回其 `user` 记忆；删除应产生 tombstone/事件，防止旧向量再次召回。
- 视频/KB 记忆遵循资源权限；资源失效时召回立即失效。
- TTL 和 decay 只是排序/过期策略，不能改变证据事实。
- 日志中只记录 memory id、scope、版本、命中数量和原因，不记录完整私密内容。
- 记忆不会跨用户共享；相似内容也不能绕过 scope。
- embedding 是可重建投影；PostgreSQL 的 item/event 是权威。

## 与 Agent 的接口

Agent 只看到结构化 `MemorySnapshot`，不直接操作存储：

```text
MemoryProvider.snapshot(request) -> items + conflicts + provenance + budget
MemoryWriter.enqueue(candidate_event) -> accepted/rejected/best-effort
```

`MemorySnapshot` 是上下文输入，`MemoryWriter` 是受治理的事件出口；两者都不返回 planner 决策，也不实现循环。Agent 的工具白名单、预算、观察和停止规则见 [agent-evolution.md](agent-evolution.md)。

## 落地约束

第一版使用 PostgreSQL 表、事务事件、可选 pgvector 投影和进程内异步 worker，不需要 Neo4j 或新的消息系统。关闭 memory 功能时，默认 RAG 和现有 Agent 结果保持一致。

本次测试已覆盖：用户隔离、四类 scope 隔离、资源越权拒绝、top-k/字符/token 上限、过期/撤回/删除/无来源过滤、冲突解释与并发完整性、删除向量投影、稳定 snapshot ids/version、planner 参数注入拒绝、敏感偏好过滤、语义检索及关系降级、embedding 与异步写入失败时回答成功、历史快照最小化，以及关闭 memory 后全量现有测试不回归。

## 剩余风险与后续边界

- extractor 是保守的规则实现；真实 LLM JSON extractor 仍应通过现有 AI profile 注入，并继续使用相同 candidate 校验和失败降级边界。
- 异步队列当前是进程内有界队列，进程在排空前崩溃会丢失尚未执行的 best-effort 写入；需要更强交付保证时可复用 RabbitMQ，但不能让消息队列成为记忆事实源。
- Run 没有独立持久化模型，本切片只能以 `user_id + run_id` 隔离 run memory；独立 Run/Step 表属于后续工作。
- 当前没有知识库 Agent；因此 `knowledge_base` scope 已具备模型、权限、召回和治理能力，但只会在后续真正的 scope-aware KB Agent 接入，不能把普通 KB RAG 描述为已使用长期记忆。
- 聊天历史采用“只保存 snapshot identity”的删除语义：删除后旧内容不能从历史快照恢复，但历史中仍保留当时用过的 memory id/version 作为最小审计标识。

验证（2026-08-30）：`go test ./...`、`go vet ./...`、`go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit`、`git diff --check` 均通过；未修改前端。
