package config

import "fmt"

// Config 全局配置结构体
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Database     DatabaseConfig     `yaml:"database"`
	LegacyMySQL  LegacyMySQLConfig  `yaml:"legacy_mysql"`
	Redis        RedisConfig        `yaml:"redis"`
	MinIO        MinIOConfig        `yaml:"minio"`
	Kafka        KafkaConfig        `yaml:"kafka"`
	AI           AIConfig           `yaml:"ai"`
	Tools        ToolsConfig        `yaml:"tools"`
	JWT          JWTConfig          `yaml:"jwt"`
	Security     SecurityConfig     `yaml:"security"`
	Upload       UploadConfig       `yaml:"upload"`
	TaskRetry    TaskRetryConfig    `yaml:"task_retry"`
	Cleanup      CleanupConfig      `yaml:"cleanup"`
	RateLimit    RateLimitConfig    `yaml:"ratelimit"`
	RAG          RAGConfig          `yaml:"rag"`
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
// topic fields are omitted from YAML.
const (
	DefaultKafkaDownloadTopic = "video-download"
	DefaultKafkaRAGIndexTopic = "video-rag-index"
)

type KafkaConfig struct {
	Brokers         []string `yaml:"brokers"`
	AnalyzeTopic    string   `yaml:"analyze_topic"`
	TranscribeTopic string   `yaml:"transcribe_topic"`
	DownloadTopic   string   `yaml:"download_topic"`
	RAGIndexTopic   string   `yaml:"rag_index_topic"`
	ConsumerGroup   string   `yaml:"consumer_group"`
}

func (k *KafkaConfig) applyDefaults() {
	if k.DownloadTopic == "" {
		k.DownloadTopic = DefaultKafkaDownloadTopic
	}
	if k.RAGIndexTopic == "" {
		k.RAGIndexTopic = DefaultKafkaRAGIndexTopic
	}
}

type AIConfig struct {
	Provider           string `yaml:"provider"`
	SiliconFlowAPIKey  string `yaml:"siliconflow_api_key"`
	SiliconFlowBaseURL string `yaml:"siliconflow_base_url"`
	MimoAPIKey         string `yaml:"mimo_api_key"`
	MimoBaseURL        string `yaml:"mimo_base_url"`
	ASRModel           string `yaml:"asr_model"`
	LLMModel           string `yaml:"llm_model"`
}

type ToolsConfig struct {
	FFmpegPath        string   `yaml:"ffmpeg_path"`
	YtDlpPath         string   `yaml:"ytdlp_path"`
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
	Enabled      bool    `yaml:"enabled"`
	Store        string  `yaml:"store"`
	ChunkSize    int     `yaml:"chunk_size"`
	ChunkOverlap int     `yaml:"chunk_overlap"`
	TopK         int     `yaml:"top_k"`
	CandidateK   int     `yaml:"candidate_k"`
	MinScore     float32 `yaml:"min_score"`
	RecentTurns  int     `yaml:"recent_turns"`
	EmbeddingDim int     `yaml:"embedding_dim"`
	VectorTable  string  `yaml:"vector_table"`
}

type MilvusConfig struct {
	Address    string `yaml:"address"`
	Collection string `yaml:"collection"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Token      string `yaml:"token"`
	Database   string `yaml:"database"`
}
