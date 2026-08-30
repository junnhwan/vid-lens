# VidLens Agent Memory：长期记忆设计

状态：设计稿，尚未实现

核验时间：2026-08-29（Asia/Shanghai）

本文从 [agent-evolution.md](agent-evolution.md) 拆出长期记忆的边界和最小实现。长期记忆是 Agent 的上下文基础设施：它可以提供有限的历史信息，但不拥有目标分解、工具选择、验证或停止能力。

## 现状

VidLens 当前只有短期会话上下文：`internal/service/chat.go` 中的 `ChatMemoryStore` 读取和保存最近消息，`RecentTurns` 控制数量；`video_agent.go` 和 research service 会把最近消息作为本次模型输入的一部分。它没有持久化的语义记忆、偏好模型、memory scope、冲突修订和删除 API。

因此不能把当前 `ChatMemoryStore` 命名为 long-term memory，也不能因为它能跨请求保存最近消息就称其为 Agent。

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

第一版只需要 PostgreSQL 表、事务事件、pgvector 投影和异步 worker，不需要 Neo4j 或新的消息系统。写入/召回在现有 ChatService 和 research service 中以可选接口接入；关闭 memory 功能时，默认 RAG 和现有 Agent 结果必须保持一致。

验收条件包括：scope 隔离、有限召回、可复现 snapshot、删除后不可召回、冲突可解释、写入失败不影响回答，以及任何 memory item 都能找到 source_ref。未实现前，文档和代码都应继续称它为“计划中的长期记忆基础设施”。
