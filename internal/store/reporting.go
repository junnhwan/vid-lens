package store

import (
	"context"
	"vidlens/internal/domain"
)

// A known, fully received truncation may change the next review task, but must
// never cause the original frozen logical input to be sent again on takeover.
func (s *Store) ModelOutputTruncated(ctx context.Context, run, key string) (bool, error) {
	var truncated bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT reporting->>'finish_reason'='length' AND reporting->>'stream_status'='complete' FROM model_submissions WHERE run_id=$1 AND operation_key=$2 ORDER BY submission DESC LIMIT 1),false)`, run, key).Scan(&truncated)
	return truncated, err
}

// A known context rejection is durable too: takeover must narrow the next
// logical input rather than paying to repeat a rejected one.
func (s *Store) ModelContextExceeded(ctx context.Context, run, key string) (bool, error) {
	var rejected bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT reporting->>'failure_code'='MODEL_CONTEXT_EXCEEDED' FROM model_submissions WHERE run_id=$1 AND operation_key=$2 ORDER BY submission DESC LIMIT 1),false)`, run, key).Scan(&rejected)
	return rejected, err
}

func (s *Store) ModelResponseBudgetExhausted(ctx context.Context, run, key string) (bool, error) {
	var limited bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT reporting->>'failure_code'='MODEL_RESPONSE_BUDGET_EXHAUSTED' FROM model_submissions WHERE run_id=$1 AND operation_key=$2 ORDER BY submission DESC LIMIT 1),false)`, run, key).Scan(&limited)
	return limited, err
}

func (s *Store) ModelCallBudgetExhausted(ctx context.Context, run, key string) (bool, error) {
	var limited bool
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT reporting->>'failure_code'='MODEL_CALL_BUDGET_EXHAUSTED' FROM model_submissions WHERE run_id=$1 AND operation_key=$2 ORDER BY submission DESC LIMIT 1),false)`, run, key).Scan(&limited)
	return limited, err
}

// Observations are attached to an already authorized physical submission. They
// cannot publish results or renew a run, including after cancellation/fencing.
func (s *Store) RecordSubmission(ctx context.Context, c domain.Claim, key string, n int, report any) error {
	_, e := s.Pool.Exec(ctx, `UPDATE model_submissions SET reporting=reporting || $5::jsonb WHERE run_id=$1 AND operation_key=$2 AND submission=$3 AND attempt_id=$4`, c.Run.ID, key, n, c.AttemptID, domain.JSON(report))
	return e
}
func (s *Store) ModelReporting(ctx context.Context, limit, offset int) (map[string]any, error) {
	stats, e := one[map[string]any](ctx, s.Pool, `SELECT jsonb_build_object('physical_submissions',count(*),'reported',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported'),'unknown',count(*) FILTER(WHERE COALESCE(reporting->'cache_read'->>'status','unknown')<>'reported'),'hits',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported' AND (reporting->'cache_read'->>'value')::bigint>0),'reported_zero',count(*) FILTER(WHERE reporting->'cache_read'->>'status'='reported' AND (reporting->'cache_read'->>'value')::bigint=0),'incomplete',count(*) FILTER(WHERE reporting->>'stream_status'<>'complete'),'cost_reporting','unknown') FROM model_submissions`)
	if e != nil {
		return nil, e
	}
	recent, e := many[map[string]any](ctx, s.Pool, `SELECT jsonb_build_object('run_id',o.run_id,'operation_key',o.operation_key,'attempt_id',o.attempt_id,'submission',o.submission,'created_at',o.created_at,'model_revision_id',m.id,'display_name',COALESCE(m.display_name,c.profile->>'model_name'),'provider_id',p.id,'provider_name',pr.name,'reporting',o.reporting) FROM model_submissions o JOIN job_runs r ON r.id=o.run_id JOIN runtime_config_revisions c ON c.id=COALESCE(o.reporting->>'route_config_id',r.config_id) LEFT JOIN model_revisions m ON m.config_id=c.id LEFT JOIN catalog_models cm ON cm.id=m.model_id LEFT JOIN model_providers p ON p.id=cm.provider_id LEFT JOIN provider_revisions pr ON pr.id=m.provider_revision_id ORDER BY o.created_at DESC, o.run_id, o.operation_key, o.submission DESC LIMIT $1 OFFSET $2`, limit, offset)
	if e != nil {
		return nil, e
	}
	var legacy int64
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations o WHERE kind='model' AND NOT EXISTS(SELECT 1 FROM model_submissions s WHERE s.run_id=o.run_id AND s.operation_key=o.operation_key)").Scan(&legacy); e != nil {
		return nil, e
	}
	return map[string]any{"summary": stats, "recent": recent, "legacy_normalized_only": legacy, "limit": limit, "offset": offset}, nil
}
