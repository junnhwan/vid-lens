# VidLens Repository 层 - 源码走读

> 源码目录：`internal/repository/`，共 12 个实现文件 + 5 个测试文件。

---

## 1. 文件表

| 文件 | 行数 | 职责 |
|------|------|------|
| `repository.go` | 46 | `Repositories` 聚合体 + `Transaction` 事务入口 |
| `task.go` | 270 | `TaskRepository`：任务 CRUD + 条件状态更新 + 重试调度 |
| `task_job.go` | 211 | `TaskJobRepository`：作业幂等投递 + 生命周期管理 |
| `asset.go` | -- | `AssetRepository`：视频资产 MD5 去重 |
| `user.go` | -- | `UserRepository`：用户注册/查询 |
| `transcription.go` | -- | `TranscriptionRepository`：转录结果存储 |
| `transcription_chunk.go` | -- | `TranscriptionChunkRepository`：转录分片 |
| `summary.go` | -- | `SummaryRepository`：AI 摘要存储 |
| `ai_profile.go` | 116 | `AIProfileRepository`：AI 配置 CRUD + 默认值互斥 |
| `video_chunk.go` | 157 | `VideoChunkRepository`：视频分块 + BM25 纯 Go 检索 |
| `rag_index.go` | -- | `RAGIndexRepository`：RAG 索引状态管理 |
| `chat.go` | 90 | `ChatRepository`：会话 + 消息 CRUD |
| `ai_call_log.go` | 81 | `AICallLogRepository`：AI 调用日志 + 每日用量原子累加 |
| `task_test.go` | 169 | 任务测试：状态条件更新、资产共享、软删除 |
| `task_job_test.go` | 121 | 作业测试：生命周期追踪、重试元数据、重置 retry_count |
| `ai_profile_test.go` | 147 | AI 配置测试：单默认值、用户隔离、跨用户访问拒绝、CreatedAt 保留 |
| `video_chunk_test.go` | 81 | 分块测试：替换语义、BM25 排序验证 |
| `chat_test.go` | 64 | 聊天测试：会话创建、消息排序、用户隔离 |
| `ai_call_log_test.go` | -- | 调用日志测试 |

---

## 2. 核心结构体

### 2.1 聚合体

```
Repositories                   repository.go:6-20
  db                 *gorm.DB           // internal, not exposed
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
```

### 2.2 子 Repository

每个子 Repository 遵循统一模式：

```go
type XxxRepository struct {
    db *gorm.DB
}

func NewXxxRepository(db *gorm.DB) *XxxRepository {
    return &XxxRepository{db: db}
}
```

`db` 可以是原始连接，也可以是事务 `tx`——由 `NewRepositories` 决定。

### 2.3 BM25 搜索结果

```
VideoChunkSearchResult         video_chunk.go:16-20
  Chunk model.VideoChunk       // matched chunk
  Score float64                // BM25 score
  Rank  int                    // rank (1-based)
```

---

## 3. 关键函数实现

### 3.1 聚合体与事务 (`repository.go:22-45`)

```go
func NewRepositories(db *gorm.DB) *Repositories {
    return &Repositories{
        db:                 db,
        User:               NewUserRepository(db),
        Asset:              NewAssetRepository(db),
        Task:               NewTaskRepository(db),
        TaskJob:            NewTaskJobRepository(db),
        Transcription:      NewTranscriptionRepository(db),
        TranscriptionChunk: NewTranscriptionChunkRepository(db),
        Summary:            NewSummaryRepository(db),
        AIProfile:          NewAIProfileRepository(db),
        VideoChunk:         NewVideoChunkRepository(db),
        RAGIndex:           NewRAGIndexRepository(db),
        Chat:               NewChatRepository(db),
        AICallLog:          NewAICallLogRepository(db),
    }
}

func (r *Repositories) Transaction(fn func(*Repositories) error) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        return fn(NewRepositories(tx))
    })
}
```

`Transaction` 的核心思想：用事务 `tx` 创建一套全新的 `Repositories`，所有子 Repository 共享同一个事务。业务函数 `fn` 内的任何数据库操作都自动参与事务，返回 `nil` 自动提交，返回 `error` 自动回滚。

### 3.2 条件状态更新 (`task.go:108-125`)

