# 最终简历针对性准备兼容入口

> 状态：历史聚合稿，已停止独立维护。原正文中的正式技术栈和上传/RAG 流程已经过期。

## 当前入口

- 最终简历文字：[`resume-final-draft.md`](resume-final-draft.md)
- 六个核心项目点：[`resume-topics/00-index.md`](resume-topics/00-index.md)
- 面试导航和使用方式：[`resume-interview-defense-index.md`](resume-interview-defense-index.md)
- 故障故事与证据：[`troubleshooting-and-interview-notes.md`](troubleshooting-and-interview-notes.md)

## 面试前最小检查

1. 只从 `resume-final-draft.md` 复制技术栈和项目经历。
2. 数据库统一说 PostgreSQL + pgvector 单库；MySQL/Milvus 只用于迁移观察期回滚。
3. 上传统一说 Redis Set 记录临时分片进度、MinIO 保存并服务端合并分片；不要声称已实现 PostgreSQL durable upload session。
4. RAG 统一说 ASR 原文、pgvector、BM25-style、RRF 和 citations。
5. Kafka 统一按 at-least-once、数据库状态/lease 和可恢复失败解释，不声称 exactly-once 或 outbox 已完成。
6. URL 下载只作为自用辅助入口，不作为核心简历能力，也不声称生产级 SSRF 防护。

## 维护规则

本文件只保留兼容导航。简历事实变化时更新专项文档和 `resume-final-draft.md`，不要恢复成另一份大型聚合稿。
