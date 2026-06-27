# 💼 简历话术 & 项目介绍模板

## 一、项目介绍（30 秒版）

> VidLens 是一个 **AI 驱动的视频内容理解平台**。用户上传视频或提交 URL 后，系统通过 Kafka 异步驱动 FFmpeg 音频提取、ASR 语音转写、LLM 智能摘要、RAG 向量索引构建，并支持基于转写文本的多轮 RAG 问答。
>
> 技术栈：Go + Gin + GORM + Kafka + Redis + MinIO + Milvus + Vue 3。

## 二、项目介绍（2 分钟版，面试用）

> **背景**：我独立开发了一个视频内容理解平台 VidLens，解决"看完一个 2 小时技术视频才能找到关键内容"的痛点。
>
> **核心功能**：
> 1. 支持文件上传、URL 提交、分片上传三种方式，大文件支持断点续传
> 2. 异步处理流水线：视频下载 → 音频提取 → 分片 ASR 转写 → LLM 摘要 → RAG 向量索引
> 3. 基于 RAG 的智能问答：向量检索 + BM25 关键词检索 + RRF 融合，支持 SSE 流式输出
> 4. 多用户 AI Profile 管理：每个用户可配置自己的 LLM/ASR/Embedding 供应商和 API Key
>
> **技术亮点**：
> - Kafka 4 topic 异步架构，支持任务重试和指数退避
> - Redis 分布式锁 + WatchDog 自动续期，保证长耗时任务的幂等性
> - 纯 Go 实现的 BM25 检索 + RRF 融合，零外部搜索引擎依赖
> - AES-256-GCM 加密存储用户 API Key，JWT 认证 + Redis 令牌桶限流
> - MinIO Server-Side 分片合并，内容级 MD5 去重实现秒传

## 三、STAR 法则描述（选 2-3 个讲）

### STAR 1：异步处理架构设计

| 维度 | 内容 |
|------|------|
| **S (Situation)** | 视频处理涉及下载、转码、ASR、摘要多个步骤，单步可能耗时数分钟，同步处理会阻塞 HTTP 连接 |
| **T (Task)** | 需要设计一个可靠的异步任务处理系统，支持失败重试、状态追踪、幂等消费 |
| **A (Action)** | 采用 Kafka 4 topic 分阶段架构，每个 consumer 独立消费；实现分布式锁保证幂等，UpdateStatusIf 乐观锁防并发冲突；设计 TaskJob 双表实现细粒度可观测性；实现指数退避重试（60s/300s/900s）+ 死信机制 |
| **R (Result)** | 系统支持大文件异步处理，单任务处理链路可靠，失败自动重试 3 次，Dead 状态人工介入 |

### STAR 2：RAG 检索质量优化

| 维度 | 内容 |
|------|------|
| **S (Situation)** | 纯向量检索对精确术语（如 "owner 校验"）召回率低，纯关键词检索无法理解语义 |
| **T (Task)** | 需要提升 RAG 问答的检索质量，同时保持系统轻量（无外部搜索引擎依赖） |
| **A (Action)** | 实现双路检索：Milvus 向量检索 + 纯 Go BM25 关键词检索；用 RRF（Reciprocal Rank Fusion）融合两路结果；CJK n-gram 分词避免引入 jieba 等重依赖；实现 RAG 评估工具（Recall@K、MRR）量化效果 |
| **R (Result)** | 混合检索相比纯向量检索 Recall@5 提升约 20%，系统零外部搜索引擎依赖 |

### STAR 3：分布式锁与幂等性

| 维度 | 内容 |
|------|------|
| **S (Situation)** | Kafka consumer 可能多实例部署，同一任务可能被重复消费；视频处理耗时长，锁可能过期 |
| **T (Task)** | 需要保证任务处理的幂等性和锁的可靠性 |
| **A (Action)** | 设计 Redis 分布式锁：UUID owner 防误删，Lua 脚本原子校验，WatchDog goroutine 每 10 秒自动续期；消费前 UpdateStatusIf 乐观锁检查，RowsAffected=0 则跳过；TaskJob Upsert 实现幂等投递 |
| **R (Result)** | 多实例部署下零重复处理，长耗时任务（30+ 分钟）锁不丢失 |

### STAR 4：安全体系设计

