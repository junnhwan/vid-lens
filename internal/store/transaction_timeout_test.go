package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestTransactionDeadlineCancelsItsBodyAndReleasesControlLock(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	started := time.Now()
	var bodyErr error
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, bodyErr = tx.Exec(ctx, "SELECT pg_sleep(3)")
		return bodyErr
	})
	t.Logf("control transaction body elapsed=%s canceled=%t", time.Since(started), bodyErr != nil)
	if err == nil || bodyErr == nil || time.Since(started) >= 2800*time.Millisecond {
		t.Fatalf("transaction body escaped deadline: body=%v transaction=%v elapsed=%s", bodyErr, err, time.Since(started))
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT 1"); return err }); err != nil {
		t.Fatal("timed out transaction retained control lock", err)
	}
}

func TestReadDeadlineCoversAllQueriesInItsBody(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	started := time.Now()
	var secondErr error
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_sleep(3)"); err != nil {
			return err
		}
		_, secondErr = tx.Exec(ctx, "SELECT pg_sleep(3)")
		return secondErr
	})
	t.Logf("read transaction body elapsed=%s canceled=%t", time.Since(started), secondErr != nil)
	if err == nil || secondErr == nil || time.Since(started) >= 5800*time.Millisecond {
		t.Fatalf("read body escaped total deadline: body=%v transaction=%v elapsed=%s", secondErr, err, time.Since(started))
	}
}
