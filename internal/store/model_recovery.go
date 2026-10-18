package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type modelRecovery struct {
	OperationKey    string    `json:"operation_key"`
	TriggerCode     string    `json:"trigger_code"`
	NotBefore       time.Time `json:"not_before"`
	Submissions     int       `json:"submissions_before"`
	SubmissionLimit int       `json:"submission_limit"`
}

type RetryWait struct{ After time.Duration }

func (e *RetryWait) Error() string { return "execution retry is not due" }

// An extra physical send is a separate, durable recovery decision. It never
// creates a new request, changes a frozen model input, or resets the ledger.
// Each logical call can receive one extra send, at most twice in a new run.
func (s *Store) ScheduleModelRecovery(ctx context.Context, c domain.Claim, key string) (delay time.Duration, reserved bool, err error) {
	err = s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		r, e := loadRun(ctx, tx, c.Run.ID)
		if e != nil || r.Kind != "article.generate" {
			return e
		}
		spec, e := one[domain.GenerationSpec](ctx, tx, "SELECT generation_spec FROM generation_requests WHERE active_run_id=$1 ORDER BY created_at LIMIT 1", r.ID)
		if e != nil {
			return e
		}
		if spec.DetailPolicy == nil || spec.DetailPolicy.ProviderRecoveryLimit <= 0 {
			return nil
		}
		var used int
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT count(*),COALESCE(bool_or(operation_key=$2),false) FROM run_operations WHERE run_id=$1 AND kind='model_recovery'", r.ID, "auto-recovery:"+key).Scan(&used, &exists); e != nil {
			return e
		}
		if exists || used >= min(2, spec.DetailPolicy.ProviderRecoveryLimit) {
			return nil
		}
		op, e := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 AND kind='model' FOR UPDATE", r.ID, key)
		if e != nil {
			return e
		}
		routes, e := modelRoutes(ctx, tx, r.ConfigID)
		if e != nil {
			return e
		}
		base := max(3, len(routes))
		if op.Status == "completed" || op.Submissions != base {
			return nil
		}
		// Only the exact, already frozen logical input is eligible.
		var frozen bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND kind='model_input' AND status='completed')", r.ID, "input:"+key).Scan(&frozen); e != nil || !frozen {
			return e
		}
		var now time.Time
		if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
			return e
		}
		delay = 30 * time.Second
		if !now.Add(delay).Before(r.DeadlineAt) {
			return nil
		}
		data := modelRecovery{OperationKey: key, TriggerCode: "PROVIDER_UNAVAILABLE", NotBefore: now.Add(delay), Submissions: op.Submissions, SubmissionLimit: base + 1}
		_, e = tx.Exec(ctx, "INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result,completed_at) VALUES($1,$2,$3,'model_recovery',$4,'completed',$5,now())", r.ID, "auto-recovery:"+key, c.AttemptID, Hash(data), domain.JSON(data))
		reserved = e == nil
		return e
	})
	return
}

func recoveredModelLimit(ctx context.Context, tx pgx.Tx, run, key string, base int) (int, error) {
	var raw json.RawMessage
	err := tx.QueryRow(ctx, "SELECT result FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND kind='model_recovery' AND status='completed'", run, "auto-recovery:"+key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return base, nil
	}
	if err != nil {
		return 0, err
	}
	var saved modelRecovery
	if err = json.Unmarshal(raw, &saved); err != nil {
		return 0, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return 0, err
	}
	if now.Before(saved.NotBefore) {
		return 0, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "正在等待模型自动恢复。", Transient: true, RetryAfter: saved.NotBefore.Sub(now)}
	}
	return max(base, min(base+1, saved.SubmissionLimit)), nil
}

func modelRetryNotice(ctx context.Context, tx pgx.Tx, r domain.Run, f domain.Failure, delay time.Duration) error {
	if r.Kind != "article.generate" {
		return nil
	}
	now := time.Now().UTC()
	step := domain.Step{ID: fmt.Sprintf("model_retry_%s_%d", r.ID, r.AttemptNo), RunID: r.ID, Kind: "retry", Status: "completed", Title: fmt.Sprintf("正在自动恢复 · 第 %d 次", r.FailureCount+1), StartedAt: &now, FinishedAt: &now, FrameIDs: []string{}, Outcome: &domain.Outcome{Message: fmt.Sprintf("%s 已保存的来源、观察和章节保留；将在 %s 之后继续。", f.Message, now.Add(delay).Format(time.RFC3339)), EvidenceStatus: "not_assessed"}}
	if r.Phase != nil {
		step.Phase = *r.Phase
	}
	return event(ctx, tx, r.ID, &step)
}
