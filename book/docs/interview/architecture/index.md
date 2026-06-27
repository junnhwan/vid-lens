# VidLens 架构与启动流程 - 面试题

---

## 题目 1: 请描述 VidLens 的启动流程，main 函数中各组件的初始化顺序是什么？

### 参考答案

VidLens 采用自底向上的依赖注入方式启动，main 函数依次完成：配置加载 → 基础设施 → AI 策略 → 消息队列 → Repository → Service → Handler → Consumer → HTTP 路由。

```go
// cmd/server/main.go:52-101
func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 数据库
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		log.Fatalf("迁移数据库失败: %v", err)
	}
	log.Println("✅ 数据库连接成功")

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	log.Println("✅ Redis 连接成功")

	// MinIO
	minioStorage, err := storage.NewMinIOStorage(
		cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket, cfg.MinIO.UseSSL,
	)
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}
	log.Println("✅ MinIO 连接成功")

	// AI
	var aiStrategy ai.Strategy
	switch strings.ToLower(cfg.AI.Provider) {
	case "", "siliconflow":
		aiStrategy = ai.NewSiliconFlowStrategy(
			cfg.AI.SiliconFlowAPIKey, cfg.AI.SiliconFlowBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	case "mimo":
		aiStrategy = ai.NewMimoStrategy(
			cfg.AI.MimoAPIKey, cfg.AI.MimoBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	default:
		log.Fatalf("不支持的 AI provider: %s", cfg.AI.Provider)
	}
```

初始化顺序为：
1. **配置加载** (`config.Load`) - 读取 YAML 并展开环境变量
2. **MySQL** (`gorm.Open`) - 建立数据库连接并执行 AutoMigrate
3. **Redis** (`redis.NewClient`) - 创建缓存客户端
4. **MinIO** (`storage.NewMinIOStorage`) - 初始化对象存储
5. **AI 策略** (`ai.NewSiliconFlowStrategy` / `ai.NewMimoStrategy`) - 根据配置创建 AI 提供商
6. **Kafka** (`mq.CreateTopics` + `mq.NewProducer`) - 创建 Topic 和生产者
7. **Repository** (`repository.NewRepositories`) - 聚合所有数据访问层
8. **Codec** (`secret.NewCodecFromPassphrase`) - API Key 加密器
9. **Service 层** - 依次创建 UserService、AIProfileService、RAGIndexService、ChatService、MediaService
10. **Handler 层** - 依次创建 UserHandler、AIProfileHandler、RAGHandler、ChatHandler、MediaHandler
11. **Consumer** - 创建 4 个 Kafka 消费者 + RetryScheduler
12. **Gin 路由** - 注册 27 个 API 路由 + 静态文件

### 追问链

**追问 1.1: 为什么配置加载要放在最前面，而不是先初始化日志？**

因为 `config.Load` 内部使用 `os.ReadFile` 和 `os.ExpandEnv` 读取 YAML 文件并展开环境变量（`internal/config/config.go:149-163`）。配置中包含日志级别、端口等运行时参数，必须先加载配置才能正确初始化其他组件。当前项目使用 `log` 标准库，不需要额外配置。

**追问 1.2: model.Migrate 在启动时执行有什么风险？**

`model.Migrate` 调用 `db.AutoMigrate(AllModels()...)`（`internal/model/model.go:26-27`），在生产环境中执行 DDL 操作可能导致锁表。VidLens 额外处理了索引兼容性（`internal/model/model.go:31-42`），先删除旧索引再创建新索引，避免迁移失败。

**追问 1.3: 如果 Redis 连接失败，VidLens 会怎样？**

当前代码中 Redis 初始化没有错误检查（`cmd/server/main.go:69-74`），连接失败不会 fatal。Redis 主要用于限流器和聊天记忆存储，如果 Redis 不可用，限流器的 `Allow` 方法会 fail-open 放行（`internal/middleware/ratelimit.go:101-104`），聊天功能可能报错。

---

## 题目 2: VidLens 的配置加载机制是如何设计的？如何支持环境变量注入？

### 参考答案

配置加载采用 YAML 文件 + 环境变量展开的方式，通过 `os.ExpandEnv` 实现敏感信息的外部注入。

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

配置文件中可以写 `${VIDLENS_DB_PASSWORD}` 这样的占位符，`os.ExpandEnv` 会在解析 YAML 之前将其替换为对应的环境变量值。这样敏感信息（密码、API Key）不需要写入配置文件，适合容器化部署。

### 追问链

