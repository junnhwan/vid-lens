package main

import (
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestValidateAuditConfigRequiresPostgresAndSelectedVectorBackend(t *testing.T) {
	cfg := &config.Config{
		RAG: config.RAGConfig{
			Store: "pgvector", ChunkSize: 800, ChunkOverlap: 120, TopK: 5,
			CandidateK: 30, EmbeddingDim: 1536,
		},
	}

	err := validateAuditConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "database.host") {
		t.Fatalf("validateAuditConfig() error = %v, want database.host", err)
	}
}

func TestValidateAuditConfigDoesNotRequireUnusedRetrievalParameters(t *testing.T) {
	cfg := &config.Config{
		RAG:      config.RAGConfig{Store: "pgvector", EmbeddingDim: 1536},
		Database: config.DatabaseConfig{Host: "127.0.0.1", Port: 5433, Username: "vidlens", DBName: "vidlens"},
	}

	if err := validateAuditConfig(cfg); err != nil {
		t.Fatalf("validateAuditConfig() error = %v", err)
	}
}
