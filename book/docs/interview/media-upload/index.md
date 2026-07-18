# 视频上传与对象存储面试题

> 分片协议细节统一见[分片上传与断点续传](/interview/resume-grill/resume-core/chunk-upload/)。本页讨论普通上传、durable session、MinIO、资产复用和删除边界。

## Q1：VidLens 有哪些视频进入方式？

**回答：**

- 普通本地上传：HTTP multipart，Service 流式落临时文件并计算完整 MD5；
- durable upload session：创建 manifest，PUT raw chunks，恢复进度后 complete；
- URL 下载：HTTP 创建异步任务，Kafka Consumer 调用 yt-dlp；这是辅助能力，不作为简历重点。

所有路径最终都把真实视频放 MinIO，把 asset/task 状态放 PostgreSQL。

## Q2：普通上传为什么要先落临时文件？

上传流需要同时完成体积限制、完整 MD5 计算和可重放的 MinIO 上传。`copyStreamToTempAndHash` 使用流式复制，不把完整文件读进 Go 堆；代价是占用本机临时磁盘。正常路径会清理临时文件，进程崩溃后的残留仍需要系统级临时目录治理。

## Q3：durable upload session 为什么不用 Redis？

上传 owner、不可变 manifest、accepted chunk ledger、完成 lease 和稳定 task identity 都是失败后仍需恢复的业务事实。当前由 PostgreSQL 的 `upload_sessions` / `upload_session_chunks` 保存；Redis TTL 丢失不能改变上传正确性。

MinIO 只保存字节，不能单凭对象列表推导业务已接受状态。

## Q4：单片怎样限制体积？

Handler 先读取 session manifest，根据 index 算出权威大小：普通片等于 `chunk_size`，末片等于剩余字节。请求 body 经过 `http.MaxBytesReader`，Service 再用有界 Reader 写临时文件并要求实际大小精确一致。

## Q5：如何处理重复 chunk？

服务端为每片计算 SHA-256，并用 `(session_id, chunk_index)` 唯一约束：

- 同 size/hash 重传是幂等成功；
- 同 index 不同内容返回 409；
- 对象名包含 hash，冲突上传不会覆盖 accepted bytes。

## Q6：complete 如何防并发？

PostgreSQL session 使用 token + expiry completion lease。活跃 lease 拒绝竞争请求；执行者宕机后，过期 lease 可被 reclaim；所有完成/释放更新都校验 token，旧 owner 不能写坏新 lease。

## Q7：最终文件如何合并？

Service 按 chunk index 打开 MinIO 对象，通过 `io.Pipe` 顺序复制到 final object，同时计算服务端完整 MD5 和字节数。它是有界流式处理，不会把完整视频一次加载到内存。

代价是数据经过 `MinIO → Go → MinIO`，不是存储侧零拷贝。当前选择优先保证端到端内容校验；未来只有在压测证明 API/worker 网络成为瓶颈后，才评估预签名 multipart 和对象存储 checksum。

## Q8：asset 和 task 为什么分表？

`video_assets` 表示真实视频对象，按完整 MD5 唯一；`video_tasks` 表示某个用户的一次处理上下文。多个 task 可以引用同一个 asset，避免重复保存字节，但用户状态、聊天和处理生命周期仍彼此隔离。

## Q9：为什么 Redis 锁不是最终防线？

普通上传和 Consumer 的某些临界区使用 owner-aware Redis lock + watchdog 来减少重复昂贵操作，但 Redis 锁可能因网络、TTL 或进程状态失效。数据库唯一约束、处理 lease 和幂等状态转换才是物理兜底。

新的 upload session complete 不依赖旧的 merge Redis 锁，而由 PostgreSQL completion lease 管理 owner 与恢复。

## Q10：删除 task 为什么不能直接删 MinIO 对象？

asset 可能被多个 task 引用。删除流程先持久化 cleanup intent，再幂等清理当前 task 的向量投影、关系数据和聊天等资源；只有最后一个引用消失时才删除 MinIO asset。失败通过 cleanup lease 和后台扫描继续恢复。

这不等于 MinIO 与 PostgreSQL 在同一事务。对象删除是外部副作用，依靠可恢复状态和幂等顺序治理。

## Q11：URL 下载安全做到了什么？

当前入口和 Consumer 执行前共享 scheme、host allowlist 和 DNS 私网地址检查，并清理 URL 中不必要信息。yt-dlp 是外部进程，可能处理重定向和再次解析，因此这只是基础防护，不是生产级网络沙箱。

URL 下载不是简历重点，后续优先级低于上传生命周期、Kafka 首次投递和 PostgreSQL 备份恢复。

## Q12：当前限制是什么？

- abandoned/expired upload session 和孤儿对象后台回收未完成；
- completed session 在关联 task 删除后的回执语义待明确；
- 预签名 multipart 直传未实现；
- 没有可用于简历的 GB 级弱网或高并发数据；
- URL 下载没有独立受限 egress worker。

## 证据路径

```text
internal/service/media_file_upload.go
internal/service/media_url_upload.go
internal/service/upload_session.go
internal/service/upload_session_chunk.go
internal/service/upload_session_complete.go
internal/repository/upload_session.go
internal/storage/minio.go
internal/service/task_cleanup.go
internal/service/task_cleanup_scheduler.go
internal/pkg/lock/redis_lock.go
```

## 面试口语版

> 本地小文件可以走普通 multipart，后端边写临时文件边算 MD5，不把整个视频读入内存。需要续传的大文件走用户级 upload session：PostgreSQL 保存不可变 manifest、分片大小和 SHA-256 台账以及完成 lease，MinIO 保存字节，Redis 不参与上传正确性。complete 时服务端顺序读取分片、流式写 final object并重新计算完整 MD5，然后在一个 PostgreSQL transaction 里创建 asset、task并保存稳定 task ID。这个方案的代价是字节经过 Go，不是零拷贝；当前还缺 abandoned session 和孤儿对象后台回收，这些我会明确作为下一步。