**追问 2.1: Config 结构体有 14 个子结构体，这种设计有什么优缺点？**

优点：按领域分组，语义清晰，每个子结构体可以独立序列化。缺点：嵌套层级深，YAML 配置文件需要对应嵌套结构。对比扁平化设计，这种方式在配置项增多时更易维护。

**追问 2.2: DatabaseConfig 的 DSN() 方法有什么设计考量？**

```go
// internal/config/config.go:42-45
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		d.Username, d.Password, d.Host, d.Port, d.DBName, d.Charset)
}
```

DSN 方法将分散的配置字段组装为 MySQL 连接字符串，硬编码了 `parseTime=True&loc=Local` 两个参数。`parseTime=True` 让 GORM 能正确解析 `datetime` 字段为 Go 的 `time.Time`，`loc=Local` 使用服务器本地时区。

**追问 2.3: 为什么配置加载不支持热更新？**

当前实现是一次性加载（`Load` 函数只在启动时调用），配置变更需要重启服务。对于 AI 平台项目，配置变更频率低（AI 模型、数据库地址等），热更新会增加复杂度且引入并发安全问题。

---

## 题目 3: VidLens 的路由分组设计体现了什么样的安全策略？

### 参考答案

路由分为公开路由和认证路由两组，认证路由进一步按功能域细分，并对高成本 AI 接口施加额外限流。

```go
// cmd/server/main.go:234-276
api := r.Group("/api/v1")
{
	api.POST("/user/register", userHandler.Register)    // 公开路由
	api.POST("/user/login", userHandler.Login)          // 公开路由

	auth := api.Group("")
	auth.Use(middleware.JWTAuth(cfg.JWT.Secret))      // JWT 中间件
	{
		auth.GET("/user/profile", userHandler.GetProfile)

		aiProfiles := auth.Group("/ai/profiles")
		{
			aiProfiles.GET("", aiProfileHandler.List)
			aiProfiles.POST("", aiProfileHandler.Create)
			aiProfiles.PUT("/:id", aiProfileHandler.Update)
			aiProfiles.DELETE("/:id", aiProfileHandler.Delete)
			aiProfiles.POST("/test", aiProfileHandler.Test)
		}

		chat := auth.Group("/chat")
		{
			chat.POST("/sessions", chatHandler.CreateSession)
			chat.GET("/sessions", chatHandler.ListSessions)
			chat.GET("/sessions/:session_id/messages", chatHandler.ListMessages)
			chat.POST("/sessions/:session_id/messages", middleware.RateLimit(rateLimiter), chatHandler.Ask)
			chat.POST("/sessions/:session_id/messages/stream", middleware.RateLimit(rateLimiter), chatHandler.AskStream)
		}

		media := auth.Group("/media")
		{
			media.POST("/upload", mediaHandler.UploadFile)
			media.POST("/upload-url", mediaHandler.UploadByURL)
			media.POST("/upload-chunk", mediaHandler.UploadChunk)
			media.GET("/check-upload", mediaHandler.CheckUpload)
			media.POST("/merge-chunks", mediaHandler.MergeChunks)
			media.GET("/list", mediaHandler.ListTasks)
			media.GET("/task/:id", mediaHandler.GetTaskDetail)
			media.DELETE("/task/:id", mediaHandler.DeleteTask)
			media.POST("/analyze/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestAnalysis)
			media.POST("/transcribe/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestTranscribe)
			media.GET("/task/:id/rag-index", ragHandler.GetTaskIndexStatus)
			media.POST("/task/:id/rag-index", middleware.RateLimit(rateLimiter), ragHandler.BuildTaskIndex)
			media.GET("/download-audio/:id", mediaHandler.DownloadAudio)
		}
	}
}
```

安全策略层次：
1. **全局层**：CORS 中间件（`middleware.CORS()`）处理跨域
2. **认证层**：`middleware.JWTAuth` 验证 JWT Token，将 userID/username/role 注入 Context
3. **限流层**：`middleware.RateLimit` 仅应用于 AI 接口（Ask、AskStream、Analyze、Transcribe、BuildTaskIndex）

### 追问链

**追问 3.1: 为什么只对部分接口施加限流，而不是全部？**

限流器基于 Redis 令牌桶实现（`internal/middleware/ratelimit.go:60-90`），每次请求都需要一次 Redis 调用。数据查询接口（ListTasks、GetTaskDetail）是轻量级的 MySQL 查询，不需要限流。AI 接口（聊天、分析、转录）涉及外部 API 调用和大量计算，成本高，必须限流保护后端资源。

