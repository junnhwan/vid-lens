# 数据模型与存储边界

## 在线持久化事实

PostgreSQL 是在线关系数据源，负责保存用户、资产、视频任务、任务阶段、转写、视觉观察、摘要及其修订与术语规则、知识库、聊天会话（含 `video`、`video_library`、`knowledge_base` 三种范围）、Agent 执行状态、Agent 长期记忆与记忆抽取任务、学习笔记（artifact）及其版本、来源快照、编辑记录和恢复位置、AI 配置（用户 profile、托管配置、prompt 偏好）、AI 调用和配额记录。当前在线 schema 由 `internal/model.AllModels()` 定义。

Agent 执行状态由 `agent_runs`、`agent_steps` 和 `agent_tool_calls` 三张权威表组成。`agent_runs` 已被多个执行主体共用：除 `subject_kind/subject_id/execution_kind/recipe_version` 外，Run 在创建时冻结 owner、goal、脱敏 AI profile、工具白名单、policy 和 budget，聊天 Run 另冻结 session 与检索 scope（单视频或知识库），artifact 主体则冻结自己的 source video scope。`internal/model/artifact_migration.go` 建立的 `chk_agent_run_subject` 约束是主体枚举的权威：`chat_session` 必须有非空 `session_id` 且 `execution_kind='chat'`；`generation_request`、`artifact_edit_request`、`summary_edit_request` 必须 `session_id IS NULL`、`subject_id` 非空、`execution_kind='artifact'` 且带非空 `recipe_version`。聊天路径在写入时把主体改写为 `chat_session`（`session_id` 的字符串形式），笔记生成、笔记编辑和摘要修订则把各自的请求 ID 写入 `subject_id`。Step 以 `(run_id, step_id, attempt)` 唯一，使用 lease token、到期时间和 version CAS 控制接管；ToolCall 同时覆盖普通工具、Planner LLM 和验证动作，保存经过验证的参数 digest、安全输入摘要、调用 digest、输出引用、结果 digest、证据引用、最终引用投影、分级命中/覆盖指标、耗时、token/cost 及 usage 来源和错误终态。Planner 的 token 只能从 provider 实际 usage 或明确标记为 estimated 的估算值写入；没有价格表时 cost 保持未知的零值而不伪造费用。已完成 step 的安全结果 checkpoint 用于 Agent 循环重建，不包含 provider prompt、Planner 草稿或 Chain-of-Thought。

lease 到期的只读检索 step 可以由另一个 worker 用 CAS 接管。LLM/视觉等不可安全重放的调用如果在 provider 返回和 PostgreSQL 终态提交之间中断，会进入 `ambiguous` 并 fail-closed；同一 attempt 不会自动再次调用，显式新 attempt 仍受 Run 创建时冻结的 attempt、step、tool、LLM 和 vision 预算限制。`completed`、`failed`、`cancelled`、`budget_exhausted` Run 都是单调终态，普通重试不能覆盖。

长期记忆以 `agent_memory_items` 保存 owner/scope 下的最新 item 投影，以 `agent_memory_events` 保存创建、冲突、撤回和删除事件。item/event 是权威数据；`agent_memory_embeddings` 是启用 memory 后按需创建的 pgvector 在线语义召回投影，embedding 失败不会回滚关系 item。撤回或删除 item 时会在同一事务中移除对应投影，避免已撤回内容再次召回。具体权限、召回和治理边界见 [agent-memory.md](agent-memory.md)。

基础引用存于聊天快照，视觉观察单独保存，记忆来源限制不变。`chat_sessions.scope_type` 由 `chk_chat_sessions_scope` 约束限定范围与标识的组合：`video` 必须带 `task_id`，`knowledge_base` 必须带 `knowledge_base_id`，`video_library` 两者都为 0。在线 API、消费者和 RAG 服务以 PostgreSQL 作为关系数据源。


任务和各处理阶段分别记录状态。处理租约使用 token、版本和到期时间做数据库 CAS，使下载、转写、画面观察、摘要和 RAG 索引能够独立重试，并能在故障后继续处理已完成的部分。Agent 恢复只读取上述独立执行表；`chat_messages.retrieval_snapshot` 是面向会话展示的派生快照，不提供执行恢复依据。

