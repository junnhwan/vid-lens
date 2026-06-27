# 🎯 面试题总览

VidLens 是一个 **AI 驱动的视频内容理解平台**：上传视频或提交 URL → 异步下载 → FFmpeg 提取音频 → ASR 转写 → LLM 摘要 → RAG 问答。

本章基于项目真实源码，覆盖 **10 大核心模块**，每模块 **10 道面试题**，共 **100+ 道**。

## 模块列表

| # | 模块 | 核心考点 |
|---|------|----------|
| 1 | [架构与启动流程](/interview/architecture/) | 分层架构、依赖注入、中间件链、Gin 路由 |
| 2 | [AI 策略层](/interview/ai-strategy/) | 策略模式、工厂模式、装饰器模式、接口隔离 |
| 3 | [Kafka 异步处理](/interview/kafka-async/) | 生产者/消费者、幂等性、分布式锁、重试退避 |
| 4 | [分布式锁](/interview/distributed-lock/) | Redis SetNX、UUID owner、WatchDog、Lua 脚本 |
| 5 | [令牌桶限流](/interview/rate-limiting/) | Redis Lua 令牌桶、Fail-Open、按路由差异化 |
| 6 | [RAG 检索管道](/interview/rag-pipeline/) | 向量检索、BM25、RRF 融合、SSE 流式输出 |
| 7 | [媒体上传与存储](/interview/media-upload/) | 分片上传、MD5 去重、MinIO Compose、级联删除 |
| 8 | [数据模型设计](/interview/data-model/) | 状态机、垂直拆分、任务-作业双表、软删除 |
| 9 | [Repository 层](/interview/repository/) | 事务管理、条件更新、BM25 纯 Go 实现 |
| 10 | [安全体系](/interview/security/) | JWT 认证、AES-GCM 加密、SSRF 防护、URL 校验 |

## 使用建议

1. **先读源码走读** → 建立全局认知
2. **再刷面试题** → 每题先自己回答，再看参考答案
3. **用追问链深挖** → 面试官最爱问的"为什么"和"如果...怎么办"
4. **最后背八股速查** → 临场快速回忆
