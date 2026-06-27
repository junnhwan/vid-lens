# VidLens 数据模型设计 - 面试题

## 交互式状态机

> 点击状态节点查看转换关系

<TaskStateMachine />

---

## 题目 1: VideoTask 的状态机设计了 6 种状态和 5 种 Stage，它们之间的关系是什么？为什么要分开设计？

### 参考答案

VideoTask 使用 `Status` 字段表示任务的宏观生命周期，`Stage` 字段表示任务内部的微观处理阶段。两者正交组合，形成完整的任务状态描述。

```go
// internal/model/task.go:11-18
const (
    TaskStatusPending   int8 = 0 // 待处理（文件已上传，等待分析）
    TaskStatusQueued    int8 = 1 // 排队中（已投递消息队列）
    TaskStatusRunning   int8 = 2 // 处理中（消费者正在执行）
    TaskStatusCompleted int8 = 3 // 已完成
    TaskStatusFailed    int8 = 4 // 失败
    TaskStatusDead      int8 = 5 // 死信（超过最大重试次数，需人工或用户重新触发）
)
```

```go
// internal/model/task.go:26-33
const (
    TaskStageNone         = "none"
    TaskStageDownloading  = "downloading"
    TaskStageUploaded     = "uploaded"
    TaskStageTranscribing = "transcribing"
    TaskStageSummarizing  = "summarizing"
    TaskStageIndexing     = "indexing"
)
```

状态流转示例：
- 用户上传文件后：`Status=Pending, Stage=uploaded`
- 提交到 Kafka 后：`Status=Queued, Stage=transcribing`
- 消费者开始处理：`Status=Running, Stage=transcribing`
- 转录完成进入总结：`Status=Running, Stage=summarizing`
- 全部完成：`Status=Completed, Stage=none`

分开设计的原因：
1. **Status 是面向调度器的**：调度器只关心"这个任务能不能被捞起来处理"，不需要知道它具体在做什么
2. **Stage 是面向用户的**：前端展示进度条时需要知道"正在转录"还是"正在总结"
3. **Stage 是面向开发者的**：排查问题时，Status=Failed 只知道失败了，Stage=downloading 才知道是下载阶段出了问题

### 追问链

**追问 1.1: Status 用 int8 而 Stage 用 string，为什么不统一用一种类型？**

Status 用 int8 是为了数据库索引效率和状态机的严格约束。int8 只有 6 个合法值，可以用 `WHERE status IN (0, 1, 2)` 高效查询。Stage 用 string 是因为阶段是开放集合，未来可能新增 `StageClassifying`、`StageTranslating` 等阶段，不需要修改数据库 schema。string 类型的可读性也更好，日志中直接看到 `"downloading"` 而不是数字。

**追问 1.2: TaskStageNone 在什么场景下使用？**

`TaskStageNone = "none"` 是 Stage 的初始值和终态值。任务刚创建时 Stage 为 "none"，全部处理完成后 Stage 回到 "none"。在 `UpdateStatusAndStage` 方法中（`internal/repository/task.go:99`），当 `stage == model.TaskStageNone` 时会自动设置 `stage_finished_at`，用于计算整个任务的耗时。

**追问 1.3: 为什么需要 TaskStatusDead 状态，它和 TaskStatusFailed 有什么区别？**

```go
// internal/model/task.go:17
TaskStatusDead int8 = 5 // 死信（超过最大重试次数，需人工或用户重新触发）
```

Failed 是暂时性失败，系统会自动重试（`retry_count < max_retries` 时）。Dead 是终态失败，表示重试次数用尽，系统不再自动处理。在 `RecordTerminalFailure` 方法中（`internal/repository/task.go:193-209`），当重试次数超过 `maxRetries` 时，会将状态设为 Dead 并记录 `finished_at`。用户可以在前端看到 Dead 状态的任务并手动重新提交。

---

## 题目 2: VideoTask 和 VideoAsset 是什么关系？为什么要拆分成两张表？

### 参考答案

VideoAsset 表示内容级的视频文件资产，VideoTask 表示用户发起的一次处理任务。两者是多对一关系：多个 VideoTask 可以指向同一个 VideoAsset。

