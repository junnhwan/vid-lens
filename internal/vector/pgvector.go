package vector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"vid-lens/internal/service"
)

const defaultPGVectorTable = "vidlens_rag_vectors"

var pgVectorIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type PGVectorConfig struct {
	Host            string
	Port            int
	Username        string
	Password        string
	Database        string
	SSLMode         string
	TableName       string
	Dim             int
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func (c PGVectorConfig) DSN() (string, error) {
	if strings.TrimSpace(c.Host) == "" {
		return "", errors.New("postgres host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return "", fmt.Errorf("postgres port must be between 1 and 65535, got %d", c.Port)
	}
	if strings.TrimSpace(c.Database) == "" {
		return "", errors.New("postgres database is required")
	}
	sslMode := strings.TrimSpace(c.SSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Username, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Database,
		RawQuery: url.Values{"sslmode": []string{sslMode}}.Encode(),
	}
	return u.String(), nil
}

type PGVectorStore struct {
	db    *sql.DB
	table string
	dim   int
}

func NewPGVectorStore(ctx context.Context, cfg PGVectorConfig) (*PGVectorStore, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	dsn, err := cfg.DSN()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres vector database: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	store := newPGVectorStore(db, cfg)
	if err := store.HealthCheck(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres vector database: %w", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure pgvector schema: %w", err)
	}
	return store, nil
}

// NewPGVectorStoreWithDB is intended for tests and applications that own the
// database connection pool. The caller remains responsible for closing db.
func NewPGVectorStoreWithDB(db *sql.DB, cfg PGVectorConfig) (*PGVectorStore, error) {
	if db == nil {
		return nil, errors.New("postgres database handle is required")
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return newPGVectorStore(db, cfg), nil
}

func newPGVectorStore(db *sql.DB, cfg PGVectorConfig) *PGVectorStore {
	return &PGVectorStore{db: db, table: quotePGVectorIdentifier(cfg.TableName), dim: cfg.Dim}
}

func (c *PGVectorConfig) normalize() error {
	c.Host = strings.TrimSpace(c.Host)
	c.Database = strings.TrimSpace(c.Database)
	c.SSLMode = strings.TrimSpace(c.SSLMode)
	c.TableName = strings.TrimSpace(c.TableName)
	if c.TableName == "" {
		c.TableName = defaultPGVectorTable
	}
	if !pgVectorIdentifierPattern.MatchString(c.TableName) {
		return fmt.Errorf("invalid postgres vector table name %q", c.TableName)
	}
	if len(c.TableName) > 63 {
		return fmt.Errorf("postgres vector table name is too long: %d", len(c.TableName))
	}
	if c.Dim <= 0 {
		return fmt.Errorf("postgres vector dimension must be positive, got %d", c.Dim)
	}
	return nil
}

func quotePGVectorIdentifier(identifier string) string {
	return `"` + identifier + `"`
}

func (s *PGVectorStore) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("postgres vector store is not initialized")
	}
	if _, err := s.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return err
	}
	createTable := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    vector_id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    chunk_id BIGINT NOT NULL,
    chunk_index INTEGER NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    embedding_model TEXT NOT NULL,
    content TEXT NOT NULL,
    embedding_dim INTEGER NOT NULL,
    embedding vector(%d) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, s.table, s.dim)
	if _, err := s.db.ExecContext(ctx, createTable); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s_scope_idx ON %s (user_id, task_id, embedding_model)`,
		strings.Trim(s.table, `"`), s.table,
	))
	return err
}

func (s *PGVectorStore) UpsertChunks(ctx context.Context, vectors []service.RAGVector) error {
	if len(vectors) == 0 {
		return nil
	}
	if s == nil || s.db == nil {
		return errors.New("postgres vector store is not initialized")
	}
	if err := s.validateVectors(vectors); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vector upsert transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.upsertChunksTx(ctx, tx, vectors); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vector upsert transaction: %w", err)
	}
	return nil
}