```go
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

这是 VidLens 并发控制的核心原语。`WHERE status IN (allowedFrom)` 将"当前状态是否合法"的判断下推到数据库，利用行级锁保证原子性。返回 `bool` 让调用方区分"更新成功"和"条件不满足"。

变体 `UpdateStatusAndStageIf`（`task.go:127-148`）额外更新 `stage` 和时间戳字段，用于阶段转移。

### 3.3 重试元数据记录 (`task.go:176-209`)

两个方法分别记录可重试失败和终态失败：

```go
// retryable failure: set next_retry_at, scheduler will re-fetch
func (r *TaskRepository) RecordRetryableFailure(id int64, jobType, stage, errMsg string,
    retryCount, maxRetries int, nextRetryAt time.Time) error

// terminal failure: next_retry_at=nil, set finished_at, no more retries
func (r *TaskRepository) RecordTerminalFailure(id int64, jobType, stage, errCode, errMsg string,
    retryCount, maxRetries int, status int8) error
```

重试调度器的查询逻辑（`task.go:211-224`）：

```go
func (r *TaskRepository) FindDueRetryTasks(now time.Time, limit int) ([]model.VideoTask, error) {
    var tasks []model.VideoTask
    err := r.db.
        Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ? AND retry_count <= max_retries AND last_job_type <> ?",
            model.TaskStatusFailed, now, "").
        Order("next_retry_at ASC").
        Limit(limit).
        Find(&tasks).Error
    return tasks, err
}
```

`ClaimRetryDispatch`（`task_lease_dispatch.go`）在一个 PostgreSQL transaction 内同时认领 task 和对应 task_job。它校验候选行的 `lease_version`，然后写入同一个 dispatch token、lease kind 和过期时间：

```go
claimed, err := repos.ClaimRetryDispatch(repository.TaskDispatchClaimRequest{
    TaskID: task.ID, JobType: task.LastJobType,
    ExpectedVersion: task.LeaseVersion,
    Now: now, LeaseUntil: now.Add(dispatchLease), Token: claimToken,
})
```

多个 scheduler 即使扫描到同一候选，也只有版本和状态仍匹配的一方能成功；旧 owner 之后不能凭过期 token 覆盖新状态。

### 3.4 幂等作业投递 (`task_job.go:29-70`)

```go
func (r *TaskJobRepository) upsertDispatchState(task *model.VideoTask, jobType string,
    status int8, stage string, maxRetries int, resetRetry bool) error {

    // ... parameter validation and defaults ...

    job := &model.TaskJob{
        TaskID: task.ID, UserID: task.UserID, JobType: jobType,
        Status: status, Stage: stage, TraceID: task.TraceID,
        RetryCount: retryCount, MaxRetries: maxRetries,
    }
    updates := map[string]interface{}{
        "user_id": task.UserID, "status": status, "stage": stage,
        "trace_id": task.TraceID, "retry_count": retryCount,
        "max_retries": maxRetries, "next_retry_at": nil,
        "last_error_code": "", "last_error_msg": "",
        "started_at": nil, "finished_at": nil,
    }
    return r.db.Clauses(clause.OnConflict{
        Columns:   []clause.Column{{Name: "task_id"}, {Name: "job_type"}},
        DoUpdates: clause.Assignments(updates),
    }).Create(job).Error
}
```

`(task_id, job_type)` is a unique index. `ON CONFLICT DO UPDATE` guarantees:
- First enqueue: INSERT creates the record
- Duplicate enqueue: UPDATE overwrites status, clears error fields, preserves retry count (`resetRetry=false`) or resets to 0 (`resetRetry=true`)

Two public methods differ:

| Method | resetRetry | Scenario |
|--------|------------|----------|
| `UpsertQueued` | true | New task or retry enqueue, reset retry_count |
| `UpsertDispatching` | false | Status transition (e.g. Queued -> Running), preserve retry_count |

### 3.5 AIProfile 默认值互斥 (`ai_profile.go:19-30`)

```go
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

Strategy: clear all defaults for the user first, then insert new record. Transaction guarantees atomicity.

`UpdateForUser`（`ai_profile.go:68-104`）uses `First` + `Save` pattern, additionally ensuring:
1. **User isolation**: `WHERE user_id = ? AND id = ?` prevents cross-user modification
2. **CreatedAt preservation**: load `existing` first, assign fields, `Save` won't overwrite `CreatedAt`
3. **Default exclusivity**: `WHERE user_id = ? AND id <> ?` excludes self