```go
// internal/model/asset.go:11-20
type VideoAsset struct {
    ID          int64          `gorm:"primaryKey;autoIncrement" json:"id"`
    FileMD5     string         `gorm:"type:char(32);uniqueIndex;not null" json:"file_md5"`
    ObjectName  string         `gorm:"type:varchar(500);not null" json:"object_name"`
    FileSize    int64          `gorm:"default:0" json:"file_size"`
    ContentType string         `gorm:"type:varchar(100)" json:"content_type"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}
```

```go
// internal/model/task.go:43
AssetID *int64 `gorm:"index" json:"asset_id"` // 可为空，URL 下载任务在下载完成前没有 Asset
```

拆分的原因：
1. **内容级去重**：同一份视频文件（相同 MD5）只需要在 MinIO 存储一份，VideoAsset 的 `FileMD5` 是唯一索引
2. **秒传实现**：上传时先算 MD5，如果 VideoAsset 已存在，直接复用，跳过文件上传
3. **关注点分离**：VideoAsset 关心"文件在哪"（ObjectName、FileSize），VideoTask 关心"谁要处理什么"（UserID、Status）
4. **AssetID 可为空**：URL 下载任务在下载完成前还没有 Asset，`AssetID` 为 nil，下载完成后才关联

### 追问链

**追问 2.1: FileMD5 用 char(32) 存储，为什么不用 binary(16) 节省空间？**

MD5 的 128 位哈希用 binary(16) 存储确实更紧凑，但 char(32) 的十六进制字符串可读性更好，日志排查时可以直接复制比对。VidLens 的 MD5 主要用于去重判断和消息队列路由（Kafka Key），不需要做范围查询，32 字节的存储开销可以接受。

**追问 2.2: VideoAsset 也有 DeletedAt 字段，软删除后引用它的 VideoTask 怎么办？**

VideoAsset 的软删除是防御性设计。正常流程中，只有当没有任何 VideoTask 引用该 Asset 时才能删除。`CountActiveByAssetID` 方法（`internal/repository/task.go:257-264`）用于检查引用计数。如果 Asset 被软删除但仍有 Task 引用，GORM 的默认查询会因为 `DeletedAt IS NOT NULL` 而查不到 Asset，导致 Task 的 Asset 关联为 nil。这是一个需要在业务层保证的约束。

**追问 2.3: ObjectName 存储的是什么？为什么不用原始文件名？**

ObjectName 是 MinIO 中的对象路径，格式类似 `videos/2024/01/15/abc123.mp4`。原始文件名（Filename）可能包含中文、特殊字符，不适合作为对象存储的 key。ObjectName 由系统生成，保证唯一性和路径规范性。

---

## 题目 3: TaskJob 的设计目的是什么？为什么有了 VideoTask 还需要 TaskJob？

### 参考答案

TaskJob 记录 VideoTask 下每个子任务（download、transcribe、analyze、rag_index）的独立执行状态，实现细粒度的任务追踪。

```go
// internal/model/task_job.go:5-10
const (
    TaskJobTypeAnalyze    = "analyze"
    TaskJobTypeTranscribe = "transcribe"
    TaskJobTypeDownload   = "download"
    TaskJobTypeRAGIndex   = "rag_index"
)
```

```go
// internal/model/task_job.go:15-32
type TaskJob struct {
    ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID        int64      `gorm:"not null;uniqueIndex:uk_task_jobs_task_type;index" json:"task_id"`
    UserID        int64      `gorm:"not null;index" json:"user_id"`
    JobType       string     `gorm:"type:varchar(30);not null;uniqueIndex:uk_task_jobs_task_type;index" json:"job_type"`
    Status        int8       `gorm:"type:tinyint;default:0;index" json:"status"`
    Stage         string     `gorm:"type:varchar(50);default:'none';index" json:"stage"`
    TraceID       string     `gorm:"type:varchar(64);index" json:"trace_id"`
    RetryCount    int        `gorm:"default:0" json:"retry_count"`
    MaxRetries    int        `gorm:"default:3" json:"max_retries"`
    NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
    LastErrorCode string     `gorm:"type:varchar(100)" json:"last_error_code"`
    LastErrorMsg  string     `gorm:"type:varchar(500)" json:"last_error_msg"`
    StartedAt     *time.Time `json:"started_at,omitempty"`
    FinishedAt    *time.Time `json:"finished_at,omitempty"`
    CreatedAt     time.Time  `json:"created_at"`
    UpdatedAt     time.Time  `json:"updated_at"`
}
```

双表设计的原因：
1. **VideoTask 是兼容状态源**：前端和外部 API 只看 VideoTask 的 Status，保持向后兼容
2. **TaskJob 是可观测性来源**：运维和开发者需要知道每个阶段的独立状态、耗时、错误信息
3. **独立重试**：transcribe 失败可以只重试 transcribe，不需要重试 download
4. **唯一约束**：`(task_id, job_type)` 唯一索引保证每个任务的每种 Job 只有一条记录

### 追问链

**追问 3.1: TaskJob 的 Status 复用了 VideoTask 的状态常量（0-5），这样设计合理吗？**

```go
// internal/repository/task_job.go:92-105
func (r *TaskJobRepository) MarkRunning(taskID int64, jobType, stage string) error {
    now := time.Now()
    return r.db.Model(&model.TaskJob{}).
        Where("task_id = ? AND job_type = ?", taskID, jobType).
        Updates(map[string]interface{}{
            "status": model.TaskStatusRunning,
            // ...
        }).Error
}
```

复用状态常量是合理的，因为 TaskJob 的生命周期和 VideoTask 完全一致（Pending -> Queued -> Running -> Completed/Failed/Dead）。分开定义常量反而会增加维护成本。但 TaskJob 不使用 Stage 来细分阶段，因为每个 Job 本身就是某个阶段的执行单元。

**追问 3.2: UpsertQueued 使用了 GORM 的 OnConflict，它的作用是什么？**

```go
// internal/repository/task_job.go:66-70
return r.db.Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "task_id"}, {Name: "job_type"}},
    DoUpdates: clause.Assignments(updates),
}).Create(job).Error
```

这是 MySQL 的 `INSERT ... ON DUPLICATE KEY UPDATE` 语法。当 `(task_id, job_type)` 已存在时，更新现有记录而不是报错。这实现了"幂等投递"：同一个任务的同一个 Job 类型，无论投递多少次，都只有一条记录。消息队列的 at-least-once 投递语义下，这种 upsert 保证了数据一致性。

**追问 3.3: ensureJob 方法的作用是什么？什么场景下会触发？**

```go
// internal/repository/task_job.go:180-210
func (r *TaskJobRepository) ensureJob(taskID int64, jobType, stage string, maxRetries int) error {
    var existing model.TaskJob
    err := r.db.Where("task_id = ? AND job_type = ?", taskID, jobType).First(&existing).Error
    if err == nil {
        return nil // 已存在，直接返回
    }
    // 不存在则创建...
}
```

`ensureJob` 是防御性编程。`RecordRetryableFailure` 和 `RecordTerminalFailure` 都会先调用 `ensureJob`，确保 Job 记录存在后再更新。在异常场景下（如进程重启后 Job 记录丢失），`ensureJob` 会从 VideoTask 重建 Job 记录。

---

## 题目 4: VideoTranscription 为什么要从 VideoTask 中拆分出来？这种垂直拆分的适用场景是什么？

### 参考答案

VideoTranscription 存储视频的逐字稿全文，使用 `longtext` 类型，可能达到数万字。将其从 VideoTask 中拆分出来是典型的垂直拆分策略。

```go
// internal/model/transcription.go:10-16
type VideoTranscription struct {
    ID      int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID  int64     `gorm:"uniqueIndex;not null" json:"task_id"`
    Content string    `gorm:"type:longtext" json:"content"` // 转录全文
    Words   int       `gorm:"default:0" json:"words"`       // 字数统计
    CreatedAt time.Time `json:"created_at"`
}
```

垂直拆分的原因：
1. **查询性能**：用户刷历史列表时，`ListByUserID` 只查 VideoTask 的小字段（`internal/repository/task.go:70`），不加载 Content
2. **避免宽表**：如果 Content 放在 VideoTask 中，每行数据都会携带一个可能很大的 longtext 字段
3. **按需加载**：只有用户点击某个任务查看详情时，才通过 `FindByIDWithDetail` 预加载 Transcription

```go
// internal/repository/task.go:35-47
func (r *TaskRepository) FindByIDWithDetail(id int64) (*model.VideoTask, error) {
    var task model.VideoTask
    err := r.db.
        Preload("Asset").
        Preload("Transcription").
        Preload("Summary").
        Preload("Jobs").
        First(&task, id).Error
    // ...
}
```

### 追问链

**追问 4.1: TaskID 上有 uniqueIndex，为什么不是普通 index？**

`uniqueIndex` 保证一个 VideoTask 只有一条 Transcription 记录（1:1 关系）。如果用普通 index，理论上可能出现重复数据。uniqueIndex 同时也是查询优化器的索引，`WHERE task_id = ?` 的查询效率和普通 index 一样。

**追问 4.2: 如果转录内容特别大（比如 2 小时的视频），longtext 够用吗？**

MySQL 的 longtext 最大 4GB，对于逐字稿绰绰有余。2 小时的视频，假设每秒 3 个字，总共约 21600 字，UTF-8 编码约 64KB。真正的瓶颈不在存储，而在网络传输——前端请求详情时需要下载整个 Content。如果未来需要优化，可以考虑分段返回或流式传输。

**追问 4.3: VideoTranscriptionChunk 表的作用是什么？和 VideoTranscription 是什么关系？**

```go
// internal/model/transcription_chunk.go:12-26
type VideoTranscriptionChunk struct {
    ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID      int64     `gorm:"index;uniqueIndex:idx_task_transcription_chunk;not null" json:"task_id"`
    ChunkIndex  int       `gorm:"uniqueIndex:idx_task_transcription_chunk;not null" json:"chunk_index"`
    AudioObject string    `gorm:"type:varchar(500)" json:"audio_object"`
    StartSecond int       `gorm:"default:0" json:"start_second"`
    EndSecond   int       `gorm:"default:0" json:"end_second"`
    Status      string    `gorm:"type:varchar(30);index;not null" json:"status"`
    Content     string    `gorm:"type:longtext" json:"content"`
    // ...
}
```

VideoTranscriptionChunk 是分段转录的中间状态表。长视频会被切分为多个音频片段（Chunk），每个 Chunk 独立调用 ASR 服务转录。所有 Chunk 转录完成后，合并为 VideoTranscription 的完整 Content。这种设计支持：1) 并行转录多个片段；2) 单个片段失败时只重试该片段；3) 展示带时间戳的分段结果。

---

## 题目 5: UserAIProfile 的 API Key 为什么使用 Ciphertext 后缀字段？`json:"-"` 标签的作用是什么？

### 参考答案

UserAIProfile 存储用户自定义的 AI 提供商配置，API Key 字段使用加密后缀命名并配合 `json:"-"` 标签，实现"加密存储 + 不序列化"的双重安全策略。

```go
// internal/model/ai_profile.go:7-27
type UserAIProfile struct {
    ID                        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID                    int64     `gorm:"index;not null" json:"user_id"`
    Name                      string    `gorm:"type:varchar(100);not null" json:"name"`
    LLMAPIKeyCiphertext       string    `gorm:"type:text;not null" json:"-"`  // 加密后存储，不序列化
    ASRAPIKeyCiphertext       string    `gorm:"type:text;not null" json:"-"`
    EmbeddingAPIKeyCiphertext string    `gorm:"type:text;not null" json:"-"`
    // ... 其他字段
}
```

设计要点：
1. **Ciphertext 后缀**：明确标识字段存储的是密文，而非明文，避免开发者误用
2. **`json:"-"` 标签**：Go 的 `encoding/json` 序列化时会完全忽略该字段，API 响应中永远不会出现 API Key
3. **`gorm:"type:text"`**：加密后的 base64 字符串可能较长，使用 text 类型而非 varchar

```go
// internal/pkg/secret/crypto.go:47-58
func (c *Codec) Encrypt(plaintext string) (string, error) {
    nonce := make([]byte, c.aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }
    sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
    payload := make([]byte, 0, len(nonce)+len(sealed))
    payload = append(payload, nonce...)
    payload = append(payload, sealed...)
    return base64.StdEncoding.EncodeToString(payload), nil
}
```

### 追问链

**追问 5.1: 为什么不使用 `json:"llm_api_key_ciphertext"` 而是 `json:"-"`？**

即使字段名带 Ciphertext，如果暴露在 API 响应中，攻击者仍然可以获取密文。虽然没有密钥无法解密，但遵循纵深防御原则，API 响应中不应该出现任何密钥相关数据。`json:"-"` 彻底消除泄露风险。

**追问 5.2: 如果用户需要查看自己配置的 API Key，前端怎么展示？**

使用 `MaskAPIKey` 函数脱敏展示（`internal/pkg/secret/crypto.go:79-87`），显示为 `sk-****xyz` 格式。如果用户需要修改 API Key，前端会要求重新输入完整 Key，后端加密后覆盖存储。

**追问 5.3: UserAIProfile 支持多个配置，IsDefault 字段是怎么使用的？**

`IsDefault` 字段标记用户的默认 AI 配置。在聊天和 RAG 场景中，Handler 通过 `profileSvc.GetDefaultAIProfile(userID)` 获取 `IsDefault=true` 的配置。一个用户可以有多个 Profile（比如一个用 SiliconFlow，一个用 OpenAI），但只有一个默认配置。`IsDefault` 上有索引，查询效率有保证。

---

## 题目 6: VideoChunk 的联合唯一索引 `idx_task_chunk_model` 包含三个字段，这样的设计解决了什么问题？

### 参考答案

VideoChunk 存储 RAG 场景下的文本分块和向量 ID，联合唯一索引 `(task_id, chunk_index, embedding_model)` 保证同一个任务、同一个分块、同一个模型下只有一条记录。

```go
// internal/model/video_chunk.go:5-18
type VideoChunk struct {
    ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID         int64     `gorm:"index;not null" json:"user_id"`
    TaskID         int64     `gorm:"index;uniqueIndex:idx_task_chunk_model;not null" json:"task_id"`
    ChunkIndex     int       `gorm:"uniqueIndex:idx_task_chunk_model;not null" json:"chunk_index"`
    Content        string    `gorm:"type:text;not null" json:"content"`
    ContentHash    string    `gorm:"type:char(32);not null;index" json:"content_hash"`
    TokenCount     int       `gorm:"default:0" json:"token_count"`
    EmbeddingModel string    `gorm:"type:varchar(100);uniqueIndex:idx_task_chunk_model;not null" json:"embedding_model"`
    EmbeddingDim   int       `gorm:"not null" json:"embedding_dim"`
    VectorID       string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"vector_id"`
    // ...
}
```

三字段联合索引解决的问题：
1. **同一个任务的同一个分块，换模型可以重新生成**：用户从 `text-embedding-ada-002` 切换到 `text-embedding-3-small`，旧的 chunk 记录不会冲突
2. **幂等写入**：重复构建索引时，`(task_id, chunk_index, embedding_model)` 已存在则更新，不会产生重复数据
3. **ContentHash 用于去重**：如果分块内容没变但模型换了，可以通过 ContentHash 跳过不必要的向量化计算

### 追问链

**追问 6.1: VectorID 存储的是什么？为什么需要 uniqueIndex？**

VectorID 是向量数据库（Milvus）中对应向量的唯一标识。查询流程是：先从 MySQL 查出 VideoChunk 得到 VectorID，再用 VectorID 去 Milvus 查询相似向量。VectorID 的 uniqueIndex 保证一个 chunk 在 Milvus 中只有一个对应的向量，避免重复索引。

**追问 6.2: ContentHash 和 FileMD5 有什么区别？**

FileMD5 是整个视频文件的哈希，用于文件级去重（秒传）。ContentHash 是单个文本分块内容的哈希，用于 chunk 级去重——如果转录内容没变但用户换了 embedding 模型，可以通过 ContentHash 判断是否需要重新向量化。

**追问 6.3: 为什么 EmbeddingDim 要单独存储，而不是从 EmbeddingModel 推导？**

不同模型的向量维度不同（如 ada-002 是 1536 维，text-embedding-3-small 可以配置为 512/1536 维）。存储 EmbeddingDim 有两个用途：1) 创建 Milvus Collection 时需要指定维度；2) 检索时验证查询向量和存储向量的维度一致性。

