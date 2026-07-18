# 最终简历问答兼容入口

> 状态：此文件原本复制了大量简历问答，现已停止独立维护。旧正文包含 MySQL、Milvus、Redis Set 分片进度和 MinIO `ComposeObject` 等已退役事实，不能再作为当前实现依据。

## 当前应该阅读什么

1. 简历唯一维护稿：[`resume-final-draft.md`](resume-final-draft.md)
2. 面试防守总入口：[`resume-interview-defense-index.md`](resume-interview-defense-index.md)
3. 专题索引：[`resume-topics/00-index.md`](resume-topics/00-index.md)
4. 真实故障记录：[`troubleshooting-and-interview-notes.md`](troubleshooting-and-interview-notes.md)
5. 代码维护地图：[`backend-maintenance-map.md`](backend-maintenance-map.md)

## 当前统一事实

- PostgreSQL 是唯一正式关系数据库，pgvector 是同库扩展。
- MySQL 和 Milvus仅作为迁移观察期回滚资产，不参与默认 server 运行，也没有双写。
- 分片上传状态由 PostgreSQL upload session/chunk ledger 持有，MinIO 保存字节，Redis 不参与上传正确性。
- RAG 使用 ASR 原文；`video_chunks` 是文本事实源，pgvector 是可重建向量投影。
- 正式检索是 pgvector 向量召回、Go 侧 BM25-style 关键词召回和 RRF 融合；不能说专业搜索引擎 BM25 或线上模型 rerank。
- task/job 入库与首次 Kafka enqueue 目前尚未实现 transactional outbox，不能声称原子提交或 exactly-once。

## 为什么保留这个文件

保留文件名只是为了兼容旧书签、文档链接和个人复习路径。新增面试问题应写入对应 `resume-topics/` 专题；不要再次在这里复制实现说明。
