# 数据模型与存储边界

## 在线持久化事实

PostgreSQL 是在线关系数据源，负责保存用户、资产、视频任务、任务阶段、转写、摘要、知识库、聊天会话、Agent 执行状态、Agent 长期记忆、Agent 证据账本以及 AI 调用和配额记录。当前在线 schema 由 `internal/model.AllModels()` 定义，核心关系图见 [database-schema.svg](database-schema.svg)。

Agent 执行状态由 `agent_runs`、`agent_steps` 和 `agent_tool_calls` 三张权威表组成。Run 在创建时冻结 owner、session、video scope、goal、脱敏 AI profile、工具白名单、policy 和 budget；Step 以 `(run_id, step_id, attempt)` 唯一，使用 lease token、过期时间和 version CAS 控制接管；ToolCall 同时覆盖普通工具、Planner LLM 和验证动作，保存经过验证的参数 digest、安全输入摘要、调用 digest、输出引用、结果 digest、证据引用、最终引用投影、分级命中/覆盖指标、耗时、token/cost 及 usage 来源和错误终态。Planner 的 token 只能从 provider 实际 usage 或明确标记为 estimated 的估算值写入；没有价格表时 cost 保持未知的零值而不伪造费用。已完成 step 的安全结果 checkpoint 用于 research loop 和固定证据漏斗重建，不包含 provider prompt、Planner 草稿或 Chain-of-Thought。

过期的只读检索 step 可以由另一个 worker 用 CAS 接管。LLM/视觉等不可安全重放的调用如果在 provider 返回和 PostgreSQL 终态提交之间中断，会进入 `ambiguous` 并 fail-closed；同一 attempt 不会自动再次调用，显式新 attempt 仍受 Run 创建时冻结的 attempt、step、tool、LLM 和 vision 预算限制。`completed`、`failed`、`cancelled`、`budget_exhausted` Run 都是单调终态，普通重试不能覆盖。

长期记忆以 `agent_memory_items` 保存 owner/scope 下的最新 item 投影，以 `agent_memory_events` 保存创建、冲突、撤回和删除事件。item/event 是权威数据；`agent_memory_embeddings` 是启用 memory 后按需创建的 pgvector 在线语义召回投影，embedding 失败不会回滚关系 item。撤回或删除 item 时会在同一事务中移除对应投影，避免旧向量再次召回。具体权限、召回和治理边界见 [agent-memory.md](agent-memory.md)。

Agent 证据账本由 `agent_claims`、`agent_evidence` 和 `agent_claim_evidence` 三张权威表组成。Claim 保存可审计事实、状态、置信度和追加式 revision；Evidence 保存 RAG `EvidenceID`、视频/文档定位、引用原文、内容哈希、独立的 `source_revision`/`source_revision_status` provenance 及时间区间；关系表显式保存支持、反驳或上下文关系。`EvidenceID` 只标识检索 evidence，不能写入 `source_revision`；当前没有真实处理版本时 revision 为空且状态为 `unavailable`。时间不能从持久化 ASR 范围或视觉帧可靠解析时，Evidence 必须保留 `0/0 + time_range_status=unknown`，对应 Claim 降级为 `uncertain`，不能根据 `chunk_index` 推导时间码。`verified` 仅表示显式绑定具备稳定来源和真实、可重放时间范围，不表示自然语言语义证明。research Planner 的引用会按本轮已观察 Evidence canonicalize，跨视频 evidence 被拒绝；账本按 `user_id + run_id` 隔离，不替代聊天历史快照，也不保存 prompt、Planner 草稿或 Chain-of-Thought。

`legacy_mysql` 只服务于 `cmd/mysql-to-postgres/` 的离线历史数据迁移和检查；在线 API、消费者和 RAG 服务不把 MySQL 当作数据源。

任务和各处理阶段分别记录状态。处理租约使用 token、版本和过期时间做数据库 CAS，使下载、转写、摘要和 RAG 索引能够独立重试，并能在故障后继续处理已完成的部分。Agent research 与 `evidence_funnel` 恢复只读取上述独立执行表；`chat_messages.retrieval_snapshot` 继续是历史 UI 的兼容派生快照，不能作为执行恢复依据。

`video_transcription_chunks` 保存每次 ASR observation 的稳定 `segment_key`、segmenter version、实际送入 provider 的 `window_start_ms/window_end_ms`，以及互不重叠的 `core_start_ms/core_end_ms`。当前 `overlap_windows_v1` 使用相邻重叠音频帮助恢复跨硬边界语句；旧 `start_second/end_second` 继续作为现有证据路径的兼容投影，并覆盖产生该行原始文本的完整 window，而不是更窄的 core。path-only 旧分片没有足够 provenance，不能启用文本去重拼接。

`evidence_funnel` 在校验前只写入带 `run_id` 的不可用 assistant 占位消息，用于取得 Claim 所需的稳定 `message_id`；Evidence/Claim 校验完成后才在同一消息上同时发布正文和终态 snapshot。校验失败时占位消息保留为明确不可用状态，不会把未验证答案注入后续聊天历史。已完成 Run 的同一 `run_id` 从终态消息幂等返回，执行恢复仍不依赖该消息。

## 检索数据

转写内容按检索粒度写入 `video_chunks`，这是 RAG 内容的主要事实来源。默认向量后端是 PostgreSQL 的 pgvector，向量投影写入配置的向量表；Milvus 仍保留兼容适配，但只有显式配置时才使用。

向量索引属于可重建投影，用于相似度检索和对账，不能替代 `video_chunks` 等关系数据中的源事实。对应的模型定义位于 [`internal/model/`](../../internal/model/)，向量适配器位于 [`internal/vector/`](../../internal/vector/)。

## 外部存储和协调组件

- MinIO：视频、音频和其他大对象
- RabbitMQ：异步任务投递、消费、手动确认和重试
- Redis：限流、配额、缓存和短期协调状态
- PostgreSQL：业务状态、转写内容、聊天记录、检索事实和审计数据

`kafka_message_failures` 是在线 schema 中仍在使用的 poison 消息隔离表，但表名沿用了历史 Kafka 命名；当前消息传输使用 RabbitMQ，不应根据表名推断实际消息中间件。

外部对象和向量投影清理必须具备幂等性；数据库中的任务状态负责记录清理意图和最终处理结果。
