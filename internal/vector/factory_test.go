package vector

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/config"
)

var _ Store = (*PGVectorStore)(nil)

func TestNewStoreRejectsUnknownBackendBeforeConnecting(t *testing.T) {
	_, err := NewStore(context.Background(), BackendConfig{Backend: "elastic-vector"})
	if err == nil || !strings.Contains(err.Error(), "unsupported vector backend") {
		t.Fatalf("NewStore() error = %v, want unsupported backend", err)
	}
}

func TestBackendConfigFromApplicationConfig(t *testing.T) {
	app := &config.Config{
		RAG:      config.RAGConfig{Store: "pgvector", EmbeddingDim: 1536, VectorTable: "vectors"},
		Database: config.DatabaseConfig{Host: "postgres", Port: 5432, Username: "pu", Password: "pp", DBName: "pdb", SSLMode: "require"},
	}

	got := BackendConfigFromApplication(app)
	if got.Backend != "pgvector" || got.Dimension != 1536 {
		t.Fatalf("top-level mapping = %+v", got)
	}
	if got.PGVector.Host != "postgres" || got.PGVector.Database != "pdb" || got.PGVector.TableName != "vectors" || got.PGVector.Dim != 1536 {
		t.Fatalf("pgvector mapping = %+v", got.PGVector)
	}
	if got.PGVector.MaxOpenConns != 8 || got.PGVector.MaxIdleConns != 4 {
		t.Fatalf("pgvector pool defaults = %d/%d, want 8/4", got.PGVector.MaxOpenConns, got.PGVector.MaxIdleConns)
	}
}
