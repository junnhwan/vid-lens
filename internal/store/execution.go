package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func (s *Store) Claim(ctx context.Context, id string, generation int64, owner string) (*domain.Claim, error) {
	var c *domain.Claim
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRun(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.DispatchGeneration != generation || domain.Terminal(r.Status) || r.Status == "waiting_dependency" {
			return nil
		}
		if r.Status == "cancel_requested" {
			return terminate(ctx, tx, r, "canceled", nil)
		}
		now := s.DBTime(ctx)
		if !r.DeadlineAt.After(now) {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "TASK_DEADLINE_EXCEEDED", Message: "处理已达到总时限。", CanRetry: true})
		}
		if r.LeaseExpiresAt != nil && r.LeaseExpiresAt.After(now) && r.Status == "running" {
			return nil
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", r.ConfigID)
		if e != nil {
			return e
		}
		queueLimit := p.QueueSeconds
		if r.Kind == "qa.answer" {
			queueLimit = p.AnswerQueueSeconds
		}
		if r.Status == "queued" && now.Sub(r.QueuedAt) > time.Duration(queueLimit)*time.Second {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "QUEUE_WAIT_TIMEOUT", Message: "等待处理已超时。", CanRetry: true})
		}
		if r.Status == "running" {
			if e = charge(ctx, tx, r.ID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome='lease_expired' WHERE run_id=$1 AND ended_at IS NULL", r.ID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "UPDATE job_runs SET failure_count=failure_count+1 WHERE id=$1", r.ID); e != nil {
				return e
			}
			r, e = loadRun(ctx, tx, r.ID)
			if e != nil {
				return e
			}
		}
		if r.FailureCount > p.MaxRetries(r.Kind) || r.ActiveMS >= p.ActiveBudget(r.Kind).Milliseconds() {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "TASK_DEADLINE_EXCEEDED", Message: "处理预算已用完。", CanRetry: true})
		}
		aid := domain.ID()
		r.ExecutionEpoch++
		r.AttemptNo++
		_, e = tx.Exec(ctx, "UPDATE job_runs SET status='running',execution_epoch=$2,attempt_no=$3,lease_owner=$4,lease_expires_at=now()+interval '60 seconds',retry_at=NULL,retry_not_before=NULL,updated_at=now() WHERE id=$1", id, r.ExecutionEpoch, r.AttemptNo, owner)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO job_attempts(id,run_id,attempt_no,execution_epoch,dispatch_generation,worker_instance) VALUES($1,$2,$3,$4,$5,$6)", aid, id, r.AttemptNo, r.ExecutionEpoch, generation, owner)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='running',updated_at=now() WHERE run_id=$1", id); e != nil {
			return e
		}
		if e = event(ctx, tx, id, nil); e != nil {
			return e
		}
		r, e = loadRun(ctx, tx, id)
		if e != nil {
			return e
		}
		c = &domain.Claim{Run: r, Owner: owner, Epoch: r.ExecutionEpoch, AttemptID: aid}
		return nil
	})
	return c, e
}
func charge(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, "UPDATE job_runs r SET active_ms=r.active_ms+GREATEST(0,EXTRACT(EPOCH FROM (LEAST(clock_timestamp(),COALESCE(r.lease_expires_at,clock_timestamp()))-a.heartbeat_at))*1000)::bigint FROM job_attempts a WHERE r.id=$1 AND a.run_id=r.id AND a.execution_epoch=r.execution_epoch AND a.ended_at IS NULL", id)
	return e
}
func (s *Store) Heartbeat(ctx context.Context, c domain.Claim) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if e := charge(ctx, tx, c.Run.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE job_attempts SET heartbeat_at=now() WHERE id=$1", c.AttemptID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()+interval '60 seconds' WHERE id=$1", c.Run.ID)
		return e
	})
}
func (s *Store) Check(ctx context.Context, c domain.Claim) error {
	return s.Tx(ctx, func(tx pgx.Tx) error { return fence(ctx, tx, c) })
}
func terminate(ctx context.Context, tx pgx.Tx, r domain.Run, status string, f *domain.Failure) error {
	if f != nil && f.DiagnosticID == "" {
		f.DiagnosticID = "vl_" + r.ID
	}
	if domain.Terminal(r.Status) {
		return nil
	}
	if e := charge(ctx, tx, r.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE job_runs SET status=$2,failure=$3,lease_owner=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1", r.ID, status, nullableJSON(f)); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome=$2 WHERE run_id=$1 AND ended_at IS NULL", r.ID, status); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE generation_requests SET status=$2,failure=$3,revision=revision+1,updated_at=now() WHERE active_run_id=$1 AND status IN ('waiting_source','generating')", r.ID, status, nullableJSON(f)); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE qa_turns SET status=$2,failure=$3,revision=revision+1,updated_at=now(),expires_at=now()+interval '30 days' WHERE run_id=$1 AND status NOT IN ('completed','failed','canceled')", r.ID, status, nullableJSON(f)); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE qa_conversations c SET active_turn_id=NULL FROM qa_turns t WHERE t.run_id=$1 AND c.id=t.conversation_id AND c.active_turn_id=t.id", r.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "DELETE FROM asset_refs WHERE owner_type='run' AND owner_id=$1", r.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "UPDATE media_assets SET expires_at=LEAST(COALESCE(expires_at,now()+interval '6 hours'),now()+interval '6 hours') WHERE run_id=$1 AND NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=media_assets.id AND (expires_at IS NULL OR expires_at>now()))", r.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "DELETE FROM storage_reservations WHERE run_id=$1", r.ID); e != nil {
		return e
	}
	if r.DependencyRunID != nil {
		if e := cancelUnused(ctx, tx, *r.DependencyRunID); e != nil {
			return e
		}
	}
	return event(ctx, tx, r.ID, nil)
}
func nullableJSON(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case *domain.Failure:
		if x == nil {
			return nil
		}
	}
	return domain.JSON(v)
}
func (s *Store) Fail(ctx context.Context, c domain.Claim, f domain.Failure, transient bool, delay time.Duration) (bool, error) {
	retry := false
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRun(ctx, tx, c.Run.ID)
		if e != nil {
			return e
		}
		if r.ExecutionEpoch != c.Epoch || r.LeaseOwner == nil || *r.LeaseOwner != c.Owner {
			return domain.ErrFenced
		}
		if domain.Terminal(r.Status) {
			return nil
		}
		if r.Status == "cancel_requested" {
			return terminate(ctx, tx, r, "canceled", nil)
		}
		if e = charge(ctx, tx, r.ID); e != nil {
			return e
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", r.ConfigID)
		if e != nil {
			return e
		}
		r, e = loadRun(ctx, tx, r.ID)
		if e != nil {
			return e
		}
		retry = transient && r.FailureCount < p.MaxRetries(r.Kind) && r.ActiveMS < p.ActiveBudget(r.Kind).Milliseconds() && time.Now().Add(delay).Before(r.DeadlineAt)
		if !retry {
			return terminate(ctx, tx, r, "failed", &f)
		}
		if _, e = tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome='retry_wait' WHERE id=$1", c.AttemptID); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE job_runs SET status='retry_wait',failure_count=failure_count+1,retry_not_before=now()+$2*interval '1 millisecond',retry_at=NULL,lease_owner=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1", r.ID, delay.Milliseconds())
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='retry_wait',updated_at=now() WHERE run_id=$1", r.ID); e != nil {
			return e
		}
		return event(ctx, tx, r.ID, nil)
	})
	return retry, e
}
func (s *Store) Checkpoint(ctx context.Context, c domain.Claim, data any) error {
	b := domain.JSON(data)
	if len(b) > 2<<20 {
		return errors.New("checkpoint exceeds limit")
	}
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE job_runs SET checkpoint=$2,updated_at=now() WHERE id=$1", c.Run.ID, b)
		return e
	})
}
func (s *Store) Suspend(ctx context.Context, c domain.Claim) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRun(ctx, tx, c.Run.ID)
		if e != nil {
			return e
		}
		if r.ExecutionEpoch != c.Epoch {
			return domain.ErrFenced
		}
		if r.Status == "cancel_requested" {
			return terminate(ctx, tx, r, "canceled", nil)
		}
		if domain.Terminal(r.Status) {
			return nil
		}
		if e = fence(ctx, tx, c); e != nil {
			return e
		}
		if e = charge(ctx, tx, r.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE job_runs SET status='retry_wait',retry_not_before=now()+interval '5 seconds',lease_owner=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1", r.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE job_attempts SET ended_at=now(),outcome='interrupted' WHERE id=$1", c.AttemptID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='retry_wait',updated_at=now() WHERE run_id=$1", r.ID); e != nil {
			return e
		}
		return event(ctx, tx, r.ID, nil)
	})
}
func (s *Store) Step(ctx context.Context, c domain.Claim, step domain.Step) (domain.Step, error) {
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		step.RunID = c.Run.ID
		if step.FrameIDs == nil {
			step.FrameIDs = []string{}
		}
		step.Frames = []domain.Frame{}
		if step.FirstSeq == 0 {
			_ = tx.QueryRow(ctx, "SELECT (step->>'first_seq')::bigint,(step->>'started_at')::timestamptz FROM run_events WHERE run_id=$1 AND step_id=$2 ORDER BY seq DESC LIMIT 1", c.Run.ID, step.ID).Scan(&step.FirstSeq, &step.StartedAt)
		}
		if step.Phase != "" {
			var old *string
			if e := tx.QueryRow(ctx, "SELECT phase FROM job_runs WHERE id=$1", c.Run.ID).Scan(&old); e != nil {
				return e
			}
			if old == nil || *old != step.Phase {
				if _, e := tx.Exec(ctx, "UPDATE job_runs SET phase=$2 WHERE id=$1", c.Run.ID, step.Phase); e != nil {
					return e
				}
				if e := event(ctx, tx, c.Run.ID, nil); e != nil {
					return e
				}
			}
		}
		if _, e := tx.Exec(ctx, "UPDATE job_runs SET had_activity=true WHERE id=$1", c.Run.ID); e != nil {
			return e
		}
		return event(ctx, tx, c.Run.ID, &step)
	})
	return step, e
}

