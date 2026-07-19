# 媒体上传与存储 — 面试题

> 基于 `internal/service/media.go` 和 `internal/service/remote_video_url.go` 真实源码，共 10 道题。
> 每题含：考察点、代码片段（带行号）、参考答案、追问链。

---

## Q1: UploadFile 为什么先写临时文件再上传 MinIO，而不是直接流式上传？

**考察点**: 流式处理、MD5 计算时机、失败回滚

**关键代码** `internal/service/media.go:93-120`

```go
// 行 93: UploadFile 入口
func (s *MediaService) UploadFile(ctx context.Context, userID int64, filename string, fileStream io.Reader, fileSize int64) (*UploadResult, error) {
    if err := s.validateUploadSize(fileSize); err != nil {
        return nil, err
    }
    // 行 98: 先写临时文件并计算 MD5
    tmpPath, fileMD5, actualSize, err := copyStreamToTempAndHash(fileStream, s.cfg.MaxFileSize)
    if err != nil {
        return nil, fmt.Errorf("读取文件失败: %w", err)
    }
    defer os.Remove(tmpPath) // 行 102: 无论成功失败都清理临时文件

    // 行 104-105: 内容级去重检查
    asset, err := s.repo.Asset.FindByMD5(fileMD5)
```

**参考答案**

两个核心原因：

1. **MD5 需要完整内容**：MD5 是全文哈希，必须读完整个流才能计算。如果直接流式上传到 MinIO，上传完成后才知道 MD5，此时如果发现重复，已经白白消耗了 MinIO 带宽和存储。
2. **去重后可跳过上传**：行 105 先查数据库，如果 `FindByMD5` 命中，直接复用已有 asset（行 119），完全跳过 MinIO 上传。

临时文件用 `defer os.Remove(tmpPath)`（行 102）保证清理，不会泄漏。

**追问链**

1. 如果文件非常大（10GB），临时文件会撑爆磁盘怎么办？可以改为流式计算 MD5 + 分片上传，先上传再比对，但需要设计清理策略。
2. `copyStreamToTempAndHash` 用了 `io.TeeReader`，它是什么？`TeeReader(r, w)` 从 `r` 读取的同时写入 `w`，实现"读一遍，算两份"——既写临时文件又算 MD5。
3. 临时文件放在 `os.TempDir()`，在容器环境下有什么风险？容器的 `/tmp` 通常挂载在内存文件系统（tmpfs），大小有限。大文件上传可能撑爆 tmpfs，应配置 `TMPDIR` 到持久卷。

---

## Q2: io.LimitReader 如何实现"超大文件检测"？为什么不直接检查 fileSize？

**考察点**: 流式大小校验、io.LimitReader 语义

**关键代码** `internal/service/media.go:244-272`

```go
// 行 244: copyStreamToTempAndHash 函数
func copyStreamToTempAndHash(r io.Reader, maxSize int64) (string, string, int64, error) {
    tmp, err := os.CreateTemp("", "vidlens_upload_*")
    // ...
    hasher := md5.New()
    reader := r
    if maxSize > 0 {
        reader = io.LimitReader(r, maxSize+1) // 行 254: 限制读取 maxSize+1 字节
    }

    size, err := io.Copy(tmp, io.TeeReader(reader, hasher))
    // ...
    if maxSize > 0 && size > maxSize { // 行 262: 读到的字节数 > maxSize 说明超限
        os.Remove(tmp.Name())
        return "", "", 0, fmt.Errorf("文件大小超过限制: 最大 %d 字节", maxSize)
    }
```

**参考答案**

`io.LimitReader(r, n)` 返回一个 Reader，最多读取 `n` 字节后返回 EOF。关键在于行 254 传的是 `maxSize+1` 而不是 `maxSize`：

