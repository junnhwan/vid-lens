package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
	"vidlens/internal/domain"
)

func (s *Store) CleanupAssets(ctx context.Context, _ bool) ([]domain.Asset, error) {
	var out []domain.Asset
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "DELETE FROM asset_refs WHERE expires_at<now()"); e != nil {
			return e
		}
		var e error
		out, e = many[domain.Asset](ctx, tx, "UPDATE media_assets SET state='deleting' WHERE id IN (SELECT a.id FROM media_assets a WHERE (state='deleting' OR state='ready' AND kind='frame' AND expires_at<now()) AND (delete_not_before IS NULL OR delete_not_before<now()) AND NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=a.id AND (expires_at IS NULL OR expires_at>now())) ORDER BY expires_at NULLS LAST LIMIT 100 FOR UPDATE) RETURNING to_jsonb(media_assets)")
		return e
	})
	return out, e
}
func (s *Store) Deleted(ctx context.Context, id string, success bool) error {
	if success {
		_, e := s.Pool.Exec(ctx, "UPDATE media_assets SET state='deleted',delete_not_before=NULL WHERE id=$1 AND state='deleting'", id)
		return e
	}
	_, e := s.Pool.Exec(ctx, "UPDATE media_assets SET delete_failures=delete_failures+1,delete_not_before=now()+CASE delete_failures WHEN 0 THEN interval '1 minute' WHEN 1 THEN interval '5 minutes' WHEN 2 THEN interval '30 minutes' ELSE interval '1 hour' END WHERE id=$1 AND state='deleting'", id)
	return e
}
func (s *Store) CleanupMetadata(ctx context.Context) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// Private page bodies must not survive in either snapshots or tool
		// replay payloads after the reference's 24-hour retention window.
		if _, e := tx.Exec(ctx, "UPDATE run_operations o SET result='{}' WHERE kind IN ('web.read','web.search') AND (EXISTS(SELECT 1 FROM external_references r WHERE r.run_id=o.run_id AND r.expires_at<now()) OR EXISTS(SELECT 1 FROM qa_turns t WHERE t.run_id=o.run_id AND t.expires_at<now())) AND result<>'{}'::jsonb"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE job_runs j SET checkpoint='{\"schema_version\":1}' WHERE kind='qa.answer' AND status IN ('succeeded','failed','canceled') AND EXISTS(SELECT 1 FROM external_references r WHERE r.run_id=j.id AND r.expires_at<now()) AND checkpoint<>'{\"schema_version\":1}'::jsonb"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE external_references SET body=NULL WHERE expires_at<now() AND body IS NOT NULL"); e != nil {
			return e
		}
		queries := []string{"UPDATE job_runs r SET checkpoint='{\"schema_version\":1}' FROM qa_turns t WHERE t.run_id=r.id AND t.expires_at<now() AND r.checkpoint<>'{\"schema_version\":1}'::jsonb", "DELETE FROM evidence_questions q USING qa_turns t WHERE q.run_id=t.run_id AND t.expires_at<now()", "DELETE FROM run_events e USING job_runs r WHERE e.run_id=r.id AND r.status IN ('succeeded','failed','canceled') AND r.updated_at<now()-interval '7 days'", "UPDATE qa_turns SET answer=NULL,question='',context='{\"frame_id\":null,\"passage\":null}' WHERE expires_at<now() AND question<>''", "DELETE FROM observations o USING qa_turns t WHERE o.run_id=t.run_id AND t.expires_at<now()", "DELETE FROM run_operations o USING job_runs r WHERE o.run_id=r.id AND r.status IN ('succeeded','failed','canceled') AND r.updated_at<now()-interval '30 days'", "DELETE FROM task_outbox o USING job_runs r WHERE o.run_id=r.id AND o.delivered_at IS NOT NULL AND r.status IN ('succeeded','failed','canceled') AND r.updated_at<now()-interval '30 days'", "DELETE FROM job_attempts a USING job_runs r WHERE a.run_id=r.id AND r.status IN ('succeeded','failed','canceled') AND r.updated_at<now()-interval '30 days'", "DELETE FROM idempotency WHERE created_at<now()-interval '30 days'", "DELETE FROM storage_reservations WHERE expires_at<now()"}
		for _, q := range queries {
			if _, e := tx.Exec(ctx, q); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Store) KnownStorageKeys(ctx context.Context) (map[string]bool, error) {
	rows, e := s.Pool.Query(ctx, "SELECT storage_key FROM media_assets WHERE state<>'deleted'")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			return nil, e
		}
		out[k] = true
	}
	return out, rows.Err()
}
func (s *Store) ProtectedRun(ctx context.Context, id string) (bool, error) {
	var ok bool
	e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM job_runs WHERE id=$1 AND status NOT IN ('succeeded','failed','canceled') AND deadline_at+interval '5 minutes'>now())", id).Scan(&ok)
	return ok, e
}
func (s *Store) MarkMissing(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, "UPDATE media_assets SET state='unavailable' WHERE id=$1 AND state='ready'", id)
	return e
}
func (s *Store) DeleteLease(ctx context.Context) (func(), bool, error) {
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return nil, false, e
	}
	var ok bool
	e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(867914032)").Scan(&ok)
	if e != nil || !ok {
		conn.Release()
		return nil, ok, e
	}
	return func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.Exec(c, "SELECT pg_advisory_unlock(867914032)")
		conn.Release()
	}, true, nil
}
