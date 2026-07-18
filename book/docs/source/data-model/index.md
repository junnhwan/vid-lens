# 源码走读 - 数据模型设计

## 基于 VidLens 项目的模型层分析

<div class="diagram-container">

![任务状态机](/diagrams/task-state-machine.svg)

</div>

## 交互式状态机

<TaskStateMachine />

---

## 文件结构

| 文件 | 用途 | 行数 |
|------|------|------|
| `internal/model/model.go` | 模型注册与迁移 | 43 |
| `internal/model/user.go` | 用户模型 | 25 |
| `internal/model/asset.go` | 视频资产模型 | 24 |
| `internal/model/task.go` | 视频任务模型（枢纽表） | 77 |
| `internal/model/task_job.go` | 子任务模型 | 36 |
| `internal/model/transcription.go` | 转录结果模型 | 20 |
| `internal/model/transcription_chunk.go` | 分段转录模型 | 30 |
| `internal/model/summary.go` | AI 总结模型 | 20 |
| `internal/model/ai_profile.go` | 用户 AI 配置模型 | 31 |
| `internal/model/video_chunk.go` | RAG 分块模型 | 22 |
| `internal/model/rag_index.go` | RAG 索引状态模型 | 30 |
| `internal/model/chat.go` | 聊天会话与消息模型 | 31 |
| `internal/model/ai_call_log.go` | AI 调用日志与用量统计 | 54 |

---

## 模型总览

### AllModels 注册 (model.go:6-23)

```go
func AllModels() []interface{} {
    return []interface{}{
        &User{},
        &VideoAsset{},
        &VideoTask{},
        &TaskJob{},
        &VideoTranscription{},
        &VideoTranscriptionChunk{},
        &AISummary{},
        &UserAIProfile{},
        &VideoChunk{},
        &VideoRAGIndex{},
        &ChatSession{},
        &ChatMessage{},
        &AICallLog{},
        &UserUsageDaily{},
    }
}
```

14 个模型覆盖了 VidLens 的全部业务域：用户管理、文件存储、异步任务、AI 处理、RAG 检索、对话交互、可观测性。

---

## 关系图

```
User (1) --< VideoTask (N)           用户拥有多条视频任务
User (1) --< UserAIProfile (N)       用户可配置多个 AI 配置
VideoAsset (1) --< VideoTask (N)     一份文件资产可被多个任务引用（内容级去重）
VideoTask (1) --< TaskJob (N)        一条任务拆分为多个子任务（download/transcribe/analyze/rag_index）
VideoTask (1) --| VideoTranscription (1)   一条任务对应一份转录全文
VideoTask (1) --| AISummary (1)            一条任务对应一份 AI 总结
VideoTask (1) --< VideoTranscriptionChunk (N)  一条任务的分段转录中间状态
VideoTask (1) --< VideoChunk (N)     一条任务拆分为多个 RAG 分块
VideoTask (1) --< VideoRAGIndex (N)  一条任务可构建多个模型的 RAG 索引
VideoTask (1) --< ChatSession (N)    一条任务可发起多个对话会话
ChatSession (1) --< ChatMessage (N)  一个会话包含多条消息
```

---

## 核心模型详解

### 1. VideoTask - 枢纽表 (task.go:40-73)

VideoTask 是整个异步架构的核心，所有业务流都围绕它展开。

