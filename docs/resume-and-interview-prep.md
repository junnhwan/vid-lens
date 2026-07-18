# 简历与面试准备总兼容入口

> 状态：已退役的大型聚合稿。文件名保留用于兼容旧链接；当前内容不再在这里重复维护。

## 唯一入口

| 目的 | 当前文档 |
|---|---|
| 复制简历文字 | [`resume-final-draft.md`](resume-final-draft.md) |
| 决定哪些能力可以讲 | [`resume-interview-defense-index.md`](resume-interview-defense-index.md) |
| 按专题练习 | [`resume-topics/00-index.md`](resume-topics/00-index.md) |
| 讲真实故障和排查 | [`troubleshooting-and-interview-notes.md`](troubleshooting-and-interview-notes.md) |
| 找代码 owner 和调用链 | [`backend-maintenance-map.md`](backend-maintenance-map.md) |
| 判断文档优先级 | [`README.md`](README.md) |

## 当前项目一句话

VidLens 是一个 Go 编写的 AI 视频理解后端：视频进入 MinIO 后由 Kafka 驱动下载、分段 ASR、摘要和 RAG 索引，PostgreSQL 保存业务状态与文本事实，pgvector 保存向量投影，问答使用向量与关键词候选融合并返回引用。

## 当前迁移边界

- 正式本地架构已经是 PostgreSQL + pgvector 单库。
- MySQL/Milvus 仍保留离线回滚资产，但默认 server 不连接它们。
- 本地迁移和 smoke 已完成；远端环境尚不能声称完成迁移。
- 相关工作区改动尚未完成最终提交收口。

## 文档维护原则

不要在本文件恢复“所有内容都放一份”的模式。项目事实只维护一次，其他入口链接过去；历史描述必须明确标记为迁移背景或已退役协议。