---

## 题目 7: VideoTask 的重试机制是如何设计的？RetryCount、MaxRetries、NextRetryAt 三个字段如何协作？

### 参考答案

VideoTask 的重试机制基于三个字段实现指数退避重试：`RetryCount` 记录已重试次数，`MaxRetries` 记录最大重试次数，`NextRetryAt` 记录下次重试时间。

```go
// internal/model/task.go:53-55
RetryCount  int        `gorm:"default:0" json:"retry_count"`
MaxRetries  int        `gorm:"default:3" json:"max_retries"`
NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
```

重试流程：
1. 任务失败时，`RecordRetryableFailure` 更新 `retry_count++`、`next_retry_at = now + backoff`
2. `FindDueRetryTasks` 查询 `status=Failed AND next_retry_at <= now AND retry_count <= max_retries`
3. `ClaimRetryTask` 通过乐观锁抢占任务，重新投递到消息队列

```go
// internal/repository/task.go:211-224
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

```go
// internal/repository/task.go:226-241
func (r *TaskRepository) ClaimRetryTask(id int64, now time.Time, status int8, stage string) (bool, error) {
    tx := r.db.Model(&model.VideoTask{}).
        Where("id = ? AND status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?", id, model.TaskStatusFailed, now).
        Updates(updates)
    // RowsAffected == 0 表示被其他进程抢占
    return tx.RowsAffected > 0, nil
}
```

### 追问链

**追问 7.1: ClaimRetryTask 为什么用 `WHERE status = Failed` 而不是用版本号？**

使用状态字段作为乐观锁比版本号更简洁。`ClaimRetryTask` 的 WHERE 条件包含 `status = Failed AND next_retry_at <= now`，如果另一个进程已经抢占了这个任务（把 status 改为 Queued/Running），WHERE 条件不匹配，`RowsAffected` 为 0，当前进程就知道抢占失败了。这避免了引入额外的 version 字段。

**追问 7.2: `last_job_type <> ''` 这个条件的作用是什么？**

`FindDueRetryTasks` 中的 `last_job_type <> ''` 排除了没有执行过任何 Job 的任务。如果一个任务从未被处理过（last_job_type 为空），它不应该进入重试队列，而是应该通过正常的调度流程处理。

**追问 7.3: RestoreAfterDispatchFailure 方法的使用场景是什么？**

```go
// internal/repository/task.go:243-255
func (r *TaskRepository) RestoreRetryAfterDispatchFailure(id int64, stage, errMsg string, nextRetryAt time.Time) error {
    // 将任务状态恢复为 Failed，保持 next_retry_at 不变
}
```

当重试调度器成功抢占任务（ClaimRetryTask 返回 true），但在投递消息到 Kafka 时失败了，需要把任务状态恢复为 Failed，让下一轮调度器可以重新捞起。这是一种"先改状态再投递"的事务补偿模式。

---

## 题目 8: TaskJob 的 UpsertQueued 和 UpsertDispatching 有什么区别？resetRetry 参数的作用是什么？

### 参考答案

两个方法都调用 `upsertDispatchState`，区别在于是否重置重试计数。

```go
// internal/repository/task_job.go:21-23
func (r *TaskJobRepository) UpsertQueued(task *model.VideoTask, jobType, stage string, maxRetries int) error {
    return r.upsertDispatchState(task, jobType, model.TaskStatusQueued, stage, maxRetries, true)
}

