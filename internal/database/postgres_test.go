package database

import (
	"net/url"
	"strings"
	"testing"

	"vid-lens/internal/config"
)

func TestPostgresDSNEncodesCredentialsAndIPv6(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "2001:db8::1",
		Port:     5432,
		Username: "user:name",
		Password: "p@ss/word?with#symbols",
		DBName:   "vid lens",
		SSLMode:  "require",
	}

	dsn, err := PostgresDSN(cfg)
	if err != nil {
		t.Fatalf("PostgresDSN() error = %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	password, ok := parsed.User.Password()
	if !ok || parsed.User.Username() != cfg.Username || password != cfg.Password {
		t.Fatalf("credentials did not round trip through URL-safe DSN")
	}
	if parsed.Hostname() != cfg.Host || parsed.Port() != "5432" {
		t.Fatalf("host = %q, port = %q", parsed.Hostname(), parsed.Port())
	}
	if strings.TrimPrefix(parsed.Path, "/") != cfg.DBName {
		t.Fatalf("database path = %q, want %q", parsed.Path, cfg.DBName)
	}
	if parsed.Query().Get("sslmode") != "require" {
		t.Fatalf("sslmode = %q, want require", parsed.Query().Get("sslmode"))
	}
}

func TestPostgresDSNUsesSafeLocalSSLDefault(t *testing.T) {
	dsn, err := PostgresDSN(config.DatabaseConfig{
		Host: "127.0.0.1", Port: 5432, Username: "vidlens", DBName: "vidlens",
	})
	if err != nil {
		t.Fatalf("PostgresDSN() error = %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	if parsed.Query().Get("sslmode") != "disable" {
		t.Fatalf("sslmode = %q, want disable", parsed.Query().Get("sslmode"))
	}
}

func TestPostgresDSNValidationDoesNotLeakPassword(t *testing.T) {
	const password = "never-print-this-password"
	_, err := PostgresDSN(config.DatabaseConfig{Port: 70000, Password: password})
	if err == nil {
		t.Fatal("PostgresDSN() error = nil, want validation error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("PostgresDSN() leaked password: %v", err)
	}
	for _, field := range []string{"host", "port", "username", "database"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("PostgresDSN() error %q does not mention %s", err, field)
		}
	}
}
