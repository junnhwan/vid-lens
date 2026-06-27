# VidLens 架构与启动流程 - 源码走读

---

## 架构概览

<div class="diagram-container">

![VidLens 架构总览](/diagrams/architecture-overview.svg)

</div>

### 项目概况

VidLens 是一个 AI 驱动的视频内容理解平台，支持视频上传、音频提取、语音转文字（ASR）、AI 总结、RAG 索引和智能问答。技术栈：Go 1.24.0 + Gin + GORM + Kafka + Redis + MinIO + Milvus。

### 文件清单

| 文件 | 行数 | 职责 |
|------|------|------|
| `cmd/server/main.go` | 295 | 应用入口，依赖组装，路由注册 |
| `internal/config/config.go` | 163 | 14 个配置子结构体 + YAML 加载 |
| `internal/model/model.go` | 43 | AutoMigrate 入口，14 个模型注册 |
| `internal/model/task.go` | 77 | VideoTask 模型，任务状态机定义 |
| `internal/model/user.go` | 25 | User 模型，bcrypt 密码存储 |
| `internal/repository/repository.go` | 45 | 12 个 Repository 聚合 + 事务管理 |
| `internal/service/user.go` | 89 | 用户注册/登录，bcrypt + JWT |
| `internal/service/media.go` | 80+ | 媒体上传、分片合并、任务管理 |
| `internal/handler/user.go` | 84 | 用户 Handler，Register/Login/GetProfile |
| `internal/handler/chat.go` | 155 | 聊天 Handler，SSE 流式响应 |
| `internal/handler/media.go` | 240 | 媒体 Handler，分片上传/URL 下载/分析 |
| `internal/handler/ai_profile.go` | 144 | AI 配置 Handler，CRUD + 测试 |
| `internal/handler/rag.go` | 70 | RAG 索引 Handler |
| `internal/middleware/auth.go` | 49 | JWT 认证中间件 |
| `internal/middleware/cors.go` | 19 | CORS 跨域中间件 |
| `internal/middleware/ratelimit.go` | 132 | Redis 令牌桶限流，按用户+路由维度 |
| `internal/ai/strategy.go` | 17 | AI 策略接口（ASR + 总结） |
| `internal/ai/factory.go` | 133 | AI 工厂，按 Profile 创建客户端 |
| `internal/ai/embedding.go` | 76 | Embedding 客户端，OpenAI 兼容 |
| `internal/mq/producer.go` | 184 | Kafka 生产者，4 个 Topic |
| `internal/mq/consumer.go` | 851 | Kafka 消费者，4 个消费组 |
| `internal/mq/retry.go` | 251 | 重试策略 + 重试调度器 |
| `internal/storage/minio.go` | 147 | MinIO 对象存储封装 |
| `internal/vector/milvus.go` | 50+ | Milvus 向量存储 |
| `internal/pkg/response/response.go` | 73 | 统一响应结构 |
| `internal/pkg/secret/crypto.go` | 87 | AES-GCM 加密/解密 |
| `internal/pkg/lock/redis_lock.go` | - | Redis 分布式锁 |

### 分层架构

```
┌─────────────────────────────────────────────────────────────┐
│                        cmd/server/main.go                    │
│                    (依赖组装 + 路由注册)                      │
├─────────────────────────────────────────────────────────────┤
│  Middleware Layer                                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                  │
│  │  CORS    │  │ JWTAuth  │  │RateLimit │                  │
│  └──────────┘  └──────────┘  └──────────┘                  │
├─────────────────────────────────────────────────────────────┤
│  Handler Layer (internal/handler/)                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  User    │  │  Media   │  │  Chat    │  │  RAG     │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│  Service Layer (internal/service/)                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  User    │  │  Media   │  │  Chat    │  │RAGIndex  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│  Repository Layer (internal/repository/)                     │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Repositories (聚合 12 个 Repository + Transaction)  │   │
│  └──────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│  Infrastructure Layer                                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  MySQL   │  │  Redis   │  │  MinIO   │  │  Kafka   │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
│  ┌──────────┐  ┌──────────┐                                │
│  │  Milvus  │  │  AI API  │                                │
│  └──────────┘  └──────────┘                                │
└─────────────────────────────────────────────────────────────┘
```

---

## 核心数据结构

### 1. Config - 全局配置