```go
type VideoTask struct {
    ID              int64          `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID          int64          `gorm:"index;not null" json:"user_id"`
    AssetID         *int64         `gorm:"index" json:"asset_id"`
    FileMD5         string         `gorm:"type:char(32);index;not null" json:"file_md5"`
    Filename        string         `gorm:"type:varchar(255);not null" json:"filename"`
    FileURL         string         `gorm:"type:varchar(500)" json:"file_url"`
    FileSize        int64          `gorm:"default:0" json:"file_size"`
    Status          int8           `gorm:"type:tinyint;default:0;index:idx_status_time" json:"status"`
    Stage           string         `gorm:"type:varchar(50);default:'none';index" json:"stage"`
    TraceID         string         `gorm:"type:varchar(64);index" json:"trace_id"`
    SourceType      string         `gorm:"type:varchar(20);index" json:"source_type"`
    SourceURL       string         `gorm:"type:varchar(1000)" json:"source_url,omitempty"`
    RetryCount      int            `gorm:"default:0" json:"retry_count"`
    MaxRetries      int            `gorm:"default:3" json:"max_retries"`
    NextRetryAt     *time.Time     `json:"next_retry_at,omitempty"`
    LastErrorCode   string         `gorm:"type:varchar(100)" json:"last_error_code"`
    LastErrorMsg    string         `gorm:"type:varchar(500)" json:"last_error_msg"`
    LastJobType     string         `gorm:"type:varchar(30);index" json:"last_job_type"`
    StageStartedAt  *time.Time     `json:"stage_started_at,omitempty"`
    StageFinishedAt *time.Time     `json:"stage_finished_at,omitempty"`
    StartedAt       *time.Time     `json:"started_at,omitempty"`
    FinishedAt      *time.Time     `json:"finished_at,omitempty"`
    ErrorMsg        string         `gorm:"type:varchar(500)" json:"error_msg"`
    CreatedAt       time.Time      `gorm:"index:idx_status_time" json:"created_at"`
    UpdatedAt       time.Time      `json:"updated_at"`
    DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

    // ORM 关联（不存储在数据库列中）
    Asset         *VideoAsset         `gorm:"foreignKey:AssetID;references:ID" json:"asset,omitempty"`
    Transcription *VideoTranscription `gorm:"foreignKey:TaskID;references:ID" json:"transcription,omitempty"`
    Summary       *AISummary          `gorm:"foreignKey:TaskID;references:ID" json:"summary,omitempty"`
    Jobs          []TaskJob           `gorm:"foreignKey:TaskID;references:ID" json:"jobs,omitempty"`
}
```

**字段分组分析**：

| 分组 | 字段 | 用途 |
|------|------|------|
| 标识 | ID, UserID, AssetID, FileMD5 | 主键、外键、去重键 |
| 文件信息 | Filename, FileURL, FileSize | 文件元数据 |
| 状态机 | Status, Stage | 宏观生命周期 + 微观处理阶段 |
| 链路追踪 | TraceID | 分布式追踪 ID，串联日志 |
| 来源 | SourceType, SourceURL | 上传方式（upload/chunked/url） |
| 重试 | RetryCount, MaxRetries, NextRetryAt | 指数退避重试控制 |
| 错误 | LastErrorCode, LastErrorMsg, LastJobType, ErrorMsg | 错误诊断信息 |
| 时间戳 | StageStartedAt, StageFinishedAt, StartedAt, FinishedAt | 耗时计算 |
| 审计 | CreatedAt, UpdatedAt, DeletedAt | 时间审计 + 软删除 |

**状态机定义** (task.go:11-33):

```go
// Status: 宏观生命周期
TaskStatusPending   int8 = 0  // 待处理
TaskStatusQueued    int8 = 1  // 排队中
TaskStatusRunning   int8 = 2  // 处理中
TaskStatusCompleted int8 = 3  // 已完成
TaskStatusFailed    int8 = 4  // 失败（可重试）
TaskStatusDead      int8 = 5  // 死信（重试耗尽）

// Stage: 微观处理阶段
TaskStageNone         = "none"
TaskStageDownloading  = "downloading"
TaskStageUploaded     = "uploaded"
TaskStageTranscribing = "transcribing"
TaskStageSummarizing  = "summarizing"
TaskStageIndexing     = "indexing"
```

**状态流转图**：

```
                    +----------------------------------------------+
                    |                                              |
                    v                                              |
[创建] --> Pending --> Queued --> Running --> Completed             |
                |        |         |                                |
                |        |         +-- downloading --> uploaded     |
                |        |         +-- transcribing                |
                |        |         +-- summarizing                 |
                |        |         +-- indexing                    |
                |        |                                         |
                |        +-- (MQ failed) --> Failed                |
                |                                                  |
                +-- Failed --(retry_count < max)--> Queued --------+
                          +--(retry_count >= max)--> Dead (terminal)
