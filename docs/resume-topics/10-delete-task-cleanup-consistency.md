# 专题 10：删除任务、向量清理和共享资产生命周期

> 面试高频问题："用户删除一个视频任务时，你要删哪些数据？MinIO 和 Milvus 里的数据怎么办？"
> 这类问题考的是工程完整性。上传、分析和问答只是正向链路，删除和清理能看出你有没有考虑数据生命周期。

## 1. 先给总答案

推荐先这样答：

> 删除视频任务不能只删 `video_tasks` 一行。这个任务关联了转写、转写分片、摘要、RAG chunk、RAG index、聊天记录、task_jobs 和 Milvus 向量，都要清理。同时真实视频文件在 `video_assets` 中可能被多个任务复用，所以不能直接删 MinIO 对象。当前实现会先收集该任务用到的 embedding model，清理 Milvus 中对应 `user_id + task_id + embedding_model` 的向量，再在 MySQL 事务里删除相关业务表，最后检查 asset 是否还有其他 task 引用，没有引用才删除 MinIO 对象和 asset 记录。

一句话：

> 删除 task 是业务数据清理，删除 asset 是引用计数归零后的文件资产清理，两者不能混在一起。

## 2. 当前删除流程

核心入口：

```text
internal/service/media.go
DeleteTask
```

当前流程：

1. 查询 task，校验 task 属于当前 user。
2. 收集当前 task 涉及的 embedding models。
3. 如果配置了向量 cleaner，按 `userID + taskID + embeddingModel` 删除 Milvus 向量。
4. 开启 MySQL transaction。
5. 删除转写全文。
6. 删除转写分片。
7. 删除摘要。
8. 删除 MySQL video chunks。
9. 删除 RAG index 状态。
10. 删除 chat sessions 和 chat messages。
11. 删除 task_jobs。
12. 逻辑删除 video_task。
13. 在事务中统计该 asset 是否还有其他 task 引用。
14. 事务提交后，如果没有引用，删除 MinIO 对象，再删除 `video_assets` 记录。

相关代码：

```text
internal/service/media.go              DeleteTask
internal/service/media.go              collectTaskEmbeddingModels
internal/vector/milvus.go              DeleteTaskChunks
internal/repository/transcription.go   DeleteByTaskID
internal/repository/summary.go         DeleteByTaskID
internal/repository/video_chunk.go     DeleteByTaskID
internal/repository/rag_index.go       DeleteByTaskID
internal/repository/chat.go            DeleteByTaskID
internal/repository/task_job.go        DeleteByTaskID
internal/repository/task.go            CountActiveByAssetID
```

## 3. 为什么不能只删任务表

只删 `video_tasks` 会留下：

- 转写全文。
- 分片转写结果。
- AI 摘要。
- MySQL RAG chunks。
- Milvus 向量。
- RAG index 状态。
- 问答会话和消息。
- 子任务 job 状态。
- 可能没人引用的 MinIO 视频对象。

这些残留会带来问题：

- 用户以为删了，但数据库仍有视频文本。
- RAG 可能残留隐私片段。
- MinIO 存储成本持续增长。
- 后续同 taskID 或 vectorID 清理困难。
- 排障时状态混乱。

推荐回答：

> 删除是反向生命周期，和上传处理一样重要。尤其 RAG chunk 和向量本质上能还原视频内容，不能只删任务主表。

## 4. 为什么 MinIO 对象不能直接删

因为项目做了 MD5 asset 复用。

一个真实视频文件对应一条 `video_assets`，多个 `video_tasks` 可以引用同一个 asset。

如果用户 A 和用户 B 上传了同一个视频：

```text
video_assets.id = 1
video_tasks.id = 10, asset_id = 1, user_id = A
video_tasks.id = 11, asset_id = 1, user_id = B
```

删除任务 10 时，不能直接删 asset 1 对应的 MinIO 对象，否则任务 11 的视频文件也没了。

当前做法：

> 先删除当前 task 的派生数据，再统计 asset 是否还有 active task 引用。只有引用数为 0，才删除对象存储里的真实视频文件。

## 5. 为什么要先清理 Milvus

Milvus 和 MySQL 不在同一个事务里。当前实现先尝试删除 Milvus 向量，如果失败就终止删除，避免 MySQL task 已删但向量残留。

这个选择有 tradeoff：

- 好处：尽量避免用户数据在向量库残留。
- 风险：如果 Milvus 删除成功、后续 MySQL 事务失败，会出现 task 仍在但向量已删的短暂不一致。

