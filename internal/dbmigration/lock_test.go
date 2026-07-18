package dbmigration

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTryAcquireMigrationAdvisoryLockOwnsAndReleasesSessionLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(migrationAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(migrationAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))

	release, err := TryAcquireMigrationAdvisoryLock(context.Background(), gormDB)
	if err != nil {
		t.Fatalf("TryAcquireMigrationAdvisoryLock() error = %v", err)
	}
	if err := release(context.Background()); err != nil {
		t.Fatalf("release migration advisory lock: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTryAcquireMigrationAdvisoryLockRejectsConcurrentOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(migrationAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))

	if _, err := TryAcquireMigrationAdvisoryLock(context.Background(), gormDB); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("TryAcquireMigrationAdvisoryLock() error = %v, want contention", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
