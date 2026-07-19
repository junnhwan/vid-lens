package vector

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"vid-lens/internal/service"
)

func testPGConfig() PGVectorConfig {
	return PGVectorConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", Database: "vidlens", Dim: 3}
}

func newMockPGStore(t *testing.T, cfg PGVectorConfig) (*PGVectorStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	store, err := NewPGVectorStoreWithDB(db, cfg)
	if err != nil {
		db.Close()
		t.Fatalf("NewPGVectorStoreWithDB() error = %v", err)
	}
	return store, mock, func() { _ = db.Close() }
}

func TestPGVectorConfigDSN(t *testing.T) {
	cfg := testPGConfig()
	cfg.Username = "user@example"
	cfg.Password = "p@ss word"
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	want := "postgres://user%40example:p%40ss%20word@127.0.0.1:5433/vidlens?sslmode=disable"
	if dsn != want {
		t.Fatalf("DSN() = %q, want %q", dsn, want)
	}
}

func TestPGVectorConfigRejectsUnsafeTableAndInvalidDimension(t *testing.T) {
	cfg := testPGConfig()
	cfg.TableName = `vectors; DROP TABLE users;`
	if _, err := NewPGVectorStoreWithDB(&sql.DB{}, cfg); err == nil {
		t.Fatal("expected unsafe table name error")
	}

	cfg = testPGConfig()
	cfg.Dim = 0
	if _, err := NewPGVectorStoreWithDB(&sql.DB{}, cfg); err == nil {
		t.Fatal("expected invalid dimension error")
	}
}

