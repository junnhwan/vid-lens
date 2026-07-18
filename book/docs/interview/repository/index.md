# Repository 层面试题

## 1. Repositories 聚合根的设计意图是什么？与直接在 Service 层注入 *gorm.DB 相比有什么优势？

VidLens 采用 `Repositories` 结构体将所有子 Repository 聚合在一起：

```go
// internal/repository/repository.go:6-20
type Repositories struct {
    db                 *gorm.DB
    User               *UserRepository
    Asset              *AssetRepository
    Task               *TaskRepository
    TaskJob            *TaskJobRepository
    Transcription      *TranscriptionRepository
    TranscriptionChunk *TranscriptionChunkRepository
    Summary            *SummaryRepository
    AIProfile          *AIProfileRepository
    VideoChunk         *VideoChunkRepository
    RAGIndex           *RAGIndexRepository
    Chat               *ChatRepository
    AICallLog          *AICallLogRepository
}
```

核心优势：
- **统一事务入口**：通过一个 `*gorm.DB` 传入所有子 Repository，保证事务一致性
- **构造简单**：`NewRepositories(db)` 一行代码完成 12 个子 Repository 的初始化
- **依赖注入友好**：Service 层只需注入一个 `*Repositories`，减少构造参数爆炸
- **测试可替换**：传入测试数据库连接即可获得完整的 Repository 集合

---

## 2. `Repositories.Transaction` 如何保证所有子 Repository 共享同一个数据库事务？

```go
// internal/repository/repository.go:41-45
func (r *Repositories) Transaction(fn func(*Repositories) error) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        return fn(NewRepositories(tx))
    })
}
```

关键设计：传入 `tx`（事务连接）而非 `r.db`（原始连接），调用 `NewRepositories(tx)` 创建一组**全部使用同一个事务连接**的子 Repository。这样 `fn` 内部无论调用 `txRepos.Task.Create()` 还是 `txRepos.Summary.Create()`，都在同一个事务中，任一步骤失败回滚时所有操作都会撤销。

如果直接传入 `r.db`，子 Repository 将各自使用独立的数据库连接，无法保证原子性。

---

## 3. `UpdateStatusIf` 的条件更新模式解决了什么并发问题？

```go
// internal/repository/task.go:110-125
func (r *TaskRepository) UpdateStatusIf(id int64, allowedFrom []int8, status int8, errMsg string) (bool, error) {
    updates := map[string]interface{}{
        "status":    status,
        "error_msg": errMsg,
    }
    if errMsg != "" {
        updates["last_error_msg"] = errMsg
    }
    tx := r.db.Model(&model.VideoTask{}).
        Where("id = ? AND status IN ?", id, allowedFrom).
        Updates(updates)
    if tx.Error != nil {
        return false, tx.Error
    }
    return tx.RowsAffected > 0, nil
}
```

这是**乐观锁状态机**模式。`WHERE status IN (allowedFrom)` 确保只有当前状态在允许列表中时才执行更新。返回 `RowsAffected == 0` 表示状态已被其他请求抢先修改，调用方应停止当前操作。

典型场景：两个并发请求同时对同一个任务执行分析。只有一个请求的 `UpdateStatusIf` 会成功将状态从 `Pending` 改为 `Queued`，另一个返回 `false` 后直接返回"任务正在处理中"，避免重复投递。

---

## 4. `UpdateStatusAndStageIf` 相比 `UpdateStatusIf` 增加了什么逻辑？

```go
// internal/repository/task.go:127-148
func (r *TaskRepository) UpdateStatusAndStageIf(id int64, allowedFrom []int8, status int8, stage, errMsg string) (bool, error) {
    now := time.Now()
    updates := map[string]interface{}{
        "status":           status,
        "stage":            stage,
        "stage_started_at": &now,
        "error_msg":        errMsg,
    }
    if stage == model.TaskStageNone || status == model.TaskStatusCompleted ||
       status == model.TaskStatusFailed || status == model.TaskStatusDead {
        updates["stage_finished_at"] = &now
    }
    // ...
    tx := r.db.Model(&model.VideoTask{}).
        Where("id = ? AND status IN ?", id, allowedFrom).
        Updates(updates)
```

它额外维护了**阶段时间线**：进入新阶段时记录 `stage_started_at`，退出阶段（完成/失败/死信/回到 None）时记录 `stage_finished_at`。这为前端进度条和后端排障提供了精确的时间线数据。

---

## 5. AIProfile 的 `Create` 方法如何保证"默认配置互斥"？

```go
// internal/repository/ai_profile.go:19-29
func (r *AIProfileRepository) Create(profile *model.UserAIProfile) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        if profile.IsDefault {
            if err := tx.Model(&model.UserAIProfile{}).
                Where("user_id = ?", profile.UserID).
                Update("is_default", false).Error; err != nil {
                return err
            }
        }
        return tx.Create(profile).Error
    })
}
```

在同一个事务中，先将该用户所有现有配置的 `is_default` 设为 `false`，再插入新记录。事务保证这两个操作的原子性：如果插入失败，前面的批量更新也会回滚。`UpdateForUser` 方法（`ai_profile.go:68-104`）在更新时也做了相同处理，且排除了自身 ID（`WHERE id <> ?`）避免误清。

---

## 6. BM25 纯 Go 实现的核心算法是什么？为什么选择纯 Go 而非外部搜索引擎？