- 如果文件恰好 `maxSize` 字节，读到 `maxSize` 后遇到 EOF，`size == maxSize`，校验通过。
- 如果文件 `maxSize+1` 字节，`LimitReader` 读满 `maxSize+1` 后 EOF，`size == maxSize+1 > maxSize`，校验失败。

**为什么不直接检查 fileSize 参数？**

行 94 已经做了 `validateUploadSize(fileSize)` 前置校验，但 `fileSize` 是客户端声明的，不可信。行 262 是**服务端实际校验**，防止客户端伪造 `fileSize=0` 绕过检查后上传超大文件。两层校验，纵深防御。

**追问链**

1. `io.LimitReader` 返回 EOF 后，底层 Reader 的读取位置在哪里？在 `maxSize+1` 处。后续读取会继续从该位置读，但 `LimitReader` 不再返回数据。
2. 除了 `LimitReader`，还有什么方式做流式大小校验？自定义 `io.Reader` 包装器，每次 `Read` 时累加计数器，超过阈值返回错误。
3. 如果客户端断点续传，这个校验逻辑还适用吗？不适用，分片上传有独立的大小校验（行 587-589），按单个分片大小限制。

---

## Q3: 内容级去重的并发问题——两个请求同时上传相同文件会怎样？

**考察点**: 并发竞态、数据库唯一键、乐观补偿

**关键代码** `internal/service/media.go:104-119` 和 `internal/service/media.go:218-242`

```go
// 行 104-117: UploadFile 中的去重逻辑
asset, err := s.repo.Asset.FindByMD5(fileMD5)
if err != nil {
    return nil, err
}
ext := filepath.Ext(filename)
objectName := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)
if asset == nil {
    asset, err = s.createAssetFromLocalFile(ctx, fileMD5, tmpPath, objectName, contentTypeForFilename(filename), actualSize)
    if err != nil {
        return nil, err
    }
}

// 行 218-242: createAssetFromLocalFile 的补偿逻辑
func (s *MediaService) createAssetFromLocalFile(ctx context.Context, fileMD5, localPath, objectName, contentType string, size int64) (*model.VideoAsset, error) {
    if _, err := s.storage.UploadFromPath(ctx, localPath, objectName, contentType); err != nil {
        return nil, fmt.Errorf("上传到 MinIO 失败: %w", err)
    }
    asset := &model.VideoAsset{FileMD5: fileMD5, ObjectName: objectName, FileSize: size, ContentType: contentType}
    if err := s.repo.Asset.Create(asset); err != nil {
        // 行 232-236: 并发补偿——另一个请求已创建，复用并删除多余对象
        existing, findErr := s.repo.Asset.FindByMD5(fileMD5)
        if findErr == nil && existing != nil {
            _ = s.storage.DeleteObject(ctx, objectName)
            return existing, nil
        }
        _ = s.storage.DeleteObject(ctx, objectName)
        return nil, err
    }
    return asset, nil
}
```

**参考答案**

并发场景下的执行时序：

1. 请求 A 和 B 同时 `FindByMD5`，都返回 nil（行 105）。
2. 两者都上传到 MinIO（各自不同的 `objectName`，UUID 保证不冲突）。
3. 请求 A 先 `repo.Asset.Create` 成功（行 229），写入数据库。
4. 请求 B 的 `Create` 因唯一键冲突失败（行 229），进入补偿分支（行 232）。
5. 请求 B 再次 `FindByMD5`，拿到 A 创建的 asset，删除自己上传的多余 MinIO 对象（行 234），返回 A 的 asset。

**关键设计**：`objectName` 用 UUID 生成（行 111），保证并发上传不会覆盖彼此的文件。补偿逻辑保证最终只有一个 MinIO 对象和一条数据库记录。

**追问链**

1. 行 234 删除 MinIO 对象失败怎么办？不处理——多余对象浪费少量存储，不影响业务正确性。可通过定时清理任务回收。
2. 为什么不用分布式锁防止并发？锁会降低吞吐；当前方案用数据库唯一键 + 乐观补偿，无锁且最终一致。
3. `FindByMD5` 查询走了什么索引？`VideoAsset.FileMD5` 应该有唯一索引，保证查询性能和 Create 的唯一约束。