**追问 3.2: JWTAuth 中间件是如何将用户信息传递给 Handler 的？**

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

通过 `c.Set("userID", claims.UserID)` 将解析后的用户信息存入 Gin Context，Handler 层通过 `middleware.GetUserID(c)` 获取。

**追问 3.3: 为什么没有实现角色鉴权（RBAC）？**

当前所有认证路由使用同一个 `auth` 组，没有按角色区分权限。User 模型有 `Role` 字段（`internal/model/user.go:16`，值为 USER/ADMIN），但中间件没有检查。这是一个设计取舍——VidLens 是面向普通用户的 AI 平台，所有用户权限相同，不需要 RBAC。

---

## 题目 4: Handler 层的依赖注入模式有什么特点？ChatHandler 和 MediaHandler 的依赖差异说明了什么？

### 参考答案

Handler 层采用构造函数注入，不同 Handler 的依赖数量不同，反映了业务复杂度的差异。

```go
// cmd/server/main.go:183-188
userHandler := handler.NewUserHandler(userSvc)
aiProfileHandler := handler.NewAIProfileHandler(aiProfileSvc)
ragHandler := handler.NewRAGHandler(ragIndexSvc, aiProfileSvc, aiFactory)
chatHandler := handler.NewChatHandler(chatSvc, aiProfileSvc, aiFactory)
mediaHandler := handler.NewMediaHandler(mediaSvc)
```

```go
// internal/handler/chat.go:13-21
type ChatHandler struct {
	chatSvc    *service.ChatService
	profileSvc *service.AIProfileService
	aiFactory  *ai.Factory
}

func NewChatHandler(chatSvc *service.ChatService, profileSvc *service.AIProfileService, aiFactory *ai.Factory) *ChatHandler {
	return &ChatHandler{chatSvc: chatSvc, profileSvc: profileSvc, aiFactory: aiFactory}
}
```

```go
// internal/handler/media.go:13-19
type MediaHandler struct {
	svc *service.MediaService
}

func NewMediaHandler(svc *service.MediaService) *MediaHandler {
	return &MediaHandler{svc: svc}
}
```

依赖差异说明：
- **UserHandler / MediaHandler**：只依赖一个 Service，业务简单（CRUD + 文件操作）
- **ChatHandler / RAGHandler**：依赖 3 个组件（chatSvc + profileSvc + aiFactory），因为聊天和 RAG 需要在请求时动态解析用户的 AI 配置并创建客户端

### 追问链

**追问 4.1: ChatHandler.Ask 方法中为什么需要 profileSvc 和 aiFactory？**