```go
// internal/repository/video_chunk.go:47-128
// 关键公式：
const k1 = 1.5
const b = 0.75

// IDF 计算 (video_chunk.go:106)
idf := math.Log(1 + (n-df+0.5)/(df+0.5))

// TF 归一化 + BM25 得分 (video_chunk.go:107-108)
denom := tf + k1*(1-b+b*(docLengths[i]/avgDocLength))
score += idf * ((tf * (k1 + 1)) / denom)
```

BM25 是经典的文档检索算法。VidLens 在 `video_chunk.go` 中用纯 Go 实现了完整的 BM25 检索：

1. 将所有 VideoChunk 加载到内存
2. 对每个 chunk 的 content 统计 term frequency
3. 计算 document frequency（包含该 term 的 chunk 数）
4. 按 BM25 公式打分，降序排列取 top-N

选择纯 Go 的原因：
- VidLens 的语料是单个视频的转录文本（通常几十到几百个 chunk），数据量小
- 引入 Elasticsearch 或 Meilisearch 会增加部署复杂度
- 纯 Go 实现零外部依赖，测试简单，适合嵌入式场景

---

## 7. `upsertDispatchState` 在任务投递中的作用是什么？如何实现幂等？

```go
// internal/repository/task_job.go 中的 upsertDispatchState 方法
// 核心逻辑：
return r.db.Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "task_id"}, {Name: "job_type"}},
    DoUpdates: clause.Assignments(updates),
}).Create(job).Error
```

`TaskJob` 的 `(task_id, job_type)` 是唯一索引（`model/task_job.go:18`）。`ON CONFLICT DO UPDATE` 语义：如果该任务类型已存在，更新其状态并清除错误字段；否则插入新记录。

两个公开方法的区别：
- `UpsertQueued`：`resetRetry=true`，重置 `retry_count=0`（新投递）
- `UpsertDispatching`：`resetRetry=false`，保留原 `retry_count`（状态转移）

这保证了消息队列投递失败后重试时不会产生重复的 Job 记录。

---

## 8. `RecordRetryableFailure` 和 `RecordTerminalFailure` 的区别是什么？

```go
// internal/repository/task.go:176-191 - 可重试失败
func (r *TaskRepository) RecordRetryableFailure(id int64, jobType, stage, errMsg string,
    retryCount, maxRetries int, nextRetryAt time.Time) error {
    updates := map[string]interface{}{
        "status":        model.TaskStatusFailed,
        "retry_count":   retryCount,
        "max_retries":   maxRetries,
        "next_retry_at": nextRetryAt,  // 设置下次重试时间
    }
    // ...
}

// internal/repository/task.go:193-209 - 终态失败
func (r *TaskRepository) RecordTerminalFailure(id int64, jobType, stage, errCode, errMsg string,
    retryCount, maxRetries int, status int8) error {
    updates := map[string]interface{}{
        "status":        status,       // 可能是 Failed 或 Dead
        "next_retry_at": nil,          // 清除重试时间
        "finished_at":   now,          // 标记结束
    }
    // ...
}
```

- `RecordRetryableFailure`：设置 `next_retry_at`，调度器会定期捞取到期任务重新投递
- `RecordTerminalFailure`：设置 `finished_at`，不再重试。超过 `max_retries` 的任务状态设为 `Dead`（死信），需要人工介入

---

## 9. `ClaimRetryDispatch` 如何防止多个调度实例重复认领？

```go
claimed, err := repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{
    TaskID: task.ID,
    JobType: task.LastJobType,
    ExpectedVersion: task.LeaseVersion,
    Now: now,
    LeaseUntil: now.Add(dispatchLease),
    Token: claimToken,
})
```

候选任务来自一次普通扫描，真正的所有权由事务性 claim 决定。Repository 会检查 `lease_version`、终态、job retry 上限和现有 lease，再把同一个 token/version 写入 `video_tasks` 与 `task_jobs`。并发实例使用同一旧 version 时，只有一个能提交有效 claim。

Kafka producer 返回错误后，`RestoreRetryDispatch` 还会校验 task/job 都仍属于该 token；如果 lease 已被新实例接管，它返回 `restored=false`，不会把新 owner 状态回滚。进程直接崩溃时则依靠 `lease_expires_at` 被后续扫描恢复。

---

## 10. Repository 层的测试策略是什么？

VidLens 的 Repository 测试文件包括：
- `task_test.go` / `task_job_test.go`
- `ai_profile_test.go`
- `video_chunk_test.go`
- `chat_test.go`
- `ai_call_log_test.go`

测试策略：

```go
// internal/repository/task_test.go:12-24
func newTestRepositories(t *testing.T) *Repositories {
    t.Helper()
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }
    if err := db.AutoMigrate(model.AllModels()...); err != nil {
        t.Fatalf("migrate test db: %v", err)
    }
    return NewRepositories(db)
}
```

1. **使用 SQLite 内存数据库**：`gorm.Open(sqlite.Open(":memory:"))` 创建临时数据库，执行 `model.Migrate(db)` 建表
2. **测试真实 SQL**：不 mock GORM，直接验证 SQL 逻辑（条件更新、事务、唯一约束等）
3. **并发安全测试**：对 `UpdateStatusIf` 等方法测试并发场景
4. **清理隔离**：每个测试函数结束后数据库自动销毁，无需清理

这种策略的优点是覆盖率高、接近生产行为；缺点是比 mock 慢，但在 Repository 层这是值得的权衡。