---

## Q4: UploadByURL 为什么用 Kafka 异步下载而不是同步等待？

**考察点**: 异步架构、用户体验、失败重试

**关键代码** `internal/service/media.go:150-195`

```go
// 行 150-195: UploadByURL 函数
func (s *MediaService) UploadByURL(ctx context.Context, userID int64, videoURL string) (*UploadResult, error) {
    // 行 152: URL 安全验证
    checkedURL, err := newRemoteVideoURLValidator(s.tools, s.remoteURLResolver).validate(ctx, videoURL)
    if err != nil {
        return nil, err
    }

    // 行 158-170: 创建任务，状态=RUNNING，阶段=DOWNLOADING
    task := &model.VideoTask{
        UserID:     userID,
        Status:     model.TaskStatusRunning,
        Stage:      model.TaskStageDownloading,
        SourceType: model.TaskSourceTypeURL,
        SourceURL:  checkedURL.Sanitized,
        MaxRetries: 3,
        // ...
    }
    // 行 174: 创建 TaskJob 记录
    s.repo.TaskJob.UpsertQueued(task, model.TaskJobTypeDownload, model.TaskStageDownloading, task.MaxRetries)

    // 行 178: 投递 Kafka，立即返回
    s.mq.EnqueueDownload(mq.ContextWithTraceID(ctx, task.TraceID), task.ID, key)

    return &UploadResult{TaskID: task.ID, ...}, nil // 行 185: 立即返回
}
```

**参考答案**

视频下载可能耗时数分钟（取决于文件大小和网络），同步等待会导致：

1. **HTTP 请求超时**：客户端/网关通常有 30s 超时，大文件下载必然超时。
2. **连接占用**：同步等待会长期占用 goroutine 和 HTTP 连接，高并发下耗尽资源。
3. **无法重试**：同步失败后客户端需要重新提交；异步模式下 `MaxRetries: 3`（行 168）由 Kafka Consumer 自动重试。

**追问链**

1. 立即返回的 `UploadResult` 里 Status 是什么？是 `TaskStatusRunning`，客户端需要轮询或 WebSocket 监听状态变化。
2. Kafka 投递失败怎么办？行 179-183：更新任务状态为 FAILED，记录失败原因到 TaskJob，返回错误给客户端。
3. 为什么用 `TaskJobTypeDownload` 而不是直接更新 Task 状态？TaskJob 是独立的作业记录，支持重试计数、失败原因追踪，比直接改 Task 更精细。

---

## Q5: 分片上传的 InitChunkedUpload 用 Redis 记录了什么？为什么不用数据库？

**考察点**: Redis 数据结构选型、临时状态 vs 持久状态

**关键代码** `internal/service/media.go:550-555`

```go
// 行 550-555: InitChunkedUpload
func (s *MediaService) InitChunkedUpload(ctx context.Context, fileMD5 string, totalChunks int) error {
    key := fmt.Sprintf("upload:chunks:%s", fileMD5)
    s.rdb.Set(ctx, key+":total", totalChunks, 24*time.Hour) // 行 552: 总分片数
    s.rdb.Set(ctx, key+":status", "INIT", 24*time.Hour)     // 行 553: 上传状态
    return nil
}
```

**参考答案**

Redis 存储的是**临时上传状态**，不是最终业务数据：

1. **`upload:chunks:{md5}:total`** — 总分片数，用于校验完整性。
2. **`upload:chunks:{md5}:status`** — 状态标记（INIT / COMPLETED）。
3. **`upload:chunks:{md5}`** — Set 类型，存储已上传的分片编号（行 596-597）。

**为什么不用数据库？**

