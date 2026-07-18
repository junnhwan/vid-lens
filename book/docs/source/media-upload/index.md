# Media Upload Source Guide

> 本页面向维护者，只描述当前代码入口。面试话术见[视频上传与对象存储](/interview/media-upload/)，分片协议见[分片上传与断点续传](/interview/resume-grill/resume-core/chunk-upload/)。

## 1. 模块职责

| 路径 | 当前职责 |
|---|---|
| `internal/service/media.go` | `MediaService` 依赖 façade；普通上传、URL task、任务提交 |
| `internal/service/media_file_upload.go` | 普通 multipart 上传、流式临时文件、MD5、asset/task 创建 |
| `internal/service/media_url_upload.go` | URL 下载任务创建 |
| `internal/service/media_tasks.go` | 查询、提交分析/转写等 task 操作 |
| `internal/service/upload_session.go` | session 创建、恢复、owner/manifest 校验 |
| `internal/service/upload_session_chunk.go` | exact-size chunk 接收、SHA-256、幂等/冲突 |
| `internal/service/upload_session_complete.go` | completion lease、流式合并、完整 MD5、DB finalization |
| `internal/repository/upload_session.go` | session/chunk 持久化、CAS、transaction-scoped repository |
| `internal/handler/upload_session.go` | durable session HTTP adapter 和 body 上限 |
| `internal/storage/minio.go` | 对象上传、打开、删除、下载和预签名 URL |
| `internal/service/task_cleanup*.go` | task 删除 intent、lease 和资源清理恢复 |
| `internal/pkg/remoteurl/` | URL allowlist/DNS 基础策略 |
| `web/src/chunkedUpload.js` | 前端 manifest、分片调度、恢复与 complete |

旧 `/upload-chunk`、`/check-upload`、`/merge-chunks` 路由和 Redis 分片协议已经退役，不应重新添加到 `MediaService`。

## 2. 普通上传调用链

```text
POST /api/v1/media/upload
  -> MediaHandler.UploadFile
  -> MediaService.UploadFile
  -> copyStreamToTempAndHash
  -> Asset.FindByMD5 / MinIO UploadFromPath
  -> Asset.CreateOrRestore
  -> Task.Create
```

维护不变量：

- `upload.max_file_size` 在流式读取过程中生效；
- 完整文件不通过 `io.ReadAll` 进入内存；
- asset 是真实字节身份，task 是用户处理上下文；
- Redis 锁是减少竞争的优化，asset unique constraint 是兜底；
- 任何新失败补偿都必须区分“对象已写、DB 未写”和“DB 已写、响应丢失”。

## 3. Durable upload session 调用链

```text
POST upload-sessions
  -> Create(userID, manifest)
  -> manifest fingerprint + user-bound active key
  -> upload_sessions

GET upload-sessions/:id
  -> FindByIDAndUser
  -> upload_session_chunks ordered ledger

PUT upload-sessions/:id/chunks/:index
  -> Handler derives exact size + MaxBytesReader
  -> spool temp + SHA-256
  -> content-addressed MinIO object
  -> insert unique (session_id, chunk_index)

POST upload-sessions/:id/complete
  -> claim token/expiry lease
  -> validate ordered ledger
  -> reuse matching asset OR stream MinIO chunks through io.Pipe
  -> verify complete size + MD5
  -> PostgreSQL transaction:
       resolve asset
       create task
       token-CAS session completed
  -> best-effort temporary object cleanup
```

## 4. 状态 owner

```text
upload_sessions
  owner: UploadSessionService + UploadSessionRepository
  durable facts: user, manifest, lifecycle, lease, final identities

upload_session_chunks
  owner: UploadSessionService + UploadSessionRepository
  durable facts: accepted index, size, SHA-256, object name

MinIO chunk/final
  owner: UploadObjectStore implementation
  facts: bytes only; object existence is not accepted business state

video_assets / video_tasks
  owner: MediaService or upload finalization transaction
  facts: reusable asset and user task
```

Redis is not an upload-session owner.

## 5. Failure windows

| Window | Expected handling |
|---|---|
| candidate chunk stored, ledger insert fails | resolve concurrent winner; otherwise delete candidate best-effort |
| complete claim acquired, chunks missing | release token-owned claim |
| final stream/MD5 fails | delete final best-effort and mark/release according to error kind |
| asset/task/session transaction fails | rollback relationship rows, release claim, delete candidate final best-effort |
| response lost after completed | repeat complete reads stable session task ID |
| process dies after object side effect | later retry/cleanup must be idempotent; not a cross-system transaction |

## 6. Tests to extend before changing behavior

```text
internal/repository/upload_session_test.go
internal/service/upload_session_test.go
internal/handler/upload_session_test.go
cmd/server/router_test.go
web/test/chunkedUpload.test.mjs
```

For behavior changes, add a failing test first. High-value missing cases:

- completed session after associated task deletion;
- abandoned session scanner and object cleanup recovery;
- process interruption around final-object upload and DB finalization;
- real PostgreSQL concurrent complete integration coverage;
- MinIO integration coverage for ordered stream assembly.

## 7. Do not reintroduce

- global client MD5 as session identity;
- Redis Set/TTL as accepted chunk source;
- multipart metadata repeated on every chunk;
- overwriting accepted chunk bytes with different content;
- status-only completion locks without owner token and expiry;
- claims that MinIO and PostgreSQL commit atomically;
- new abstraction layers without a concrete second implementation or tested failure boundary.

## 8. Related docs

- `docs/resume-topics/03-chunk-upload-resume.md`
- `docs/resume-topics/08-large-video-handling.md`
- `docs/backend-maintenance-map.md`
- `docs/stress-test-guide.md`
