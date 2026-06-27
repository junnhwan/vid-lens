# 媒体上传与存储 - 源码走读

> 基于 `internal/service/media.go` 和 `internal/service/remote_video_url.go`，覆盖上传、分片、合并、删除全链路。

---

## 涉及文件

| 文件 | 职责 |
|------|------|
| `internal/service/media.go` | MediaService 核心：普通上传、URL 上传、分片上传、合并、删除 |
| `internal/service/remote_video_url.go` | URL 安全验证：白名单、SSRF 防护、DNS Rebinding 防御 |
| `internal/storage/minio.go` | MinIOStorage：对象上传、下载、ComposeObject、预签名 URL |
| `internal/repository/` | 数据访问层：Asset、Task、TaskJob 等子 Repo |
| `internal/model/task.go` | VideoTask / VideoAsset 数据模型 |
| `internal/mq/producer.go` | Kafka 生产者：EnqueueDownload、EnqueueAnalyze |
| `internal/pkg/lock/redis_lock.go` | RedisLock：分片合并防并发 |

---

## 核心结构体

### MediaService

```go
// media.go:42-52
type MediaService struct {
    repo              *repository.Repositories   // data access layer aggregate
    storage           *storage.MinIOStorage      // MinIO object storage
    objectDeleter     objectDeleter              // object deletion interface (mockable)
    taskVectorCleaner TaskVectorCleaner          // vector cleanup interface (lazy inject)
    remoteURLResolver remoteURLResolver          // DNS resolver interface (mockable)
    mq                mediaProducer              // Kafka producer interface
    rdb               redis.Cmdable              // Redis client
    cfg               config.UploadConfig        // upload config (MaxFileSize, ChunkSize)
    tools             config.ToolsConfig         // tool config (AllowedVideoHosts)
}
```

**Design notes**:

- `objectDeleter` and `taskVectorCleaner` are interfaces, supporting mock in tests.
- `taskVectorCleaner` is lazily injected via `SetTaskVectorCleaner` (line 73), resolving circular dependency.
- `cfg` is typed as `config.UploadConfig` (not `config.Config`), exposing only upload-related config.

### UploadResult

```go
// media.go:81-90
type UploadResult struct {
    TaskID   int64  `json:"task_id"`    // task ID for polling
    FileMD5  string `json:"file_md5"`   // content hash, dedup key
    Filename string `json:"filename"`   // original filename
    FileURL  string `json:"file_url"`   // MinIO object name
    FileSize int64  `json:"file_size"`  // file size in bytes
    Status   int8   `json:"status"`     // task status code
    Stage    string `json:"stage"`      // processing stage
    TraceID  string `json:"trace_id"`   // distributed tracing ID
}
```

---

## 关键函数

### 1. UploadFile - 普通文件上传

**Location**: `media.go:93-120`

**Call chain**:
```
Handler.UploadFile()
  -> MediaService.UploadFile()
    -> validateUploadSize()              // pre-check size
    -> copyStreamToTempAndHash()         // write temp file + compute MD5
    -> repo.Asset.FindByMD5()            // content-level dedup query
    -> [hit] createTaskFromAsset()       // reuse existing asset
    -> [miss] createAssetFromLocalFile() // upload to MinIO + create asset
      -> storage.UploadFromPath()
      -> repo.Asset.Create()
    -> createTaskFromAsset()             // create VideoTask
```

**Key logic**:

```go
// media.go:98-102
tmpPath, fileMD5, actualSize, err := copyStreamToTempAndHash(fileStream, s.cfg.MaxFileSize)
defer os.Remove(tmpPath) // cleanup regardless of success/failure

// media.go:104-117
asset, err := s.repo.Asset.FindByMD5(fileMD5) // dedup query
if asset == nil {
    asset, err = s.createAssetFromLocalFile(ctx, fileMD5, tmpPath, ...) // upload + create
}
return s.createTaskFromAsset(userID, filename, asset, model.TaskStatusPending)
```

### 2. copyStreamToTempAndHash - Streaming write + MD5 computation

**Location**: `media.go:244-272`

**Core trick**: `io.TeeReader` + `io.LimitReader`

