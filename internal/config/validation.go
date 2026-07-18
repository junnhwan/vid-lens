package config

import (
	"fmt"
	"strings"
)

type validationErrors struct {
	issues []string
}

func (v *validationErrors) add(field, message string) {
	v.issues = append(v.issues, fmt.Sprintf("%s: %s", field, message))
}

func (v *validationErrors) require(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.add(field, "不能为空")
	}
}

func (v *validationErrors) port(field string, value int) {
	if value < 1 || value > 65535 {
		v.add(field, fmt.Sprintf("必须在 1..65535 之间，当前为 %d", value))
	}
}

func (v *validationErrors) merge(err error) {
	if err == nil {
		return
	}
	if nested, ok := err.(*validationErrors); ok {
		v.issues = append(v.issues, nested.issues...)
		return
	}
	v.issues = append(v.issues, err.Error())
}

func (v *validationErrors) err() error {
	if len(v.issues) == 0 {
		return nil
	}
	return v
}

func (v *validationErrors) Error() string {
	return "配置校验失败: " + strings.Join(v.issues, "; ")
}

// ValidateServer checks configuration required by the long-running API and
// worker process. Load intentionally does not call this method because the
// maintenance commands support narrower configurations and dry-run modes.
func (c *Config) ValidateServer() error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}

	var problems validationErrors
	problems.port("server.port", c.Server.Port)
	switch strings.ToLower(strings.TrimSpace(c.Server.Mode)) {
	case "", "debug", "release":
	default:
		problems.add("server.mode", "仅支持 debug 或 release")
	}

	problems.merge(c.ValidatePostgres())
	problems.require("redis.host", c.Redis.Host)
	problems.port("redis.port", c.Redis.Port)
	if c.Redis.DB < 0 {
		problems.add("redis.db", "不能为负数")
	}

	problems.require("minio.endpoint", c.MinIO.Endpoint)
	problems.require("minio.bucket", c.MinIO.Bucket)

	if len(c.Kafka.Brokers) == 0 {
		problems.add("kafka.brokers", "至少需要一个 broker")
	}
	for i, broker := range c.Kafka.Brokers {
		problems.require(fmt.Sprintf("kafka.brokers[%d]", i), broker)
	}
	problems.require("kafka.analyze_topic", c.Kafka.AnalyzeTopic)
	problems.require("kafka.transcribe_topic", c.Kafka.TranscribeTopic)
	problems.require("kafka.download_topic", c.Kafka.DownloadTopic)
	problems.require("kafka.rag_index_topic", c.Kafka.RAGIndexTopic)
	problems.require("kafka.consumer_group", c.Kafka.ConsumerGroup)

	problems.require("jwt.secret", c.JWT.Secret)
	if c.JWT.ExpireHours <= 0 {
		problems.add("jwt.expire_hours", "必须为正数")
	}
	if c.Upload.MaxFileSize <= 0 {
		problems.add("upload.max_file_size", "必须为正数")
	}
	if c.Upload.ChunkSize <= 0 {
		problems.add("upload.chunk_size", "必须为正数")
	} else if c.Upload.MaxFileSize > 0 && c.Upload.ChunkSize > c.Upload.MaxFileSize {
		problems.add("upload.chunk_size", "不能大于 upload.max_file_size")
	}

	if c.TaskRetry.MaxRetries < 0 {
		problems.add("task_retry.max_retries", "不能为负数")
	}
	if c.TaskRetry.ScanIntervalSeconds <= 0 {
		problems.add("task_retry.scan_interval_seconds", "必须为正数")
	}
	if c.TaskRetry.BatchSize <= 0 {
		problems.add("task_retry.batch_size", "必须为正数")
	}
	for i, seconds := range c.TaskRetry.BackoffSeconds {
		if seconds <= 0 {
			problems.add(fmt.Sprintf("task_retry.backoff_seconds[%d]", i), "必须为正数")
		}
	}

	if c.Cleanup.ScanIntervalSeconds <= 0 {
		problems.add("cleanup.scan_interval_seconds", "必须为正数")
	}
	if c.Cleanup.BatchSize <= 0 {
		problems.add("cleanup.batch_size", "必须为正数")
	}
	if c.Cleanup.LeaseSeconds <= 0 {
		problems.add("cleanup.lease_seconds", "必须为正数")
	}
	if c.Cleanup.RetryBackoffSeconds <= 0 {
		problems.add("cleanup.retry_backoff_seconds", "必须为正数")
	}

	if c.RateLimit.Capacity <= 0 {
		problems.add("ratelimit.capacity", "必须为正数")
	}
	if c.RateLimit.Rate <= 0 {
		problems.add("ratelimit.rate", "必须为正数")
	}
	for route, limit := range c.RateLimit.Routes {
		route = strings.TrimSpace(route)
		if route == "" {
			problems.add("ratelimit.routes", "路由键不能为空")
		}
		if limit.Capacity <= 0 {
			problems.add(fmt.Sprintf("ratelimit.routes[%q].capacity", route), "必须为正数")
		}
		if limit.Rate <= 0 {
			problems.add(fmt.Sprintf("ratelimit.routes[%q].rate", route), "必须为正数")
		}
	}

	switch strings.ToLower(strings.TrimSpace(c.AI.Provider)) {
	case "", "siliconflow", "mimo":
	default:
		problems.add("ai.provider", "仅支持 siliconflow 或 mimo")
	}

	if c.RAG.Enabled {
		problems.merge(c.ValidateRAG())
	}
	return problems.err()
}