- 分片上传是高频操作（每个分片都要更新状态），Redis 内存操作比 DB 快 100 倍。
- 24 小时过期（行 552-553）自动清理，不需要手动维护。
- 分片状态是临时的——合并完成后就不再需要，最终数据（asset/task）才写数据库。

**追问链**

1. 24 小时过期够吗？对于 GB 级文件的断点续传，24 小时可能不够。可以通过客户端心跳续期。
2. 如果 Redis 在上传过程中宕机，分片状态丢失怎么办？`CheckUploadProgress`（行 557-580）查不到记录时返回 `status: "new"`，客户端从头上传。合并时 `MergeChunks`（行 629-638）会校验所有分片是否存在。
3. 用 Set 存分片编号有什么好处？`SAdd` 幂等——重复上传同一分片不会产生重复记录；`SMembers` 可以快速查出已上传的分片列表。

---

## Q6: MergeChunks 的分布式锁做了什么？双重检查解决了什么问题？

**考察点**: 分布式锁、双重检查锁定模式、并发合并

**关键代码** `internal/service/media.go:602-627`

```go
// 行 602: MergeChunks 入口
func (s *MediaService) MergeChunks(ctx context.Context, userID int64, fileMD5, filename string, totalChunks int) (*UploadResult, error) {
    // ...
    // 行 610-616: 第一次检查——是否已合并
    existingAsset, err := s.repo.Asset.FindByMD5(fileMD5)
    if existingAsset != nil {
        return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
    }

    // 行 618-626: 加锁 + 第二次检查
    mergeLock := lock.NewRedisLock(s.rdb, fmt.Sprintf("vidlens:merge:%s", fileMD5))
    acquired, err := mergeLock.TryLock(ctx, 0)
    if err != nil || !acquired {
        // 行 621: 锁获取失败，再次检查是否已合并
        existingAsset, findErr := s.repo.Asset.FindByMD5(fileMD5)
        if findErr == nil && existingAsset != nil {
            return s.createTaskFromAsset(userID, filename, existingAsset, model.TaskStatusPending)
        }
        return nil, fmt.Errorf("合并操作正在进行中，请稍后")
    }
    defer mergeLock.Unlock(ctx) // 行 627
```

**参考答案**

**分布式锁的作用**：防止多个并发请求同时执行 MinIO `ComposeObject`，避免重复合并和资源浪费。

**双重检查模式**：

1. **第一次检查**（行 610-616）：无锁快速路径——如果已合并完成，直接返回，不争抢锁。
2. **获取锁**（行 619）：`TryLock(ctx, 0)` 不等待，获取失败立即返回。
3. **第二次检查**（行 621-624）：锁获取失败时再查一次——可能在等待锁的过程中，其他请求已经合并完成。

这种模式将大多数并发请求挡在锁外面，只有第一个请求真正执行合并，其余请求要么命中第一次检查（已合并），要么命中第二次检查（合并中已完成），要么收到"请稍后"提示。

**追问链**

1. `TryLock(ctx, 0)` 的 0 是什么意思？等待超时为 0，即不等待。获取不到锁立即失败，不自旋。
2. 如果所有请求都收到"请稍后"，客户端怎么处理？客户端轮询 `CheckUploadProgress`，发现 status=COMPLETED 后重新调用 `MergeChunks`，命中第一次检查。
3. 锁的 key 为什么用 `vidlens:merge:{fileMD5}` 而不是 `vidlens:merge:{userID}`？同一文件可能被不同用户上传，锁的粒度是文件内容（MD5），不是用户。

---

## Q7: MinIO ComposeObject 是什么？为什么用它而不是下载-拼接-上传？

**考察点**: 对象存储 Server-Side 操作、零拷贝、带宽优化

**关键代码** `internal/service/media.go:640-654`