// internal/repository/task_job.go:25-27
func (r *TaskJobRepository) UpsertDispatching(task *model.VideoTask, jobType string, status int8, stage string) error {
    return r.upsertDispatchState(task, jobType, status, stage, task.MaxRetries, false)
}
```

```go
// internal/repository/task_job.go:29-70
func (r *TaskJobRepository) upsertDispatchState(task *model.VideoTask, jobType string, status int8, stage string, maxRetries int, resetRetry bool) error {
    retryCount := task.RetryCount
    if resetRetry {
        retryCount = 0  // 首次投递，重置重试计数
    }
    // ...
}
```

区别分析：
- **UpsertQueued**（resetRetry=true）：首次投递时调用，重置 `retry_count=0`，表示这是一个全新的 Job
- **UpsertDispatching**（resetRetry=false）：重试调度时调用，保留 `retry_count`，延续之前的重试历史

### 追问链

**追问 8.1: upsertDispatchState 中的 updates map 包含 `started_at: nil` 和 `finished_at: nil`，为什么？**

```go
// internal/repository/task_job.go:53-65
updates := map[string]interface{}{
    "status":          status,
    "stage":           stage,
    "started_at":      nil,  // 重置为未开始
    "finished_at":     nil,  // 重置为未完成
    "last_error_code": "",
    "last_error_msg":  "",
    // ...
}
```

每次投递/重试都是一个新的执行周期，需要清空上一次的执行时间戳和错误信息。这保证了 Job 记录反映的是当前这次执行的状态，而不是历史累积。

**追问 8.2: 如果 maxRetries <= 0，为什么要 fallback 到 3？**

```go
// internal/repository/task_job.go:33-38
if maxRetries <= 0 {
    maxRetries = task.MaxRetries
}
if maxRetries <= 0 {
    maxRetries = 3
}
```

两层 fallback：先取调用方传入的值，再取 Task 上的值，最后兜底为 3。这防止了因为配置遗漏导致任务永远不重试。硬编码 3 是一个经验值——网络临时故障通常 3 次重试可以恢复。

**追问 8.3: TaskJob 没有 DeletedAt 字段，为什么不用软删除？**

TaskJob 的生命周期完全由 VideoTask 驱动。当 VideoTask 被软删除时，TaskJob 应该被物理删除（`DeleteByTaskID`，`internal/repository/task_job.go:176-178`）。TaskJob 没有独立的业务价值，不需要保留历史记录。

---

## 题目 9: model.go 中的 Migrate 函数为什么要做索引兼容性处理？这段代码解决了什么问题？

### 参考答案

Migrate 函数在 AutoMigrate 之后，额外处理了 `video_tasks.file_md5` 索引的兼容性问题。

```go
// internal/model/model.go:26-43
func Migrate(db *gorm.DB) error {
    if err := db.AutoMigrate(AllModels()...); err != nil {
        return err
    }

    // 旧版本 file_md5 是唯一索引，新版本改为普通索引
    if db.Migrator().HasIndex(&VideoTask{}, "idx_file_md5") {
        if err := db.Migrator().DropIndex(&VideoTask{}, "idx_file_md5"); err != nil {
            return err
        }
    }
    if !db.Migrator().HasIndex(&VideoTask{}, "idx_video_tasks_file_md5") {
        if err := db.Migrator().CreateIndex(&VideoTask{}, "FileMD5"); err != nil {
            return err
        }
    }

    return nil
}
```

这段代码解决的问题：
1. **索引类型变更**：旧版本 `file_md5` 是 uniqueIndex（保证任务级去重），新版本改为普通 index（去重逻辑移到 VideoAsset 层）
2. **AutoMigrate 不会修改已有索引**：GORM 的 AutoMigrate 只会创建缺失的索引，不会删除或修改已有索引
3. **索引命名不一致**：旧索引名是 `idx_file_md5`，GORM 生成的新索引名是 `idx_video_tasks_file_md5`，需要先删旧的再建新的

### 追问链

**追问 9.1: 为什么要把 file_md5 从唯一索引改为普通索引？**

旧设计中，VideoTask 的 file_md5 是唯一索引，意味着同一个 MD5 只能有一个任务。新设计中，去重逻辑移到 VideoAsset 层（asset.go:13 的 `FileMD5` 唯一索引），多个用户上传相同文件可以创建不同的 VideoTask，但共享同一个 VideoAsset。这支持了"多用户共享同一份视频资产"的业务场景。

**追问 9.2: AllModels 返回 14 个模型，为什么不让数据库自己管理迁移顺序？**

```go
// internal/model/model.go:6-23
func AllModels() []interface{} {
    return []interface{}{
        &User{}, &VideoAsset{}, &VideoTask{}, &TaskJob{},
        &VideoTranscription{}, &VideoTranscriptionChunk{}, &AISummary{},
        &UserAIProfile{}, &VideoChunk{}, &VideoRAGIndex{},
        &ChatSession{}, &ChatMessage{}, &AICallLog{}, &UserUsageDaily{},
    }
}
```

GORM 的 AutoMigrate 会自动处理外键依赖顺序。但 AllModels 的顺序仍然重要：User 必须在 VideoTask 之前创建（因为 VideoTask 引用 User）。虽然 AutoMigrate 会重试失败的表，但显式排列顺序可以减少不必要的重试。

**追问 9.3: 生产环境中可以直接用 AutoMigrate 吗？**

不建议。AutoMigrate 只支持安全操作（创建表、创建索引、添加列），不支持删除列、修改列类型等破坏性变更。生产环境应该使用专业的迁移工具（如 golang-migrate/migrate）管理版本化的 SQL 迁移脚本。VidLens 使用 AutoMigrate 是因为在开发阶段 schema 还在频繁变化。

---

## 题目 10: 从整个数据模型来看，VidLens 的设计体现了哪些架构原则？有哪些设计取舍？

### 参考答案

VidLens 的数据模型体现了以下架构原则：

**1. 关注点分离**

```go
// 内容层：VideoAsset 关心文件存储
// 任务层：VideoTask 关心任务调度
// 子任务层：TaskJob 关心执行细节
// 结果层：VideoTranscription + AISummary 关心输出
// RAG 层：VideoChunk + VideoRAGIndex 关心向量检索
// 交互层：ChatSession + ChatMessage 关心用户对话
```

**2. 垂直拆分避免宽表**

大字段（Transcription.Content、AISummary.Content）独立成表，主表只存轻量级元数据。用户列表查询不会被大字段拖慢。

**3. 内容级去重**

VideoAsset 的 `FileMD5` 唯一索引实现文件去重，VideoTask 通过 `AssetID` 外键关联。同一份文件只需在 MinIO 存储一份。

**4. 乐观并发控制**

```go
// internal/repository/task.go:110-125
func (r *TaskRepository) UpdateStatusIf(id int64, allowedFrom []int8, status int8, errMsg string) (bool, error) {
    tx := r.db.Model(&model.VideoTask{}).
        Where("id = ? AND status IN ?", id, allowedFrom).
        Updates(updates)
    return tx.RowsAffected > 0, nil
}
```

通过 `WHERE status IN (allowedFrom)` 实现状态机的严格约束，防止并发场景下的非法状态流转。

**5. 纵深防御的安全设计**

UserAIProfile 的 API Key 字段：`json:"-"` 防止序列化泄露，`Ciphertext` 后缀防止开发者误用明文，AES-GCM 加密防止数据库泄露。

### 追问链

**追问 10.1: 没有使用外键约束（DB 层面），而是在应用层维护引用完整性，这是为什么？**

GORM 默认不创建数据库层面的外键约束（`gorm:"foreignKey:TaskID;references:ID"` 只是 ORM 映射）。原因是：1) 外键约束会降低写入性能；2) 分布式场景下外键约束可能成为瓶颈；3) 应用层可以通过事务保证一致性。代价是需要开发者自己维护引用完整性，不能依赖数据库的 CASCADE 删除。

**追问 10.2: UserUsageDaily 表的设计目的是什么？**

```go
// internal/model/ai_call_log.go:37-50
type UserUsageDaily struct {
    ID                int64  `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID            int64  `gorm:"uniqueIndex:idx_user_usage_daily;not null" json:"user_id"`
    Date              string `gorm:"type:char(10);uniqueIndex:idx_user_usage_daily;not null" json:"date"`
    ASRSeconds        int    `gorm:"default:0" json:"asr_seconds"`
    LLMRequests       int    `gorm:"default:0" json:"llm_requests"`
    EmbeddingRequests int    `gorm:"default:0" json:"embedding_requests"`
    // ...
}
```

UserUsageDaily 是按用户按天聚合的用量统计表，`(user_id, date)` 唯一索引保证每天每个用户只有一条记录。用途：1) 限流配额的计算依据；2) 用户用量展示；3) 成本核算。避免每次都从 AICallLog 表聚合查询。

**追问 10.3: 如果要支持视频分片上传的断点续传，数据模型需要怎么扩展？**

当前 VideoTask 的 `SourceType` 已经支持 `"chunked"` 类型。断点续传需要记录已上传的分片信息，可以在应用层使用 Redis Set 记录已上传的 chunk 序号（`uploaded_chunks:task_id -> {0,1,2,...}`），不需要额外的数据库表。分片合并后，通过 `MergeChunks` 接口更新 VideoTask 的 Status 和 FileURL。