```go
// internal/config/config.go:11-26
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	MinIO     MinIOConfig     `yaml:"minio"`
	Kafka     KafkaConfig     `yaml:"kafka"`
	AI        AIConfig        `yaml:"ai"`
	Tools     ToolsConfig     `yaml:"tools"`
	JWT       JWTConfig       `yaml:"jwt"`
	Security  SecurityConfig  `yaml:"security"`
	Upload    UploadConfig    `yaml:"upload"`
	TaskRetry TaskRetryConfig `yaml:"task_retry"`
	RateLimit RateLimitConfig `yaml:"ratelimit"`
	RAG       RAGConfig       `yaml:"rag"`
	Milvus    MilvusConfig    `yaml:"milvus"`
}
```

14 个子结构体按领域分组，YAML 标签对应配置文件的层级结构。

### 2. VideoTask - 任务模型（异步架构枢纽）

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
// internal/model/task.go:27-33
const (
	TaskStageNone         = "none"
	TaskStageDownloading  = "downloading"
	TaskStageUploaded     = "uploaded"
	TaskStageTranscribing = "transcribing"
	TaskStageSummarizing  = "summarizing"
	TaskStageIndexing     = "indexing"
)
```

```go
// internal/model/task.go:40-73
type VideoTask struct {
	ID              int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID          int64          `gorm:"index;not null" json:"user_id"`
	AssetID         *int64         `gorm:"index" json:"asset_id"`
	FileMD5         string         `gorm:"type:char(32);index;not null" json:"file_md5"`
	Filename        string         `gorm:"type:varchar(255);not null" json:"filename"`
	FileURL         string         `gorm:"type:varchar(500)" json:"file_url"` // MinIO 存储路径
	FileSize        int64          `gorm:"default:0" json:"file_size"`        // 文件大小（字节）
	Status          int8           `gorm:"type:tinyint;default:0;index:idx_status_time" json:"status"`
	Stage           string         `gorm:"type:varchar(50);default:'none';index" json:"stage"`
	TraceID         string         `gorm:"type:varchar(64);index" json:"trace_id"`
	SourceType      string         `gorm:"type:varchar(20);index" json:"source_type"`
	SourceURL       string         `gorm:"type:varchar(1000)" json:"source_url,omitempty"`
	RetryCount      int            `gorm:"default:0" json:"retry_count"`
	MaxRetries      int            `gorm:"default:3" json:"max_reries"`
	NextRetryAt     *time.Time     `json:"next_retry_at,omitempty"`
	LastErrorCode   string         `gorm:"type:varchar(100)" json:"last_error_code"`
	LastErrorMsg    string         `gorm:"type:varchar(500)" json:"last_error_msg"`
	LastJobType     string         `gorm:"type:varchar(30);index" json:"last_job_type"`
	StageStartedAt  *time.Time     `json:"stage_started_at,omitempty"`
	StageFinishedAt *time.Time     `json:"stage_finished_at,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	ErrorMsg        string         `gorm:"type:varchar(500)" json:"error_msg"` // 失败原因
	CreatedAt       time.Time      `gorm:"index:idx_status_time" json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联（不存储在数据库）
	Asset         *VideoAsset         `gorm:"foreignKey:AssetID;references:ID" json:"asset,omitempty"`
	Transcription *VideoTranscription `gorm:"foreignKey:TaskID;references:ID" json:"transcription,omitempty"`
	Summary       *AISummary          `gorm:"foreignKey:TaskID;references:ID" json:"summary,omitempty"`
	Jobs          []TaskJob           `gorm:"foreignKey:TaskID;references:ID" json:"jobs,omitempty"`
}
```

状态机设计：Pending(0) → Queued(1) → Running(2) → Completed(3)，任何阶段失败 → Failed(4)，超过重试次数 → Dead(5)。

### 3. Repositories - Repository 聚合

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

### 4. Response - 统一响应

```go
// internal/pkg/response/response.go:10-14
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
```

### 5. AI Profile - AI 配置

```go
// internal/ai/factory.go:9-23
type Profile struct {
	LLMProvider       string
	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	ASRProvider       string
	ASRBaseURL        string
	ASRAPIKey         string
	ASRModel          string
	EmbeddingProvider string
	EmbeddingEndpoint string
	EmbeddingAPIKey   string
	EmbeddingModel    string
	EmbeddingDim      int
}
```

### 6. RateLimiter - 限流器

```go
// internal/middleware/ratelimit.go:19-25
type RateLimiter struct {
	client    redis.Cmdable
	capacity  int // 全局默认桶容量
	rate      int // 全局默认每秒令牌数
	overrides map[string]routeLimit
	mu        sync.RWMutex
}
```

---

## 关键函数完整实现

### 1. 配置加载

```go
// internal/config/config.go:149-163
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &cfg, nil
}
```

**设计要点**：`os.ExpandEnv` 在 YAML 解析前展开 `${VAR}` 占位符，支持敏感信息通过环境变量注入。

### 2. 数据库迁移

```go
// internal/model/model.go:6-43
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