```go
// 行 640-654: 构建 ComposeObject 参数
ext := filepath.Ext(filename)
dst := fmt.Sprintf("videos/%s%s", uuid.New().String(), ext)

srcs := make([]minio.CopySrcOptions, 0, totalChunks)
for i := range totalChunks {
    srcs = append(srcs, minio.CopySrcOptions{
        Bucket: s.storage.BucketName(),
        Object: fmt.Sprintf("chunks/%s/%d", fileMD5, i),
    })
}

size, err := s.storage.ComposeObject(ctx, dst, srcs) // 行 651: 服务端合并
```

**参考答案**

`ComposeObject` 是 MinIO/S3 的 **Server-Side Copy + Concat** 操作：

- 在 MinIO 服务端将多个分片对象拼接成一个完整对象。
- **数据不经过应用服务器**，完全在 MinIO 内部完成。
- 零网络传输开销，合并速度取决于 MinIO 内部 I/O。

**对比下载-拼接-上传**：

| 维度 | ComposeObject | 下载-拼接-上传 |
|------|---------------|----------------|
| 网络流量 | 0（服务端完成） | 2x（下载+上传） |
| 应用服务器内存 | 不需要缓冲 | 需要缓冲整个文件 |
| 耗时 | 秒级 | 分钟级（取决于文件大小） |

**追问链**

1. ComposeObject 有大小限制吗？MinIO 单个分片最大 5GB，ComposeObject 最多 10000 个分片，理论上限 48.8TB。
2. 分片对象的存储路径 `chunks/{md5}/{index}` 有什么设计考量？按 MD5 分目录，方便批量清理（行 676-685 的 `cleanupMergedChunks`）；index 是自然数，有序且可预测。
3. 合并后分片什么时候清理？行 670-671：合并成功后立即 best-effort 清理。如果清理失败，分片残留只浪费存储，不影响业务。

---

## Q8: DeleteTask 的级联删除顺序有什么讲究？为什么先清向量再删数据库？

**考察点**: 删除顺序、数据一致性、事务边界

**关键代码** `internal/service/media.go:428-498`

```go
// 行 428-498: DeleteTask 函数
func (s *MediaService) DeleteTask(ctx context.Context, userID, taskID int64) error {
    // 行 437-447: 第一步——清理向量数据库（事务外面）
    embeddingModels, err := s.collectTaskEmbeddingModels(userID, taskID)
    if s.taskVectorCleaner != nil {
        for _, modelName := range embeddingModels {
            if err := s.taskVectorCleaner.DeleteTaskChunks(ctx, userID, taskID, modelName); err != nil {
                return fmt.Errorf("清理向量数据失败: %w", err)
            }
        }
    }

    // 行 452-487: 第二步——DB 事务内删除所有关联数据
    s.repo.Transaction(func(txRepos *repository.Repositories) error {
        txRepos.Transcription.DeleteByTaskID(taskID)
        txRepos.TranscriptionChunk.DeleteByTaskID(taskID)
        txRepos.Summary.DeleteByTaskID(taskID)
        txRepos.VideoChunk.DeleteByTaskID(taskID)
        txRepos.RAGIndex.DeleteByTaskID(taskID)
        txRepos.Chat.DeleteByTaskID(taskID)
        txRepos.TaskJob.DeleteByTaskID(taskID)
        txRepos.Task.Delete(taskID)
        // 行 477-483: 检查 asset 引用计数
        activeRefs, _ := txRepos.Task.CountActiveByAssetID(*assetID)
        deleteAssetObject = activeRefs == 0
    })

    // 行 489-496: 第三步——事务成功后删除 MinIO 对象
    if deleteAssetObject {
        s.deleteObject(ctx, objectName)
        s.repo.Asset.Delete(*assetID)
    }
```

**参考答案**

删除顺序遵循**先外后内、先无事务后有事务**原则：

