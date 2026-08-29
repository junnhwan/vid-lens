package main

import (
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestValidateEvalConfigRequiresPostgresAndSelectedVectorBackend(t *testing.T) {
	cfg := &config.Config{
		RAG: config.RAGConfig{
			Store: "pgvector", ChunkSize: 800, ChunkOverlap: 120, TopK: 5,
			CandidateK: 30, EmbeddingDim: 1536,
		},
	}

	err := validateEvalConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "database.host") {
		t.Fatalf("validateEvalConfig() error = %v, want database.host", err)
	}
}

func TestValidateEvalConfigDoesNotRequireLegacyMySQL(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Host: "127.0.0.1", Port: 5432, Username: "vidlens", DBName: "vidlens"},
		RAG: config.RAGConfig{
			Store: "pgvector", ChunkSize: 800, ChunkOverlap: 120, TopK: 5,
			CandidateK: 30, EmbeddingDim: 1536,
		},
	}
	if err := validateEvalConfig(cfg); err != nil {
		t.Fatalf("validateEvalConfig() error = %v", err)
	}
}
