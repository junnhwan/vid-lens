# 面试问答脚本兼容入口

> 状态：已退役聚合稿。原文件重复维护了大量 Kafka、MySQL、Milvus、Redis 分片和 RAG 问答，现已由小型专题文档接管。

## 推荐复习顺序

1. [`resume-interview-defense-index.md`](resume-interview-defense-index.md)：先确定可讲范围。
2. [`resume-topics/00-index.md`](resume-topics/00-index.md)：按六个核心点复习。
3. [`troubleshooting-and-interview-notes.md`](troubleshooting-and-interview-notes.md)：准备真实问题、根因和修复过程。
4. [`backend-maintenance-map.md`](backend-maintenance-map.md)：需要指代码时沿 owner 和调用链定位。

## 回答结构

每个问题优先按以下顺序口述：

```text
VidLens 中的具体问题
  -> 不处理会发生什么
  -> 当前代码如何处理
  -> 失败窗口与兜底
  -> 代码证据
  -> 当前限制和下一步
```

不要用“提升扩展性、增强稳定性、削峰填谷”替代项目事实。要说清具体阶段、状态字段、lease、错误记录、重试或资源 owner。

## 当前禁止口径

- MySQL 是正式业务库；
- Milvus 是正式向量库；
- Redis Set 保存当前上传进度；
- 当前分片通过 MinIO `ComposeObject` 合并；
- 已实现 Kafka exactly-once、transactional outbox 或 RAG 全链路强一致；
- 已上线 Cross-Encoder/模型 rerank；
- URL 下载具备生产级 SSRF 防护。

本文件只保留旧链接兼容，不再接受新增问答正文。
