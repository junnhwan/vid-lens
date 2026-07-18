# 专题 10：任务删除、向量清理与共享资产生命周期

## 1. 面试口语答案

> 删除任务不是一条 SQL，因为一个 task 关联转写、ASR 分片、摘要、RAG chunk、向量投影、聊天记录和可能被其他用户任务共享的 MinIO asset。PostgreSQL、pgvector 连接池和 MinIO 之间也不存在一个跨资源事务。
>
> 当前做法是先在 PostgreSQL 事务中锁定 task，拒绝删除 queued/running 任务，创建唯一的 `task_cleanup_jobs` durable intent，然后软删除 task。对用户来说任务立即不可见；真正资源回收由 cleanup service 立即尝试，并由 scheduler 持续扫描失败或 lease 过期的 job。
>
> worker claim cleanup lease 后，先从 `video_chunks` 和索引状态收集该 task 使用过的 embedding model，按 `user_id + task_id + model` 删除 pgvector 投影。之后在资产行锁下统计仍存活的 task 引用；只有最后引用者才能标记 asset 为 deleting 并取得删除 owner。MinIO 删除成功后，再用 PostgreSQL 事务删除 task-owned rows、删除已取得所有权的 asset，并用 token 完成 cleanup job。任一步失败都会记录原因和下次重试时间，操作按幂等方式重放。

## 2. 当前流程

```text
DELETE request
  -> PostgreSQL transaction
       FOR UPDATE task
       reject active task
       create task_cleanup_job
       soft-delete task
  -> ExecuteJob / scheduler
       claim cleanup lease
       delete pgvector projection by scope
       reserve shared asset deletion under row lock
       delete MinIO object if this job owns last asset
       PostgreSQL transaction:
         delete task-owned rows
         delete owned asset
         mark cleanup completed
```

## 3. 为什么顺序重要

- 先保存 intent：进程在外部清理前宕机仍能恢复。
- 先采集 embedding model 再删关系 chunk：否则失去精确向量 scope。
- 先判断共享引用再删对象：否则一个用户删除 task 会破坏其他 task。
- 最后完成 job：只有 token owner 能把本次 lease 标为完成。

## 4. 高频追问

### PostgreSQL 和 MinIO 怎么保证事务？

> 没有伪装成分布式事务。PostgreSQL 先持久化意图，外部删除要求幂等，失败后 scheduler 重试，最终再完成关系收尾。这是 durable intent + retry 的最终一致方案。

### pgvector 已经同库，为什么不和关系删除放一个事务？

> 当前 vector store 使用独立 pgx pool，cleanup 通过向量 backend 接口执行，仍不在 GORM transaction 中。同一个数据库减少运维成本，不会自动把跨连接、跨阶段操作变成一个事务。

### 删除一个任务会不会删除共享视频？

> 不会直接删。资产行加锁后统计 active task references，只有引用为零且当前 cleanup job 获得 deletion ownership 才删除对象和 asset。

### 删除失败用户还能看到任务吗？

> task intent 提交后任务已软删除，对用户不可见；cleanup job 保留失败原因并重试。这里应说“用户删除请求已接受、物理资源最终回收”，不能说所有资源同步原子消失。

## 5. 代码证据

- `internal/service/task_cleanup.go`：请求事务、清理顺序和资产 ownership。
- `internal/service/task_cleanup_scheduler.go`：失败/过期 lease 扫描。
- `internal/repository/task_cleanup_job.go`：claim、失败退避和完成 CAS。
- `internal/model/task_cleanup_job.go`：durable intent。
- `internal/repository/asset.go`：共享资产行锁和 deletion owner。
- `internal/service/task_cleanup_test.go`：幂等、共享资产和失败窗口测试。

## 6. 当前限制

- 不支持删除正在 queued/running 的任务；尚未实现取消协议和 consumer 协作终止。
- completed upload session 指向已删除 task 时，重复 complete 的稳定响应仍有生命周期缺口。
- MinIO/向量删除持续失败时需要运维观察 cleanup 指标和错误记录；目前没有专门管理后台。