```go
// media.go:253-257
reader = io.LimitReader(r, maxSize+1)     // read 1 extra byte to detect oversize
size, err := io.Copy(tmp, io.TeeReader(reader, hasher)) // one-pass: write file + compute MD5

// media.go:262-265
if maxSize > 0 && size > maxSize {        // actual bytes > limit, oversize
    return "", "", 0, fmt.Errorf("file size exceeds limit")
}
```

**Why `maxSize+1`**: If the file is exactly `maxSize` bytes, `LimitReader` reads to EOF, `size == maxSize`, check passes. If the file is `maxSize+1` bytes, `LimitReader` reads `maxSize+1`, check fails. Reading 1 extra byte distinguishes "exactly at limit" from "over limit".

### 3. createAssetFromLocalFile - Upload + concurrency compensation

**Location**: `media.go:218-242`

```go
// media.go:218-242
func (s *MediaService) createAssetFromLocalFile(...) (*model.VideoAsset, error) {
    s.storage.UploadFromPath(ctx, localPath, objectName, contentType) // upload first
    asset := &model.VideoAsset{FileMD5: fileMD5, ObjectName: objectName, ...}
    if err := s.repo.Asset.Create(asset); err != nil {
        // concurrency compensation: another request already created, reuse it, delete own object
        existing, findErr := s.repo.Asset.FindByMD5(fileMD5)
        if findErr == nil && existing != nil {
            _ = s.storage.DeleteObject(ctx, objectName) // cleanup redundant MinIO object
            return existing, nil
        }
        _ = s.storage.DeleteObject(ctx, objectName)
        return nil, err
    }
    return asset, nil
}
```

**Concurrency timeline**:
```
Request A: Upload -> Create(asset) success
Request B: Upload -> Create(asset) unique key conflict -> FindByMD5 -> get A's asset -> DeleteObject(own) -> return A's asset
```

### 4. UploadByURL - Async download

**Location**: `media.go:150-195`

**Call chain**:
```
Handler.UploadByURL()
  -> MediaService.UploadByURL()
    -> remoteVideoURLValidator.validate()    // URL security check
    -> repo.Task.Create()                    // create task (RUNNING/DOWNLOADING)
    -> repo.TaskJob.UpsertQueued()           // create job record
    -> mq.EnqueueDownload()                  // enqueue Kafka
    -> return UploadResult                   // return immediately
```

**Key design**:

```go
// media.go:159-170
task := &model.VideoTask{
    Status:     model.TaskStatusRunning,    // initial status is RUNNING
    Stage:      model.TaskStageDownloading, // stage=downloading
    SourceType: model.TaskSourceTypeURL,    // source=URL
    MaxRetries: 3,                          // max 3 retries
}

// media.go:178-183: Kafka enqueue failure rollback
if err := s.mq.EnqueueDownload(...); err != nil {
    _ = s.repo.Task.UpdateStatusAndStage(task.ID, model.TaskStatusFailed, ...)
    _ = s.repo.TaskJob.RecordTerminalFailure(...)
    return nil, fmt.Errorf("download task enqueue failed: %w", err)
}
```

### 5. URL security validation

**Location**: `remote_video_url.go:52-96`

**Validation flow**:
```
validate(rawURL)
  -> neturl.Parse()                    // parse URL
  -> scheme check                      // only allow http/https
  -> host == "localhost" check         // block localhost
  -> hostAllowed(host, allowedHosts)   // domain whitelist suffix match
  -> net.ParseIP(host)                 // if host is IP address
    -> unsafeIP(ip)                    // direct IP safety check
  -> resolver.LookupIP(ctx, host)      // if host is domain name
    -> iterate all resolved IPs
    -> unsafeIP(ip)                    // check each IP safety
```

**unsafeIP check** (`remote_video_url.go:116-124`):
```go
func unsafeIP(ip net.IP) bool {
    return ip == nil ||
        ip.IsLoopback() ||           // 127.0.0.0/8, ::1
        ip.IsPrivate() ||            // 10/8, 172.16/12, 192.168/16, fc00::/7
        ip.IsUnspecified() ||        // 0.0.0.0, ::
        ip.IsLinkLocalUnicast() ||   // 169.254/16, fe80::/10
        ip.IsLinkLocalMulticast() || // 224.0.0.0/24, ff02::/16
        ip.IsMulticast()             // 224.0.0.0/4, ff00::/8
}
```