```go
// internal/handler/chat.go:68-107
func (h *ChatHandler) Ask(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sessionID <= 0 {
		response.BadRequest(c, "会话 ID 错误")
		return
	}

	var req struct {
		Question string `json:"question" binding:"required"`
		TopK     int    `json:"top_k"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	profile, err := h.profileSvc.GetDefaultAIProfile(userID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	embeddingClient, err := h.aiFactory.NewEmbeddingClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	chatClient, err := h.aiFactory.NewChatClient(*profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.chatSvc.Ask(c.Request.Context(), userID, sessionID, req.Question, req.TopK, embeddingClient, chatClient, *profile)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.OK(c, result)
}
```

因为每个用户可能配置了不同的 AI 提供商（SiliconFlow / MiMo / OpenAI Compatible），Handler 需要在请求时：1) 从 profileSvc 获取用户的默认 AI 配置；2) 用 aiFactory 创建对应的 embedding 和 chat 客户端；3) 将客户端传给 chatSvc 执行。

**追问 4.2: 为什么不把 AI 客户端创建逻辑下沉到 Service 层？**

这是一种设计权衡。如果将 aiFactory 注入到 ChatService，Service 层会依赖具体的 AI 实现细节。当前设计让 Handler 负责"编排"（解析配置、创建客户端），Service 负责"业务逻辑"（RAG 检索、对话管理），职责更清晰。

**追问 4.3: 为什么 MediaHandler 只需要一个 Service？**

MediaService 内部已经聚合了所有依赖：`repo`（数据库）、`storage`（MinIO）、`mq`（Kafka Producer）、`rdb`（Redis）。文件上传、分片合并、任务管理等操作不需要动态解析 AI 配置，所以 Handler 只需要一个 Service。

---

## 题目 5: VidLens 的统一响应结构是如何设计的？为什么 Code 字段和 HTTP 状态码重复？

### 参考答案

统一响应结构定义在 `internal/pkg/response/response.go`，提供了一致的 JSON 格式和语义化的错误分类。

```go
// internal/pkg/response/response.go:10-14
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
```

```go
// internal/pkg/response/response.go:24-31
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "success",
		Data:    data,
	})
}
```

```go
// internal/pkg/response/response.go:42-48
func Fail(c *gin.Context, httpCode int, msg string) {
	c.JSON(httpCode, Response{
		Code:    httpCode,
		Message: msg,
	})
}
```

```go
// internal/pkg/response/response.go:65-68
func TooManyRequests(c *gin.Context, msg string) {
	Fail(c, http.StatusTooManyRequests, msg)
}
```

Code 字段和 HTTP 状态码重复的设计原因：
1. **前端友好**：前端可以直接读 `response.code` 判断业务状态，不需要解析 HTTP 状态码
2. **扩展性**：业务错误码可以与 HTTP 状态码解耦（如 40001 表示 token 过期），但当前实现复用了 HTTP 状态码
3. **API 网关兼容**：某些网关会改写 HTTP 响应体，保留业务 Code 可以穿透网关

### 追问链

**追问 5.1: `data:"data,omitempty"` 的 omitempty 有什么作用？**

当 `Data` 为 `nil` 时，JSON 序列化会省略 `data` 字段，避免返回 `"data": null`。这在错误响应中很常见——失败时不需要返回数据字段，减少响应体大小。

**追问 5.2: 为什么没有定义业务错误码枚举？**

当前实现直接复用 HTTP 状态码作为 Code（200、400、401、403、404、429、500）。对于 VidLens 这种规模的项目，引入独立的业务错误码（如 10001、10002）会增加维护成本。如果未来需要国际化错误消息或多端统一错误码，可以扩展 Code 字段。

**追问 5.3: PageResult 结构体是如何使用的？**

```go
// internal/pkg/response/response.go:17-22
type PageResult struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}
```

PageResult 定义了分页响应的标准格式，但实际 Handler 中使用了 `gin.H` 直接构造（`internal/handler/media.go:204`），没有使用 PageResult。这说明 PageResult 可能是预留的，或者在其他地方使用。

---

## 题目 6: 限流器是如何实现按用户和按路由维度限流的？

### 参考答案

限流器基于 Redis + Lua 脚本实现令牌桶算法，支持全局默认配额 + 按路由覆盖。

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

```go
// internal/middleware/ratelimit.go:42-47
func (r *RateLimiter) SetRouteLimit(path string, capacity, rate int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[path] = routeLimit{capacity: capacity, rate: rate}
}
```

```go
// internal/middleware/ratelimit.go:60-90
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
```

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

维度设计：
- **按用户**：已登录用户使用 `path:user:userID` 作为 key，每个用户的限流配额独立
- **按路由**：`configFor(path)` 查找路由专属配额，未找到则使用全局默认
- **未登录用户**：使用 `path:ip:clientIP` 降级为 IP 维度

### 追问链

**追问 6.1: Lua 脚本为什么要在 Redis 中原子执行？**

令牌桶算法需要"读取当前令牌数 → 计算新令牌数 → 扣减令牌 → 写回"四步操作。如果用多条 Redis 命令，在高并发下会出现竞态条件（两个请求同时读到相同的令牌数）。Lua 脚本在 Redis 中原子执行，保证操作的线程安全。

**追问 6.2: 为什么 Redis 异常时选择 fail-open 放行？**

```go
// internal/middleware/ratelimit.go:94-106
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

限流是保护手段而非关键路径。如果 Redis 宕机时选择 fail-close（拒绝所有请求），会导致整个系统不可用，限流器反而成为单点故障。fail-open 策略允许在 Redis 短暂不可用时继续服务，优先保证可用性。

**追问 6.3: 为什么令牌桶的过期时间是 60 秒？**

`redis.call("EXPIRE", key, 60)` 设置 60 秒过期。这是一个安全兜底：如果用户长时间不活跃，令牌桶数据会自动清理，避免 Redis 内存泄漏。实际的令牌补充由 `elapsed / 1000 * rate` 计算，不依赖过期时间。

---

## 题目 7: API Key 加密是如何实现的？为什么选择 AES-GCM？

### 参考答案

VidLens 使用 AES-256-GCM 对用户的 AI API Key 进行加密存储，基于用户提供的 passphrase 派生密钥。

