# VidLens 长期记忆与回答偏好

实现核对：2026-09-12。长期记忆为 Chat 和 Agent 提供受授权的偏好上下文，不选择工具、不验证视频事实，也不控制 Agent 循环。

## 存储与边界

PostgreSQL 的 `agent_memory_items`、`agent_memory_events` 和 `memory_capture_jobs` 是关系记忆、生命周期事件和待处理任务的权威状态。pgvector 是可重建的投影。短期上下文仍由 `ChatMemoryStore` 和最近消息管理，长期记忆开关不删除或替代聊天历史。

| scope | 内容边界 | 读取授权 |
|---|---|---|
| `user` | 回答语言、详略、格式等用户偏好 | 当前用户 |
| `video` | 有来源的人工说明或用户确认 | 当前用户拥有的视频 |
| `knowledge_base` | 有来源的术语或人工说明 | 当前用户拥有的知识库 |
| `run` | 本次执行的有限上下文 | 持久化 Run 的 owner |

自动抽取只写 `user` scope。视频/知识库写入接口接受 `verified_claim`、`user_confirmation`、`manual` 来源类型，但本期没有把模型答案自动验证并写成事实记忆的流程，也没有已交付的 Claim Ledger 投影链路。

## 结构化偏好

默认 `ExplicitPreferenceExtractor` 使用确定性规则，提取版本为 `explicit-preferences/v2`：

| kind | 规范化内容 |
|---|---|
| `response.language` | 中文或英文 |
| `response.verbosity` | 简洁或详细 |
| `response.format` | 要点列表或段落 |

抽取要求明确的长期意图，例如“以后”“默认”或 `from now on`；临时请求、已识别的否定和转述表达被保守过滤。凭据、token、私钥和数据库认证 URL 等敏感内容不进入候选。规则只保存规范化偏好文本和来源消息 ID，不复制原始用户文本。它不是完整的自然语言意图识别器。

不同维度共存。同一维度的新明确偏好替代旧值：旧 item 转为 `withdrawn`，版本递增并追加 `superseded` 事件，新值成为 active。来源消息 ID 用于判断先后，迟到的旧来源不能覆盖新选择或 tombstone。同一来源重放复用既有 item，不复活该来源已经撤回或删除的记录；用户通过更新的消息重新明确表达偏好可以建立新记录。

普通非结构化记忆仍保留原有冲突机制：同一 owner/scope/kind 的不同内容成组标记 conflicted，不静默挑选一个值。

旧 `response_preference` 仅对已知规范化文本进行确定性迁移，保留来源、版本与迁移事件。不能确定映射的旧内容留在治理列表，不猜测转换，也不参与自动偏好召回。撤回或删除的旧记录不迁移为 active。

## 用户授权与会话策略

唯一服务端输出是 `EffectiveMemoryPolicy`，由以下三层计算：

| 能力开关 `memory.enabled` | 会话策略 | 用户默认偏好 | effective | reason |
|---|---|---|---|---|
| false | 任意 | 任意 | false | `capability_disabled` |
| true | disabled | 任意 | false | `session_disabled` |
| true | enabled | 任意 | true | `session_enabled` |
| true | inherit | true | true | `user_enabled` |
| true | inherit | false/无记录 | false | `user_disabled` |

能力开关和用户默认值均默认为关闭，历史空会话策略按 `inherit` 处理。策略读取失败时问答可以继续，长期记忆 fail closed，响应使用 `policy_unavailable`，不能伪装成用户主动关闭。

关闭策略阻止后续召回和自动写入，不删除已有 item；重新开启后，仍有效的既有记录可以重新进入候选。治理接口始终允许用户查看、撤回和删除自己的数据。

用户偏好及会话覆盖使用 `expected_version` 乐观并发控制，过期版本返回 HTTP 409。策略更新和 `agent_memory_policy_events` 在同一事务提交。审计记录 actor、目标、前后值、版本、effective 状态和时间，不保存问题或回答。

## 读路径

Chat 与 Agent 使用相同的 effective policy 和用户结构化偏好。普通视频 Chat 和知识库 Chat 只注入用户偏好；Agent 还可召回当前已授权的视频/知识库以及当前 Run 范围。

生产仓库的用户偏好通过 `ListStructuredPreferences` 从关系表读取，不依赖 query embedding。当前实现的其他 scope 也通过有界关系查询读取。通用 retriever 仍保留可选向量检索接口，但不能把它描述为当前偏好读取的必经路径。

