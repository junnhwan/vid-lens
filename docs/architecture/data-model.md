# 数据模型与存储边界

## 在线持久化事实

PostgreSQL 是在线关系数据源，负责保存用户、资产、视频任务、任务阶段、转写、摘要、知识库、聊天会话、Agent 长期记忆以及 AI 调用和配额记录。当前在线 schema 由 `internal/model.AllModels()` 定义，关系图见 [database-schema.svg](database-schema.svg)。

长期记忆以 `agent_memory_items` 保存 owner/scope 下的最新 item 投影，以 `agent_memory_events` 保存创建、冲突、撤回和删除事件。item/event 是权威数据；`agent_memory_embeddings` 是启用 memory 后按需创建的 pgvector 在线语义召回投影，embedding 失败不会回滚关系 item。撤回或删除 item 时会在同一事务中移除对应投影，避免旧向量再次召回。具体权限、召回和治理边界见 [agent-memory.md](agent-memory.md)。

`legacy_mysql` 只服务于 `cmd/mysql-to-postgres/` 的离线历史数据迁移和检查；在线 API、消费者和 RAG 服务不把 MySQL 当作数据源。

任务和各处理阶段分别记录状态。处理租约使用 token、版本和过期时间做数据库 CAS，使下载、转写、摘要和 RAG 索引能够独立重试，并能在故障后继续处理已完成的部分。

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
