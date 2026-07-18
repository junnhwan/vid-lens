# 专题 3：耐久化分片上传与断点续传

## 1. 推荐简历表述

> 设计用户级耐久化上传会话：由 PostgreSQL 持久化不可变 manifest、已接收分片台账、完成 lease 与最终任务身份，MinIO 保存分片和最终对象；支持断点恢复、同分片幂等、冲突检测、服务端完整性校验和重复完成请求幂等返回。

不要再写“Redis Set 是上传状态事实源”或“MinIO ComposeObject 合并”。那是已退役的旧协议。

## 2. 为什么要从全局 MD5 协议重构

旧协议以客户端传入的 `file_md5` 作为全局会话身份，并把已上传片号存在 Redis。它有几个具体问题：

- 会话没有绑定 `user_id`，不同用户提交同一 MD5 时容易共享状态边界；
- Redis key 只保存临时进度，重启、过期或异常清理会丢失业务事实；
- 客户端 MD5 只被信任，没有服务端对完整内容重新计算；
- 相同 index 的不同内容没有稳定冲突语义；
- merge 锁只能防并发执行，不能保存稳定的完成结果和 task identity；
- Redis key、MinIO object 和数据库 task 分散，后续开发者很难判断谁是事实源。

重构后的原则是：**PostgreSQL 管状态，MinIO 管字节，Redis 不参与上传正确性。**

## 3. 当前事实源与职责

```text
PostgreSQL
├── upload_sessions
│   ├── user_id + session_id 所有权
│   ├── immutable manifest
│   ├── active/completing/completed/failed/expired
│   ├── completion token + lease expiry
│   └── verified MD5 + asset_id + task_id
└── upload_session_chunks
    └── (session_id, chunk_index) -> exact size + SHA-256 + MinIO object name

MinIO
├── upload-sessions/<session>/chunks/<index>/<sha256>
└── final video object

Redis
└── 不保存上传会话或分片完成事实
```

`video_assets` 仍以完整文件 MD5 支持资产复用；这与“恢复同一 upload session”是两个概念。

## 4. HTTP 协议

```text
POST /api/v1/media/upload-sessions
GET  /api/v1/media/upload-sessions/:session_id
PUT  /api/v1/media/upload-sessions/:session_id/chunks/:index
POST /api/v1/media/upload-sessions/:session_id/complete
```

创建请求提交：

```json
{
  "filename": "demo.mp4",
  "file_size": 123456,
  "chunk_size": 5242880,
  "total_chunks": 1,
  "expected_md5": "32位十六进制"
}
```

分片请求使用原始 `application/octet-stream`，不是 multipart。Handler 先读取服务端 manifest 计算该 index 的权威大小，再用 `http.MaxBytesReader` 限制请求体。

## 5. 正常流程

1. 前端增量计算完整文件 MD5，并提交 immutable manifest。
2. Service 用 manifest fingerprint 和 active key 查找当前用户可恢复的 session；同一 manifest 可稳定恢复，不同用户互不共享 session。
3. `GET session` 从 PostgreSQL chunk ledger 重建 `uploaded` 列表，而不是查询客户端或 Redis 缓存。
4. 上传分片时，服务端把请求体写入有上限的临时文件，同时计算 SHA-256，并要求实际大小与 manifest 精确一致。
5. 分片对象写入 content-addressed MinIO 路径，再插入 PostgreSQL ledger。
6. 同 index、同 size/hash 的重试返回幂等结果；同 index、不同内容返回冲突，不能覆盖已经接受的字节。
7. complete 先用 CAS 获取带 token 和过期时间的 completion lease；live lease 拒绝并发完成，过期 lease 可以回收。
8. 如果存在 size/MD5 匹配的 active asset，可跳过再次上传字节并复用 asset。
9. 否则按 chunk index 流式打开 MinIO 对象，通过 `io.Pipe` 写入最终对象，同时计算完整文件 MD5 和总大小；不把完整视频一次载入内存。
10. 完整性通过后，在同一个 PostgreSQL transaction 中创建/锁定 asset、创建 task，并用 lease token CAS 将 session 标记为 completed，保存稳定的 `asset_id/task_id`。
11. 重复 complete 返回 session 中同一个 task；成功后的临时 chunk 删除是 best-effort。

## 6. 关键不变量

### Owner isolation