// Migrate 执行模型迁移，并兼容旧版本 video_tasks.file_md5 唯一索引。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return err
	}

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

**设计要点**：14 个模型一次性迁移，额外处理了旧版本索引的兼容性（`idx_file_md5` → `idx_video_tasks_file_md5`）。

### 3. Repository 聚合与事务

```go
// internal/repository/repository.go:23-45
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

**设计要点**：`Transaction` 方法用事务 DB 创建新的 Repositories，确保事务内所有操作使用同一个连接。

### 4. 用户注册

```go
// internal/service/user.go:31-64
func (s *UserService) Register(username, password, nickname string) (*model.User, string, error) {
	existing, _ := s.repo.FindByUsername(username)
	if existing != nil {
		return nil, "", ErrUserExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return nil, "", err
	}

	if nickname == "" {
		nickname = "用户" + fmt.Sprintf("%d", time.Now().UnixMilli())
	}

	user := &model.User{
		Username:     username,
		PasswordHash: string(hashedPassword),
		Nickname:     nickname,
		Role:         "USER",
	}

	if err := s.repo.Create(user); err != nil {
		return nil, "", err
	}

	token, err := jwt.GenerateToken(user.ID, user.Username, user.Role,
		s.jwtCfg.Secret, s.jwtCfg.ExpireHours)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}
```

**设计要点**：bcrypt cost=10 进行密码哈希，注册成功后立即返回 JWT Token，避免二次登录。

### 5. JWT 认证中间件

```go
// internal/middleware/auth.go:12-39
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "请先登录")
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Unauthorized(c, "Token 格式错误")
			c.Abort()
			return
		}

		claims, err := jwt.ParseToken(parts[1], secret)
		if err != nil {
			response.Unauthorized(c, "Token 无效或已过期")
			c.Abort()
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}
```

**设计要点**：解析 Bearer Token，验证后将用户信息注入 Gin Context，Handler 通过 `GetUserID(c)` 获取。

### 6. 令牌桶限流器

```go
// internal/middleware/ratelimit.go:32-106
func NewRateLimiter(client redis.Cmdable, capacity, rate int) *RateLimiter {
	return &RateLimiter{
		client:    client,
		capacity:  capacity,
		rate:      rate,
		overrides: make(map[string]routeLimit),
	}
}

func (r *RateLimiter) SetRouteLimit(path string, capacity, rate int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[path] = routeLimit{capacity: capacity, rate: rate}
}

var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local bucket = redis.call("HMGET", key, "tokens", "last_time")
local tokens = tonumber(bucket[1])
local last_time = tonumber(bucket[2])

if tokens == nil then
    tokens = capacity
    last_time = now
end

local elapsed = now - last_time
local new_tokens = elapsed / 1000 * rate
tokens = math.min(capacity, tokens + new_tokens)
last_time = now

if tokens >= 1 then
    tokens = tokens - 1
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 1
else
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 0
end
`)

func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) bool {
	now := time.Now().UnixMilli()
	result, err := tokenBucketScript.Run(ctx, r.client,
		[]string{fmt.Sprintf("rate_limiter:%s", key)},
		rate, capacity, now,
	).Int()
	if err != nil {
		log.Printf("[ratelimit] Redis 异常，fail-open 放行 key=%s err=%v", key, err)
		return true
	}
	return result == 1
}
```

**设计要点**：
- Lua 脚本保证原子性，避免竞态条件
- 支持按路由差异化配额（`overrides` map）
- Redis 异常时 fail-open 放行，不成为单点故障

### 7. RateLimit 中间件

