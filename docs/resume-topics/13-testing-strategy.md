# 专题 13：测试策略、外部依赖隔离和前端策略测试

> 面试高频问题："你这个项目写了哪些测试？怎么证明不是只跑通 demo？"
> 这类问题不要只回答"我跑了 go test"，要讲清哪些风险被测试覆盖，哪些外部依赖通过 fake/mock 隔离，哪些还需要集成测试补充。

## 1. 先给总答案

推荐先这样答：

> 我项目里的测试重点不是覆盖所有 UI，而是覆盖高风险业务策略：Kafka 消费失败和重试、AI provider 适配、RAG chunk 构建、混合检索、用户 AI profile 加密、分片上传、任务状态策略和前端 SSE 解析。外部依赖比如 AI provider、Milvus、Kafka、MinIO 在单元测试里不会真实调用，而是用 fake client 或 sqlite repository 隔离。验证时后端跑 `go test ./...`，前端跑 `npm test` 和 `npm run build`。

一句话：

> 测试重点放在异步状态、失败治理、RAG 检索和前端策略，而不是只测 happy path。

## 2. 当前有哪些测试层次

### 2.1 Go 后端单元测试

覆盖目录：

```text
internal/ai
internal/config
internal/handler
internal/mq
internal/pkg/ffmpeg
internal/pkg/secret
internal/pkg/ytdlp
internal/repository
internal/service
```

常用命令：

```powershell
go test ./...
```

### 2.2 Repository 测试

repository 层主要用 sqlite 或测试数据库能力验证：

- 创建和查询用户。
- task 状态更新。
- task_job queued/running/failed/completed。
- chat session/message。
- AI profile 默认配置。
- video chunk 替换和 BM25 搜索。
- AI call log 日聚合。

重点：

> repository 测试验证 SQL 条件、唯一约束、状态更新和删除逻辑，不依赖真实 MySQL 容器。

### 2.3 Service 测试

service 层覆盖业务策略：

- MediaService 上传、MD5 复用、删除清理。
- RAGIndexService chunk 切分、embedding 维度校验、索引状态。
- ChatService 问答、citations、Redis memory fallback。
- Retrieval fusion 的 RRF 排序。
- AI profile 创建、更新、加密和默认配置。
- RAG eval 的 Recall@K、MRR、无结果率。

常见 fake：

- fake embedding client。
- fake chat client。
- fake retriever。
- fake vector store。
- fake object storage。
- fake media producer。

### 2.4 MQ / Consumer 测试

这是项目里很关键的测试。

覆盖点：

- 消费任务前检查状态。
- 分布式锁和幂等路径。
- 长音频必须先切片再 ASR。
- 分片转写结果可复用。
- 失败写入 task 和 task_job。
- 可重试错误进入 next_retry_at。
- 不可重试错误快速失败。
- RAG index enqueue 成功/失败状态。
- retry scheduler 到期重新投递。

推荐说法：

> Kafka 本身不在单元测试里真实跑 broker，重点测试 Consumer 的业务状态推进和失败处理。Kafka 是通道，业务正确性在 Consumer 和 repository。

### 2.5 AI provider 测试

覆盖点：

- OpenAI-compatible chat 请求格式。
- StreamingChatClient SSE delta 解析。
- Embedding 响应解析。
- MiMo ASR 请求体和错误处理。
- Observed wrapper 记录成功/失败调用。

不会真实调用外部 AI：

> 测试里用 httptest server 或 fake client，避免消耗真实 token，也避免测试受外部 provider 波动影响。

### 2.6 前端策略测试

前端不是只靠浏览器手测，当前有一组 `.mjs` 策略测试：

```text
web/test/authErrorPolicy.test.mjs
web/test/apiEnvelope.test.mjs
web/test/authSession.test.mjs
web/test/taskActionPolicy.test.mjs
web/test/taskPollingPolicy.test.mjs
web/test/taskDetailPolicy.test.mjs
web/test/taskListLoadingPolicy.test.mjs
web/test/chatStreamParser.test.mjs
web/test/chatHistoryPolicy.test.mjs
web/test/citationDisplayPolicy.test.mjs
web/test/taskResultDisplayPolicy.test.mjs
```

覆盖：

- API envelope 解包。
- 鉴权错误是否清 session。
- 任务按钮是否可点。
- 轮询何时停止。
- 任务详情是否需要补拉。
- SSE 事件解析。
- 聊天历史复用策略。
- citations 展开/收起。
- 长转写/摘要折叠展示。

命令：

```powershell
cd web
npm test
npm run build
```

## 3. 为什么要隔离外部依赖

这个项目依赖很多外部系统：

- Kafka
- Redis
- MinIO
- Milvus
- MySQL
- yt-dlp
- FFmpeg
- ASR/LLM/Embedding provider

如果所有测试都真实启动这些依赖，会有问题：

- 慢。
- 不稳定。
- 需要真实 API Key。
- CI 成本高。
- 很难稳定复现错误。

所以单元测试策略是：

> 业务逻辑用 fake 隔离外部依赖；真正的 Docker Compose 和端到端验证作为单独集成测试或本地手动验证。

