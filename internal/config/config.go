package config

import "fmt"

// Config 全局配置结构体
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Database     DatabaseConfig     `yaml:"database"`
	LegacyMySQL  LegacyMySQLConfig  `yaml:"legacy_mysql"`
	Redis        RedisConfig        `yaml:"redis"`
	MinIO        MinIOConfig        `yaml:"minio"`
	MQ           MQConfig           `yaml:"mq"`
	AI           AIConfig           `yaml:"ai"`
	Tools        ToolsConfig        `yaml:"tools"`
	JWT          JWTConfig          `yaml:"jwt"`
	Security     SecurityConfig     `yaml:"security"`
	Upload       UploadConfig       `yaml:"upload"`
	TaskRetry    TaskRetryConfig    `yaml:"task_retry"`
	Cleanup      CleanupConfig      `yaml:"cleanup"`
	RateLimit    RateLimitConfig    `yaml:"ratelimit"`
	RAG          RAGConfig          `yaml:"rag"`
	Memory       MemoryConfig       `yaml:"memory"`
	Milvus       MilvusConfig       `yaml:"milvus"`
	AIGovernance AIGovernanceConfig `yaml:"-"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

// DatabaseConfig is the single application database. PostgreSQL stores both
// relational business data and pgvector embeddings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

// LegacyMySQLConfig is retained temporarily for the offline migration and
// rollback-period audit tool. The API server never reads this configuration.
type LegacyMySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	Charset  string `yaml:"charset"`
}

func (d *LegacyMySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		d.Username, d.Password, d.Host, d.Port, d.DBName, d.Charset)
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type MinIOConfig struct {
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
}

// Defaults shared by the server and maintenance commands when the optional
// queue fields are omitted from YAML.
const (
	DefaultMQDownloadQueue = "video-download"
	DefaultMQRAGIndexQueue = "video-rag-index"
	DefaultASRConcurrency  = 3
	DefaultASRMaxRetries   = 2
	MaxASRConcurrency      = 16
	MaxASRMaxRetries       = 5
)

// MQConfig is the RabbitMQ-backed task queue configuration. VidLens uses MQ
// for reliable task dispatch (durable dispatch lease handoff), failure
// recovery (ack redelivery + dead-letter), and peak shaving (ASR quota is
// scarce), not high-throughput log aggregation — which is why RabbitMQ was
// chosen over Kafka. See docs/architecture/reliability.md.
type MQConfig struct {
	Brokers         []string `yaml:"brokers"`
	AnalyzeQueue    string   `yaml:"analyze_queue"`
	TranscribeQueue string   `yaml:"transcribe_queue"`
	DownloadQueue   string   `yaml:"download_queue"`
	RAGIndexQueue   string   `yaml:"rag_index_queue"`
	ConsumerGroup   string   `yaml:"consumer_group"`
	// Prefetch caps buffered unacked deliveries. Each queue handler remains
	// serial; ASRConcurrency controls bounded fan-out inside one video task.
	Prefetch int `yaml:"prefetch"`
	// ASRConcurrency bounds per-task provider calls. Provider admission and the
	// shared retry budget still apply independently to every attempt.
	ASRConcurrency    int   `yaml:"asr_concurrency"`
	ASRMaxRetries     int   `yaml:"asr_max_retries"`
	ASRRetryBackoffMS []int `yaml:"asr_retry_backoff_ms"`
}

func (m *MQConfig) applyDefaults() {
	if m.DownloadQueue == "" {
		m.DownloadQueue = DefaultMQDownloadQueue
	}
	if m.RAGIndexQueue == "" {
		m.RAGIndexQueue = DefaultMQRAGIndexQueue
	}
	if m.Prefetch <= 0 {
		m.Prefetch = 1
	}
	if m.ASRConcurrency <= 0 {
		m.ASRConcurrency = DefaultASRConcurrency
	}
	if len(m.ASRRetryBackoffMS) == 0 {
		m.ASRRetryBackoffMS = []int{1000, 3000}
	}
}

type AIConfig struct {
	// Provider is retained as a compatibility/observability label. New
	// deployments should prefer capability-specific fields below. The shared
	// fields remain a fallback for simple/single-relay deployments.
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`

	LLMProvider string `yaml:"llm_provider"`
	LLMBaseURL  string `yaml:"llm_base_url"`
	LLMAPIKey   string `yaml:"llm_api_key"`
	LLMModel    string `yaml:"llm_model"`

	ASRProvider string `yaml:"asr_provider"`
	ASRBaseURL  string `yaml:"asr_base_url"`
	ASRAPIKey   string `yaml:"asr_api_key"`
	ASRModel    string `yaml:"asr_model"`

	EmbeddingProvider string `yaml:"embedding_provider"`
	EmbeddingEndpoint string `yaml:"embedding_endpoint"`
	EmbeddingAPIKey   string `yaml:"embedding_api_key"`
	EmbeddingModel    string `yaml:"embedding_model"`
	EmbeddingDim      int    `yaml:"embedding_dim"`

	RerankProvider string `yaml:"rerank_provider"`
	RerankEndpoint string `yaml:"rerank_endpoint"`
	RerankAPIKey   string `yaml:"rerank_api_key"`
	RerankModel    string `yaml:"rerank_model"`

	VisionProvider string `yaml:"vision_provider"`
	VisionBaseURL  string `yaml:"vision_base_url"`
	VisionAPIKey   string `yaml:"vision_api_key"`
	VisionModel    string `yaml:"vision_model"`

	// Legacy vendor-specific fields. They are read only when the resolved
	// provider is the corresponding legacy label or during migration fallback.
	SiliconFlowAPIKey  string `yaml:"siliconflow_api_key"`
	SiliconFlowBaseURL string `yaml:"siliconflow_base_url"`
	MimoAPIKey         string `yaml:"mimo_api_key"`
	MimoBaseURL        string `yaml:"mimo_base_url"`
}