**Whitelist suffix match** (`remote_video_url.go:102-114`):
```go
func hostAllowed(host string, allowedHosts []string) bool {
    for _, allowed := range allowedHosts {
        if host == allowed || strings.HasSuffix(host, "."+allowed) {
            return true  // bilibili.com or xxx.bilibili.com both pass
        }
    }
    return false
}
```

**URL sanitization** (`remote_video_url.go:126-140`):
```go
func sanitizeRemoteVideoURL(parsed neturl.URL) string {
    parsed.User = nil    // strip credentials
    parsed.RawQuery = "" // strip all query params
    parsed.Fragment = "" // strip fragment
    if isYouTubeWatchURL(parsed) {
        videoID := strings.TrimSpace(query.Get("v"))
        if videoID != "" {
            values := neturl.Values{}
            values.Set("v", videoID)
            parsed.RawQuery = values.Encode() // only keep v= for YouTube
        }
    }
    return parsed.String()
}
```

### 6. InitChunkedUpload - Initialize chunked upload

**Location**: `media.go:550-555`

```go
func (s *MediaService) InitChunkedUpload(ctx context.Context, fileMD5 string, totalChunks int) error {
    key := fmt.Sprintf("upload:chunks:%s", fileMD5)
    s.rdb.Set(ctx, key+":total", totalChunks, 24*time.Hour) // total chunk count
    s.rdb.Set(ctx, key+":status", "INIT", 24*time.Hour)     // status marker
    return nil
}
```

**Redis data structures**:
| Key | Type | Value | TTL |
|-----|------|------|-----|
| `upload:chunks:{md5}` | Set | uploaded chunk number set | 24h |
| `upload:chunks:{md5}:total` | String | total chunk count | 24h |
| `upload:chunks:{md5}:status` | String | INIT / COMPLETED | 24h |

### 7. UploadChunk - Upload single chunk

**Location**: `media.go:582-599`

```go
func (s *MediaService) UploadChunk(ctx context.Context, fileMD5 string, chunkNumber int, chunkData []byte, chunkSize int64) error {
    validateFileMD5(fileMD5)                                    // MD5 format check
    if s.cfg.ChunkSize > 0 && chunkSize > s.cfg.ChunkSize {    // chunk size check
        return fmt.Errorf("chunk size exceeds limit")
    }

    objectName := fmt.Sprintf("chunks/%s/%d", fileMD5, chunkNumber) // chunk path
    s.storage.UploadFile(ctx, objectName, &readerWrapper{data: chunkData}, ...) // upload to MinIO

    key := fmt.Sprintf("upload:chunks:%s", fileMD5)
    s.rdb.SAdd(ctx, key, chunkNumber)   // record uploaded chunk
    s.rdb.Expire(ctx, key, 24*time.Hour) // renew TTL
    return nil
}
```

**"Write then account"**: Upload to MinIO first, then write Redis. If MinIO upload succeeds but Redis write fails, the chunk exists in MinIO but is not recorded -- MergeChunks will detect the missing chunk and the client can re-upload.

### 8. MergeChunks - Merge chunks

**Location**: `media.go:602-672`

**Call chain**:
```
MergeChunks(userID, fileMD5, filename, totalChunks)
  -> validateFileMD5()
  -> repo.Asset.FindByMD5()           // fast path: already merged?
  -> lock.NewRedisLock().TryLock()     // acquire distributed lock
  -> repo.Asset.FindByMD5()           // double-check after lock acquired
  -> iterate totalChunks, SIsMember    // verify all chunks uploaded
  -> storage.ComposeObject(dst, srcs)  // MinIO server-side merge
  -> repo.Asset.Create(asset)          // create asset record
  -> rdb.Set(status, "COMPLETED")      // mark completed
  -> cleanupMergedChunks()             // cleanup chunks (best-effort)
  -> createTaskFromAsset()             // create task
```

