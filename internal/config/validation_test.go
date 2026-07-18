package config

import (
	"strings"
	"testing"
)

func validServerConfig() Config {
	return Config{
		Server:   ServerConfig{Port: 8080, Mode: "debug"},
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 5432, Username: "vidlens", DBName: "vidlens"},
		Redis:    RedisConfig{Host: "127.0.0.1", Port: 6379, DB: 0},
		MinIO:    MinIOConfig{Endpoint: "127.0.0.1:9000", Bucket: "vidlens"},
		Kafka: KafkaConfig{
			Brokers: []string{"127.0.0.1:9092"}, AnalyzeTopic: "video-analyze", TranscribeTopic: "video-transcribe",
			DownloadTopic: "video-download", RAGIndexTopic: "video-rag-index", ConsumerGroup: "vidlens-worker",
		},
		AI:        AIConfig{Provider: "mimo"},
		JWT:       JWTConfig{Secret: "test-secret", ExpireHours: 72},
		Upload:    UploadConfig{MaxFileSize: 2 << 30, ChunkSize: 5 << 20},
		TaskRetry: TaskRetryConfig{MaxRetries: 3, BackoffSeconds: []int{60, 300, 900}, ScanIntervalSeconds: 30, BatchSize: 20},
		Cleanup:   CleanupConfig{ScanIntervalSeconds: 30, BatchSize: 20, LeaseSeconds: 120, RetryBackoffSeconds: 60},
		RateLimit: RateLimitConfig{Capacity: 10, Rate: 10},
	}
}

func TestValidateServerAcceptsCompleteConfigurationWithoutRAG(t *testing.T) {
	cfg := validServerConfig()

	if err := cfg.ValidateServer(); err != nil {
		t.Fatalf("ValidateServer() error = %v", err)
	}
}

func TestValidateServerDoesNotRequireLegacyMySQL(t *testing.T) {
	cfg := validServerConfig()
	cfg.LegacyMySQL = LegacyMySQLConfig{}

	if err := cfg.ValidateServer(); err != nil {
		t.Fatalf("ValidateServer() error = %v, want PostgreSQL-only server config to pass", err)
	}
}

func TestValidateServerReportsInvalidPostgresFields(t *testing.T) {
	cfg := validServerConfig()
	cfg.Database.Host = ""
	cfg.Database.Port = 0
	cfg.Database.DBName = ""

	err := cfg.ValidateServer()
	if err == nil {
		t.Fatal("ValidateServer() error = nil, want invalid PostgreSQL configuration error")
	}
	for _, field := range []string{"database.host", "database.port", "database.dbname"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("ValidateServer() error %q does not mention %s", err, field)
		}
	}
}

func TestValidateMySQLRemainsAvailableForMigrationTools(t *testing.T) {
	cfg := validServerConfig()
	cfg.LegacyMySQL = LegacyMySQLConfig{Host: "127.0.0.1", Port: 3306, Username: "vidlens", DBName: "vidlens"}

	if err := cfg.ValidateMySQL(); err != nil {
		t.Fatalf("ValidateMySQL() error = %v", err)
	}
}

func TestValidateServerReportsInvalidCoreFields(t *testing.T) {
	cfg := validServerConfig()
	cfg.Server.Port = 70000
	cfg.Kafka.Brokers = []string{"", "127.0.0.1:9092"}
	cfg.JWT.Secret = ""

	err := cfg.ValidateServer()
	if err == nil {
		t.Fatal("ValidateServer() error = nil, want invalid configuration error")
	}
	for _, field := range []string{"server.port", "kafka.brokers[0]", "jwt.secret"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("ValidateServer() error %q does not mention %s", err, field)
		}
	}
}

func TestValidateServerValidatesEnabledPGVectorConfiguration(t *testing.T) {
	cfg := validServerConfig()
	cfg.RAG = RAGConfig{
		Enabled: true, Store: "pgvector", ChunkSize: 800, ChunkOverlap: 120,
		TopK: 5, CandidateK: 30, MinScore: 0.35, RecentTurns: 8, EmbeddingDim: 1536,
	}
	cfg.Database = DatabaseConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", DBName: "vidlens"}

	if err := cfg.ValidateServer(); err != nil {
		t.Fatalf("ValidateServer() error = %v", err)
	}

	cfg.Database.Host = ""
	if err := cfg.ValidateServer(); err == nil || !strings.Contains(err.Error(), "database.host") {
		t.Fatalf("ValidateServer() error = %v, want database.host validation", err)
	}
}