`video_transcription_chunks` 保存每次 ASR observation 的稳定 `segment_key`、segmenter version、实际送入 provider 的 `window_start_ms/window_end_ms`，以及互不重叠的 `core_start_ms/core_end_ms`。`overlap_windows_v1` 使用相邻重叠音频帮助恢复跨硬边界语句；`start_second/end_second` 作为现有证据路径的秒级时间投影，覆盖产生该行原始文本的完整 window，而不是更窄的 core。缺少 provenance 的分片不参与文本去重拼接。

转写和摘要以 `task_id` 唯一保存任务自己的结果，以非唯一 `file_md5` 索引查找可复用的最早结果。用户强制重新处理同一文件时，新任务可保存自己的转写或摘要，不会改写其他任务的内容。新转写在处理租约事务中使该任务的 RAG 索引进入重建状态，并移除旧的关系 chunk；实际 ASR 完成后会重新投递索引任务。

Agent 直接通过 CreateAgentRunExchange 事务保存最终消息与快照，按 run 去重；成功后才刷新近期消息和触发偏好提取。完成 run 重放复用原 message_id。

学习笔记（artifact）子系统的数据面保存在同一套在线 schema 中。`artifacts` 用 `head_version` 计数器和 `current_version_id` 指向当前版本；`artifact_versions` 以 `(artifact_id, version)` 唯一，保存 origin、来源清单 ID、`output_role`、候选标记和采纳血缘，版本体作为独立修订存在。`generation_requests` 与 `artifact_edit_requests` 是按 `(user_id, idempotency_key)` 唯一的不可变执行输入，执行状态一律落在 `agent_runs`。生成、笔记编辑和摘要修订各有自己的持久 outbox（`GenerationDispatch`、`ArtifactEditDispatch`、`SummaryEditDispatch`），保存租约 token、到期时间和下一次尝试时间；分表属于兼容契约的一部分，滚动部署中只理解生成队列的消费者不能确认它不认识的编辑或摘要消息。`RunEvent` 以 `(run_id, seq)` 保存持久事件流，供 SSE 续传和刷新后回放。`SourceManifest` 与 `SourceSnapshotItem` 在提交时冻结该视频的转写与画面观察原子及其模态、时间状态和内容哈希，`ArtifactEvidenceRef` 记录版本块到证据 ID 的引用；证据身份由服务端产生，模型和前端都不能新建。摘要修订与术语规则分别由 `SummaryRevisionHead`/`SummaryRevision` 与 `VideoTermRuleHead`/`VideoTermRuleVersion` 保存，规则版本是完整快照，运行中的请求可以读回它冻结的那一版。`LearningPosition` 每个用户一行，用 revision 做跨标签页 CAS；`AnswerImport` 与 `ArtifactEditOutcome` 保存幂等结果；`ArtifactCanvasLayout` 按内容版本和视图保存独立的画布修订，笔记内容仍留在版本表。模型定义见 `internal/model/artifact.go`、`internal/model/artifact_layout.go` 和 `internal/model/summary_revision.go`。

## 检索数据

转写内容按检索粒度写入 `video_chunks`，这是 RAG 内容与来源映射的主要事实来源。每行保存 modality、毫秒范围、`exact/coarse/unknown` 时间状态、`mapped/partial/unmapped` 映射状态、稳定 source refs 和 chunker provenance。ASR source ref 优先使用 `segment_key`，视觉 source ref 使用稳定 frame observation ID；`chunk_index` 只表示展示顺序，不能映射 ASR identity。pgvector 是唯一的向量后端，向量投影写入配置的向量表。检索命中必须从关系行回填 provenance；缺少完整来源映射的行安全降级为 `unknown/unmapped`。

向量索引属于可重建投影，用于相似度检索和对账，不能替代 `video_chunks` 等关系数据中的源事实。对应的模型定义位于 [`internal/model/`](../../internal/model/)，向量适配器位于 [`internal/vector/`](../../internal/vector/)。

## 外部存储和协调组件

- MinIO：视频、音频和其他大对象
- RabbitMQ：异步任务投递、消费、手动确认和重试
- Redis：限流、配额、缓存和短期协调状态
- PostgreSQL：业务状态、转写内容、聊天记录、检索事实和审计数据


外部对象和向量投影清理必须具备幂等性；数据库中的任务状态负责记录清理意图和最终处理结果。
