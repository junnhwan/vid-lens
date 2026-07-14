# 简历主线三：分片上传、断点续传与 MinIO

> 练法：先只看问题口述 30 秒，再展开答案；每题都要能说出当前边界。

## Q1：为什么大文件要分片上传？

**直接回答：**
整文件请求中断后只能从头传，而且业务服务要长时间持有连接。分片后每片独立上传，客户端查询已完成编号，只补缺失部分。

**追问与防守：**
分片降低重传成本，但协议、状态和临时对象清理更复杂。

**项目证据：** `internal/handler/media.go:66-138`；`internal/service/media.go:550-599`。

## Q2：Redis 中具体记录什么？

**直接回答：**
upload:chunks:{md5} Set 保存已落盘的 chunk number，附加 total 和 status key，并设置 24 小时 TTL。查询时用 SMembers 返回已上传编号。

**追问与防守：**
Redis 是上传会话状态，不是最终文件唯一事实来源。

**项目证据：** `internal/service/media.go:550-580`。

## Q3：为什么必须先落盘再记账？

**直接回答：**
UploadChunk 先写 MinIO，成功后才 SAdd。若先记 Redis、对象写失败，合并会误以为分片已完成。

**追问与防守：**
当前 SAdd/Expire 错误处理仍可加强，生产实现应检查 Redis 返回值并提供对象反查。

**项目证据：** `internal/service/media.go:582-599`。

## Q4：合并前怎么验证完整性？

**直接回答：**
拿到合并锁后遍历 0 到 totalChunks-1，用 SIsMember 检查每片；缺任何一片就返回具体编号，不执行 Compose。

**追问与防守：**
仅信客户端传来的 total 不够，初始化会话还应绑定文件大小、分片大小和用户。

**项目证据：** `internal/service/media.go:602-638`。

## Q5：为什么使用 MinIO ComposeObject？

**直接回答：**
Go 服务只构造按编号排序的源对象列表，MinIO 在存储侧合并，避免业务进程把整个视频读回、落临时盘再上传。

**追问与防守：**
它降低业务服务 I/O，不等于合并无成本；仍受对象存储 multipart/compose 限制。

**项目证据：** `internal/service/media.go:640-654`；`internal/storage/minio.go:105-112`。

## Q6：为什么合并需要分布式锁？

**直接回答：**
两个实例可能同时发现 asset 不存在并同时合并。同 MD5 锁把检查、Compose、资产落库串行化，第二个请求可复用已创建 asset。

**追问与防守：**
锁不是唯一保障，数据库唯一约束和幂等资产查询仍要兜底。

**项目证据：** `internal/service/media.go:610-627`。

## Q7：锁提前过期或误删怎么办？

**直接回答：**
自定义锁使用 UUID owner，WatchDog 每 ttl/3 续期；续期和释放用 Lua 先校验 owner，避免旧请求删除新持有者的锁。

**追问与防守：**
WatchDog 失败仍需要业务幂等兜底，不能声称锁解决所有一致性问题。

**项目证据：** `internal/pkg/lock/redis_lock.go:13-21`；`internal/pkg/lock/redis_lock.go:76-139`。

## Q8：断点续传和秒传有什么区别？

**直接回答：**
断点续传复用同一次上传已完成的 chunk；秒传或内容去重复用数据库中同 MD5 的完整 VideoAsset。两者共享 MD5，但解决不同问题。

**追问与防守：**
MD5 用于内容标识和项目级去重，不应包装成对抗恶意碰撞的安全哈希。

**项目证据：** `internal/service/media.go:98-119`；`internal/service/media.go:610-616`。

## Q9：合并成功后如何清理？

**直接回答：**
最终 asset 落库后把 status 设为 COMPLETED，再 best-effort 删除临时 chunk 和 Redis Set；清理失败只浪费存储，不回滚已成功资产。

**追问与防守：**
需要后台垃圾回收任务处理遗留对象，当前 best-effort 日志不是完整生命周期治理。

**项目证据：** `internal/service/media.go:656-684`。

## Q10：Redis 宕机或状态过期怎么办？

**直接回答：**
当前上传会话依赖 Redis，状态丢失后客户端可能需要重新初始化和补传。最终已合并资产仍由数据库与 MinIO 保存。

**追问与防守：**
生产方案可把 upload session 持久化到 MySQL，并用 MinIO StatObject 重建部分状态。

**项目证据：** `internal/service/media.go:557-580`；`internal/model/video_asset.go:1-80`。