## 4. 高频追问

### Q1：你怎么测试 Kafka 消费？

答：

> 单元测试不需要真实 Kafka broker。核心是测试 Consumer 收到消息后的业务行为：状态是否从 queued 到 running，成功是否 completed，失败是否写入 error 和 retry 信息，RAG 索引是否投递，重复任务是否幂等。Kafka 本身是通道，业务正确性在 Consumer。

### Q2：AI 接口怎么测？会不会消耗 token？

答：

> 不会在单元测试里真实调用 AI provider。Chat、Embedding、ASR 都可以用 fake client 或 httptest server 模拟响应，测试请求格式、响应解析、错误处理和记录日志。真实 provider 连通性通过 profile test 或手动集成验证。

### Q3：Milvus 怎么测？

答：

> RAGIndexService 单元测试用 fake vector store，验证 chunk 切分、维度校验、状态更新和 Upsert 调用。Milvus SDK 连接和 collection 行为更适合集成测试，不放在普通单元测试里硬依赖。

### Q4：前端为什么要写策略测试？

答：

> 前端最容易出错的是状态分支，比如任务什么时候停止轮询、失败时按钮能不能点、SSE 事件怎么解析、长 citations 怎么展示。这些逻辑抽成策略函数后，用 Node 测试比只靠页面手测稳定。

### Q5：有没有端到端测试？

答：

> 当前主要是后端单元/服务测试和前端策略测试，还没有完整自动化 E2E。项目本地可以用 Docker Compose 启动 MySQL、Redis、Kafka、MinIO、Milvus，再手动走上传、转写、RAG 问答链路。生产化下一步可以补 Playwright 或 API E2E。

### Q6：怎么证明错误分类有效？

答：

> `internal/mq/retry.go` 有错误分类和 scheduler 测试，覆盖 retryable 和 non-retryable。比如 timeout、429、5xx 会进入退避重试；配置缺失、维度不匹配、文件不存在会快速失败。

### Q7：测试里怎么处理时间？

答：

> 重试策略里有可注入的 `Now`，测试可以固定当前时间，避免依赖真实时钟导致 flaky。前端轮询策略也是纯函数测试。

### Q8：你项目测试的不足是什么？

答：

> 当前不足是自动化 E2E 和真实中间件集成测试还不完整。Milvus、Kafka、MinIO 和真实 AI provider 没有在常规测试里全量跑。后续可以加 docker compose 集成测试、Playwright UI 测试和少量 provider sandbox 测试。

## 5. 可以主动提到的验证命令

后端：

```powershell
go test ./...
```

前端：

```powershell
cd web
npm test
npm run build
```

本地集成验证：

```powershell
docker-compose up -d
go run ./cmd/server
Invoke-RestMethod http://localhost:8080/health
```

## 6. 30 秒话术

> 这个项目测试重点放在高风险业务策略上。后端用 Go 测试覆盖 AI provider 适配、Kafka Consumer 状态推进、失败重试、RAG chunk 构建、RRF 融合、AI profile 加密和 repository 状态更新；外部 AI、Milvus、对象存储用 fake 隔离。前端用 Node 策略测试覆盖 API envelope、鉴权、任务轮询、SSE 解析、chat history 和 citations 展示。常规验证是 `go test ./...`、`npm test` 和 `npm run build`。

## 7. 2 分钟话术

> 我没有把测试重点放在简单 controller happy path，而是放在这个项目最容易出问题的地方：异步任务状态、失败治理、RAG 检索和前端状态分支。
>
> 后端 service 和 mq 测试会用 fake producer、fake retriever、fake embedding、fake chat client、fake vector store 隔离外部依赖。比如 Consumer 测试不会真的启动 Kafka，而是验证收到消息后任务状态如何更新、失败时 retry_count 和 next_retry_at 是否正确、不可重试错误是否快速失败。RAG 测试会验证 chunk 切分、embedding 维度和向量写入调用；Chat 测试会验证 citations、RRF 和流式回答。
>
> 前端也有策略测试，覆盖任务轮询何时停止、SSE 事件怎么解析、长 citations 怎么折叠、鉴权错误怎么处理。这样比只手动点页面更可靠。
>
> 当前不足是还没有完整自动化 E2E，后续可以用 docker compose 集成测试和 Playwright 补齐。

## 8. 不要这么说

- 不要说已经有完整 E2E，如果没有。
- 不要说测试真实调用 AI provider，常规测试应该避免消耗 token。
- 不要说 fake 测试能完全替代集成测试。
- 不要说通过 `go test` 就证明部署环境一定没问题。
- 不要只说"我写了单元测试"，要说覆盖了哪些风险。

## 9. 代码证据路径

```text
internal/ai/*_test.go
internal/config/config_test.go
internal/handler/ai_profile_test.go
internal/mq/consumer_test.go
internal/mq/producer_test.go
internal/pkg/ffmpeg/ffmpeg_test.go
internal/pkg/ytdlp/ytdlp_test.go
internal/pkg/secret/crypto_test.go
internal/repository/*_test.go
internal/service/*_test.go

web/test/*.test.mjs
web/package.json
```

