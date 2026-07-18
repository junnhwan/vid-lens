package vector

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"vid-lens/internal/service"
)

// TestPGVectorStoreIntegration is opt-in because it requires a real
// PostgreSQL server with permission to install/use the vector extension.
// Run with VIDLENS_PGVECTOR_INTEGRATION=1 after starting the compose profile.
func TestPGVectorStoreIntegration(t *testing.T) {
	if os.Getenv("VIDLENS_PGVECTOR_INTEGRATION") != "1" {
		t.Skip("set VIDLENS_PGVECTOR_INTEGRATION=1 to run against real PostgreSQL + pgvector")
	}
	port := 5433
	if raw := os.Getenv("VIDLENS_PGVECTOR_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("invalid VIDLENS_PGVECTOR_PORT: %v", err)
		}
		port = parsed
	}
	table := fmt.Sprintf("vidlens_pgvector_it_%d", time.Now().UnixNano())
	cfg := PGVectorConfig{
		Host: envOrDefault("VIDLENS_PGVECTOR_HOST", "127.0.0.1"), Port: port,
		Username: envOrDefault("VIDLENS_PGVECTOR_USER", "vidlens"), Password: envOrDefault("VIDLENS_PGVECTOR_PASSWORD", "vidlens"),
		Database: envOrDefault("VIDLENS_PGVECTOR_DATABASE", "vidlens"), SSLMode: envOrDefault("VIDLENS_PGVECTOR_SSLMODE", "disable"),
		TableName: table, Dim: 3,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewPGVectorStore(ctx, cfg)
	if err != nil {
		t.Fatalf("NewPGVectorStore() error = %v", err)
	}
	cleanup := func(s *PGVectorStore) {
		if s != nil && s.db != nil {
			_, _ = s.db.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, quotePGVectorIdentifier(table)))
			_ = s.Close()
		}
	}
	defer func() { cleanup(store) }()

	vectors := []service.RAGVector{
		{VectorID: "v-1", UserID: 7, TaskID: 8, ChunkID: 9, ChunkIndex: 0, ContentHash: "h1", EmbeddingModel: "embed", Content: "first", Vector: []float32{1, 0, 0}},
		{VectorID: "v-2", UserID: 7, TaskID: 8, ChunkID: 10, ChunkIndex: 1, ContentHash: "h2", EmbeddingModel: "embed", Content: "second", Vector: []float32{0, 1, 0}},
		{VectorID: "other-user", UserID: 99, TaskID: 8, ChunkID: 11, ChunkIndex: 2, ContentHash: "h3", EmbeddingModel: "embed", Content: "isolated", Vector: []float32{1, 0, 0}},
	}
	if err := store.UpsertChunks(ctx, vectors); err != nil {
		t.Fatalf("UpsertChunks() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close before persistence check: %v", err)
	}
	store = nil

	store, err = NewPGVectorStore(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen pgvector store: %v", err)
	}
	results, err := store.Search(ctx, []float32{1, 0, 0}, service.RetrievalRequest{UserID: 7, TaskID: 8, EmbeddingModel: "embed", TopK: 5, MinScore: 0.5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].EvidenceID != "v-1" || results[0].Score < 0.99 {
		t.Fatalf("Search() = %+v", results)
	}
	isolated, err := store.Search(ctx, []float32{1, 0, 0}, service.RetrievalRequest{UserID: 8, TaskID: 8, EmbeddingModel: "embed", TopK: 5})
	if err != nil || len(isolated) != 0 {
		t.Fatalf("tenant-isolated Search() = %+v, err=%v", isolated, err)
	}
	manifest, err := store.ListTaskVectorManifest(ctx, 7, 8, "embed")
	if err != nil || len(manifest) != 2 || manifest[0].EvidenceID != "v-1" {
		t.Fatalf("manifest = %+v, err=%v", manifest, err)
	}
	if err := store.DeleteTaskChunks(ctx, 7, 8, "embed"); err != nil {
		t.Fatalf("DeleteTaskChunks() error = %v", err)
	}
	remaining, err := store.Search(ctx, []float32{1, 0, 0}, service.RetrievalRequest{UserID: 7, TaskID: 8, EmbeddingModel: "embed", TopK: 5})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("Search() after delete = %+v, err=%v", remaining, err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