来源为用户消息的结构化偏好在读取时再次校验消息 role、owner 和所属会话存在；源消息或会话删除后不进入 prompt。Snapshot 在查询前校验 scope 权限，查询固定 user/scope，随后应用 top-k、字符和近似 token 上限。过期、撤回、删除、无来源或敏感记录被排除，普通冲突项成组处理。

偏好只影响表达和问题理解，当前视频证据优先。Memory 不作为引用证据。Agent planner 和最终回答只使用服务端注入的可信 snapshot，工具参数不能自行提供 `memory_context`。持久化 Agent 历史只保存 snapshot schema/version/memory ids，不保存 snapshot 正文或 source ref；历史 identity 不能恢复已经删除的内容。

## 持久任务与写入

Chat 和 Agent 的成功消息交换与 capture job 在同一数据库事务中提交。任务记录原始 user message ID、owner、session 和 extractor version，不复制正文。任务写入失败会使该消息事务失败，不能声称已经可靠入队；模型回答不等待后台抽取或 embedding。

生产 capture 使用 PostgreSQL outbox 和后台 worker。旧的 `AsyncMemoryWriter.Enqueue` 内存队列仍是非阻塞 best-effort 接口，但它不是生产 capture 的可靠性边界。持久 worker 调用 writer 的写入路径完成关系持久化。

1. CAS 取得 60 秒租约，单次处理限时 45 秒。
2. 按 owner/session 读取原 user message，检查 extractor version 和当前策略。
3. 确定性提取并校验候选；写入事务再次校验 session owner、scope、当前策略及来源消息。
4. 关系 item 与事件提交。结构化偏好无需向量投影；其他记忆的投影失败保留关系事实并单独标记 `projection_pending`。
5. 使用 lease token CAS 完成任务；失败采用有限退避，最多 5 次尝试，过期租约可被回收。

策略更新与 capture 写入按 user 串行化，PostgreSQL 使用 advisory transaction lock 并锁定 session 行。若关闭策略先提交，随后执行的 writer 不得追加；若 writer 的授权事务先提交，该写入发生在关闭之前。缺少 session、owner/scope 不符或源消息失效均不能产生新记忆。

关系 item 真正持久化后才可称“已记住”。任务 pending、失败和投影待补偿是不同状态，不能用队列接受代替写入成功。关闭部署能力时不启动 capture worker。

## HTTP 与界面契约

以下接口要求 JWT，user id 只来自服务端认证上下文。普通响应沿用 `{code,message,data}` 信封。

| 接口 | 行为 |
|---|---|
| `GET /api/v1/memories/preferences` | 读取用户默认偏好、版本和能力状态 |
| `PATCH /api/v1/memories/preferences` | 提交 `enabled`、`expected_version` |
| `GET /api/v1/chat/sessions/:session_id/memory-policy` | 读取 owner-scoped 会话策略和 effective policy |
| `PATCH /api/v1/chat/sessions/:session_id/memory-policy` | 提交 `policy: inherit/enabled/disabled`、`expected_version` |
| `GET /api/v1/memories?scope_type=...&scope_id=...` | 查看授权范围内记忆 |
| `POST /api/v1/memories/:memory_id/withdraw` | 撤回自己的记忆 |
| `DELETE /api/v1/memories/:memory_id` | 删除自己的记忆 |
| `GET /api/v1/memories/capture-status` | 返回 pending/processing/failed 及 projection_pending 数量 |
| `POST /api/v1/memories/capture-retry` | 重新排队当前用户的失败任务 |

删除和撤回均增加版本、追加事件并清理向量投影。客户端不能指定其他 owner。设置页提供记忆治理与任务状态，会话侧提供策略覆盖；读取失败不能显示成空列表。

Chat 与 Agent 非流式响应返回 `memory_policy`；Chat SSE 的 `done` 及 Agent SSE 的 `run_start`/`done` 返回请求的 effective policy。Agent 的 `memory` 字段是实际 snapshot identity；enabled 且零命中与 disabled 是不同情况。

## 验证范围

对应测试覆盖结构化多维偏好、临时/否定过滤、替代与来源顺序、源消息删除、Chat/Agent 授权读取、持久任务恢复、用户/scope 隔离、策略版本冲突及写入前复核。测试文件包括 `memory_preferences_test.go`、`memory_capture_durable_test.go`、`agent_memory_policy_test.go` 和 repository 的记忆相关测试。

这些机制不提供自动视频事实验证、通用知识图谱或任意模型输出记忆。Agent 的工具、预算和停止条件见 [agent-evolution.md](agent-evolution.md)。
