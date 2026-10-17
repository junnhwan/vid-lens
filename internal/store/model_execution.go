package store

import (
	"context"
	"vidlens/internal/domain"
)

type ModelExecutionStats struct {
	Calls       int   `json:"calls"`
	Submissions int   `json:"submissions"`
	ResponseMS  int64 `json:"response_ms"`
}

// Aggregate transport observations, including incomplete responses. These are
// persisted independently of the worker checkpoint and never reset on takeover.
func (s *Store) ModelExecution(ctx context.Context, run string) (ModelExecutionStats, error) {
	return one[ModelExecutionStats](ctx, s.Pool, `SELECT jsonb_build_object('calls',count(DISTINCT operation_key),'submissions',count(*),'response_ms',COALESCE(sum((reporting->>'total_ms')::bigint),0)) FROM model_submissions WHERE run_id=$1`, run)
}

func (s *Store) OpenModelSubmissions(ctx context.Context, run string, call int) ([]domain.Step, error) {
	return many[domain.Step](ctx, s.Pool, `SELECT step FROM (SELECT DISTINCT ON (step_id) step FROM run_events WHERE run_id=$1 AND step->>'kind'='model_submission' AND (step->>'model_call')::integer=$2 ORDER BY step_id,seq DESC) x WHERE step->>'status'='running'`, run, call)
}

func modelSubmissionLimit(submissions int) error {
	return &domain.Problem{Status: 422, Code: "PROVIDER_UNAVAILABLE", Detail: "同一模型调用多次未取得完整响应，已停止重复发送；已保存的观察和章节保留，可从检查点继续。", Meta: map[string]any{"can_retry": true, "retry_action": "resume_checkpoint", "submissions": submissions}}
}