// ReplaceTaskChunks atomically replaces one task/model projection inside
// PostgreSQL. It does not make the preceding relational chunk transaction atomic
// with this transaction; a failed build remains recoverable by reindexing the
// PostgreSQL relational source of truth.
func (s *PGVectorStore) ReplaceTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string, vectors []service.RAGVector) error {
	if s == nil || s.db == nil {
		return errors.New("postgres vector store is not initialized")
	}
	if strings.TrimSpace(embeddingModel) == "" {
		return errors.New("embedding model is required")
	}
	if err := s.validateReplacementScope(userID, taskID, embeddingModel, vectors); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vector replacement transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3`, s.table),
		userID, taskID, embeddingModel,
	); err != nil {
		return fmt.Errorf("delete old task vectors: %w", err)
	}
	if err := s.upsertChunksTx(ctx, tx, vectors); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit vector replacement transaction: %w", err)
	}
	return nil
}

func (s *PGVectorStore) upsertChunksTx(ctx context.Context, tx *sql.Tx, vectors []service.RAGVector) error {
	query := fmt.Sprintf(`
INSERT INTO %s (
    vector_id, user_id, task_id, chunk_id, chunk_index, content_hash,
    embedding_model, content, embedding_dim, embedding, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::vector, NOW())
ON CONFLICT (vector_id) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    task_id = EXCLUDED.task_id,
    chunk_id = EXCLUDED.chunk_id,
    chunk_index = EXCLUDED.chunk_index,
    content_hash = EXCLUDED.content_hash,
    embedding_model = EXCLUDED.embedding_model,
    content = EXCLUDED.content,
    embedding_dim = EXCLUDED.embedding_dim,
    embedding = EXCLUDED.embedding,
    updated_at = NOW()`, s.table)
	for _, vector := range vectors {
		if _, err := tx.ExecContext(ctx, query,
			vector.VectorID, vector.UserID, vector.TaskID, vector.ChunkID,
			vector.ChunkIndex, vector.ContentHash, vector.EmbeddingModel,
			vector.Content, len(vector.Vector), formatPGVector(vector.Vector),
		); err != nil {
			return fmt.Errorf("upsert vector %q: %w", vector.VectorID, err)
		}
	}
	return nil
}

func (s *PGVectorStore) validateVectors(vectors []service.RAGVector) error {
	for i := range vectors {
		if err := s.validateVector(vectors[i]); err != nil {
			return fmt.Errorf("validate vector %d: %w", i, err)
		}
	}
	return nil
}

func (s *PGVectorStore) validateReplacementScope(userID, taskID int64, embeddingModel string, vectors []service.RAGVector) error {
	if err := s.validateVectors(vectors); err != nil {
		return err
	}
	for i, vector := range vectors {
		if vector.UserID != userID || vector.TaskID != taskID || vector.EmbeddingModel != embeddingModel {
			return fmt.Errorf("vector %d does not match replacement scope", i)
		}
	}
	return nil
}

func (s *PGVectorStore) DeleteTaskChunks(ctx context.Context, userID, taskID int64, embeddingModel string) error {
	if s == nil || s.db == nil {
		return errors.New("postgres vector store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3`, s.table),
		userID, taskID, embeddingModel,
	)
	return err
}