**Distributed lock double-check**:
```go
// media.go:610-627
existingAsset, _ := s.repo.Asset.FindByMD5(fileMD5) // fast path without lock
if existingAsset != nil { return createTaskFromAsset(...) }

mergeLock := lock.NewRedisLock(s.rdb, "vidlens:merge:"+fileMD5)
acquired, _ := mergeLock.TryLock(ctx, 0) // no wait
if !acquired {
    existingAsset, _ := s.repo.Asset.FindByMD5(fileMD5) // check while waiting for lock
    if existingAsset != nil { return createTaskFromAsset(...) }
    return nil, fmt.Errorf("merge operation in progress")
}
defer mergeLock.Unlock(ctx)
```

**ComposeObject construction**:
```go
// media.go:643-651
srcs := make([]minio.CopySrcOptions, 0, totalChunks)
for i := range totalChunks {
    srcs = append(srcs, minio.CopySrcOptions{
        Bucket: s.storage.BucketName(),
        Object: fmt.Sprintf("chunks/%s/%d", fileMD5, i), // chunk path
    })
}
size, err := s.storage.ComposeObject(ctx, dst, srcs) // server-side zero-copy merge
```

### 9. DeleteTask - Cascade delete

**Location**: `media.go:428-498`

**Deletion order**:
```
DeleteTask(userID, taskID)
  -> ownership check (task.UserID == userID)
  -> collectTaskEmbeddingModels()              // collect all embedding model names
  -> taskVectorCleaner.DeleteTaskChunks() x N  // 1. cleanup vectors (outside transaction)
  -> repo.Transaction(func(tx) {               // 2. DB transaction
      tx.Transcription.DeleteByTaskID()
      tx.TranscriptionChunk.DeleteByTaskID()
      tx.Summary.DeleteByTaskID()
      tx.VideoChunk.DeleteByTaskID()
      tx.RAGIndex.DeleteByTaskID()
      tx.Chat.DeleteByTaskID()
      tx.TaskJob.DeleteByTaskID()
      tx.Task.Delete()
      activeRefs := tx.Task.CountActiveByAssetID() // reference count check
      deleteAssetObject = activeRefs == 0
    })
  -> [deleteAssetObject] deleteObject()        // 3. delete MinIO object (after transaction)
  -> [deleteAssetObject] repo.Asset.Delete()   // 4. delete asset record
```

**Why this order**:

1. **Vectors first**: Milvus does not support distributed transactions with MySQL. Delete vectors first; if failed, abort (DB and MinIO untouched).
2. **DB transaction batch delete**: 8 tables atomically deleted, guaranteeing consistency.
3. **MinIO last**: Delete objects after transaction succeeds. If objects are deleted first then DB rolls back, files are lost.
4. **Reference count check**: `CountActiveByAssetID` checks if other tasks reference the same asset; if yes, skip MinIO object deletion (content-level dedup sharing mechanism).

### 10. cleanupMergedChunks - Chunk cleanup

**Location**: `media.go:676-685`

```go
func (s *MediaService) cleanupMergedChunks(ctx context.Context, fileMD5 string, totalChunks int) {
    for i := range totalChunks {
        objName := fmt.Sprintf("chunks/%s/%d", fileMD5, i)
        if err := s.deleteObject(ctx, objName); err != nil {
            log.Printf("[media] chunk cleanup failed (ignorable): %s err=%v", objName, err)
        }
    }
    key := fmt.Sprintf("upload:chunks:%s", fileMD5)
    s.rdb.Del(ctx, key, key+":total") // keep :status=COMPLETED for check-upload queries
}
```

**Best-effort design**: Individual chunk cleanup failures only log, do not block successful merge. Residual chunks waste minimal storage and do not affect business correctness. A scheduled job can reclaim residual chunks.

---

## 完整调用链路图

