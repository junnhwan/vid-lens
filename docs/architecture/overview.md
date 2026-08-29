# 架构总览

VidLens 是面向视频的 AI 知识库与问答平台。系统把视频处理拆成可重试的异步阶段，并把转写内容加工成可检索、可引用的知识。

## 运行拓扑

- `frontend/` 是当前唯一的 Next.js Web 前端，独立构建和部署。
- `cmd/server/` 是 Go 后端入口，负责 HTTP API、依赖组装和异步任务消费者的运行时启动。
- `deploy/frontend-deploy.sh` 和 `deploy/server-deploy.sh` 分别发布前端和后端，两个发布过程相互独立。
- 后端依赖 PostgreSQL、Redis、MinIO、RabbitMQ 和按能力配置的 AI 服务。

## 主要组件

- `cmd/server/`：HTTP 服务入口、路由、依赖组装和运行时启动
- `internal/handler/`：HTTP 接口层
- `internal/service/`：任务、媒体、聊天、RAG 和 AI 调用编排
- `internal/mq/`：RabbitMQ 投递、消费、重试、手动确认和处理租约
- `internal/repository/`：关系数据访问和持久化边界
- `internal/storage/`：MinIO 对象存储适配器
- `internal/vector/`：pgvector 默认实现和 Milvus 兼容适配器
- `internal/ai/`：LLM、ASR、Embedding、Rerank、Vision 协议适配及调用治理
- `internal/observability/`：结构化日志、指标和运行状态观测
- `internal/dbmigration/`、`cmd/mysql-to-postgres/`：仅用于离线历史数据迁移和检查
- `frontend/`：当前 Next.js Web 应用

## 视频处理链路

1. 前端上传视频或提交远程视频地址。
2. API 在 PostgreSQL 创建任务和阶段记录，再把下载、转写、摘要和索引阶段投递到 RabbitMQ。
3. 消费者使用 FFmpeg 提取音频、切分长音频，并按转写分片调用 ASR。
4. 转写内容切块后写入 PostgreSQL，并生成 pgvector 检索所需的向量投影。
5. 处理失败时由阶段级状态、处理租约、幂等键和重试调度共同恢复，已完成的阶段不需要重复执行。

## 问答链路

标准问答路径由意图路由选择执行策略，再进入检索、排序、证据约束和回答生成。具体阶段见[检索与回答链路](retrieval.md)。

`/chat/.../messages/agent` 是显式调用的实验性 Video Agent 工具循环，不是默认产品问答路径；默认请求仍使用标准聊天接口。

## 设计边界

- PostgreSQL 是在线业务数据源，也承载 `video_chunks` 和默认的 pgvector 投影。
- `legacy_mysql` 只供离线迁移工具使用，在线 API 和消费者不读取它。
- Milvus 只在显式配置回滚兼容后端时使用，不是当前默认在线向量后端。
- MinIO 保存视频、音频和其他大对象；Redis 用于限流、配额、缓存和短期协调状态，不承担主要业务事实。
- RabbitMQ 负责长耗时阶段的异步调度和失败恢复；向量索引是可重建投影，不能替代关系数据中的源事实。