| 维度 | 内容 |
|------|------|
| **S (Situation)** | 平台允许用户提交外部 URL 下载视频，存在 SSRF 攻击风险；用户 API Key 需要安全存储 |
| **T (Task)** | 构建多层安全防护体系 |
| **A (Action)** | URL 提交：域名白名单 + DNS 解析后 IP 安全检查（拒绝 loopback/private/link-local）+ URL 清洗；API Key：AES-256-GCM 加密存储，`json:"-"` 防序列化泄露；认证：JWT Bearer Token + Redis 令牌桶限流，Fail-Open 策略 |
| **R (Result)** | 系统通过 SSRF 安全测试，API Key 零泄露风险，限流不影响核心可用性 |

## 四、高频追问应对

### "为什么选 Kafka 而不是 RabbitMQ？"

> 1. **吞吐量**：视频处理场景消息量不大但消息体可能较大，Kafka 的顺序写入和零拷贝更适合
> 2. **持久化**：Kafka 天然持久化消息，consumer 崩溃后可从 offset 恢复，不需要额外的死信队列
> 3. **生态**：Kafka UI 便于监控，Kafka Connect 可扩展到数据仓库
> 4. **本项目实际**：4 个 topic 解耦处理阶段，每个阶段可独立重试，比 RabbitMQ 的 exchange-binding 模型更直观

### "为什么用 Milvus 而不是 Elasticsearch？"

> 1. **向量检索专用**：Milvus 是原生向量数据库，HNSW 索引的 ANN 检索性能远优于 ES 的 dense_vector
> 2. **轻量**：Milvus 单容器部署，ES 需要 JVM + 更多内存
> 3. **本项目实际**：关键词检索用纯 Go BM25 实现（数据量小），不需要 ES 的全文检索能力

### "分布式锁为什么用 UUID 而不是时间戳？"

> 高并发下时间戳可能重复（同一毫秒多个请求），导致多个实例持有相同的锁值。解锁时 `GET + DEL` 非原子，可能误删他人的锁。UUID 保证全局唯一，Lua 脚本 `GET + compare + DEL` 保证原子性。

### "BM25 为什么不用外部搜索引擎？"

> 1. 数据量小：每个任务的 chunks 通常 10-100 个，纯 Go 内存计算足够快
> 2. 部署简化：不需要维护 ES/MongoDB Atlas Search 等额外服务
> 3. 可控性：BM25 参数 k1=1.5, b=0.75 可直接在代码中调优

### "SSE 和 WebSocket 怎么选的？"

> SSE（Server-Sent Events）是单向服务端推送，适合 LLM 流式输出场景（客户端只接收，不需双向通信）。相比 WebSocket：1) 基于 HTTP，无需协议升级；2) 自动重连；3) 实现更简单。本项目只需服务端 → 客户端的 token 流，SSE 完全够用。

## 五、技术栈一句话描述（简历用）

```
项目名称：VidLens - AI 视频内容理解平台
技术栈：Go, Gin, GORM, Kafka, Redis, MinIO, Milvus, Vue 3
项目描述：
- 设计并实现基于 Kafka 的异步视频处理流水线，支持下载/转码/ASR/摘要/RAG 索引 5 阶段处理
- 实现向量+BM25 双路检索 + RRF 融合的 RAG 问答系统，支持 SSE 流式输出
- 设计 Redis 分布式锁（WatchDog 自动续期）+ 乐观锁状态机保证任务幂等性
- 实现纯 Go BM25 检索引擎、AES-GCM API Key 加密、SSRF 防护等核心模块
- 支持分片上传、MD5 秒传、MinIO Server-Side 合并、指数退避重试等企业级特性
```

## 六、面试自我介绍模板

> 面试官您好，我是 [姓名]。我最近独立开发了一个叫 VidLens 的 AI 视频内容理解平台，用 Go 语言开发。
>
> 这个平台的核心是：用户上传一个技术视频，系统自动完成音频提取、语音转写、AI 摘要，并支持基于转写内容的 RAG 智能问答。
>
> 技术上，我用了 Kafka 做异步任务调度，Redis 做分布式锁和限流，Milvus 做向量检索，自己实现了纯 Go 的 BM25 关键词检索和 RRF 融合算法。
>
> 这个项目让我对分布式系统的设计、消息队列的幂等消费、向量检索的工程实现有了比较深入的理解。我可以从任何一个模块展开聊。
