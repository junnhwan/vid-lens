package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// RunDiagnostics is an allow-list projection. It never returns prompts, tool
// arguments, raw candidates, question text, credential refs or signed URLs.
func (s *Store) RunDiagnostics(ctx context.Context, id string) (out map[string]any, err error) {
	err = s.read(ctx, func(ctx context.Context, tx pgx.Tx) error { out, err = s.runDiagnostics(ctx, tx, id); return err })
	return out, err
}
func (s *Store) runDiagnostics(ctx context.Context, q Querier, id string) (map[string]any, error) {
	run, err := one[map[string]any](ctx, q, `SELECT jsonb_build_object('id',id,'kind',kind,'status',status,'phase',phase,'snapshot_id',snapshot_id,'created_at',created_at,'updated_at',updated_at,'deadline_at',deadline_at,'active_ms',active_ms,'execution_epoch',execution_epoch,'attempt_no',attempt_no,'failure',failure,'progress',checkpoint->'progress','harness_revision',checkpoint->'harness_revision') FROM job_runs WHERE id=$1`, id)
	if err != nil {
		return nil, NotFound(err)
	}
	requests, err := many[map[string]any](ctx, q, `SELECT jsonb_build_object('request_id',id,'http_trace_id',http_trace_id,'status',status,'retry_of_id',retry_of_id,'detail_level',generation_spec->>'detail_level','detail_policy_revision',generation_spec->>'detail_policy_revision') FROM generation_requests WHERE active_run_id=$1 OR EXISTS(SELECT 1 FROM jsonb_array_elements(segments) e WHERE e->>'run_id'=$1) ORDER BY created_at LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	turns, err := many[map[string]any](ctx, q, `SELECT jsonb_build_object('turn_id',id,'request_id',request_id,'http_trace_id',http_trace_id,'status',status,'retry_of_id',retry_of_id) FROM qa_turns WHERE run_id=$1 ORDER BY created_at LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	attempts, err := many[map[string]any](ctx, q, `SELECT jsonb_build_object('id',id,'attempt_no',attempt_no,'execution_epoch',execution_epoch,'started_at',started_at,'ended_at',ended_at,'outcome',outcome) FROM job_attempts WHERE run_id=$1 ORDER BY attempt_no LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	ops, err := many[map[string]any](ctx, q, `SELECT jsonb_build_object('operation_key',operation_key,'attempt_id',attempt_id,'kind',kind,'status',status,'submissions',submissions,'started_at',started_at,'completed_at',completed_at,'candidate_id',CASE WHEN kind='candidate_attempt' THEN split_part(operation_key,':',2) END,'candidate_status',CASE WHEN kind='candidate_attempt' THEN result->>'status' END,'candidate_hash',CASE WHEN kind='candidate_attempt' THEN result->>'arguments_hash' END,'draft_version',CASE WHEN kind='article_draft' THEN result->'version' END,'tool',CASE WHEN kind='tool_feedback' THEN result->>'tool' END,'effective_progress',CASE WHEN kind='tool_feedback' THEN result->'effective_progress' END,'rejected',CASE WHEN kind='tool_feedback' THEN result->'rejected' END,'parent_candidate_id',CASE WHEN kind='field_repair' THEN result->>'parent_candidate_id' END,'tool_calls',CASE WHEN kind='model' THEN (SELECT jsonb_agg(jsonb_build_object('tool_call_id',t->>'id','tool',t->'function'->>'name')) FROM jsonb_array_elements(COALESCE(result->'tool_calls','[]')) t) END) FROM run_operations WHERE run_id=$1 ORDER BY started_at,operation_key LIMIT 400`, id)
	if err != nil {
		return nil, err
	}
	subs, err := many[map[string]any](ctx, q, `SELECT jsonb_build_object('operation_key',operation_key,'attempt_id',attempt_id,'submission',submission,'created_at',created_at,'finish_reason',reporting->'finish_reason','stream_status',reporting->'stream_status','http_status',reporting->'http_status','failure_message',reporting->'failure_message','failure_code',reporting->'failure_code','route_cooldown_until',reporting->'route_cooldown_until','first_meaningful_delta_ms',reporting->'first_meaningful_delta_ms','tool_arguments_valid',reporting->'tool_arguments_valid','output_limit_reached',reporting->'output_limit_reached','input',reporting->'input','output',reporting->'output','reasoning',reporting->'reasoning','requested_output_tokens',reporting->'requested_output_tokens','thinking_request',reporting->'thinking_request','cache_read',reporting->'cache_read','budget',reporting->'budget','response_finished_at',reporting->'response_finished_at','total_ms',reporting->'total_ms') FROM model_submissions WHERE run_id=$1 ORDER BY created_at,submission LIMIT 300`, id)
	if err != nil {
		return nil, err
	}
	facts, err := many[json.RawMessage](ctx, q, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='source_diagnostic' ORDER BY started_at,operation_key LIMIT 40`, id)
	if err != nil {
		return nil, err
	}
	budgets, err := many[json.RawMessage](ctx, q, `SELECT jsonb_build_object('budget',result->'budget','reason',result->>'reason','model_revision_id',result->>'model_revision_id') FROM run_operations WHERE run_id=$1 AND kind='budget_diagnostic' ORDER BY started_at,operation_key LIMIT 40`, id)
	if err != nil {
		return nil, err
	}
	recoveries, err := many[modelRecovery](ctx, q, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='model_recovery' ORDER BY started_at,operation_key LIMIT 10`, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema_version": 1, "run": run, "requests": requests, "turns": turns, "attempts": attempts, "operations": ops, "submissions": subs, "source_diagnostics": facts, "budget_diagnostics": budgets, "automatic_recoveries": recoveries, "usage_note": "Token estimates are heuristic. Provider usage and cache billing remain unknown when unreported.", "limits": map[string]int{"requests": 100, "turns": 100, "attempts": 100, "operations": 400, "submissions": 300, "source_diagnostics": 40, "budget_diagnostics": 40, "automatic_recoveries": 10}}, nil
}

// Only the caller's numeric budget and rejection category belong here. Never
// freeze rejected prompts or credentials just to explain a preflight failure.
func (s *Store) BudgetDiagnostic(ctx context.Context, c domain.Claim, key string, data any) error {
	key = "budget-diagnostic:" + key
	if _, err := s.BeginOperation(ctx, c, key, "budget_diagnostic", data, false); err != nil {
		return err
	}
	return s.FinishOperation(ctx, c, key, data, "", nil)
}

func (s *Store) SourceDiagnostic(ctx context.Context, c domain.Claim, key string, data any) error {
	key = "source-diagnostic:" + c.AttemptID + ":" + key
	if _, err := s.BeginOperation(ctx, c, key, "source_diagnostic", data, false); err != nil {
		return err
	}
	return s.FinishOperation(ctx, c, key, data, "", nil)
}

func (s *Store) NotifySourceMetadata(ctx context.Context, c domain.Claim) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		return event(ctx, tx, c.Run.ID, nil)
	})
}