func (s *PGVectorStore) Search(ctx context.Context, query []float32, req service.RetrievalRequest) ([]service.RetrievedChunk, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("postgres vector store is not initialized")
	}
	if err := validateEmbedding(query, s.dim); err != nil {
		return nil, err
	}
	taskIDs := normalizeVectorTaskIDs(req.TaskIDs)
	if len(taskIDs) == 0 && req.TaskID > 0 {
		taskIDs = []int64{req.TaskID}
	}
	if len(taskIDs) == 0 {
		return nil, errors.New("task_ids must not be empty")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	placeholders := make([]string, len(taskIDs))
	args := make([]any, 0, len(taskIDs)+4)
	args = append(args, formatPGVector(query), req.UserID)
	for i, taskID := range taskIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, taskID)
	}
	modelPos := len(taskIDs) + 3
	limitPos := modelPos + 1
	args = append(args, req.EmbeddingModel, topK)
	sqlText := fmt.Sprintf(`
SELECT vector_id, task_id, chunk_id, chunk_index, content,
       1 - (embedding <=> $1::vector) AS score
FROM %s
WHERE user_id = $2 AND task_id IN (%s) AND embedding_model = $%d
ORDER BY embedding <=> $1::vector
LIMIT $%d`, s.table, strings.Join(placeholders, ","), modelPos, limitPos)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]service.RetrievedChunk, 0, topK)
	for rows.Next() {
		var result service.RetrievedChunk
		var score float64
		if err := rows.Scan(&result.EvidenceID, &result.TaskID, &result.ChunkID, &result.ChunkIndex, &result.Content, &score); err != nil {
			return nil, err
		}
		if req.MinScore > 0 && float32(score) < req.MinScore {
			continue
		}
		result.Score = float32(score)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func normalizeVectorTaskIDs(taskIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(taskIDs))
	out := make([]int64, 0, len(taskIDs))
	for _, id := range taskIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *PGVectorStore) HealthCheck(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("postgres vector store is not initialized")
	}
	return s.db.PingContext(ctx)
}

func (s *PGVectorStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PGVectorStore) validateVector(vector service.RAGVector) error {
	if strings.TrimSpace(vector.VectorID) == "" {
		return errors.New("vector id is required")
	}
	if strings.TrimSpace(vector.EmbeddingModel) == "" {
		return errors.New("embedding model is required")
	}
	if vector.ChunkID <= 0 {
		return errors.New("chunk id must reference a persisted relational chunk")
	}
	return validateEmbedding(vector.Vector, s.dim)
}

func validateEmbedding(vector []float32, expectedDim int) error {
	if len(vector) == 0 {
		return errors.New("embedding vector is empty")
	}
	if expectedDim > 0 && len(vector) != expectedDim {
		return fmt.Errorf("embedding dim = %d, want %d", len(vector), expectedDim)
	}
	for i, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("embedding contains non-finite value at index %d", i)
		}
	}
	return nil
}

func formatPGVector(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// ListTaskVectorManifest returns the stable metadata needed by strict RAG
// evaluation. It deliberately does not read embeddings, so snapshots remain
// backend-neutral and cheap to build.
func (s *PGVectorStore) ListTaskVectorManifest(ctx context.Context, userID, taskID int64, embeddingModel string) ([]service.RAGVectorManifestEntry, error) {
	return s.listVectorManifest(ctx, fmt.Sprintf(`
SELECT vector_id, user_id, task_id, chunk_id, chunk_index, content_hash, embedding_model
FROM %s
WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3
ORDER BY chunk_index ASC, vector_id ASC`, s.table), userID, taskID, embeddingModel)
}

// ListAllVectorManifest returns every pgvector projection row without reading
// embeddings. It is intentionally pgvector-specific because the all-scope gate
// is used to complete the PostgreSQL single-database migration.
func (s *PGVectorStore) ListAllVectorManifest(ctx context.Context) ([]service.RAGVectorManifestEntry, error) {
	return s.listVectorManifest(ctx, fmt.Sprintf(`
SELECT vector_id, user_id, task_id, chunk_id, chunk_index, content_hash, embedding_model
FROM %s
ORDER BY user_id ASC, task_id ASC, embedding_model ASC, chunk_index ASC, vector_id ASC`, s.table))
}

func (s *PGVectorStore) listVectorManifest(ctx context.Context, query string, args ...any) ([]service.RAGVectorManifestEntry, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("postgres vector store is not initialized")
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]service.RAGVectorManifestEntry, 0)
	for rows.Next() {
		var entry service.RAGVectorManifestEntry
		if err := rows.Scan(&entry.EvidenceID, &entry.UserID, &entry.TaskID, &entry.ChunkID, &entry.ChunkIndex, &entry.ContentHash, &entry.EmbeddingModel); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
