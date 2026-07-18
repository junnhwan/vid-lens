# 分片上传与断点续传：PostgreSQL Durable Upload Session

> 本页是书站内的上传专项权威页。仓库级更完整证据见 [`docs/resume-topics/03-chunk-upload-resume.md`](/interview/resume-grill/resume-core/chunk-upload/) 对应源文档。其他页面不应复制另一套协议细节。

## Q1：为什么要做分片上传？

**直接回答：**

大视频在弱网下用单个 HTTP 请求上传，一旦连接中断就要从头重传。VidLens 把文件拆成多个独立 chunk，并把上传进度持久化成用户级 session；客户端恢复时查询 accepted indexes，只补缺失片。

它改善的是失败恢复，不代表支持任意文件大小，也不自动解决并发完成、完整性、过期回收和对象清理。

## Q2：当前谁是事实源？

```text
PostgreSQL
├── upload_sessions
│   └── owner、immutable manifest、status、completion lease、asset/task identity
└── upload_session_chunks
    └── chunk index、exact size、SHA-256、MinIO object name

MinIO
└── chunk/final bytes

Redis
└── 不参与上传会话正确性
```

过去的全局 MD5 + Redis 片号协议已经退役。当前进度不能从 Redis Set 推导。

## Q3：HTTP 协议是什么？

```text
POST /api/v1/media/upload-sessions
GET  /api/v1/media/upload-sessions/:session_id
PUT  /api/v1/media/upload-sessions/:session_id/chunks/:index
POST /api/v1/media/upload-sessions/:session_id/complete
```

创建时固化 filename、file size、chunk size/count 和 expected MD5。chunk 请求直接发送 `application/octet-stream`，不重复提交 multipart 元数据。

## Q4：怎么防止用户越权或篡改 manifest？

每次 Get、AcceptChunk、Complete 都按 `user_id + session_id` 查询。manifest 创建后不可变，并生成 fingerprint/active key；同一用户相同 manifest 可恢复，其他用户不能因为知道 UUID 或 MD5 访问该 session。

## Q5：单片如何校验和幂等？

服务端根据 manifest 计算该 index 的精确大小，Handler 用 `MaxBytesReader` 限制 body，Service 写临时文件时计算 SHA-256。

- 同 index、同 size/hash：幂等返回；
- 同 index、不同内容：返回 409；
- 唯一约束 `(session_id, chunk_index)` 是并发写的物理兜底；
- 对象名包含 SHA-256，冲突请求不会覆盖已接受对象。

## Q6：complete 为什么需要 lease？

只存 `status=completing` 无法判断执行者仍活着还是已经宕机。当前使用 completion token + expiry：

- live lease 拒绝另一个 complete；
- lease 过期后允许新执行者 reclaim；
- 只有当前 token owner 能完成或释放；
- CAS 防止旧 owner 写坏新 owner 的结果。

这比依赖某个进程内 mutex 或无归属 Redis 锁更容易恢复。

## Q7：怎么合并，为什么不吃满内存？

Service 按 chunk index 打开 MinIO 对象，通过 `io.Pipe` 顺序复制，同时计算完整 MD5 和总 size，并把流写成 final object。完整视频不会一次载入 Go 内存。

需要主动说明 tradeoff：字节会经过 `MinIO → Go → MinIO`，这不是对象存储侧零拷贝。当前选择是为了服务端重新校验完整内容，也因此不受旧对象合并协议的 5 MiB part 下限约束。

## Q8：数据库一致性做到哪一步？

完整性通过后，在同一个 PostgreSQL transaction 中：

1. 创建或锁定 `video_assets`；
2. 创建 `video_tasks`；
3. 用 completion token CAS 把 session 标成 completed；
4. 保存稳定的 `asset_id/task_id`。

MinIO final object 是数据库事务外副作用，所以失败时使用补偿删除和 claim 释放，不能说成跨 MinIO/PostgreSQL 的原子事务。

## Q9：重复完成请求怎么办？

completed session 保存 task ID。客户端第一次响应丢失后再次 complete，服务端返回同一个 task，而不是重复创建。

当前边界是：关联 task 被删除后，completed session 的长期返回语义仍需明确，因此不能说它是永久不可变回执。

## Q10：哪些还没做？

- expired/abandoned session 后台扫描；
- 孤儿 chunk/final object 的耐久回收；
- task 删除后的 completed session 生命周期；
- 预签名 multipart 直传；
- GB 级并发容量数据与基于证据的连接池/带宽调优。

成功后的临时 chunk 仅 best-effort 清理。面试中应把这些说成下一步，而不是已实现能力。

## 代码证据

```text
internal/model/upload_session.go
internal/model/upload_session_chunk.go
internal/repository/upload_session.go
internal/service/upload_session.go
internal/service/upload_session_chunk.go
internal/service/upload_session_complete.go
internal/handler/upload_session.go
cmd/server/router.go
web/src/chunkedUpload.js
```

测试证据：

```text
internal/repository/upload_session_test.go
internal/service/upload_session_test.go
internal/handler/upload_session_test.go
cmd/server/router_test.go
web/test/chunkedUpload.test.mjs
```

## 面试口语版

> 我最初的分片协议只用客户端 MD5 作为全局身份，把片号放 Redis，这对 demo 足够，但 owner、manifest、冲突和完成回执都不清楚。后来我改成用户级 durable upload session：PostgreSQL 保存不可变 manifest、分片 size/SHA-256 台账和完成 lease，MinIO 只保存字节，Redis 不再参与正确性。每片相同内容可幂等重试，不同内容返回冲突；complete 会按顺序流式读取分片，重新计算完整 MD5，再在一个 PostgreSQL transaction 中创建 asset、task 并保存稳定 task ID。它没有把整段视频读进内存，但会多占用一次 MinIO 到 Go 再到 MinIO 的网络，这是服务端完整校验的取舍。当前仍缺 abandoned session 和孤儿对象的后台回收，我会明确承认这个边界。