1. **向量数据库先删**（行 441-446）：Milvus 不支持与 MySQL 的分布式事务。如果先删 DB 再删向量，DB 删成功但向量删失败，向量数据变成孤儿——查询不到关联的 task，无法重试清理。
2. **DB 事务内批量删**（行 452-487）：8 张表在一个事务内删除，要么全成功要么全回滚，保证 DB 一致性。
3. **MinIO 对象最后删**（行 489-496）：事务成功后再删存储对象。如果先删对象再删 DB，DB 事务失败会导致对象已删除但记录还在——文件丢失。

**追问链**

1. 如果向量删除失败，整个删除操作怎么处理？行 443-446 直接返回错误，DB 和 MinIO 不动。客户端可重试。
2. `CountActiveByAssetID` 检查的是什么？检查还有多少 task 引用这个 asset。如果 > 0，说明其他任务也在用同一个文件，不能删除 MinIO 对象。
3. 行 489 的 `deleteAssetObject` 为什么在事务外面执行？MinIO 删除不是数据库操作，不能放在 DB 事务里。事务提交后再删，失败了可以重试。

---

## Q9: URL 安全验证做了哪些防护？DNS Rebinding 攻击如何防御？

**考察点**: SSRF 防护、DNS Rebinding、白名单校验

**关键代码** `internal/service/remote_video_url.go:52-96`

```go
// 行 52-96: validate 函数
func (v remoteVideoURLValidator) validate(ctx context.Context, rawURL string) (checkedRemoteVideoURL, error) {
    parsed, err := neturl.Parse(rawURL)
    // 行 59-62: 只允许 http/https
    scheme := strings.ToLower(parsed.Scheme)
    if scheme != "http" && scheme != "https" {
        return checkedRemoteVideoURL{}, fmt.Errorf("仅支持 http/https 视频链接")
    }

    // 行 68-69: 禁止 localhost
    if host == "localhost" {
        return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问本地地址")
    }
    // 行 71-73: 域名白名单
    if !hostAllowed(host, v.allowedHosts) {
        return checkedRemoteVideoURL{}, fmt.Errorf("不支持的视频平台域名: %s", host)
    }

    // 行 75-92: IP 安全检查（防 SSRF）
    if ip := net.ParseIP(host); ip != nil {
        if unsafeIP(ip) {
            return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问内网或本地地址")
        }
    } else {
        ips, err := v.resolver.LookupIP(ctx, host)
        for _, ip := range ips {
            if unsafeIP(ip) {
                return checkedRemoteVideoURL{}, fmt.Errorf("视频链接域名解析到内网或本地地址")
            }
        }
    }
```

**参考答案**

VidLens 的 URL 安全验证采用**三层纵深防御**：

1. **白名单域名**（行 71-73）：只允许 bilibili.com、youtube.com 等已知视频平台。
2. **localhost 封禁**（行 68-69）：直接拒绝 `localhost`。
3. **IP 安全检查**（行 75-92）：DNS 解析后检查 IP 是否为内网/环回/链路本地地址。

**DNS Rebinding 防御**：

攻击者注册域名 `evil.com`，第一次 DNS 解析返回合法 IP（通过白名单检查），第二次解析返回 `127.0.0.1`。VidLens 在行 80-92 **解析域名后逐一检查所有 IP**，如果任何一个 IP 是内网地址就拒绝。

`unsafeIP`（行 116-124）检查：`IsLoopback`、`IsPrivate`、`IsUnspecified`、`IsLinkLocalUnicast`、`IsLinkLocalMulticast`、`IsMulticast`。

**追问链**

1. 白名单用 `hostAllowed` 做后缀匹配（行 109），`evilbilibili.com` 会通过吗？不会，行 109 检查 `host == allowed || strings.HasSuffix(host, "."+allowed)`，`evilbilibili.com` 不以 `.bilibili.com` 结尾。
2. 如果攻击者用 IPv6 地址绕过呢？`net.ParseIP` 同时支持 IPv4 和 IPv6，`IsPrivate` 等方法对 IPv6 也有效（如 `::1` 是 loopback，`fc00::/7` 是 private）。
3. 这套防护还有什么遗漏？未检查 URL 中的 `@` 符号（`http://trusted.com@evil.com`）——但 `neturl.Parse` 会正确分离 host 和 userinfo，行 94 的 `sanitizeRemoteVideoURL` 会清除 `User` 信息。

