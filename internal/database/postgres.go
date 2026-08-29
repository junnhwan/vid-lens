package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vid-lens/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

// Connection owns the GORM handle and its underlying PostgreSQL connection
// pool. Callers must close it during application shutdown.
type Connection struct {
	GORM *gorm.DB
	SQL  *sql.DB
}

// PostgresDSN builds a URL-safe PostgreSQL DSN without logging credentials.
func PostgresDSN(cfg config.DatabaseConfig) (string, error) {
	host := strings.TrimSpace(cfg.Host)
	username := strings.TrimSpace(cfg.Username)
	database := strings.TrimSpace(cfg.DBName)

	var issues []string
	if host == "" {
		issues = append(issues, "host is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		issues = append(issues, fmt.Sprintf("port must be between 1 and 65535, got %d", cfg.Port))
	}
	if username == "" {
		issues = append(issues, "username is required")
	}
	if database == "" {
		issues = append(issues, "database is required")
	}
	if len(issues) > 0 {
		return "", errors.New("invalid postgres configuration: " + strings.Join(issues, "; "))
	}

	sslMode := strings.TrimSpace(cfg.SSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}

	user := url.User(username)
	if cfg.Password != "" {
		user = url.UserPassword(username, cfg.Password)
	}
	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     user,
		Host:     net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
		Path:     "/" + database,
		RawQuery: url.Values{"sslmode": []string{sslMode}}.Encode(),
	}).String()
	return dsn, nil
}

// OpenPostgres creates and verifies the application's PostgreSQL pool. Schema
// migration remains an explicit caller responsibility so maintenance commands
// can open the database without mutating it.
func OpenPostgres(ctx context.Context, cfg config.DatabaseConfig) (*Connection, error) {
	if ctx == nil {
		return nil, errors.New("open postgres database: context is required")
	}
	dsn, err := PostgresDSN(cfg)
	if err != nil {
		return nil, err
	}

	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, connectionError("open postgres database", err, cfg.Password)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, connectionError("get postgres connection pool", err, cfg.Password)
	}

	sqlDB.SetMaxOpenConns(defaultMaxOpenConns)
	sqlDB.SetMaxIdleConns(defaultMaxIdleConns)
	sqlDB.SetConnMaxLifetime(defaultConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(defaultConnMaxIdleTime)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, connectionError("ping postgres database", err, cfg.Password)
	}
	return &Connection{GORM: gormDB, SQL: sqlDB}, nil
}

func (c *Connection) Close() error {
	if c == nil || c.SQL == nil {
		return nil
	}
	return c.SQL.Close()
}

func connectionError(operation string, err error, password string) error {
	message := err.Error()
	if password != "" {
		message = strings.ReplaceAll(message, password, "[REDACTED]")
	}
	return fmt.Errorf("%s: %s", operation, message)
}