```go
// internal/middleware/ratelimit.go:108-132
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		var key string
		if userID, ok := c.Get("userID"); ok {
			key = fmt.Sprintf("%s:user:%v", path, userID)
		} else {
			key = fmt.Sprintf("%s:ip:%s", path, c.ClientIP())
		}

		capacity, rate := limiter.configFor(path)
		if !limiter.Allow(c.Request.Context(), key, capacity, rate) {
			response.TooManyRequests(c, "当前请求过多，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}
```

**设计要点**：key = 路由路径 + 用户ID/IP，实现 (接口, 用户) 双维度计数隔离。

### 8. AES-GCM 加密

```go
// internal/pkg/secret/crypto.go:17-23
func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("api key secret is required")
	}
	sum := sha256.Sum256([]byte(passphrase))
	return newCodecFromKey(sum[:])
}

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

// internal/pkg/secret/crypto.go:60-77
func (c *Codec) Decrypt(ciphertext string) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonceSize := c.aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", fmt.Errorf("ciphertext is too short")
	}

	nonce := payload[:nonceSize]
	sealed := payload[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
```

**设计要点**：SHA-256 派生 32 字节密钥 → AES-256-GCM 加密 → nonce 拼接密文 → Base64 编码存储。

### 9. MinIO 存储

```go
// internal/storage/minio.go:24-44
func NewMinIOStorage(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 MinIO 客户端失败: %w", err)
	}

	s := &MinIOStorage{
		client:   client,
		bucket:   bucket,
		endpoint: endpoint,
	}

	if err := s.ensureBucket(context.Background()); err != nil {
		return nil, err
	}

	return s, nil
}

// internal/storage/minio.go:80-98
func (s *MinIOStorage) GetPresignedURL(ctx context.Context, objectName string) (string, error) {
	reqParams := make(url.Values)
	if ext := filepath.Ext(objectName); ext != "" {
		switch ext {
		case ".mp4":
			reqParams.Set("response-content-type", "video/mp4")
		case ".mp3":
			reqParams.Set("response-content-type", "audio/mpeg")
		case ".wav":
			reqParams.Set("response-content-type", "audio/wav")
		}
	}

	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucket, objectName, 5*time.Minute, reqParams)
	if err != nil {
		return "", fmt.Errorf("生成预签名 URL 失败: %w", err)
	}
	return presignedURL.String(), nil
}
```

**设计要点**：启动时自动创建 bucket，预签名 URL 根据文件扩展名设置 Content-Type，5 分钟有效。

### 10. AI 工厂

```go
// internal/ai/factory.go:25-74
type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) NewASRStrategy(profile Profile) (Strategy, error) {
	switch normalizeProvider(profile.ASRProvider) {
	case "mimo":
		return NewMimoStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	case "siliconflow":
		return NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	case "openai_compatible":
		return NewSiliconFlowStrategy(profile.ASRAPIKey, profile.ASRBaseURL, profile.ASRModel, profile.ASRModel), nil
	default:
		return nil, fmt.Errorf("不支持的 ASR provider: %s", profile.ASRProvider)
	}
}

func (f *Factory) NewChatClient(profile Profile) (ChatClient, error) {
	switch normalizeProvider(profile.LLMProvider) {
	case "openai_compatible", "siliconflow":
		return NewOpenAIChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), nil
	case "mimo":
		return NewMimoChatClient(profile.LLMBaseURL, profile.LLMAPIKey, profile.LLMModel), nil
	default:
		return nil, fmt.Errorf("不支持的 LLM provider: %s", profile.LLMProvider)
	}
}

func (f *Factory) NewEmbeddingClient(profile Profile) (EmbeddingClient, error) {
	switch normalizeProvider(profile.EmbeddingProvider) {
	case "openai_compatible", "siliconflow":
		return NewOpenAIEmbeddingClient(profile.EmbeddingEndpoint, profile.EmbeddingAPIKey, profile.EmbeddingModel), nil
	default:
		return nil, fmt.Errorf("不支持的 Embedding provider: %s", profile.EmbeddingProvider)
	}
}

func (f *Factory) NewAnalysisStrategy(profile Profile) (Strategy, error) {
	asr, err := f.NewASRStrategy(profile)
	if err != nil {
		return nil, err
	}
	chat, err := f.NewChatClient(profile)
	if err != nil {
		return nil, err
	}
	return &CompositeStrategy{asr: asr, chat: chat}, nil
}
```

**设计要点**：工厂模式按 Profile 动态创建 ASR/Chat/Embedding 客户端，`CompositeStrategy` 组合 ASR 和 Chat 实现完整的分析策略。