```

**索引设计**：

| 索引 | 字段 | 用途 |
|------|------|------|
| 主键 | ID | 自增主键 |
| idx_status_time | (Status, CreatedAt) | 调度器捞取积压任务 |
| 普通索引 | UserID | 按用户查询任务列表 |
| 普通索引 | AssetID | 按资产反查关联任务 |
| 普通索引 | FileMD5 | 按 MD5 查找（非唯一，去重在 Asset 层） |
| 普通索引 | Stage | 按阶段筛选 |
| 普通索引 | TraceID | 按追踪 ID 查链路 |
| 普通索引 | SourceType | 按来源类型筛选 |
| 普通索引 | LastJobType | 按最后失败的 Job 类型筛选 |
| 软删除 | DeletedAt | 逻辑删除 |

---

### 2. TaskJob - 子任务表 (task_job.go:15-32)

```go
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

**JobType 枚举**：

| JobType | 说明 | 对应 Stage |
|---------|------|-----------|
| download | URL 下载文件到 MinIO | downloading |
| transcribe | ASR 语音转文字 | transcribing |
| analyze | LLM 生成总结 | summarizing |
| rag_index | Embedding + 向量入库 | indexing |

**与 VideoTask 的关系**：

VideoTask 是面向前端的兼容状态源，TaskJob 是面向运维的可观测性来源。一个 VideoTask 的处理流程可能包含多个 TaskJob，每个 TaskJob 独立追踪状态和重试。

**唯一索引 `uk_task_jobs_task_type`**：`(task_id, job_type)` 保证每个任务的每种 Job 类型只有一条记录，配合 `INSERT ... ON DUPLICATE KEY UPDATE` 实现幂等写入。

---

### 3. VideoAsset - 内容级资产表 (asset.go:11-20)