// ValidatePostgres checks the PostgreSQL fields required by the long-running
// application. PostgreSQL owns both business tables and pgvector data.
func (c *Config) ValidatePostgres() error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}
	var problems validationErrors
	problems.require("database.host", c.Database.Host)
	problems.port("database.port", c.Database.Port)
	problems.require("database.username", c.Database.Username)
	problems.require("database.dbname", c.Database.DBName)
	return problems.err()
}

// ValidateMySQL checks the legacy MySQL source fields used only by migration
// and rollback-period audit commands. It does not try to connect to MySQL.
func (c *Config) ValidateMySQL() error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}
	var problems validationErrors
	problems.require("legacy_mysql.host", c.LegacyMySQL.Host)
	problems.port("legacy_mysql.port", c.LegacyMySQL.Port)
	problems.require("legacy_mysql.username", c.LegacyMySQL.Username)
	problems.require("legacy_mysql.dbname", c.LegacyMySQL.DBName)
	return problems.err()
}

// ValidateRAG validates the configured vector backend and retrieval/indexing
// parameters. Callers decide whether a disabled RAG capability should be
// skipped; maintenance tools may still need to inspect a disabled backend.
func (c *Config) ValidateRAG() error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}

	var problems validationErrors
	if c.RAG.ChunkSize <= 0 {
		problems.add("rag.chunk_size", "必须为正数")
	}
	if c.RAG.ChunkOverlap < 0 {
		problems.add("rag.chunk_overlap", "不能为负数")
	} else if c.RAG.ChunkSize > 0 && c.RAG.ChunkOverlap >= c.RAG.ChunkSize {
		problems.add("rag.chunk_overlap", "必须小于 rag.chunk_size")
	}
	if c.RAG.TopK <= 0 {
		problems.add("rag.top_k", "必须为正数")
	}
	if c.RAG.CandidateK <= 0 {
		problems.add("rag.candidate_k", "必须为正数")
	} else if c.RAG.TopK > 0 && c.RAG.CandidateK < c.RAG.TopK {
		problems.add("rag.candidate_k", "不能小于 rag.top_k")
	}
	if c.RAG.MinScore < -1 || c.RAG.MinScore > 1 {
		problems.add("rag.min_score", "必须在 -1..1 之间")
	}
	if c.RAG.RecentTurns < 0 {
		problems.add("rag.recent_turns", "不能为负数")
	}
	problems.merge(c.ValidateVectorBackend())
	return problems.err()
}

// ValidateVectorBackend validates only the selected vector store connection
// fields. Read-only projection audits use this narrower boundary and do not
// depend on indexing or retrieval tuning parameters.
func (c *Config) ValidateVectorBackend() error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}

	var problems validationErrors
	if c.RAG.EmbeddingDim <= 0 {
		problems.add("rag.embedding_dim", "必须为正数")
	}
	switch strings.ToLower(strings.TrimSpace(c.RAG.Store)) {
	case "", "pgvector":
		problems.merge(c.validatePGVectorDestination(false))
	case "milvus":
		problems.require("milvus.address", c.Milvus.Address)
	default:
		problems.add("rag.store", "仅支持 milvus 或 pgvector")
	}
	return problems.err()
}

// ValidatePGVectorDestination checks the planned pgvector destination used by
// rag-reindex in both dry-run and execute modes. table_name may be empty because
// the backend owns its default.
func (c *Config) ValidatePGVectorDestination() error {
	return c.validatePGVectorDestination(true)
}

func (c *Config) validatePGVectorDestination(checkDimension bool) error {
	if c == nil {
		return fmt.Errorf("配置校验失败: config: 不能为空")
	}

	var problems validationErrors
	problems.require("database.host", c.Database.Host)
	problems.port("database.port", c.Database.Port)
	problems.require("database.dbname", c.Database.DBName)
	if checkDimension && c.RAG.EmbeddingDim <= 0 {
		problems.add("rag.embedding_dim", "必须为正数")
	}
	return problems.err()
}