任何 `Get`、`AcceptChunk`、`Complete` 都使用 `user_id + session_id` 查找。不能仅凭 UUID 或 MD5 访问其他用户会话。

### Immutable manifest

`filename/file_size/chunk_size/total_chunks/expected_md5` 共同形成 fingerprint。恢复时不能把旧 session 换成另一种分片布局。

### Exact chunk size

除最后一片外，每片必须等于 `chunk_size`；最后一片大小由 `file_size - (total_chunks-1)*chunk_size` 唯一计算。过短和过长都拒绝。

### Accepted-chunk idempotency

唯一索引 `(session_id, chunk_index)` 是最终物理兜底。对象路径包含 SHA-256，冲突重试不会覆盖 winner 的对象。

### Completion ownership

只有持有当前 `completion_token` 的执行者能完成、释放或标记失败。仅有 `status=completing` 不足以证明所有权。

### Stable completed result

asset、task 和 completed session 必须在一个 PostgreSQL transaction 中提交。HTTP 重试不能创建第二个 task。

## 7. 失败窗口怎么回答

### MinIO 分片写成功，ledger 插入失败

Service best-effort 删除刚写入的 candidate object。即使删除失败，该对象也不是事实源；没有 ledger 行就不会被 complete 使用。

### complete 进程崩溃

completion lease 到期后可由新 owner 回收。最终对象写入使用稳定命名且后续数据库提交带 CAS；外部对象和数据库无法共享事务，因此仍依赖幂等对象操作、lease 和可恢复重试，而不是声称分布式强事务。

### 完整 MD5 或 size 不匹配

session 标记为 `failed` 并保存受限错误信息，不创建 asset/task。客户端不能靠重新调用 complete 把损坏内容变成成功。

### PostgreSQL 最终事务失败

释放当前 completion claim，使会话回到可重试状态；事务回滚保证不会留下半创建 task/session 完成状态。

### 临时分片清理失败

完成结果仍然有效。清理是 best-effort，残留只影响存储成本。废弃/过期 session 的对象定时清理目前仍是后续工作，不能说已经完全解决。

## 8. 当前限制

- 前端完整 MD5 仍在上传前计算，超大文件会增加开始上传前的等待；可以进一步评估 Web Worker、分块 hash 流水线或服务端 checksum 协议。
- 当前完成逻辑以应用层流式读取/写入实现完整 MD5 校验，不再依赖 ComposeObject；这会产生 MinIO 到应用再到 MinIO 的数据路径，是为了换取服务端内容校验和不受 5MiB Compose part 下限约束。
- 废弃 session 与 orphan MinIO candidate object 还需要单独的生命周期扫描/保留策略。
- completed session 当前依赖关联 task 返回重复完成结果；任务删除后的 session 生命周期需要继续明确，不能把它描述成永久回执。
- 尚未做大规模弱网、并发 session 和 MinIO 故障注入压测。

## 9. 项目证据路径

- `internal/model/upload_session.go`
- `internal/model/upload_session_chunk.go`
- `internal/repository/upload_session.go`
- `internal/service/upload_session.go`
- `internal/service/upload_session_chunk.go`
- `internal/service/upload_session_complete.go`
- `internal/handler/upload_session.go`
- `cmd/server/router.go`
- `web/src/chunkedUpload.js`
- `internal/service/upload_session_test.go`
- `internal/handler/upload_session_test.go`
- `internal/repository/upload_session_test.go`

## 10. 面试口语版

> 我一开始用客户端 MD5 加 Redis Set 记录分片片号，能做基础断点续传，但复盘后发现它没有用户级会话边界，Redis 过期会丢失事实，而且服务端没有重算完整文件内容。后来我把它重构成 PostgreSQL durable upload session：创建时固定 manifest，每个 session 绑定用户；每个已接受分片在数据库保存 index、精确大小、SHA-256 和 MinIO 对象名。同一个 index 重传相同内容是幂等，传不同内容直接冲突。完成时用带 token 的 lease 防止并发 complete，按顺序流式读取分片并在服务端重新计算完整 MD5 和大小，最后把 asset、task 和 session 完成状态放在一个 PostgreSQL 事务里提交。Redis 不再决定上传是否完成，重复 complete 会返回同一个 task。外部 MinIO 和 PostgreSQL 之间仍然不是分布式事务，所以我用 lease、稳定对象身份和可重试状态来收敛失败窗口。
