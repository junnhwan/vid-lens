package main

import (
	"context"
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestOpenServerDatabaseUsesPostgresConfiguration(t *testing.T) {
	cfg := &config.Config{
		Database:    config.DatabaseConfig{},
		LegacyMySQL: config.LegacyMySQLConfig{Host: "127.0.0.1", Port: 3306, Username: "legacy", DBName: "legacy"},
	}

	_, err := openServerDatabase(context.Background(), cfg)
	if err == nil {
		t.Fatal("openServerDatabase() error = nil, want invalid PostgreSQL configuration")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("openServerDatabase() error = %v, want PostgreSQL error", err)
	}
	if strings.Contains(err.Error(), "mysql") {
		t.Fatalf("openServerDatabase() error = %v, server must not open MySQL", err)
	}
}
