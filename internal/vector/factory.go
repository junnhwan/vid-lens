package vector

import (
	"context"
	"fmt"

	"vid-lens/internal/config"
	"vid-lens/internal/service"
)

// Store is the application-facing contract shared by the configured vector
// backends. Keeping backend construction here prevents server commands and
// evaluation tools from drifting into separate backend switch logic.
type Store interface {
	service.RAGVectorStore
	service.RAGRetriever
	ListTaskVectorManifest(context.Context, int64, int64, string) ([]service.RAGVectorManifestEntry, error)
	HealthCheck(context.Context) error
	Close() error
}

// BackendConfig contains only backend-specific connection settings. The
// caller still owns the higher-level RAG configuration (top-k, chunking,
// retrieval fusion, etc.).
type BackendConfig struct {
	Backend   string
	Dimension int
	PGVector  PGVectorConfig
}

// BackendConfigFromApplication converts the user-facing application config
// into the backend-neutral vector store config. Keeping this adapter next to
// the factory prevents server, evaluation, and maintenance commands from
// silently drifting in how they connect to the same backend.
func BackendConfigFromApplication(cfg *config.Config) BackendConfig {
	if cfg == nil {
		return BackendConfig{}
	}
	return BackendConfig{
		Backend:   cfg.RAG.Store,
		Dimension: cfg.RAG.EmbeddingDim,
		PGVector: PGVectorConfig{
			Host:         cfg.Database.Host,
			Port:         cfg.Database.Port,
			Username:     cfg.Database.Username,
			Password:     cfg.Database.Password,
			Database:     cfg.Database.DBName,
			SSLMode:      cfg.Database.SSLMode,
			TableName:    cfg.RAG.VectorTable,
			Dim:          cfg.RAG.EmbeddingDim,
			MaxOpenConns: 8,
			MaxIdleConns: 4,
		},
	}
}

// NewStore creates and health-checks the selected vector backend. An empty
// backend uses pgvector, matching the single-database application architecture.
func NewStore(ctx context.Context, cfg BackendConfig) (Store, error) {
	backend := NormalizeBackendName(cfg.Backend)
	switch backend {
	case "pgvector":
		pgCfg := cfg.PGVector
		if pgCfg.Dim == 0 {
			pgCfg.Dim = cfg.Dimension
		}
		return NewPGVectorStore(ctx, pgCfg)
	default:
		return nil, fmt.Errorf("unsupported vector backend %q", backend)
	}
}
