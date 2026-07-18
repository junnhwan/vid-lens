package dbmigration

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// migrationAdvisoryLockKey is the stable PostgreSQL session-lock namespace for
// the one-off MySQL-to-PostgreSQL relational migration. A single key is used
// across target schemas because every run reads the same mutable legacy source.
const migrationAdvisoryLockKey int64 = 0x5649444c454e53 // "VIDLENS"

// TryAcquireMigrationAdvisoryLock obtains a non-blocking PostgreSQL session
// advisory lock and pins the owning database/sql connection until release. The
// returned release function is idempotent and must be called before closing the
// GORM pool.
func TryAcquireMigrationAdvisoryLock(ctx context.Context, db *gorm.DB) (func(context.Context) error, error) {
	if db == nil {
		return nil, errors.New("acquire migration advisory lock: target database is nil")
	}
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("acquire migration advisory lock: PostgreSQL target is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire migration advisory lock: get connection pool: %w", err)
	}
	conn, err := sqlDB.Conn(nonNilContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("acquire migration advisory lock: pin connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(nonNilContext(ctx), "SELECT pg_try_advisory_lock($1)", migrationAdvisoryLockKey).Scan(&acquired); err != nil {
		return nil, errors.Join(fmt.Errorf("acquire migration advisory lock: query: %w", err), conn.Close())
	}
	if !acquired {
		_ = conn.Close()
		return nil, errors.New("acquire migration advisory lock: another relational migration is already in progress")
	}

	var once sync.Once
	var releaseErr error
	release := func(releaseCtx context.Context) error {
		once.Do(func() {
			var unlocked bool
			unlockErr := conn.QueryRowContext(nonNilContext(releaseCtx), "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey).Scan(&unlocked)
			if unlockErr == nil && !unlocked {
				unlockErr = errors.New("PostgreSQL reported that the migration advisory lock was not owned")
			}
			releaseErr = errors.Join(unlockErr, conn.Close())
		})
		return releaseErr
	}
	return release, nil
}