### 3.6 BM25 纯 Go 实现 (`video_chunk.go:47-128`)

完整流程：

```
1. normalizeSearchTerms(terms)     -- trim, lowercase, dedup
2. ListByTaskID(userID, taskID)    -- load all chunks from DB
3. compute Term Frequency (TF)     -- occurrence count per term per chunk
4. compute Document Frequency (DF) -- number of chunks containing each term
5. compute avgDocLength            -- mean of all chunk content lengths
6. BM25 scoring                    -- weighted score per chunk
7. sort.SliceStable                -- score descending, tie by chunk_index ascending
8. truncate to limit               -- default 20, max 50
```

BM25 公式参数：

| Parameter | Value | Meaning |
|-----------|-------|---------|
| k1 | 1.5 | Term frequency saturation. Score growth slows after tf exceeds threshold |
| b | 0.75 | Document length normalization. b=1 means fully length-normalized |
| IDF | ln(1 + (N-df+0.5)/(df+0.5)) | Inverse document frequency. Rare terms get higher weight |

`normalizeSearchTerms`（`video_chunk.go:144-156`）确保搜索词一致性：

```go
func normalizeSearchTerms(terms []string) []string {
    seen := make(map[string]bool, len(terms))
    out := make([]string, 0, len(terms))
    for _, term := range terms {
        term = strings.ToLower(strings.TrimSpace(term))
        if term == "" || seen[term] { continue }
        seen[term] = true
        out = append(out, term)
    }
    return out
}
```

### 3.7 ChatRepository 级联删除 (`chat.go:74-89`)

```go
func (r *ChatRepository) DeleteByTaskID(taskID int64) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        var sessionIDs []int64
        if err := tx.Model(&model.ChatSession{}).
            Where("task_id = ?", taskID).
            Pluck("id", &sessionIDs).Error; err != nil {
            return err
        }
        if len(sessionIDs) > 0 {
            if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.ChatMessage{}).Error; err != nil {
                return err
            }
        }
        return tx.Where("task_id = ?", taskID).Delete(&model.ChatSession{}).Error
    })
}
```

Cascade delete order: query session IDs -> delete messages -> delete sessions. Transaction ensures consistency. `Pluck` extracts ID list, avoiding N+1 queries.

### 3.8 每日用量原子累加 (`ai_call_log.go:47-80`)

```go
func (r *AICallLogRepository) IncrementDailyUsage(userID int64, date, kind, status string,
    inputChars, outputChars, asrSeconds int) error {

    updates := map[string]interface{}{
        "input_chars":  gorm.Expr("input_chars + ?", inputChars),
        "output_chars": gorm.Expr("output_chars + ?", outputChars),
    }
    switch kind {
    case model.AICallKindASR:
        updates["asr_requests"] = gorm.Expr("asr_requests + ?", 1)
        updates["asr_seconds"] = gorm.Expr("asr_seconds + ?", asrSeconds)
    case model.AICallKindLLM:
        updates["llm_requests"] = gorm.Expr("llm_requests + ?", 1)
    case model.AICallKindEmbedding:
        updates["embedding_requests"] = gorm.Expr("embedding_requests + ?", 1)
    }

    return r.db.Clauses(clause.OnConflict{
        Columns:   []clause.Column{{Name: "user_id"}, {Name: "date"}},
        DoUpdates: clause.Assignments(updates),
    }).Create(&usage).Error
}
```

`gorm.Expr("column + ?", value)` generates `column = column + value`, achieving database-level atomic increment. `ON CONFLICT (user_id, date)` guarantees one record per user per day, multiple calls automatically accumulate.

---

## 4. 状态机与并发控制

### 4.1 任务状态枚举 (`model/task.go:12-18`)

```
Pending(0) --> Queued(1) --> Running(2) --> Completed(3)
                                    |
                                    +--> Failed(4) --[retry]--> Queued(1)
                                    |
                                    +--> Dead(5) [exceeds max_retries]
```

| Status | Value | Meaning |
|--------|-------|---------|
| Pending | 0 | File uploaded, waiting for analysis |
| Queued | 1 | Enqueued to message queue |
| Running | 2 | Consumer executing |
| Completed | 3 | All done |
| Failed | 4 | Failed (retryable or terminal) |
| Dead | 5 | Dead letter, requires manual intervention |