```go
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

**设计决策**：

VideoAsset 将文件的存储信息从 VideoTask 中解耦出来，实现了内容级去重：
- `FileMD5` 是唯一索引，相同内容的文件只产生一条 Asset 记录
- 多个用户的 VideoTask 可以通过 `AssetID` 引用同一个 Asset
- MinIO 中只需要存储一份文件，节省存储空间
- 秒传场景：上传时先算 MD5，查 Asset 表是否存在，存在则跳过上传

**与 VideoTask 的关系**：

```
VideoAsset.FileMD5 (uniqueIndex) --> 多个 VideoTask.AssetID
VideoTask.FileMD5 (plain index)  --> 用于查询，不做唯一约束
```

VideoTask 的 `AssetID` 是可空指针 `*int64`，因为 URL 下载任务在下载完成前还没有 Asset。

---

### 4. VideoTranscription - 转录结果表 (transcription.go:10-16)

```go
type VideoTranscription struct {
    ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
    Content   string    `gorm:"type:longtext" json:"content"`
    Words     int       `gorm:"default:0" json:"words"`
    CreatedAt time.Time `json:"created_at"`
}
```

**垂直拆分策略**：

转录全文（Content）使用 `longtext` 类型，可能达到数万字。将其从 VideoTask 中拆分出来：
- 用户刷历史列表时，`ListByUserID` 只查 VideoTask 的轻量字段，不加载 Content
- 只有查看详情时，才通过 `Preload("Transcription")` 按需加载
- TaskID 上的 `uniqueIndex` 保证 1:1 关系

---

### 5. VideoTranscriptionChunk - 分段转录表 (transcription_chunk.go:12-26)

```go
type VideoTranscriptionChunk struct {
    ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID      int64     `gorm:"index;uniqueIndex:idx_task_transcription_chunk;not null" json:"task_id"`
    ChunkIndex  int       `gorm:"uniqueIndex:idx_task_transcription_chunk;not null" json:"chunk_index"`
    AudioObject string    `gorm:"type:varchar(500)" json:"audio_object"`
    StartSecond int       `gorm:"default:0" json:"start_second"`
    EndSecond   int       `gorm:"default:0" json:"end_second"`
    Status      string    `gorm:"type:varchar(30);index;not null" json:"status"`
    Content     string    `gorm:"type:longtext" json:"content"`
    Chars       int       `gorm:"default:0" json:"chars"`
    ErrorMsg    string    `gorm:"type:varchar(500)" json:"error_msg"`
    RetryCount  int       `gorm:"default:0" json:"retry_count"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

**分段转录流程**：

```
长视频 --> 切分为多个音频片段 --> 每个片段独立 ASR --> 合并为 VideoTranscription
              |                        |
              v                        v
         AudioObject              VideoTranscriptionChunk
        (MinIO path)            (Content + StartSecond + EndSecond)
```

每个 Chunk 有独立的 Status 和 RetryCount，单个片段失败只重试该片段。`(task_id, chunk_index)` 联合唯一索引保证幂等。

---

### 6. AISummary - AI 总结表 (summary.go:10-16)

```go
type AISummary struct {
    ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TaskID    int64     `gorm:"uniqueIndex;not null" json:"task_id"`
    Content   string    `gorm:"type:longtext" json:"content"`
    ModelName string    `gorm:"type:varchar(100)" json:"model_name"`
    CreatedAt time.Time `json:"created_at"`
}
```

与 VideoTranscription 类似，AISummary 也是垂直拆分的结果。Content 存储 Markdown 格式的 AI 分析结果，前端可以直接渲染。ModelName 记录使用的模型名称，便于成本核算和效果对比。

---

### 7. UserAIProfile - 用户 AI 配置表 (ai_profile.go:7-27)

```go
type UserAIProfile struct {
    ID                          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID                      int64     `gorm:"index;not null" json:"user_id"`
    Name                        string    `gorm:"type:varchar(100);not null" json:"name"`
    LLMProvider                 string    `gorm:"type:varchar(50);not null" json:"llm_provider"`
    LLMBaseURL                  string    `gorm:"type:varchar(500);not null" json:"llm_base_url"`
    LLMAPIKeyCiphertext         string    `gorm:"type:text;not null" json:"-"`
    LLMModel                    string    `gorm:"type:varchar(100);not null" json:"llm_model"`
    ASRProvider                 string    `gorm:"type:varchar(50);not null" json:"asr_provider"`
    ASRBaseURL                  string    `gorm:"type:varchar(500);not null" json:"asr_base_url"`
    ASRAPIKeyCiphertext         string    `gorm:"type:text;not null" json:"-"`
    ASRModel                    string    `gorm:"type:varchar(100);not null" json:"asr_model"`
    EmbeddingProvider           string    `gorm:"type:varchar(50);not null" json:"embedding_provider"`
    EmbeddingEndpoint           string    `gorm:"type:varchar(500);not null" json:"embedding_endpoint"`
    EmbeddingAPIKeyCiphertext   string    `gorm:"type:text;not null" json:"-"`
    EmbeddingModel              string    `gorm:"type:varchar(100);not null" json:"embedding_model"`
    EmbeddingDim                int       `gorm:"not null" json:"embedding_dim"`
    IsDefault                   bool      `gorm:"default:false;index" json:"is_default"`
    CreatedAt                   time.Time `json:"created_at"`
    UpdatedAt                   time.Time `json:"updated_at"`
}
```

**安全设计**：

| 字段 | 安全措施 | 说明 |
|------|---------|------|
| LLMAPIKeyCiphertext | `json:"-"` | API 响应中不序列化 |
| LLMAPIKeyCiphertext | `gorm:"type:text"` | 存储 AES-256-GCM 加密后的 base64 密文 |
| ASRAPIKeyCiphertext | `json:"-"` | 同上 |
| EmbeddingAPIKeyCiphertext | `json:"-"` | 同上 |

**三类 AI 能力**：

- **LLM**：大语言模型，用于生成总结和对话
- **ASR**：自动语音识别，用于视频转录
- **Embedding**：向量模型，用于 RAG 索引构建

用户可以为每种能力配置不同的 Provider（如 SiliconFlow、MiMo、OpenAI Compatible），实现灵活的多模型切换。

---

### 8. VideoChunk - RAG 分块表 (video_chunk.go:5-18)

```go
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
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

**RAG 索引构建流程**：

```
VideoTranscription.Content
        |
        v
    text chunking (ChunkIndex: 0, 1, 2, ...)
        |
        v
    compute ContentHash --> check if already vectorized
        |
        v
    call Embedding API --> generate vector --> write to pgvector projection --> record VectorID
        |
        v
    VideoChunk (Content + ContentHash + VectorID + EmbeddingModel)
```

**联合唯一索引 `idx_task_chunk_model`**：`(task_id, chunk_index, embedding_model)` 保证同一个任务的同一个分块在同一个模型下只有一条记录。切换模型时可以重新构建索引而不冲突。

---

### 9. VideoRAGIndex - RAG 索引状态表 (rag_index.go:12-26)

```go
type VideoRAGIndex struct {
    ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID         int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"user_id"`
    TaskID         int64      `gorm:"index;uniqueIndex:idx_user_task_model;not null" json:"task_id"`
    EmbeddingModel string     `gorm:"type:varchar(100);uniqueIndex:idx_user_task_model;not null" json:"embedding_model"`
    EmbeddingDim   int        `gorm:"not null" json:"embedding_dim"`
    Status         string     `gorm:"type:varchar(30);index;not null" json:"status"`
    ChunkCount     int        `gorm:"default:0" json:"chunk_count"`
    LastError      string     `gorm:"type:varchar(500)" json:"last_error"`
    BuildVersion   int        `gorm:"default:1" json:"build_version"`
    StartedAt      *time.Time `json:"started_at,omitempty"`
    FinishedAt     *time.Time `json:"finished_at,omitempty"`
    CreatedAt      time.Time  `json:"created_at"`
    UpdatedAt      time.Time  `json:"updated_at"`
}
```

**RAG 索引状态**：

```go
const (
    RAGIndexStatusNotIndexed = "not_indexed"
    RAGIndexStatusIndexing   = "indexing"
    RAGIndexStatusIndexed    = "indexed"
    RAGIndexStatusFailed     = "failed"
)
```

VideoRAGIndex 是索引级别的状态表，VideoChunk 是分块级别的数据表。一个 VideoTask 可以有多个 VideoRAGIndex（不同模型），每个 VideoRAGIndex 对应多个 VideoChunk。

**BuildVersion 字段**：每次重建索引时递增，用于版本管理和增量更新。

---

### 10. ChatSession + ChatMessage - 对话模型 (chat.go:5-31)

```go
type ChatSession struct {
    ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID    int64     `gorm:"index;not null" json:"user_id"`
    TaskID    int64     `gorm:"index;not null" json:"task_id"`
    Title     string    `gorm:"type:varchar(200)" json:"title"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessage struct {
    ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    SessionID         int64     `gorm:"index;not null" json:"session_id"`
    UserID            int64     `gorm:"index;not null" json:"user_id"`
    Role              string    `gorm:"type:varchar(20);not null" json:"role"`
    Content           string    `gorm:"type:longtext;not null" json:"content"`
    RetrievalSnapshot *string   `gorm:"type:json" json:"retrieval_snapshot,omitempty"`
    ModelName         string    `gorm:"type:varchar(100)" json:"model_name,omitempty"`
    CreatedAt         time.Time `json:"created_at"`
}
```

**对话流程**：

```
User question --> Embedding query --> pgvector TopK + BM25 --> RRF --> Build Prompt --> LLM generate answer
                                                |
                                                v
                                       RetrievalSnapshot (JSON)
                                     records RAG retrieval context
```

**RetrievalSnapshot**：存储在 assistant 消息中，记录 RAG 检索的上下文快照。用户可以回溯查看"这个回答是基于哪些内容生成的"。

---

### 11. AICallLog + UserUsageDaily - 可观测性 (ai_call_log.go:16-54)

```go
type AICallLog struct {
    ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID      int64     `gorm:"index;not null" json:"user_id"`
    TaskID      int64     `gorm:"index" json:"task_id,omitempty"`
    SessionID   int64     `gorm:"index" json:"session_id,omitempty"`
    Kind        string    `gorm:"type:varchar(30);index;not null" json:"kind"`       // asr/llm/embedding
    Provider    string    `gorm:"type:varchar(50);index" json:"provider"`
    ModelName   string    `gorm:"type:varchar(100);index" json:"model"`
    Status      string    `gorm:"type:varchar(30);index;not null" json:"status"`     // success/failed
    DurationMs  int64     `gorm:"default:0" json:"duration_ms"`
    InputChars  int       `gorm:"default:0" json:"input_chars"`
    OutputChars int       `gorm:"default:0" json:"output_chars"`
    ErrorCode   string    `gorm:"type:varchar(100)" json:"error_code,omitempty"`
    ErrorMsg    string    `gorm:"type:varchar(500)" json:"error_msg,omitempty"`
    CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type UserUsageDaily struct {
    ID                int64  `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID            int64  `gorm:"uniqueIndex:idx_user_usage_daily;not null" json:"user_id"`
    Date              string `gorm:"type:char(10);uniqueIndex:idx_user_usage_daily;not null" json:"date"`
    ASRSeconds        int    `gorm:"default:0" json:"asr_seconds"`
    ASRRequests       int    `gorm:"default:0" json:"asr_requests"`
    LLMRequests       int    `gorm:"default:0" json:"llm_requests"`
    EmbeddingRequests int    `gorm:"default:0" json:"embedding_requests"`
    FailedRequests    int    `gorm:"default:0" json:"failed_requests"`
    InputChars        int    `gorm:"default:0" json:"input_chars"`
    OutputChars       int    `gorm:"default:0" json:"output_chars"`
    CreatedAt         time.Time `json:"created_at"`
    UpdatedAt         time.Time `json:"updated_at"`
}
```

**两层可观测性**：

| 层级 | 表 | 粒度 | 用途 |
|------|-----|------|------|
| 明细层 | AICallLog | 每次 API 调用 | 排查错误、性能分析、审计 |
| 聚合层 | UserUsageDaily | 每用户每天 | 用量统计、成本核算、限流依据 |

AICallLog 通过 `Kind` 字段区分三种 AI 调用类型（asr/llm/embedding），通过 `TaskID` 和 `SessionID` 关联到具体的业务上下文。

---

## 迁移策略 (model.go:26-43)

```go
func Migrate(db *gorm.DB) error {
    if err := db.AutoMigrate(AllModels()...); err != nil {
        return err
    }

    // backward compat: file_md5 unique index -> plain index
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

**迁移兼容性处理**：

1. `AutoMigrate` 创建缺失的表和索引
2. 额外处理 `file_md5` 索引的类型变更（uniqueIndex -> plain index）
3. 旧索引名 `idx_file_md5` 与 GORM 生成的 `idx_video_tasks_file_md5` 不一致，需要先删后建

---

## 设计决策总结

### 1. 关注点分离

| 层次 | 模型 | 职责 |
|------|------|------|
| 存储层 | VideoAsset | 文件在哪（ObjectName, FileSize） |
| 任务层 | VideoTask | 谁要处理什么（UserID, Status, Stage） |
| 执行层 | TaskJob | 每个子任务的执行状态 |
| 结果层 | VideoTranscription, AISummary | 处理输出 |
| 检索层 | VideoChunk, VideoRAGIndex | RAG 向量索引 |
| 交互层 | ChatSession, ChatMessage | 用户对话 |
| 配置层 | UserAIProfile | 用户自定义 AI 配置 |
| 观测层 | AICallLog, UserUsageDaily | 调用日志和用量统计 |

### 2. 垂直拆分策略

大字段（Content）独立成表，主表只存轻量级元数据：
- VideoTranscription.Content (longtext) 从 VideoTask 拆出
- AISummary.Content (longtext) 从 VideoTask 拆出
- ChatMessage.Content (longtext) 从 ChatSession 拆出

### 3. 内容级去重

```
User A uploads video.mp4 --> VideoTask(A) --+
                                            +-> VideoAsset (FileMD5: abc123, ObjectName: videos/abc123.mp4)
User B uploads same file --> VideoTask(B) --+
```

### 4. 乐观并发控制

```go
// UpdateStatusIf: only update when current status is in allowedFrom
WHERE id = ? AND status IN (?, ?, ...)
```

通过 WHERE 条件实现状态机约束，`RowsAffected == 0` 表示状态已被其他进程改变。

### 5. 纵深防御安全

```
API Key storage: AES-256-GCM encrypt --> base64 encode --> store in Ciphertext field
API Key transport: json:"-" tag --> completely ignored in API response
API Key display: MaskAPIKey --> masked as sk-****xyz
```

### 6. 软删除与物理删除

| 模型 | 删除策略 | 原因 |
|------|---------|------|
| User | soft delete (DeletedAt) | user data retention |
| VideoAsset | soft delete (DeletedAt) | file reference integrity |
| VideoTask | soft delete (DeletedAt) | task audit history |
| TaskJob | hard delete | cleaned with Task soft delete |
| VideoTranscription | hard delete | cleaned with Task soft delete |
| AISummary | hard delete | cleaned with Task soft delete |
| ChatSession | hard delete | cleaned with Task soft delete |
| ChatMessage | hard delete | cleaned with Session |