---

## Q10: UploadResult 的 TraceID 有什么用？如何贯穿整个异步链路？

**考察点**: 分布式追踪、日志关联、异步链路可观测性

**关键代码** `internal/service/media.go:81-90` 和 `internal/service/media.go:159-178`

```go
// 行 81-90: UploadResult 结构体
type UploadResult struct {
    TaskID   int64  `json:"task_id"`
    FileMD5  string `json:"file_md5"`
    Filename string `json:"filename"`
    FileURL  string `json:"file_url"`
    FileSize int64  `json:"file_size"`
    Status   int8   `json:"status"`
    Stage    string `json:"stage"`
    TraceID  string `json:"trace_id"` // 行 89: 追踪 ID
}

// 行 159-165: UploadByURL 中生成 TraceID
task := &model.VideoTask{
    // ...
    TraceID: uuid.New().String(), // 行 165: 任务创建时生成
}

// 行 178: 投递 Kafka 时注入 TraceID
s.mq.EnqueueDownload(mq.ContextWithTraceID(ctx, task.TraceID), task.ID, key)
```

**参考答案**

`TraceID` 是贯穿整个异步处理链路的唯一标识：

1. **生成时机**：任务创建时（行 165），UUID v4 保证全局唯一。
2. **注入 Context**：`mq.ContextWithTraceID` 将 TraceID 写入 Context，Kafka 消息头携带。
3. **Consumer 提取**：Kafka Consumer 消费时从消息头提取 TraceID，写入日志。
4. **端到端关联**：从用户上传 → Kafka 下载 → FFmpeg 处理 → ASR 转写 → LLM 摘要，所有环节的日志都携带同一个 TraceID。

**实际用途**：

- 用户反馈"视频处理失败"时，通过 TraceID 可以在日志系统中检索整个链路的所有日志。
- 分布式系统中，一个 TraceID 对应一个完整的业务流程，便于定位卡在哪个阶段。

**追问链**

1. 为什么用 UUID 而不是 Snowflake？UUID 无依赖、无需协调；Snowflake 需要机器 ID 分配。TraceID 不需要有序，UUID 足够。
2. TraceID 和 TaskID 什么关系？一个 TaskID 对应一个 TraceID（一对一）。TaskID 是数据库自增 ID，TraceID 是全局唯一标识。TaskID 在数据库内唯一，TraceID 跨系统唯一。
3. 如果要接入 OpenTelemetry，TraceID 怎么改？用 OTel 的 `trace.SpanContext.TraceID()`（128 bit）替换 UUID，自动关联 Span 链路。

---

## 总结

这些面试题覆盖了媒体上传模块的核心知识点：

1. **流式处理**: 临时文件 + io.TeeReader 一遍读取算两份
2. **大小校验**: io.LimitReader 的 maxSize+1 技巧
3. **并发去重**: 数据库唯一键 + 乐观补偿，无锁设计
4. **异步架构**: Kafka 解耦长耗时下载，支持重试
5. **临时状态**: Redis 存分片进度，24h 自动过期
6. **分布式锁**: 双重检查模式，最小化锁竞争
7. **Server-Side 合并**: ComposeObject 零拷贝，应用层不搬运数据
8. **级联删除**: 先清向量 → 再删 DB（事务） → 最后删存储
9. **SSRF 防护**: 白名单 + DNS 解析 IP 检查，三层纵深防御
10. **分布式追踪**: TraceID 贯穿异步链路，端到端可观测

掌握这些知识点，能够应对大多数文件上传与存储相关的面试问题。