```go
// internal/pkg/secret/crypto.go:13-15
type Codec struct {
	aead cipher.AEAD
}
```

```go
// internal/pkg/secret/crypto.go:17-23
func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("api key secret is required")
	}
	sum := sha256.Sum256([]byte(passphrase))
	return newCodecFromKey(sum[:])
}
```

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

```go
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

选择 AES-GCM 的原因：
1. **认证加密（AEAD）**：GCM 模式同时提供加密和完整性校验，防止密文被篡改
2. **性能**：GCM 支持硬件加速（AES-NI），比 AES-CBC + HMAC 更快
3. **标准化**：GCM 是 NIST 推荐的 AEAD 模式，广泛用于 TLS 1.3

### 追问链

**追问 7.1: nonce 是如何管理的？会重复吗？**

每次加密都用 `crypto/rand.Reader` 生成随机 nonce（`internal/pkg/secret/crypto.go:48-50`），nonce 拼接在密文前面一起存储。GCM 的 nonce 是 12 字节，随机生成的碰撞概率极低（2^96 种可能），可以忽略。

**追问 7.2: 为什么用 SHA-256 派生密钥而不是 PBKDF2？**

```go
// internal/pkg/secret/crypto.go:17-23
func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("api key secret is required")
	}
	sum := sha256.Sum256([]byte(passphrase))
	return newCodecFromKey(sum[:])
}
```

SHA-256 的 32 字节输出恰好满足 AES-256 的密钥长度要求。这里 passphrase 是服务器配置中的密钥（不是用户密码），不需要抗暴力破解的慢哈希。PBKDF2 适用于用户密码场景，这里用 SHA-256 更简洁高效。

**追问 7.3: MaskAPIKey 函数有什么用途？**

```go
// internal/pkg/secret/crypto.go:79-87
func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:3] + "****" + key[len(key)-4:]
}
```

用于日志脱敏，显示为 `sk-****xyz` 格式。在 API 响应中返回已保存的 AI 配置时，API Key 应该被脱敏显示，防止泄露。

---

## 题目 8: Kafka Producer 的消息投递策略是什么？如何保证消息不丢失？

### 参考答案

Kafka Producer 采用同步发送 + 全 ISR 确认 + MD5 Key 路由的策略。

```go
// internal/mq/producer.go:39-44
type Producer struct {
	analyzeWriter    *kafka.Writer
	transcribeWriter *kafka.Writer
	downloadWriter   *kafka.Writer
	ragIndexWriter   *kafka.Writer
}
```

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
```