### 11. Kafka 生产者

```go
// internal/mq/producer.go:47-72
func NewProducer(brokers []string, analyzeTopic, transcribeTopic, downloadTopic string, ragIndexTopic ...string) *Producer {
	newWriter := func(topic string) *kafka.Writer {
		if topic == "" {
			return nil
		}
		return &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{}, // 按负载均衡选择分区
			RequiredAcks: kafka.RequireAll,    // 等所有 ISR 副本确认（消息不丢失）
			MaxAttempts:  3,                   // 发送失败最多重试 3 次
			Async:        false,               // 同步发送，确保消息投递成功
		}
	}

	var ragTopic string
	if len(ragIndexTopic) > 0 {
		ragTopic = ragIndexTopic[0]
	}
	return &Producer{
		analyzeWriter:    newWriter(analyzeTopic),
		transcribeWriter: newWriter(transcribeTopic),
		downloadWriter:   newWriter(downloadTopic),
		ragIndexWriter:   newWriter(ragTopic),
	}
}

// internal/mq/producer.go:77-88
func (p *Producer) EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:  taskID,
		MD5:     md5,
		TraceID: TraceIDFromContext(ctx),
	})

	return p.analyzeWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(md5), // Key = MD5，保证同视频进入同一分区
		Value: payload,
	})
}
```

**设计要点**：同步发送 + RequiredAcks=All 保证消息不丢失，MD5 作为 Key 保证同视频消息的消费顺序。

### 12. Kafka 消费者（核心业务）

```go
// internal/mq/consumer.go:393-461
func (c *Consumer) handleAnalyze(ctx context.Context, msg kafka.Message) error {
	// 第 1 步：解析消息
	var payload AnalyzePayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	log.Printf("[Kafka] 收到分析任务: traceID=%s taskID=%d, md5=%s", payload.TraceID, payload.TaskID, payload.MD5)

	// 第 2 步：基于 MD5 获取分布式锁
	lockKey := fmt.Sprintf("vidlens:lock:%s", payload.MD5)
	distLock := lock.NewRedisLock(c.rdb, lockKey)

	acquired, err := distLock.TryLock(ctx, 5*time.Second)
	if err != nil {
		return fmt.Errorf("获取分布式锁失败: %w", err)
	}
	if !acquired {
		log.Printf("[Kafka] 抢锁失败，跳过: md5=%s", payload.MD5)
		return fmt.Errorf("同一视频正在处理中")
	}
	defer distLock.Unlock(ctx)

	// 第 3 步：幂等校验
	task, err := c.repo.Task.FindByID(payload.TaskID)
	if err != nil {
		return fmt.Errorf("查询任务失败: %w", err)
	}
	if task.Status == model.TaskStatusCompleted {
		summary, err := c.repo.Summary.FindByTaskID(task.ID)
		if err != nil {
			return fmt.Errorf("查询任务总结失败: %w", err)
		}
		if summary != nil {
			log.Printf("[Kafka] 任务已完成，跳过: taskID=%d", payload.TaskID)
			return nil
		}
		log.Printf("[Kafka] 任务已完成但缺少总结，继续分析: taskID=%d", payload.TaskID)
	}

	// 第 4 步：更新状态为处理中
	updated, err := c.repo.Task.UpdateStatusAndStageIf(payload.TaskID,
		[]int8{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusFailed, model.TaskStatusCompleted},
		model.TaskStatusRunning, model.TaskStageSummarizing, "")
	if err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}
	if !updated {
		log.Printf("[Kafka] 任务状态已变化，跳过: taskID=%d", payload.TaskID)
		return nil
	}
	c.markTaskJobRunning(task, TaskJobAnalyze, model.TaskStageSummarizing)

	// 第 5 步：核心业务
	if err := c.processVideo(ctx, task); err != nil {
		if updateErr := c.recordTaskFailure(payload.TaskID, TaskJobAnalyze, "", err); updateErr != nil {
			return fmt.Errorf("任务失败且状态更新失败: %w", updateErr)
		}
		return nil
	}

	// 第 6 步：更新状态为已完成
	if err := c.repo.Task.UpdateStatusAndStage(payload.TaskID, model.TaskStatusCompleted, model.TaskStageNone, ""); err != nil {
		return fmt.Errorf("更新完成状态失败: %w", err)
	}
	c.markTaskJobCompleted(payload.TaskID, TaskJobAnalyze, model.TaskStageSummarizing)
	log.Printf("[Kafka] 任务完成: taskID=%d", payload.TaskID)
	return nil
}
```

