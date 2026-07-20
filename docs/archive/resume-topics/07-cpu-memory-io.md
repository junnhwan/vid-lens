# 专题 7：CPU、内存、IO 与连接资源排查

> 本专题区分“当前已实现的观测能力”和“排查建议”。不要把没有保存证据的压测现象写成真实事故。

## 1. 先按症状分类

```text
CPU 接近满且吞吐不再增长
  -> profile CPU、GC、调度和 FFmpeg 并发

CPU 较低但延迟高
  -> 查 PostgreSQL/Redis 连接等待、Kafka lag、MinIO 和外部 AI 网络等待

RSS/heap 持续增长
  -> 查大对象、base64、并发 ASR chunk、goroutine 和临时缓冲

磁盘或网络吞吐打满
  -> 查 FFmpeg/yt-dlp 临时文件、MinIO 往返、AI 请求和 pgvector 批量写入
```

先判断“在计算”还是“在等待”，不要看到接口慢就直接优化 CPU。

## 2. 当前可观测性事实

项目已经有独立 Prometheus 管理监听器，默认只绑定 loopback，并暴露 `/metrics`。当前指标覆盖：

- task stage 次数和耗时；
- retry、dead task 和 Kafka job 耗时；
- ASR chunk 成功/失败、耗时和复用；
- AI 调用次数、耗时、token 与可估算成本；
- RAG 检索耗时、结果数与 context tokens；
- 限流 decision。

仍不能声称：

- 已接入完整分布式 tracing/OTel；
- 已有所有数据库、Redis、MinIO 和主机层指标；
- pprof 已安全暴露为线上管理端点；
- 已用正式容量测试确定最优连接池和并发参数。

## 3. CPU

### 真实热点候选

- FFmpeg 音频提取与编码是外部进程 CPU；
- 大量并发 chunk 会增加 Go 调度、序列化和 GC 压力；
- AI profile 加解密、hash 和 JSON 通常不是首要瓶颈，除非 profile 证据显示；
- pgvector 查询主要消耗 PostgreSQL 侧 CPU，不应只看 Go 进程。

### 排查顺序

1. `docker stats`/系统监控确认是 Go、FFmpeg 还是 PostgreSQL 占用。
2. Go 进程具备安全采样条件时抓 CPU profile 和 goroutine dump。
3. PostgreSQL 查看 `pg_stat_activity`、慢 SQL 与查询计划。
4. 对照 Prometheus task/AI/RAG duration 和 Kafka lag，判断是否只是下游变慢造成并发堆积。
5. 再决定限制 FFmpeg/consumer 并发、优化 SQL/索引或降低非必要工作。

## 4. 内存

### 当前做得较好的边界

- 普通上传通过临时文件和 `io.Copy + io.TeeReader` 计算 hash，不把整个视频读入内存。
- durable session complete 使用 `io.Pipe` 按序从 MinIO 读取 chunks 并写回最终对象，同时计算完整 MD5；不会构造完整视频字节数组。
- 聊天完整记录在 PostgreSQL，Redis 只缓存最近轮次。

### 仍需关注

- AI provider 若要求 base64 音频，请求构造可能产生大对象；
- 同时执行多个 ASR chunk、FFmpeg 或下载任务会叠加内存和临时文件；
- goroutine/heartbeat/watchdog 未按 context 退出会形成生命周期泄漏；
- `io.Pipe` 降低 heap 占用，但不能减少 MinIO → Go → MinIO 的网络流量。

排查时使用 heap profile、goroutine dump、进程 RSS 和 GC 指标交叉判断；不要只看容器总内存就断言是 Go heap 泄漏。

## 5. 磁盘与网络 IO

### 磁盘热点

- yt-dlp 下载和 FFmpeg 中间音频；
- 普通上传服务端临时文件；
- PostgreSQL WAL、表和索引；
- MinIO 对象与 upload-session 临时 chunks；
- 本地日志、迁移 dump 和评测产物。

### 网络热点

- MinIO 上传、读取 chunk 和流式合并回写；
- ASR、LLM、embedding 等外部 API；
- PostgreSQL/pgvector 查询和批量写入；
- Kafka producer/consumer 流量。

当前 session complete 选择应用层流式合并，是为了获得不依赖旧 Compose part 限制的精确顺序和完整 hash 校验；代价是字节经过 Go 服务，消耗应用与 MinIO 之间的双向带宽。未来只有在真实带宽瓶颈证据出现后，再评估原生 multipart compose 或独立合并 worker。

## 6. 数据库与连接池

正式业务库是 PostgreSQL，不再使用 MySQL `SHOW PROCESSLIST`。常用观察入口：

```sql
SELECT pid, state, wait_event_type, wait_event, query_start, query
FROM pg_stat_activity
WHERE datname = current_database();
```

当前连接池事实：

- GORM PostgreSQL pool 在 `internal/database/postgres.go` 设置 max open、max idle、max lifetime 和 max idle time；
- pgvector 使用独立 pgx pool，连接同一个 PostgreSQL，server factory 当前给出较小的独立 pool 配置；
- Redis client 仍主要使用 go-redis 默认池参数；
- “两个 PostgreSQL pool”不等于两个数据库，但总连接预算要一起计算。

连接池参数不能照搬固定数字。应依据 PostgreSQL `max_connections`、实例数、worker 并发、查询耗时和实际 wait 指标分配，并给管理/迁移工具预留连接。

## 7. 故障现象速查

| 现象 | 优先证据 | VidLens 候选原因 |
|---|---|---|
| CPU 高、吞吐不涨 | 进程占用 + CPU profile | FFmpeg 并发、GC/调度、PostgreSQL 查询 |
| CPU 低、P99 高 | goroutine + `pg_stat_activity` + AI duration | 连接等待、外部 AI、MinIO、锁等待 |
| heap/RSS 上升 | heap + RSS + goroutine | base64、大并发 chunk、生命周期泄漏 |
| 磁盘满 | volume usage + temp dirs | 下载/FFmpeg 临时文件、MinIO、PostgreSQL、日志 |
| 网络满 | 容器/主机网络 + stage duration | session 流式合并、AI API、MinIO、Kafka |
| Kafka lag 上升 | consumer group lag + job duration | AI/FFmpeg 下游变慢、consumer 并发不足、失败重试 |
| RAG 延迟升高 | RAG metrics + PostgreSQL plan | candidate 过大、pgvector 查询、关键词遍历、外部 embedding |

## 8. 面试话术

> 我不会把接口慢直接归因于 CPU。先看 CPU 与吞吐关系：CPU 高就区分 Go、FFmpeg 和 PostgreSQL；CPU 低但延迟高就查连接等待、Kafka lag、MinIO 和外部 AI。普通上传流式落临时文件，分片上传由 MinIO 保存并服务端合并；Redis 只保存临时片号。数据库已经切到 PostgreSQL，GORM 与 pgvector 分别有连接池，所以容量规划要计算总连接预算。

## 9. 不要夸大

- 不要再使用 MySQL processlist 或把 Milvus写成当前网络依赖。
- 不要说 ComposeObject 让当前上传完全不经过应用网络。
- 不要说没有 Prometheus；也不要说已有完整 APM/OTel。
- 不要把建议性的压测、连接池数字或 pprof 方案说成已验证结论。
- 不要声称 consumer、FFmpeg 和外部 AI 并发已经完成生产容量规划。
