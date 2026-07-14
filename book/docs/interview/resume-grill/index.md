# 简历拷打

> 这一栏按“面试官盯着简历逐句追问”的方式写。先背短答，再展开深答；每一页都刻意保留当前边界，避免把未来规划说成已实现。

## 怎么用

先按下面顺序过一遍：

| 顺序 | 页面 | 适合解决的问题 |
|---|---|---|
| 1 | [四条简历核心能力](/interview/resume-grill/core-capabilities/) | 按当前四条简历逐句准备：短答、深答、追问、防守、证据与边界 |
| 2 | [项目定位与总览](/interview/resume-grill/overview/) | 2 分钟项目介绍、是不是 AI 套壳、技术栈选择 |
| 3 | [Kafka 异步任务](/interview/resume-grill/kafka-async/) | 为什么异步、消息丢失/重复、重试、任务状态 |
| 4 | [Redis 锁与限流](/interview/resume-grill/redis-lock-rate-limit/) | WatchDog、owner 校验、Lua 令牌桶、成本控制 |
| 5 | [上传、断点续传与 MinIO](/interview/resume-grill/upload-minio/) | 分片上传、MD5 复用、ComposeObject、预签名 URL |
| 6 | [RAG 与 Milvus](/interview/resume-grill/rag-milvus/) | ASR 文本做知识源、chunk、向量检索、BM25 风格召回、RRF |
| 7 | [可靠性与系统设计](/interview/resume-grill/reliability-system-design/) | 外部 AI 失败、URL 下载安全、BYOK、审计、扩展方案 |
| 8 | [MySQL/GORM 数据模型](/interview/resume-grill/mysql-gorm-data-model/) | 任务表、Job 表、软删除、JSON 字段、事务边界 |
| 9 | [URL 安全与部署](/interview/resume-grill/url-security-deploy/) | SSRF 第一层防护、yt-dlp、代理、720p、Milvus readiness |
| 10 | [Debug 复盘](/interview/resume-grill/debugging-war-stories/) | 长视频 ASR、RAG 状态、MySQL JSON、Retry 半成功、AI 辅助项目答法 |
| 11 | [系统设计压力面](/interview/resume-grill/hard-system-design/) | 1000 用户、10GB 视频、Kafka 扩容、Redis/MySQL 故障、微服务拆分 |

## 简历话术到追问映射

| 简历关键词 | 面试官会追问 | 先看 |
|---|---|---|
| 当前四条简历 | 四条怎么串成一条业务闭环？每条有哪些边界？ | [四条简历核心能力](/interview/resume-grill/core-capabilities/) |
| AI 视频理解后端 | 你到底解决什么问题？为什么不是调 API？ | [项目定位与总览](/interview/resume-grill/overview/) |
| Kafka 异步处理 | 为什么不用同步 HTTP 或本地 goroutine？失败怎么恢复？ | [Kafka 异步任务](/interview/resume-grill/kafka-async/) |
| Redis 分布式锁 | SETNX 有什么坑？WatchDog 怎么防误删？ | [Redis 锁与限流](/interview/resume-grill/redis-lock-rate-limit/) |
| Redis Lua 令牌桶 | 为什么 Lua？Redis 挂了怎么办？这算 quota 吗？ | [Redis 锁与限流](/interview/resume-grill/redis-lock-rate-limit/) |
| 分片断点续传 | Redis Set 记录什么？什么时候合并？并发合并怎么办？ | [上传、断点续传与 MinIO](/interview/resume-grill/upload-minio/) |
| MinIO 对象存储 | 为什么不放本地或 MySQL？预签名 URL 有什么边界？ | [上传、断点续传与 MinIO](/interview/resume-grill/upload-minio/) |
| Milvus RAG 问答 | 为什么不用摘要做知识库？怎么隔离用户？检索差怎么办？ | [RAG 与 Milvus](/interview/resume-grill/rag-milvus/) |
| BYOK | 用户 key 怎么保护？公开部署为什么不能用服务端 key？ | [可靠性与系统设计](/interview/resume-grill/reliability-system-design/) |
| URL 下载 | SSRF 防到什么程度？为什么不能说生产级？ | [可靠性与系统设计](/interview/resume-grill/reliability-system-design/) |
| MySQL/GORM | 主任务表为什么不塞大字段？TaskJob 为什么拆出来？ | [MySQL/GORM 数据模型](/interview/resume-grill/mysql-gorm-data-model/) |
| task_jobs | 主任务状态和子作业状态为什么分开？重试怎么记录？ | [MySQL/GORM 数据模型](/interview/resume-grill/mysql-gorm-data-model/) |
| URL 下载安全 | SSRF 防护做到哪？B 站 412 和 YouTube 代理怎么排？ | [URL 安全与部署](/interview/resume-grill/url-security-deploy/) |
| 真实 Debug | 长视频 ASR 截断、MySQL JSON、RAG 旧向量怎么复盘？ | [Debug 复盘](/interview/resume-grill/debugging-war-stories/) |
| 系统设计 | 1000 用户、10GB 视频、Kafka/Redis/MySQL 挂了怎么设计？ | [系统设计压力面](/interview/resume-grill/hard-system-design/) |

## 口径底线

可以说：

- VidLens 是 Go 后端 AI 视频理解项目，核心问题是长耗时、高成本、外部依赖不稳定的视频处理链路。
- 当前有 Kafka 异步任务、DB retry state、Redis 锁与 token bucket、MinIO、Milvus RAG、BYOK、AI 调用审计和第一层 URL 下载安全校验。
- 当前简历拷打栏覆盖 10 个方向、80 道追问题，优先服务压力面和口语化答辩。
- 当前 RAG 是单视频范围，知识源是 ASR 转写文本。
- 当前关键词召回是 Go 侧 BM25 风格实现，再用 RRF 融合，不是 Elasticsearch/OpenSearch。

不要说：

- 不要说 VidLens 使用 RocketMQ 或 Redisson。
- 不要说 URL 下载已经是生产级 SSRF 沙箱。
- 不要说所有 provider 都是真 token streaming；代码里仍保留 fallback。
- 不要说已有完整计费系统；当前是 AI 调用审计和每日用量聚合。
- 不要说已有 rerank、Function Calling、跨视频知识库或大规模生产流量。

## 证据入口

常用证据路径：

- 项目介绍与能力边界：`README.md:17`, `README.md:46`, `README.md:220`
- 长视频 ASR 复盘：`docs/troubleshooting-and-interview-notes.md:34`, `docs/troubleshooting-and-interview-notes.md:225`
- BYOK 与 RAG 接入复盘：`docs/troubleshooting-and-interview-notes.md:403`, `docs/troubleshooting-and-interview-notes.md:524`
- URL 下载安全复盘：`docs/troubleshooting-and-interview-notes.md:2783`, `docs/troubleshooting-and-interview-notes.md:2856`
- RAG 混合检索复盘：`docs/troubleshooting-and-interview-notes.md:2882`, `docs/troubleshooting-and-interview-notes.md:2977`
- TaskJob 状态拆分复盘：`docs/troubleshooting-and-interview-notes.md:3749`, `docs/troubleshooting-and-interview-notes.md:3898`

