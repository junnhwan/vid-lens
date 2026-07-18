package main

import (
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestValidateReindexConfigRequiresPostgresAndPGVectorDestination(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Host: "127.0.0.1", Port: 5432, Username: "vidlens", DBName: "vidlens"},
		RAG:      config.RAGConfig{EmbeddingDim: 1536},
	}

	if err := validateReindexConfig(cfg); err != nil {
		t.Fatalf("validation error = %v", err)
	}

	cfg.Database.Host = ""
	if err := validateReindexConfig(cfg); err == nil || !strings.Contains(err.Error(), "database.host") {
		t.Fatalf("invalid validation error = %v, want database.host", err)
	}
}

func TestValidateReindexConfigRequiresDestinationDimensionForDryRun(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Host: "127.0.0.1", Port: 5432, Username: "vidlens", DBName: "vidlens"},
	}

	err := validateReindexConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "rag.embedding_dim") {
		t.Fatalf("dry-run validation error = %v, want destination dimension validation", err)
	}
}