### 4.2 任务阶段 (`model/task.go:26-33`)

```
None --> Downloading --> Uploaded --> Transcribing --> Summarizing --> Indexing
```

Stage and status correspondence: a Running task can be in any stage. Stage transitions via `UpdateStatusAndStageIf`.

### 4.3 并发控制策略

```
Scenario: multiple consumers pick up the same task

Consumer A: UpdateStatusIf(id, [Pending], Running)  --> true  (success)
Consumer B: UpdateStatusIf(id, [Pending], Running)  --> false (RowsAffected=0, abandon)

Scenario: retry scheduler and normal consumer compete

Scheduler A: ClaimRetryDispatch(version=3, token=A) --> true
Scheduler B: ClaimRetryDispatch(version=3, token=B) --> false (version/lease changed)
Consumer:   UpdateStatusIf(id, [Pending], Running)    --> false (status already Running)
```

---

## 5. 事务使用模式

### 5.1 Repositories 级事务（跨 Repository）

```go
// repository.go:41-45
r.Transaction(func(repos *Repositories) error {
    repos.Task.Create(task)      // same transaction
    repos.Asset.Create(asset)    // same transaction
    repos.TaskJob.UpsertQueued() // same transaction
    return nil
})
```

### 5.2 单 Repository 内事务

```go
// ai_profile.go:19-30 -- mutually exclusive defaults
r.db.Transaction(func(tx *gorm.DB) error {
    tx.Model(...).Update("is_default", false)
    tx.Create(profile)
    return nil
})

// video_chunk.go:26-37 -- delete then insert
r.db.Transaction(func(tx *gorm.DB) error {
    tx.Where(...).Delete(...)
    tx.Create(&chunks)
    return nil
})

// chat.go:74-89 -- cascade delete
r.db.Transaction(func(tx *gorm.DB) error {
    tx.Pluck("id", &sessionIDs)
    tx.Where("session_id IN ?", sessionIDs).Delete(...)
    tx.Where("task_id = ?", taskID).Delete(...)
    return nil
})
```

---

## 6. 设计决策表

| Decision | Approach | Reason | Location |
|----------|----------|--------|----------|
| Aggregate pattern | `Repositories` holds 12 sub-Repos | Unified init, transaction passing, dependency narrowing | `repository.go:6-20` |
| Transaction passing | `NewRepositories(tx)` creates new instance | Avoid data race from mutating existing instance | `repository.go:41-45` |
| Conditional update | `WHERE status IN (allowedFrom)` + `RowsAffected` | Optimistic concurrency control, lock-free, high perf | `task.go:110-125` |
| Idempotent enqueue | `ON CONFLICT (task_id, job_type) DO UPDATE` | DB-level atomic upsert, eliminates TOCTOU race | `task_job.go:66-69` |
| Retry scheduling | `next_retry_at` + periodic query + conditional Claim | Exponential backoff, stateless scheduler, horizontally scalable | `task.go:211-241` |
| Default exclusivity | Transaction: UPDATE then CREATE | Guarantees at most one default, no DB feature dependency | `ai_profile.go:19-30` |
| BM25 implementation | Pure Go in-memory computation | No external deps, limited chunks, acceptable perf | `video_chunk.go:47-128` |
| Delete then insert | DELETE + INSERT in transaction | Full replacement semantics, upsert can't handle quantity changes | `video_chunk.go:26-37` |
| Atomic increment | `gorm.Expr("column + ?")` + ON CONFLICT | DB-level atomic operation, concurrent safe | `ai_call_log.go:47-80` |
| Cascade delete | Query ID list then batch delete | Avoid N+1, transaction ensures consistency | `chat.go:74-89` |
| Hard vs soft delete | Task soft delete, Chunk/Job hard delete | Task needs audit history, subsidiary data doesn't | `task.go` vs `video_chunk.go` |
| ensureJob defense | Check Job existence before recording failure | Consumer may skip Job creation, defensive programming | `task_job.go:180-210` |
| Test strategy | SQLite in-memory + glebarez/sqlite | Zero external deps, isolation, CI-friendly | `task_test.go:12-24` |

---

## 7. 测试覆盖

### 7.1 测试基础设施

```go
// task_test.go:12-24
func newTestRepositories(t *testing.T) *Repositories {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    db.AutoMigrate(model.AllModels()...)
    return NewRepositories(db)
}
```