func TestPGVectorStoreEnsureSchema(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS vector`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE INDEX IF NOT EXISTS`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreUpsertChunksUsesTransaction(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO`).
		WithArgs("v-1", int64(7), int64(8), int64(9), 0, "hash", "embed-model", "hello", 3, "[0.1,0.2,0.3]").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.UpsertChunks(context.Background(), []service.RAGVector{{
		VectorID: "v-1", UserID: 7, TaskID: 8, ChunkID: 9, ContentHash: "hash",
		EmbeddingModel: "embed-model", Content: "hello", Vector: []float32{0.1, 0.2, 0.3},
	}})
	if err != nil {
		t.Fatalf("UpsertChunks() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreReplaceTaskChunksUsesSingleTransaction(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "vidlens_rag_vectors" WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3`)).
		WithArgs(int64(7), int64(8), "embed-model").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO`).
		WithArgs("v-1", int64(7), int64(8), int64(9), 0, "hash", "embed-model", "hello", 3, "[0.1,0.2,0.3]").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.ReplaceTaskChunks(context.Background(), 7, 8, "embed-model", []service.RAGVector{{
		VectorID: "v-1", UserID: 7, TaskID: 8, ChunkID: 9, ContentHash: "hash",
		EmbeddingModel: "embed-model", Content: "hello", Vector: []float32{0.1, 0.2, 0.3},
	}})
	if err != nil {
		t.Fatalf("ReplaceTaskChunks() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreReplaceTaskChunksRollsBackOnInsertFailure(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "vidlens_rag_vectors" WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3`)).
		WithArgs(int64(7), int64(8), "embed-model").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO`).
		WithArgs("v-1", int64(7), int64(8), int64(9), 0, "hash", "embed-model", "hello", 3, "[0.1,0.2,0.3]").
		WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	err := store.ReplaceTaskChunks(context.Background(), 7, 8, "embed-model", []service.RAGVector{{
		VectorID: "v-1", UserID: 7, TaskID: 8, ChunkID: 9, ContentHash: "hash",
		EmbeddingModel: "embed-model", Content: "hello", Vector: []float32{0.1, 0.2, 0.3},
	}})
	if err == nil {
		t.Fatal("ReplaceTaskChunks() succeeded, want insert failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreReplaceTaskChunksRejectsMixedScope(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()

	err := store.ReplaceTaskChunks(context.Background(), 7, 8, "embed-model", []service.RAGVector{{
		VectorID: "v-1", UserID: 99, TaskID: 8, ChunkID: 9, ContentHash: "hash",
		EmbeddingModel: "embed-model", Content: "hello", Vector: []float32{0.1, 0.2, 0.3},
	}})
	if err == nil {
		t.Fatal("ReplaceTaskChunks() succeeded with a mixed replacement scope")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database call: %v", err)
	}
}

func TestPGVectorStoreUpsertChunksRejectsUnpersistedChunkID(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()

	err := store.UpsertChunks(context.Background(), []service.RAGVector{{
		VectorID: "v-1", UserID: 7, TaskID: 8, ChunkID: 0, ContentHash: "hash",
		EmbeddingModel: "embed-model", Content: "hello", Vector: []float32{0.1, 0.2, 0.3},
	}})
	if err == nil {
		t.Fatal("UpsertChunks() succeeded with chunk_id=0")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database call: %v", err)
	}
}

func TestPGVectorStoreSearchConvertsDistanceToSimilarity(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectQuery(`SELECT vector_id`).
		WithArgs("[1,0,0]", int64(7), int64(8), "embed-model", 3).
		WillReturnRows(sqlmock.NewRows([]string{"vector_id", "task_id", "chunk_id", "chunk_index", "content", "score"}).
			AddRow("v-1", int64(8), int64(9), 2, "hello", 0.9).
			AddRow("v-2", int64(8), int64(10), 3, "below threshold", 0.2))

	results, err := store.Search(context.Background(), []float32{1, 0, 0}, service.RetrievalRequest{
		UserID: 7, TaskID: 8, EmbeddingModel: "embed-model", TopK: 3, MinScore: 0.5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].EvidenceID != "v-1" || results[0].Score != 0.9 {
		t.Fatalf("Search() = %+v, want one similarity-scored result", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestValidateEmbeddingRejectsNonFiniteAndWrongDimension(t *testing.T) {
	if err := validateEmbedding([]float32{1, 2}, 3); err == nil {
		t.Fatal("expected dimension error")
	}
	if err := validateEmbedding([]float32{1, float32(math.NaN()), 3}, 3); err == nil {
		t.Fatal("expected non-finite value error")
	}
}

func TestPGVectorStoreDeleteTaskChunksScopesByTenantAndModel(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "vidlens_rag_vectors" WHERE user_id = $1 AND task_id = $2 AND embedding_model = $3`)).
		WithArgs(int64(7), int64(8), "embed-model").
		WillReturnResult(sqlmock.NewResult(0, 2))
	if err := store.DeleteTaskChunks(context.Background(), 7, 8, "embed-model"); err != nil {
		t.Fatalf("DeleteTaskChunks() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreListTaskVectorManifestScopesAndOrdersRows(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectQuery(`SELECT vector_id, user_id, task_id, chunk_id, chunk_index, content_hash, embedding_model`).
		WithArgs(int64(7), int64(8), "embed-model").
		WillReturnRows(sqlmock.NewRows([]string{"vector_id", "user_id", "task_id", "chunk_id", "chunk_index", "content_hash", "embedding_model"}).
			AddRow("v-1", int64(7), int64(8), int64(9), 0, "hash-1", "embed-model").
			AddRow("v-2", int64(7), int64(8), int64(10), 1, "hash-2", "embed-model"))

	entries, err := store.ListTaskVectorManifest(context.Background(), 7, 8, "embed-model")
	if err != nil {
		t.Fatalf("ListTaskVectorManifest() error = %v", err)
	}
	if len(entries) != 2 || entries[0].EvidenceID != "v-1" || entries[1].ChunkIndex != 1 {
		t.Fatalf("ListTaskVectorManifest() = %+v", entries)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPGVectorStoreListAllVectorManifestOrdersEveryScope(t *testing.T) {
	store, mock, cleanup := newMockPGStore(t, testPGConfig())
	defer cleanup()
	mock.ExpectQuery(`SELECT vector_id, user_id, task_id, chunk_id, chunk_index, content_hash, embedding_model`).
		WillReturnRows(sqlmock.NewRows([]string{"vector_id", "user_id", "task_id", "chunk_id", "chunk_index", "content_hash", "embedding_model"}).
			AddRow("u1", int64(1), int64(10), int64(100), 0, "h1", "m1").
			AddRow("u2", int64(2), int64(20), int64(200), 0, "h2", "m2"))

	entries, err := store.ListAllVectorManifest(context.Background())
	if err != nil {
		t.Fatalf("ListAllVectorManifest() error = %v", err)
	}
	if len(entries) != 2 || entries[0].UserID != 1 || entries[1].UserID != 2 {
		t.Fatalf("ListAllVectorManifest() = %+v", entries)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