**设计要点**：六步流程（解析 → 分布式锁 → 幂等校验 → 状态更新 → 业务处理 → 完成标记），保证 at-least-once 语义下的幂等性。

### 13. 重试调度器

```go
// internal/mq/retry.go:103-138
func (c *Consumer) recordTaskFailure(taskID int64, jobType, stage string, err error) error {
	task, findErr := c.repo.Task.FindByID(taskID)
	if findErr != nil {
		return findErr
	}
	if strings.TrimSpace(stage) == "" {
		stage = task.Stage
	}
	policy := c.retryPolicy.normalized()
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = policy.MaxRetries
	}

	errMsg := truncateError(err)
	if !isRetryableError(err) {
		if err := c.repo.Task.RecordTerminalFailure(taskID, jobType, stage, "non_retryable_error", errMsg, task.RetryCount, maxRetries, model.TaskStatusFailed); err != nil {
			return err
		}
		return c.repo.TaskJob.RecordTerminalFailure(taskID, jobType, stage, "non_retryable_error", errMsg, task.RetryCount, maxRetries, model.TaskStatusFailed)
	}

	nextRetryCount := task.RetryCount + 1
	if nextRetryCount > maxRetries {
		if err := c.repo.Task.RecordTerminalFailure(taskID, jobType, stage, "retry_exhausted", errMsg, nextRetryCount, maxRetries, model.TaskStatusDead); err != nil {
			return err
		}
		return c.repo.TaskJob.RecordTerminalFailure(taskID, jobType, stage, "retry_exhausted", errMsg, nextRetryCount, maxRetries, model.TaskStatusDead)
	}

	nextRetryAt := policy.Now().Add(policy.backoffForRetry(nextRetryCount))
	if err := c.repo.Task.RecordRetryableFailure(taskID, jobType, stage, errMsg, nextRetryCount, maxRetries, nextRetryAt); err != nil {
		return err
	}
	return c.repo.TaskJob.RecordRetryableFailure(taskID, jobType, stage, errMsg, nextRetryCount, maxRetries, nextRetryAt)
}
```

**设计要点**：区分可重试/不可重试错误，指数退避（60s → 300s → 900s），超过最大重试次数标记为 Dead。

---

## 调用链图

### 请求处理调用链

```
HTTP Request
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Gin Engine (cmd/server/main.go:231)                         │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ middleware.CORS() (internal/middleware/cors.go:11)   │    │
│  └─────────────────────────────────────────────────────┘    │
│                          │                                   │
│  ┌───────────────────────┴───────────────────────────────┐  │
│  │ /api/v1 (公开路由)                                     │  │
│  │  POST /user/register → UserHandler.Register           │  │
│  │  POST /user/login    → UserHandler.Login              │  │
│  └───────────────────────────────────────────────────────┘  │
│                          │                                   │
│  ┌───────────────────────┴───────────────────────────────┐  │
│  │ middleware.JWTAuth() (internal/middleware/auth.go:12)  │  │
│  └───────────────────────────────────────────────────────┘  │
│                          │                                   │
│  ┌───────────────────────┴───────────────────────────────┐  │
│  │ /api/v1 (认证路由)                                     │  │
│  │  /ai/profiles/* → AIProfileHandler                    │  │
│  │  /chat/*        → ChatHandler                         │  │
│  │  /media/*       → MediaHandler                        │  │
│  └───────────────────────────────────────────────────────┘  │
│                          │                                   │
│  ┌───────────────────────┴───────────────────────────────┐  │
│  │ middleware.RateLimit() (仅 AI 接口)                    │  │
│  │ (internal/middleware/ratelimit.go:108)                 │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 视频分析异步流程

```
用户上传视频
    │
    ▼
MediaHandler.UploadFile (internal/handler/media.go:23)
    │
    ▼
