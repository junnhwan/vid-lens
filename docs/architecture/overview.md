# 架构总览

VidLens 是面向视频的 AI 知识库与问答平台。系统把视频处理拆成可重试的异步阶段，并把转写内容加工成可检索、可引用的知识。

## 主要组件

- `cmd/server/`：HTTP 服务入口、路由和运行时组装
- `internal/handler/`：HTTP 接口层
- `internal/service/`：任务、媒体、RAG、聊天和 AI 调用编排
- `internal/mq/`：RabbitMQ 投递、消费、重试和租约
- `internal/repository/`：关系数据访问
- `internal/storage/`：MinIO 对象存储
- `internal/vector/`：向量存储适配器
- `frontend/`：Next.js Web 应用

## 处理链路

1. 前端上传视频或提交远程视频地址。
2. API 创建任务，并把下载、转写、摘要和索引阶段投递到 RabbitMQ。
3. Worker 使用 FFmpeg 提取音频、切分长音频，并按分片调用 ASR。
4. 转写内容切块后写入 PostgreSQL，并生成向量检索所需的投影。
5. 聊天请求执行查询改写、混合检索和排序，回答同时返回可回溯的引用片段。

## 设计边界

- PostgreSQL 保存任务、转写、聊天和评测相关的持久化事实。
- MinIO 保存视频和其他对象数据。
- Redis 用于缓存、配额和短期协调状态，不承担主要业务事实。
- RabbitMQ 负责长耗时阶段的异步调度和失败恢复。
- 向量索引是可重建的检索投影，不能替代关系数据中的源事实。