func TestValidateRAGEmptyStoreUsesPGVectorDefault(t *testing.T) {
	cfg := validServerConfig()
	cfg.RAG = RAGConfig{
		Enabled: true, ChunkSize: 800, ChunkOverlap: 120,
		TopK: 5, CandidateK: 30, MinScore: 0.35, RecentTurns: 8, EmbeddingDim: 1536,
	}
	cfg.Milvus = MilvusConfig{}

	if err := cfg.ValidateRAG(); err != nil {
		t.Fatalf("ValidateRAG() error = %v, want empty store to use PostgreSQL/pgvector", err)
	}
}

func TestValidateRAGAllowsMilvusRollbackWithoutPostgres(t *testing.T) {
	cfg := validServerConfig()
	cfg.RAG = RAGConfig{
		Enabled: true, Store: "milvus", ChunkSize: 800, ChunkOverlap: 120,
		TopK: 5, CandidateK: 30, MinScore: 0.35, RecentTurns: 8, EmbeddingDim: 1536,
	}
	cfg.Milvus.Address = "127.0.0.1:19530"

	if err := cfg.ValidateRAG(); err != nil {
		t.Fatalf("ValidateRAG() error = %v", err)
	}
}

func TestValidatePGVectorDestinationAllowsDefaultTableName(t *testing.T) {
	cfg := Config{
		RAG:      RAGConfig{EmbeddingDim: 1536},
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", DBName: "vidlens"},
	}

	if err := cfg.ValidatePGVectorDestination(); err != nil {
		t.Fatalf("ValidatePGVectorDestination() error = %v", err)
	}
}

func TestValidatePGVectorDestinationRejectsInvalidDimension(t *testing.T) {
	cfg := Config{
		RAG:      RAGConfig{EmbeddingDim: 0},
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", DBName: "vidlens"},
	}

	err := cfg.ValidatePGVectorDestination()
	if err == nil || !strings.Contains(err.Error(), "rag.embedding_dim") {
		t.Fatalf("ValidatePGVectorDestination() error = %v, want rag.embedding_dim validation", err)
	}
}

func TestValidateRAGReportsSharedDimensionErrorOnce(t *testing.T) {
	cfg := Config{
		RAG: RAGConfig{
			Store: "pgvector", ChunkSize: 800, ChunkOverlap: 120,
			TopK: 5, CandidateK: 30, EmbeddingDim: 0,
		},
		Database: DatabaseConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", DBName: "vidlens"},
	}

	err := cfg.ValidateRAG()
	if err == nil {
		t.Fatal("ValidateRAG() error = nil, want rag.embedding_dim validation")
	}
	if got := strings.Count(err.Error(), "rag.embedding_dim"); got != 1 {
		t.Fatalf("rag.embedding_dim error count = %d, want 1: %v", got, err)
	}
}

func TestValidateServerRejectsInvalidCleanupTiming(t *testing.T) {
	fields := []struct {
		name string
		set  func(*CleanupConfig)
		want string
	}{
		{name: "scan interval", set: func(c *CleanupConfig) { c.ScanIntervalSeconds = 0 }, want: "cleanup.scan_interval_seconds"},
		{name: "batch size", set: func(c *CleanupConfig) { c.BatchSize = 0 }, want: "cleanup.batch_size"},
		{name: "lease", set: func(c *CleanupConfig) { c.LeaseSeconds = 0 }, want: "cleanup.lease_seconds"},
		{name: "retry backoff", set: func(c *CleanupConfig) { c.RetryBackoffSeconds = 0 }, want: "cleanup.retry_backoff_seconds"},
	}
	for _, tt := range fields {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validServerConfig()
			tt.set(&cfg.Cleanup)
			err := cfg.ValidateServer()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateServer() error = %v, want %s", err, tt.want)
			}
		})
	}
}