```
+-----------------------------------------------------------------------+
|                        Normal Upload (UploadFile)                      |
+-----------------------------------------------------------------------+
|  Client -> Handler -> MediaService.UploadFile()                        |
|    -> copyStreamToTempAndHash()                                        |
|       io.TeeReader(io.LimitReader(file, max+1), md5.Hasher)           |
|       write temp file + compute MD5                                    |
|    -> repo.Asset.FindByMD5(fileMD5)                                    |
|       +- hit -> createTaskFromAsset() -> return                        |
|       +- miss -> createAssetFromLocalFile()                            |
|            -> storage.UploadFromPath() -> MinIO                        |
|            -> repo.Asset.Create()                                      |
|               +- success -> return asset                               |
|               +- unique key conflict -> FindByMD5() -> reuse + delete  |
|    -> createTaskFromAsset() -> return UploadResult                     |
+-----------------------------------------------------------------------+

+-----------------------------------------------------------------------+
|                        URL Upload (UploadByURL)                        |
+-----------------------------------------------------------------------+
|  Client -> Handler -> MediaService.UploadByURL()                       |
|    -> remoteVideoURLValidator.validate()                               |
|       -> scheme/host/whitelist/DNS IP check                            |
|    -> repo.Task.Create(RUNNING, DOWNLOADING)                           |
|    -> repo.TaskJob.UpsertQueued(DOWNLOAD)                              |
|    -> mq.EnqueueDownload() -> Kafka                                    |
|    -> return UploadResult (immediate)                                  |
|                                                                       |
|  Kafka Consumer (async)                                                |
|    -> download video -> upload MinIO -> update Task status             |
|    -> mq.EnqueueAnalyze() -> trigger AI analysis chain                 |
+-----------------------------------------------------------------------+

+-----------------------------------------------------------------------+
|                     Chunked Upload (3 steps)                           |
+-----------------------------------------------------------------------+
|  Step 1: InitChunkedUpload(fileMD5, totalChunks)                       |
|    -> Redis Set(upload:chunks:{md5}:total, N)                          |
|    -> Redis Set(upload:chunks:{md5}:status, INIT)                      |
|                                                                       |
|  Step 2: UploadChunk(fileMD5, chunkNumber, data) x N                   |
|    -> storage.UploadFile(chunks/{md5}/{number}) -> MinIO               |
|    -> Redis SAdd(upload:chunks:{md5}, number)                          |
|                                                                       |
|  Step 3: MergeChunks(userID, fileMD5, filename, totalChunks)           |
|    -> FindByMD5() fast path                                            |
|    -> RedisLock double check                                           |
|    -> SIsMember per-chunk verification                                 |
|    -> storage.ComposeObject() -> MinIO server-side merge               |
|    -> repo.Asset.Create()                                              |
|    -> cleanupMergedChunks() -> cleanup chunks                          |
|    -> createTaskFromAsset()                                            |
+-----------------------------------------------------------------------+

+-----------------------------------------------------------------------+
|                        Cascade Delete (DeleteTask)                     |
+-----------------------------------------------------------------------+
|  DeleteTask(userID, taskID)                                            |
|    -> ownership check                                                  |
|    -> collectTaskEmbeddingModels()                                     |
|    -> Milvus DeleteTaskChunks() x N  -- outside transaction           |
|    -> DB Transaction {                                                 |
|        Transcription, TranscriptionChunk, Summary,                     |
|        VideoChunk, RAGIndex, Chat, TaskJob, Task  -- atomic delete     |
|        CountActiveByAssetID()  -- reference count check                |
|      }                                                                |
|    -> [no refs] deleteObject(MinIO) + Asset.Delete() -- post-txn       |
+-----------------------------------------------------------------------+
```

---

## 设计决策总结

| Decision | Choice | Reason |
|----------|--------|--------|
| Dedup granularity | Content-level (MD5) | Same file stored once in MinIO, saves storage |
| Temp state storage | Redis | High-frequency read/write, 24h auto-expiry, no manual cleanup |
| Persistent data storage | MySQL (GORM) | Transaction support, structured queries |
| URL download method | Kafka async | Avoid HTTP timeout, support auto-retry |
| Chunk merge method | MinIO ComposeObject | Server-side zero-copy, no app-layer transfer |
| Merge concurrency control | Redis distributed lock + double check | Minimize lock contention, lock-free fast path |
| Deletion order | Vectors -> DB(txn) -> Storage | Maximum consistency without distributed transactions |
| URL security | Whitelist + DNS IP check | Three-layer defense-in-depth SSRF |
| Concurrent dedup | DB unique key + optimistic compensation | Lock-free design, throughput-first |
| Chunk path | `chunks/{md5}/{index}` | Hash-based directory, easy bulk cleanup |
