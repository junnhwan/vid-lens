package main

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestOpenServerDatabaseUsesPostgresConfiguration(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{},
	}

	_, err := openServerDatabase(context.Background(), cfg)
	if err == nil {
		t.Fatal("openServerDatabase() error = nil, want invalid PostgreSQL configuration")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("openServerDatabase() error = %v, want PostgreSQL error", err)
	}
}