type Outbox struct {
	ID                 int64     `json:"id"`
	RunID              string    `json:"run_id"`
	DispatchGeneration int64     `json:"dispatch_generation"`
	Queue              string    `json:"queue"`
	Failures           int       `json:"failures"`
	CreatedAt          time.Time `json:"created_at"`
}

func (s *Store) Outbox(ctx context.Context) ([]Outbox, error) {
	var out []Outbox
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = many[Outbox](ctx, tx, "UPDATE task_outbox SET claimed_until=now()+interval '30 seconds' WHERE id IN (SELECT id FROM task_outbox WHERE delivered_at IS NULL AND not_before<=now() AND (claimed_until IS NULL OR claimed_until<now()) ORDER BY id LIMIT 10) RETURNING to_jsonb(task_outbox)")
		return e
	})
	return out, e
}
func (s *Store) OutboxResult(ctx context.Context, o Outbox, delivered bool) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if delivered {
			_, e := tx.Exec(ctx, "UPDATE task_outbox SET delivered_at=now(),claimed_until=NULL WHERE id=$1", o.ID)
			return e
		}
		delays := []int{1, 5, 15, 30, 60}
		d := delays[min(o.Failures, len(delays)-1)]
		_, e := tx.Exec(ctx, "UPDATE task_outbox SET failures=failures+1,claimed_until=NULL,last_error='broker unavailable',not_before=now()+$2*interval '1 second' WHERE id=$1", o.ID, d)
		return e
	})
}
func (s *Store) Recoverable(ctx context.Context) ([]domain.Run, error) {
	return many[domain.Run](ctx, s.Pool, "SELECT to_jsonb(r) FROM job_runs r WHERE status NOT IN ('succeeded','failed','canceled') ORDER BY created_at LIMIT 100")
}
func (s *Store) Repair(ctx context.Context, id string, generation int64, broker string, retryAt *time.Time) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		r, e := loadRun(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.DispatchGeneration != generation || domain.Terminal(r.Status) {
			return nil
		}
		now := s.DBTime(ctx)
		if r.Status == "cancel_requested" {
			return terminate(ctx, tx, r, "canceled", nil)
		}
		if !r.DeadlineAt.After(now) {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "TASK_DEADLINE_EXCEEDED", Message: "处理已达到总时限。", CanRetry: true})
		}
		p, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", r.ConfigID)
		if e != nil {
			return e
		}
		waitLimit := p.QueueSeconds
		if r.Kind == "qa.answer" {
			waitLimit = p.AnswerQueueSeconds
		}
		waitFrom := r.QueuedAt
		if r.RetryNotBefore != nil && r.RetryNotBefore.After(waitFrom) {
			waitFrom = *r.RetryNotBefore
		}
		if (r.Status == "queued" || r.Status == "retry_wait") && now.Sub(waitFrom) > time.Duration(waitLimit)*time.Second {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "QUEUE_WAIT_TIMEOUT", Message: "等待处理已超时。", CanRetry: true})
		}
		if r.Status == "running" && now.Sub(r.UpdatedAt) > 5*time.Minute {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "INTERNAL_FAILURE", Message: "处理长时间没有业务进展。", CanRetry: true})
		}
		var oldest *time.Time
		if e = tx.QueryRow(ctx, "SELECT min(created_at) FROM task_outbox WHERE run_id=$1 AND dispatch_generation=$2 AND delivered_at IS NULL", id, generation).Scan(&oldest); e != nil {
			return e
		}
		if oldest != nil && now.Sub(*oldest) > 10*time.Minute {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "QUEUE_WAIT_TIMEOUT", Message: "暂时无法调度处理任务。", CanRetry: true})
		}
		if r.Status == "waiting_dependency" {
			return wakeDependency(ctx, tx, r)
		}
		if r.LeaseExpiresAt != nil && r.LeaseExpiresAt.After(now) {
			return nil
		}
		if broker == "unknown" {
			return nil
		}
		if broker == "archived" || broker == "completed" {
			return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "INTERNAL_FAILURE", Message: "执行已结束但没有可用结果。", CanRetry: true})
		}
		if retryAt != nil && r.Status == "retry_wait" {
			if r.RetryAt == nil || !r.RetryAt.Equal(*retryAt) {
				if _, e = tx.Exec(ctx, "UPDATE job_runs SET retry_at=$2 WHERE id=$1", id, retryAt); e != nil {
					return e
				}
				return event(ctx, tx, id, nil)
			}
		}
		if broker != "missing" {
			return nil
		}
		if r.RetryNotBefore != nil && r.RetryNotBefore.After(now) {
			return nil
		}
		var outboxPending bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM task_outbox WHERE run_id=$1 AND dispatch_generation=$2 AND delivered_at IS NULL)", id, generation).Scan(&outboxPending); e != nil {
			return e
		}
		if outboxPending {
			var age time.Duration
			var created time.Time
			if e = tx.QueryRow(ctx, "SELECT created_at FROM task_outbox WHERE run_id=$1 AND dispatch_generation=$2", id, generation).Scan(&created); e != nil {
				return e
			}
			age = now.Sub(created)
			if age > 10*time.Minute {
				return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "QUEUE_WAIT_TIMEOUT", Message: "暂时无法调度处理任务。", CanRetry: true})
			}
			return nil
		}
		if _, e = tx.Exec(ctx, "UPDATE job_runs SET dispatch_generation=dispatch_generation+1,queued_at=now(),retry_at=NULL,updated_at=now() WHERE id=$1", id); e != nil {
			return e
		}
		queue := "generation"
		if r.Kind == "qa.answer" {
			queue = "interactive"
		}
		_, e = tx.Exec(ctx, "INSERT INTO task_outbox(run_id,dispatch_generation,queue) VALUES($1,$2,$3)", id, generation+1, queue)
		return e
	})
}
func wakeDependency(ctx context.Context, tx pgx.Tx, r domain.Run) error {
	if r.DependencyRunID == nil {
		return nil
	}
	d, e := loadRun(ctx, tx, *r.DependencyRunID)
	if e != nil {
		return e
	}
	var through int64
	if e = tx.QueryRow(ctx, "SELECT dependency_seq FROM job_runs WHERE id=$1", r.ID).Scan(&through); e != nil {
		return e
	}
	if d.LastEventSeq > through {
		steps, e := many[domain.Step](ctx, tx, "SELECT step FROM (SELECT DISTINCT ON(step_id) step,seq FROM run_events WHERE run_id=$1 AND step_id IS NOT NULL AND seq>$2 ORDER BY step_id,seq DESC) x ORDER BY seq", d.ID, through)
		if e != nil {
			return e
		}
		for _, step := range steps {
			step.ID = "restore_" + r.ID + "_" + step.ID
			step.RunID = r.ID
			step.FirstSeq = 0
			step.LastSeq = 0
			step.Phase = "prepare_source"
			if e = event(ctx, tx, r.ID, &step); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "UPDATE job_runs SET had_activity=true WHERE id=$1", r.ID); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE job_runs SET dependency_seq=$2 WHERE id=$1", r.ID, d.LastEventSeq); e != nil {
			return e
		}
	}
	if d.Status == "failed" || d.Status == "canceled" {
		return terminate(ctx, tx, r, "failed", &domain.Failure{Code: "MEDIA_UNAVAILABLE", Message: "无法恢复原视频画面。", CanRetry: d.Failure != nil && d.Failure.CanRetry})
	}
	if d.Status != "succeeded" {
		return nil
	}
	if _, e = tx.Exec(ctx, "UPDATE job_runs SET status='queued',dependency_run_id=NULL,dispatch_generation=dispatch_generation+1,queued_at=now(),updated_at=now() WHERE id=$1", r.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE qa_turns SET status='queued',updated_at=now() WHERE run_id=$1", r.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO task_outbox(run_id,dispatch_generation,queue) VALUES($1,$2,'interactive')", r.ID, r.DispatchGeneration+1); e != nil {
		return e
	}
	return event(ctx, tx, r.ID, nil)
}