All tests share the same `newTestRepositories` factory function. Each test function gets an independent in-memory database, no interference.

### 7.2 核心测试场景

| Test | Coverage |
|------|----------|
| `TestUpdateStatusIfOnlyTransitionsFromAllowedStatus` | Conditional update: disallowed transition returns false, allowed returns true |
| `TestVideoAssetCanBackMultipleUserTasksWithSameMD5` | One Asset can be referenced by multiple users' Tasks |
| `TestTaskRepositoryCountActiveByAssetIDIgnoresDeletedTasks` | Soft-deleted tasks excluded from CountActiveByAssetID |
| `TestTaskJobRepositoryTracksLifecycleAndRetryMetadata` | Full lifecycle: Queued -> Running -> Failed -> Requeued -> Completed |
| `TestTaskJobRepositoryUpsertQueuedResetsRetryCountOnInsert` | UpsertQueued resets retry_count to 0 |
| `TestAIProfileRepositoryKeepsSingleDefaultPerUser` | Consecutive default creation, only one remains |
| `TestAIProfileRepositoryFindDefaultByUserIDIsUserScoped` | Different users' defaults don't interfere |
| `TestAIProfileRepositoryUpdateRejectsCrossUserAccess` | Cross-user update returns error |
| `TestAIProfileRepositoryUpdatePreservesCreatedAt` | UpdateForUser doesn't overwrite CreatedAt |
| `TestVideoChunkRepositoryReplaceTaskChunks` | Delete then insert: old data fully replaced |
| `TestVideoChunkRepositorySearchByBM25RanksKeywordMatches` | BM25 ranking: multi-keyword match ranks higher |
| `TestChatRepositoryCreatesSessionAndListsMessages` | Session creation + messages sorted by ID ascending |
| `TestChatRepositoryFindSessionIsUserScoped` | Cross-user query returns nil |

---

## 8. 依赖关系

```
internal/repository/
  repository.go          <- aggregates 12 sub-Repos + Transaction
  task.go                <- depends on model.VideoTask, model.TaskStatus*
  task_job.go            <- depends on model.TaskJob, model.VideoTask, gorm/clause
  ai_profile.go          <- depends on model.UserAIProfile
  video_chunk.go         <- depends on model.VideoChunk, math, sort, strings
  chat.go                <- depends on model.ChatSession, model.ChatMessage
  ai_call_log.go         <- depends on model.AICallLog, model.UserUsageDaily, gorm/clause
  asset.go               <- depends on model.VideoAsset
  user.go                <- depends on model.User
  transcription.go       <- depends on model.VideoTranscription
  transcription_chunk.go <- depends on model.VideoTranscriptionChunk
  summary.go             <- depends on model.AISummary
  rag_index.go           <- depends on model.VideoRAGIndex

internal/model/
  task.go                <- defines TaskStatus* / TaskStage* constants + VideoTask GORM model
  task_job.go            <- defines TaskJobType* constants + TaskJob GORM model
  ai_profile.go          <- defines UserAIProfile GORM model
  video_chunk.go         <- defines VideoChunk GORM model
  model.go               <- AllModels() + Migrate() backward compat for old indexes

internal/service/
  media_service.go       <- depends on Repositories.Transaction, Task, Asset, TaskJob
  chat_service.go        <- depends on Repositories.Chat, VideoChunk
  ai_profile_service.go  <- depends on Repositories.AIProfile
```

---

## 9. 扩展点

| Extension | Change location | Effort |
|-----------|-----------------|--------|
| Add new sub-Repository | New `xxx.go` + `Repositories` field + `NewRepositories` init | Small |
| Unified pagination | Extract `Paginate(query, page, pageSize)` utility | Small |
| BM25 Chinese tokenization | `normalizeSearchTerms` integrate tokenizer (e.g. gojieba) | Small |
| BM25 synonym expansion | Add synonym table query in term expansion phase | Medium |
| Batch chunk insert | `ReplaceTaskChunks` split by 100 rows per INSERT | Small |
| Repository interfaces | Define interface per sub-Repo for mock testing | Medium |
| Read/write splitting | `Repositories` holds `db` (write) + `rodb` (read), read methods use `rodb` | Medium |
| Audit log | GORM Plugin intercepts all writes, records to audit_log table | Medium |
| Sharding | `Repositories` routes to different `*gorm.DB` by user_id | Large |
