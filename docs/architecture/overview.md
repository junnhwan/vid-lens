# 架构总览

VidLens 是面向视频的 AI 知识库与问答平台。系统把视频处理拆成可重试的异步阶段，并把转写内容加工成可检索、可引用的知识。

## 运行拓扑

- `frontend/` 是当前唯一的 Next.js Web 前端，独立构建和部署。
- `cmd/server/` 是 Go 后端入口，负责 HTTP API、依赖组装和异步任务消费者的运行时启动。
- `deploy/frontend-deploy.sh` 和 `deploy/server-deploy.sh` 分别发布前端和后端，两个发布过程相互独立。
- 后端依赖 PostgreSQL、Redis、MinIO、RabbitMQ 和按能力配置的 AI 服务。

## 主要组件

- `cmd/server/`：HTTP 服务入口、路由、依赖组装和运行时启动
- `internal/handler/`：HTTP 接口层；聊天 handler 只负责协议解析、状态码和 SSE 写出
- `internal/service/`：任务、媒体、聊天、RAG 和 AI 调用编排；`ConversationExecution` 统一普通/Agent 会话执行，`AgentExecutionJournal` 统一可恢复 Run/Step 持久语义
- `internal/mq/`：RabbitMQ 投递、消费、重试、手动确认和处理租约
- `internal/repository/`：关系数据访问和持久化边界
- `internal/storage/`：MinIO 对象存储适配器
- `internal/vector/`：pgvector 唯一向量后端实现
- `internal/ai/`：LLM、ASR、Embedding、Rerank、Vision 协议适配及调用治理
- `internal/observability/`：结构化日志、指标和运行状态观测
- `frontend/app/`：正式 Next.js 产品路由
- `frontend/components/chat/`：`ConversationSession`、历史快照适配和聊天展示模块
- `frontend/prototype/`：独立的开发原型 Next workspace，不进入默认 production build

`frontend/go.mod` 仅是 Go 工具链的模块边界：npm 的可复现依赖 `flatted` 自带 `golang/` 示例源码，若没有该边界，安装前端依赖后从仓库根运行 `go test ./...` 会把第三方示例误当成本项目包。该空模块不包含业务 Go 代码，也不参与前端构建。

## 视频处理链路

当前链路的 ASR 边界、时间轴证据、多模态索引和延迟改造计划见[视频理解管线改造计划](media-understanding-pipeline.md)。该计划以已有 Vision/OCR 和证据账本为基线，不另建第二套视频事实源。

1. 前端上传视频或提交远程视频地址。
2. API 在 PostgreSQL 创建任务和阶段记录，再把下载、转写、摘要和索引阶段投递到 RabbitMQ。
3. 消费者使用 FFmpeg 提取音频、生成带边界上下文的长音频时间窗，并按转写分片调用 ASR；相邻输出只在窗口元数据证明重叠时做确定性拼接。
4. 转写内容切块后写入 PostgreSQL，并生成 pgvector 检索所需的向量投影。
5. 处理失败时由阶段级状态、处理租约、幂等键和重试调度共同恢复，已完成的阶段不需要重复执行。

## 问答链路

标准问答和显式 Agent 请求都先进入 `ConversationExecution`：它统一 profile/client 准备、模式选择和取消传播，之后分别调用标准聊天或受限 Agent。标准问答由意图路由选择执行策略，再进入检索、排序、证据约束和回答生成。具体阶段见[检索与回答链路](retrieval.md)。

Agent 的 Template、Research 和 Evidence Funnel 策略保持独立；它们只通过 `AgentExecutionJournal` 共享 Run 创建/恢复、lease/CAS、checkpoint、预算和单调终态语义。`/chat/.../messages/agent` 是显式调用的实验性 Video Agent 工具循环，不是默认产品问答路径；默认请求仍使用标准聊天接口。

前端的正式视频聊天与知识库聊天通过 `useConversationSession` 共用会话加载、发送、取消、消息 patch 和终态处理；SSE chunk 边界由独立 decoder 处理。旧持久快照只在兼容适配器中解析，不能参与实时 trace reducer 或后端执行恢复。兼容字段的 owner 与删除条件见[兼容边界清单](compatibility.md)。

## Repository seam 决策

本轮只新增 `AgentExecutionStore`：`AgentExecutionJournal` 已拥有稳定的八操作持久接口，生产 adapter 是现有 PostgreSQL/GORM `AgentExecutionRepository`，行为测试使用确定性的 scripted adapter。该接口比 `Repositories` 聚合小，并能在不暴露仓储内部状态的情况下验证 lease、CAS、checkpoint 和 replay，因此是真实变异点。

没有继续为 Conversation、Memory 或 RAG 批量包装 repository。`ConversationExecution` 的变异点是 profile/client/chat/agent module，而不是一组数据表；Memory 已有针对召回、写入和 embedding 的用途型 seam；RAG 与清理路径仍需要现有事务和多表协作，当前没有第二个 production adapter 或独立事务策略。出现这些真实条件前，保留 `*repository.Repositories` 比转发约 23 个仓储方法更清晰。

## 设计边界

- PostgreSQL 是在线业务数据源，也承载 `video_chunks` 和 pgvector 投影；历史 MySQL 数据源与迁移工具已退役。
- pgvector 是唯一的向量后端。
- MinIO 保存视频、音频和其他大对象；Redis 用于限流、配额、缓存和短期协调状态，不承担主要业务事实。
- RabbitMQ 负责长耗时阶段的异步调度和失败恢复；向量索引是可重建投影，不能替代关系数据中的源事实。
