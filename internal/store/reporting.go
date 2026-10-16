package store

import (
	"context"
	"vidlens/internal/domain"
)

// Observations are attached to an already authorized physical submission. They
// cannot publish results or renew a run, including after cancellation/fencing.
func (s *Store) RecordSubmission(ctx context.Context, c domain.Claim, key string, n int, report any) error {
	_, e := s.Pool.Exec(ctx, `UPDATE model_submissions SET reporting=$5 WHERE run_id=$1 AND operation_key=$2 AND submission=$3 AND attempt_id=$4`, c.Run.ID, key, n, c.AttemptID, domain.JSON(report))
	return e
}
func (s *Store) ModelReporting(ctx context.Context) (map[string]any, error) {
	stats, e := one[map[string]any](ctx, s.Pool, `SELECT jsonb_build_object('physical_submissions',count(*),'reported',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported'),'unknown',count(*) FILTER(WHERE COALESCE(reporting->'cache_read'->>'status','unknown')<>'reported'),'hits',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported' AND (reporting->'cache_read'->>'value')::bigint>0),'reported_zero',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported' AND (reporting->'cache_read'->>'value')::bigint=0),'incomplete',count(*) FILTER(WHERE reporting->>'stream_status'<>'complete'),'cost_reporting','unknown') FROM model_submissions`)
	if e != nil {
		return nil, e
	}
	recent, e := many[map[string]any](ctx, s.Pool, `SELECT jsonb_build_object('submission',o.submission,'created_at',o.created_at,'model_revision_id',m.id,'display_name',COALESCE(m.display_name,c.profile->>'model_name'),'reporting',o.reporting) FROM model_submissions o JOIN job_runs r ON r.id=o.run_id JOIN runtime_config_revisions c ON c.id=r.config_id LEFT JOIN model_revisions m ON m.config_id=r.config_id ORDER BY o.created_at DESC LIMIT 50`)
	if e != nil {
		return nil, e
	}
	var legacy int64
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations o WHERE kind='model' AND NOT EXISTS(SELECT 1 FROM model_submissions s WHERE s.run_id=o.run_id AND s.operation_key=o.operation_key)").Scan(&legacy); e != nil {
		return nil, e
	}
	return map[string]any{"summary": stats, "recent": recent, "legacy_normalized_only": legacy}, nil
}