type ToolsConfig struct {
	FFmpegPath        string   `yaml:"ffmpeg_path"`
	YtDlpPath         string   `yaml:"ytdlp_path"`
	OCRPath           string   `yaml:"ocr_path"`
	OCRLang           string   `yaml:"ocr_lang"`
	CookiesPath       string   `yaml:"cookies_path"`
	ProxyURL          string   `yaml:"proxy_url"`
	AllowedVideoHosts []string `yaml:"allowed_video_hosts"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type SecurityConfig struct {
	APIKeySecret string `yaml:"api_key_secret"`
}

type UploadConfig struct {
	MaxFileSize int64 `yaml:"max_file_size"`
	ChunkSize   int64 `yaml:"chunk_size"`
}

type TaskRetryConfig struct {
	MaxRetries          int   `yaml:"max_retries"`
	BackoffSeconds      []int `yaml:"backoff_seconds"`
	ScanIntervalSeconds int   `yaml:"scan_interval_seconds"`
	BatchSize           int   `yaml:"batch_size"`
}

// CleanupConfig controls durable task-resource cleanup independently from
// Kafka business-task retries. Keeping the policies separate avoids coupling
// media processing retry semantics to MinIO/Redis/vector cleanup recovery.
type CleanupConfig struct {
	ScanIntervalSeconds int `yaml:"scan_interval_seconds"`
	BatchSize           int `yaml:"batch_size"`
	LeaseSeconds        int `yaml:"lease_seconds"`
	RetryBackoffSeconds int `yaml:"retry_backoff_seconds"`
}

type RateLimitConfig struct {
	Capacity int `yaml:"capacity"`
	Rate     int `yaml:"rate"`
	// Routes 为指定路由单独配置令牌桶配额，覆盖全局 Capacity/Rate。
	// key 为 Gin 路由模板（c.FullPath() 形式，如 /api/v1/chat/sessions/:session_id/messages），
	// 用于对高成本 AI 接口施加更严格的限额。
	Routes map[string]RouteRateLimit `yaml:"routes"`
}

// RouteRateLimit 单个路由的专属限流配额
type RouteRateLimit struct {
	Capacity int `yaml:"capacity"`
	Rate     int `yaml:"rate"`
}

// RAGConfig controls indexing and retrieval. Store defaults to pgvector; use
// an explicit "milvus" value only during the temporary rollback window.
type RAGConfig struct {
	Enabled        bool    `yaml:"enabled"`
	Store          string  `yaml:"store"`
	ChunkSize      int     `yaml:"chunk_size"`
	ChunkOverlap   int     `yaml:"chunk_overlap"`
	TopK           int     `yaml:"top_k"`
	CandidateK     int     `yaml:"candidate_k"`
	MinScore       float32 `yaml:"min_score"`
	RecentTurns    int     `yaml:"recent_turns"`
	EmbeddingDim   int     `yaml:"embedding_dim"`
	VectorTable    string  `yaml:"vector_table"`
	RerankModel    string  `yaml:"rerank_model"`
	RewriteQueries int     `yaml:"rewrite_queries"`
}

// MemoryConfig controls the optional Agent-only long-term memory slice. It is
// disabled by default so ordinary RAG and existing Agent behavior remain
// unchanged unless an operator opts in.
type MemoryConfig struct {
	Enabled   bool `yaml:"enabled"`
	TopK      int  `yaml:"top_k"`
	MaxChars  int  `yaml:"max_chars"`
	MaxTokens int  `yaml:"max_tokens"`
	QueueSize int  `yaml:"queue_size"`
}

func (m *MemoryConfig) applyDefaults() {
	if m.TopK <= 0 {
		m.TopK = 6
	}
	if m.MaxChars <= 0 {
		m.MaxChars = 2000
	}
	if m.MaxTokens <= 0 {
		m.MaxTokens = 500
	}
	if m.QueueSize <= 0 {
		m.QueueSize = 128
	}
}

type MilvusConfig struct {
	Address    string `yaml:"address"`
	Collection string `yaml:"collection"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Token      string `yaml:"token"`
	Database   string `yaml:"database"`
}