MediaService.UploadFile
    │
    ├──► MinIO Storage.UploadFile (internal/storage/minio.go:61)
    │
    ├──► Repository.Task.Create
    │
    └──► MediaHandler.RequestAnalysis (internal/handler/media.go:143)
            │
            ▼
        MediaService.RequestAnalysis
            │
            ├──► Repository.Task.UpdateStatusAndStage (→ Queued)
            │
            └──► Producer.EnqueueAnalyze (internal/mq/producer.go:77)
                    │
                    ▼ (异步)
                Kafka Topic: video-analyze
                    │
                    ▼
                Consumer.handleAnalyze (internal/mq/consumer.go:393)
                    │
                    ├──► RedisLock.TryLock (分布式锁)
                    │
                    ├──► Repository.Task.FindByID (幂等校验)
                    │
                    ├──► Consumer.processVideo (internal/mq/consumer.go:531)
                    │       │
                    │       ├──► Storage.DownloadToTemp (下载视频)
                    │       │
                    │       ├──► ffmpeg.ExtractAudio (提取音频)
                    │       │
                    │       ├──► ffmpeg.SplitAudio (音频切片)
                    │       │
                    │       ├──► strategy.Transcribe (ASR 转文字)
                    │       │
                    │       ├──► Repository.Transcription.Upsert
                    │       │
                    │       ├──► Producer.EnqueueRAGIndex (投递 RAG 索引)
                    │       │
                    │       └──► strategy.Summarize (AI 总结)
                    │
                    ├──► Repository.Task.UpdateStatusAndStage (→ Completed)
                    │
                    └──► Repository.TaskJob.MarkCompleted
```

### RAG 索引流程

```
转录完成后自动投递
    │
    ▼
Consumer.indexAfterTranscription (internal/mq/consumer.go:730)
    │
    └──► Producer.EnqueueRAGIndex
            │
            ▼
        Kafka Topic: video-rag-index
            │
            ▼
        Consumer.handleRAGIndex (internal/mq/consumer.go:338)
            │
            ├──► Repository.Transcription.FindByTaskID
            │
            ├──► ragIndex (service.RAGIndexService.BuildTaskIndex)
            │       │
            │       ├──► ChunkSplitter.Split (文本分块)
            │       │
            │       ├──► EmbeddingClient.Embed (向量化)
            │       │
            │       └──► MilvusStore.Insert (写入向量库)
            │
            └──► Repository.TaskJob.MarkCompleted
```

---

## 设计决策表

| 决策点 | 选择 | 理由 |
|--------|------|------|
| **Web 框架** | Gin | Go 生态最主流，性能好，中间件丰富 |
| **ORM** | GORM | 自动迁移、关联查询、Hook 机制完善 |
| **消息队列** | Kafka | Go 客户端成熟，消息持久化，分区支持水平扩展 |
| **对象存储** | MinIO | S3 兼容，本地部署，适合视频文件 |
| **向量数据库** | Milvus | 专业向量检索，支持大规模 ANN 查询 |
| **缓存** | Redis | 限流器令牌桶、聊天记忆存储、分布式锁 |
| **密码存储** | bcrypt | 抗暴力破解，cost=10 平衡安全与性能 |
| **API Key 加密** | AES-256-GCM | 认证加密（AEAD），防篡改，硬件加速 |
| **配置管理** | YAML + 环境变量 | 结构化配置，敏感信息外部注入 |
| **认证方式** | JWT | 无状态，适合前后端分离架构 |
| **限流算法** | 令牌桶 | 平滑限流，支持突发流量 |
| **限流维度** | 用户+路由 | 不同用户、不同接口独立计数 |
| **限流降级** | Fail-open | Redis 故障时放行，不成为单点故障 |
| **消息投递** | 同步发送 | 保证消息不丢失，延迟可控（<50ms） |
| **消费语义** | At-least-once | 手动 commit offset + 业务幂等校验 |
| **重试策略** | 指数退避 | 60s → 300s → 900s，区分可重试/不可重试错误 |
| **死信处理** | Dead 状态 | 超过最大重试次数标记为 Dead，需人工介入 |
| **分布式锁** | Redis SETNX | 防止同一视频被多个消费者同时处理 |
| **任务状态机** | 6 状态 | Pending → Queued → Running → Completed/Failed/Dead |
| **分片上传** | MD5 去重 | 相同文件秒传，分片支持断点续传 |
| **音频处理** | FFmpeg 分片 | 长音频切片后逐段 ASR，规避单次请求体限制 |
| **API 响应格式** | Code+Message+Data | 统一格式，前端友好，支持业务错误码扩展 |
| **SPA 路由** | NoRoute fallback | 未匹配路由返回 index.html，支持前端路由 |
