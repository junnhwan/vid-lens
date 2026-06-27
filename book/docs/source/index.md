# 📖 源码走读总览

## 项目概况

| 维度 | 数据 |
|------|------|
| 语言 | Go 1.24.0 |
| 框架 | Gin + GORM |
| 消息队列 | Kafka (segmentio/kafka-go) |
| 缓存/锁 | Redis (go-redis) |
| 对象存储 | MinIO |
| 向量数据库 | Milvus |
| 前端 | Vue 3 + Vite |
| 源文件数 | 96 个 .go 文件 |
| 核心包 | 11 个 (ai, config, handler, middleware, model, mq, pkg, repository, service, storage, vector) |

## 架构总览

```
HTTP Request
    → Handler (Gin)          internal/handler/
    → Middleware (JWT/限流)   internal/middleware/
    → Service (业务逻辑)     internal/service/
    → Repository (数据访问)  internal/repository/
    → Model (GORM)           internal/model/

异步任务流:
    Service → Kafka Producer → Kafka Consumer → Service → AI/FFmpeg/MinIO
```

## 核心设计模式

| 模式 | 位置 | 用途 |
|------|------|------|
| 策略模式 | `internal/ai/strategy.go` | ASR/LLM 可独立替换 (MiMo / SiliconFlow) |
| 工厂模式 | `internal/ai/factory.go` | 从用户 AI Profile 动态创建客户端 |
| 装饰器模式 | `internal/ai/observed.go` | 为 AI 客户端附加日志记录 |
| Repository 模式 | `internal/repository/` | 聚合 12 个子 Repo + 事务支持 |
| 状态机 | `internal/model/task.go` | 6 种状态 + 条件更新防并发冲突 |
| 分布式锁 | `internal/pkg/lock/` | Redis SetNX + UUID owner + WatchDog |
| 令牌桶 | `internal/middleware/ratelimit.go` | Redis Hash + Lua 原子操作 |
| 观察者 | `internal/service/ai_observer.go` | AI 调用日志同步记录 |

## 数据流全链路

```
用户上传视频
  → MediaService.UploadFile()
    → MD5 去重 → MinIO 上传 → 创建 VideoTask + VideoAsset
    → 投递 Kafka (video-analyze)

Kafka Consumer.handleAnalyze()
  → FFmpeg 提取音频 → 分片 (300s)
  → ASR 逐片转写 → 合并 → 保存 VideoTranscription
  → LLM 摘要 → 保存 AISummary
  → 投递 Kafka (video-rag-index)

Kafka Consumer.handleRAGIndex()
  → 文本分块 → Embedding → Milvus 写入
  → 更新 VideoRAGIndex 状态

用户提问
  → ChatService.Ask()
    → 问题向量化 → Milvus 向量检索
    → BM25 关键词检索 → RRF 融合
    → 构建 RAG Prompt → LLM 回答
    → 保存 ChatMessage + 刷新 Redis 缓存
```
