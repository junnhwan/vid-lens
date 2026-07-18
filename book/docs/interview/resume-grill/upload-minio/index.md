# 上传、断点续传与 MinIO

> 上传协议的详细事实只维护在[分片上传与断点续传](/interview/resume-grill/resume-core/chunk-upload/)；本页只保留模块边界和面试导航。

## 当前上传入口

VidLens 有两条主要本地上传路径：

1. 普通 multipart 上传：适合较小文件，Service 流式写临时文件并计算 MD5；
2. durable upload session：适合需要恢复的文件，PostgreSQL 保存 manifest/ledger/lease，MinIO 保存 chunk/final bytes。

URL 下载只是辅助功能，采用 Kafka 异步执行，不作为简历重点。

## MinIO 的职责

MinIO 是对象存储，不是上传状态数据库：

- 保存普通上传的最终对象；
- 保存 session 的 content-addressed chunk；
- 保存 session 完成后的 final object；
- 提供读取、删除和预签名下载能力。

对象存在不自动证明 PostgreSQL ledger 已接受该 chunk；业务状态必须从 PostgreSQL 查询。

## MD5 资产复用

完整文件 MD5 用于 `video_assets` 的内容身份和复用。它不是 upload session 身份，也不能绕过 user ownership。session 完成时服务端重新计算完整 MD5，再决定复用已有 asset 还是创建新 asset。

## Redis 还用于什么

Redis 仍用于分布式锁、令牌桶和最近聊天记忆，但不再记录 upload session 或 chunk 完成事实。普通上传/分析消费中的锁是并发优化，PostgreSQL 唯一约束和任务幂等才是最终物理兜底。

## 高频追问

- **为什么不用对象存储侧直接合并？** 当前优先获得服务端完整 MD5/size 校验，使用 `io.Pipe` 流式传输；代价是字节经过 Go。
- **为什么不用 Redis Set？** owner、不可变 manifest、单片 hash、完成回执和过期状态都是耐久业务事实，应该由 PostgreSQL 承担。
- **为什么不用预签名直传？** 当前规模先保证协议正确；压测证明 API 带宽成为瓶颈后，再设计带 checksum、对象名权限和完成回调校验的 multipart 方案。
- **能否保证没有孤儿对象？** 不能。成功清理是 best-effort，abandoned session/孤儿对象后台回收仍是未来工作。

## 证据入口

- [分片上传与断点续传](/interview/resume-grill/resume-core/chunk-upload/)
- `internal/service/media_file_upload.go`
- `internal/service/upload_session*.go`
- `internal/storage/minio.go`
- `internal/repository/upload_session.go`
- `web/src/chunkedUpload.js`
