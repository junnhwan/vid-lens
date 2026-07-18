# 分片上传与断点续传面试防守

本文件曾描述已经退役的 `check-upload -> upload-chunk -> merge-chunks` Redis 协议。为避免两份文档继续漂移，当前实现和面试口径统一维护在：

- [`docs/resume-topics/03-chunk-upload-resume.md`](resume-topics/03-chunk-upload-resume.md)
- [`docs/backend-maintenance-map.md`](backend-maintenance-map.md)

当前结论：PostgreSQL 是 upload session、manifest、chunk ledger、completion lease 和稳定 task identity 的事实源；MinIO 只保存字节；Redis 不参与上传正确性。旧路由和 `internal/service/media_chunk_upload.go` 已移除。
