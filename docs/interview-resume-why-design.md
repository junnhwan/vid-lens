# 简历设计理由兼容入口

> 状态：历史聚合稿，已停止独立维护。设计理由必须回到当前代码、维护地图和专题文档，不再从旧技术栈倒推答案。

## 当前设计主线

- **单库边界**：PostgreSQL 持有业务关系事实，pgvector 保存同库可重建向量投影。
- **长任务边界**：HTTP 创建/请求任务，Kafka consumer 执行下载、ASR、摘要和索引；PostgreSQL 保存 job、lease、失败和重试状态。
- **上传边界**：PostgreSQL 保存 user-bound session/manifest/ledger，MinIO 保存 chunk/final bytes。
- **RAG 边界**：ASR 原文进入 `video_chunks`，向量与关键词候选经 RRF 融合，回答保存 citations 快照。
- **删除边界**：PostgreSQL 先持久化 cleanup intent，再以 lease 驱动外部资源幂等清理。
- **可观测边界**：结构化日志、task/job/AI call 状态、`/healthz`、`/readyz` 和独立 Prometheus `/metrics`；不能夸大成完整 OTel/APM。

## 权威资料

- 为什么这样分模块：[`backend-maintenance-map.md`](backend-maintenance-map.md)
- 当前简历：[`resume-final-draft.md`](resume-final-draft.md)
- 面试专题：[`resume-topics/00-index.md`](resume-topics/00-index.md)
- PostgreSQL 迁移：[`postgresql-single-database-migration.md`](postgresql-single-database-migration.md)
- pgvector 迁移：[`pgvector-migration.md`](pgvector-migration.md)

## 维护规则

新的设计决策先写到对应专项文档，并同步维护地图中的 owner、不变量和失败语义。本文件不再复制完整架构、代码片段或问答。