```go
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

消息不丢失的保障：
1. **同步发送**（`Async: false`）：`WriteMessages` 阻塞直到 Kafka 确认
2. **全 ISR 确认**（`RequiredAcks: kafka.RequireAll`）：等待所有同步副本写入
3. **重试机制**（`MaxAttempts: 3`）：发送失败自动重试 3 次
4. **MD5 Key 路由**：同一视频的消息进入同一分区，保证消费顺序

### 追问链

**追问 8.1: 为什么选择 LeastBytes 而不是 Hash 分区？**

`LeastBytes` 按负载均衡选择分区，但实际因为使用 MD5 作为 Key，Kafka 会先按 Key 哈希选择分区。LeastBytes 只在 Key 为空时生效。对于 Download 消息，使用 taskID 作为 Key，也会被哈希到固定分区。

**追问 8.2: 同步发送会增加接口延迟吗？**

会，但影响可控。`EnqueueAnalyze` 在 Handler 的 `RequestAnalysis` 中调用（`internal/handler/media.go:143-153`），投递后立即返回。Kafka 的写入延迟通常在 5-20ms，加上网络 RTT，总延迟在 50ms 以内。对比异步发送，同步发送更可靠，不会因为缓冲区满而丢消息。

**追问 8.3: CreateTopics 的 4 个分区是怎么分配的？**

```go
// internal/mq/producer.go:161-184
func CreateTopics(brokers []string, topics []string) error {
	conn, err := kafka.DialLeader(context.Background(), "tcp", brokers[0], topics[0], 0)
	if err != nil {
		// Topic 可能已存在，忽略错误
		return nil
	}
	conn.Close()

	for _, topic := range topics {
		topicConfig := kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     4, // 4 个分区，支持并行消费
			ReplicationFactor: 1, // 单机部署只有 1 个 broker
		}
		// 尝试创建，已存在会报错但不影响
		conn, err := kafka.Dial("tcp", brokers[0])
		if err == nil {
			conn.CreateTopics(topicConfig)
			conn.Close()
		}
	}
	return nil
}
```

每个 Topic 4 个分区，`ReplicationFactor: 1` 表示单副本（开发/测试环境）。生产环境应设置为 3 副本以保证高可用。

---

## 题目 9: Repository 层的聚合模式有什么优势？Transaction 方法是如何实现的？

### 参考答案

Repository 层采用聚合根模式，将所有数据访问层集中到一个 `Repositories` 结构体，并提供统一的事务管理。

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

```go
// internal/repository/repository.go:23-39
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
```

```go
// internal/repository/repository.go:41-45
func (r *Repositories) Transaction(fn func(*Repositories) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewRepositories(tx))
	})
}
```

优势：
1. **依赖简化**：Service 层只需注入一个 `*Repositories`，而不是 12 个独立的 Repository
2. **事务统一**：`Transaction` 方法传入新的 `Repositories`（使用事务 DB），所有操作在同一事务内
3. **可测试性**：Mock 时只需替换 `Repositories` 的字段

### 追问链

**追问 9.1: Transaction 方法中为什么用 NewRepositories(tx) 创建新的实例？**

`NewRepositories(tx)` 用事务 DB 创建新的 Repository 实例，确保事务内所有操作使用同一个数据库连接。如果直接用原来的 `Repositories`，部分操作可能绕过事务，导致数据不一致。

**追问 9.2: 为什么不使用接口定义 Repositories？**

Go 社区对于是否用接口包装 Repository 有争议。使用结构体的好处是简单直接，编译时类型检查。使用接口的好处是方便 Mock 测试。VidLens 选择了结构体方式，测试时通过替换字段或使用测试数据库来验证。

**追问 9.3: 12 个 Repository 的划分依据是什么？**

按数据库表划分，每个表对应一个 Repository（User、VideoTask、VideoAsset、ChatSession 等）。这遵循了单一职责原则——每个 Repository 只负责一张表的 CRUD 操作。VideoTask 和 TaskJob 是一对多关系，但因为业务逻辑不同（任务管理 vs 子任务调度），拆分为两个 Repository。

---

## 题目 10: VidLens 的中间件链执行顺序是什么？为什么 CORS 要放在全局？

### 参考答案

中间件链按洋葱模型执行，从外到内依次为：CORS → JWTAuth → RateLimit → Handler。

```go
// cmd/server/main.go:228-240
if cfg.Server.Mode == "release" {
	gin.SetMode(gin.ReleaseMode)
}
r := gin.Default()
r.Use(middleware.CORS())

api := r.Group("/api/v1")
{
	api.POST("/user/register", userHandler.Register)
	api.POST("/user/login", userHandler.Login)

	auth := api.Group("")
	auth.Use(middleware.JWTAuth(cfg.JWT.Secret))
	{
		// ... 认证路由
	}
}
```

```go
// internal/middleware/cors.go:11-19
func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:   []string{"Content-Length"},
		MaxAge:          12 * time.Hour,
	})
}
```

CORS 放在全局的原因：
1. **预检请求**：浏览器会先发 OPTIONS 请求，如果不全局处理，认证路由会拒绝 OPTIONS（因为没有 JWT Token）
2. **公开路由也需要 CORS**：`/api/v1/user/register` 和 `/api/v1/user/login` 是公开路由，跨域请求也需要 CORS 头
3. **MaxAge 缓存**：`MaxAge: 12 * time.Hour` 让浏览器缓存预检结果 12 小时，减少 OPTIONS 请求

### 追问链

**追问 10.1: AllowAllOrigins: true 在生产环境安全吗？**

不安全。这允许任何域名的请求，适合开发环境。生产环境应该配置具体的域名白名单。VidLens 当前的 CORS 配置是开发友好的默认值。

**追问 10.2: gin.Default() 和 gin.New() 有什么区别？**

`gin.Default()` 自带 Logger 和 Recovery 中间件，`gin.New()` 是空引擎。Logger 记录请求日志，Recovery 捕获 panic 防止服务崩溃。VidLens 使用 `gin.Default()` 获得开箱即用的日志和错误恢复。

**追问 10.3: 如果 JWTAuth 中间件调用 c.Abort()，后续的中间件还会执行吗？**

不会。`c.Abort()` 中止当前请求的后续处理链。如果 JWT 校验失败，`c.Abort()` 会阻止 Handler 执行，但已经注册的 Response 中间件（如 Logger）仍会在响应完成后执行，记录这次请求的日志。