面试要诚实：

> 当前项目没有 MySQL 和 Milvus 的分布式事务，采用的是最终一致和失败返回。生产上更稳的做法是引入 deletion job 或 outbox，把删除动作记录下来，失败可以重试和补偿。

## 6. 高频追问

### Q1：删除一个任务时具体删哪些表？

答：

> 转写、转写分片、摘要、video_chunks、RAG index、聊天会话和消息、task_jobs、video_task 都要删。向量库里对应的 task chunks 也要删。最后再判断 asset 是否还能被其他 task 引用。

### Q2：为什么要按 embedding model 删除向量？

答：

> 同一个 task 理论上可能用不同 embedding model 构建过索引。Milvus 删除过滤里需要 `user_id + task_id + embedding_model`，所以删除前要从 MySQL chunk 和 RAG index 里收集 model 名称。

### Q3：删除失败怎么办？

答：

> 当前同步删除失败会返回错误，不继续往后删。生产上可以把删除拆成异步 deletion job，记录每一步状态，支持重试和补偿，避免某个外部存储失败导致清理半完成。

### Q4：MySQL 删除和 MinIO 删除怎么保证事务？

答：

> 不能用单个本地事务保证，因为 MinIO 不是 MySQL。当前做法是 MySQL 先事务删除业务数据，提交后如果 asset 无引用再删 MinIO 对象。这里是最终一致。生产上可以用 outbox 记录对象删除任务，后台重试，避免对象删除失败无人处理。

### Q5：如果 MinIO 对象删除失败怎么办？

答：

> 当前会返回删除视频对象失败。更生产化的做法是保留待删除记录，后台重试对象删除，避免数据库状态和对象存储长期不一致。

### Q6：用户删除任务后还能恢复吗？

答：

> 当前是删除语义，不是回收站语义。`video_tasks` 使用 GORM 软删除，但关联的转写、摘要、RAG、聊天等会被删除。生产如果要恢复，需要设计回收站和延迟清理，而不是立即清理所有派生数据。

### Q7：删除任务会影响其他用户吗？

答：

> 当前不会直接删共享 asset，删除前会看 asset 是否还有其他 task 引用。每个 task 的转写、摘要、聊天和 RAG 数据按 task 清理，不会清理其他 task。

## 7. 30 秒话术

> 删除任务不是只删 `video_tasks`。一个视频任务下面有转写、摘要、RAG chunks、RAG index、聊天记录、task_jobs，还有 Milvus 向量。当前实现会先按 userID、taskID 和 embedding model 清理向量，再在 MySQL 事务里删除这些关联数据。真实视频文件放在 `video_assets`，可能被多个任务复用，所以只有确认没有其他 task 引用时，才删除 MinIO 对象和 asset 记录。

## 8. 2 分钟话术

> 删除链路是这个项目里很容易被忽略的一块。用户删除一个视频任务时，如果只删主任务表，会留下转写文本、RAG chunk、向量和聊天记录，这些都可能包含用户视频内容，所以必须一起清理。
>
> 当前实现先校验任务属于当前用户，然后收集这个任务用过的 embedding model，并调用 Milvus cleaner 删除对应向量。之后进入 MySQL transaction，删除转写、转写分片、摘要、video_chunks、rag_index、chat、task_jobs 和 video_task。最后根据 assetID 检查是否还有其他任务引用同一个真实视频文件，只有引用数为 0 时才删除 MinIO 对象和 `video_assets`。
>
> 这里的边界是 MySQL、Milvus、MinIO 不能放在一个本地事务里，所以当前是最终一致。生产上可以引入 deletion job 或 outbox，把外部资源删除做成可重试补偿。

## 9. 不要这么说

- 不要说删除任务就是 `DELETE video_tasks`。
- 不要说 MySQL、MinIO、Milvus 能强事务一致。
- 不要说共享 asset 可以直接删。
- 不要说当前已经有完整回收站。
- 不要忽略 Milvus 向量残留的隐私风险。

## 10. 代码证据路径

```text
internal/service/media.go
  DeleteTask
  collectTaskEmbeddingModels
  deleteObject

internal/vector/milvus.go
  DeleteTaskChunks

internal/repository/task.go
  CountActiveByAssetID
  Delete

internal/repository/chat.go
  DeleteByTaskID

internal/repository/video_chunk.go
  DeleteByTaskID

internal/repository/rag_index.go
  DeleteByTaskID
```

