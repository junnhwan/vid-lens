package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
	"vidlens/internal/domain"
)

const (
	MaxConcurrentProbes = 3
	ProbeQueueTimeout   = time.Minute
	ProbeRunTimeout     = 5 * time.Minute
)

type Probe struct {
	ID          string     `json:"id"`
	ModelID     string     `json:"model_id"`
	RevisionID  string     `json:"revision_id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Epoch       int64      `json:"epoch"`
	Submissions int        `json:"submissions"`
	Result      any        `json:"result"`
	FailureCode *string    `json:"failure_code"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	DeadlineAt  time.Time  `json:"deadline_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (s *Store) CreateProbe(ctx context.Context, model, revision, mode, key string) (Accepted, error) {
	var a Accepted
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var found bool
		var e error
		scope := "probe:" + model
		h := Hash(map[string]string{"revision_id": revision, "mode": mode})
		if a, found, e = replay(ctx, tx, scope, key, h); e != nil || found {
			return e
		}
		if e = expireProbes(ctx, tx); e != nil {
			return e
		}
		var current string
		var provider bool
		if e = tx.QueryRow(ctx, "SELECT m.current_revision_id,p.enabled FROM catalog_models m JOIN model_providers p ON p.id=m.provider_id WHERE m.id=$1", model).Scan(&current, &provider); e != nil {
			return NotFound(e)
		}
		if revision != current {
			return domain.Err(409, "REVISION_CONFLICT", "模型修订已改变，请刷新后检查。")
		}
		if !provider {
			return domain.Err(409, "MODEL_UNAVAILABLE", "请先启用供应商。")
		}
		if mode != "capabilities" && mode != "cache" {
			return domain.Err(422, "INVALID_ARGUMENT", "未知探测模式。")
		}
		var duplicate bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_probes WHERE model_id=$1 AND status IN ('queued','running'))", model).Scan(&duplicate); e != nil {
			return e
		}
		if duplicate {
			return domain.Err(409, "PROBE_ALREADY_RUNNING", "这个模型正在检查，请等待当前结果，避免重复调用。")
		}
		var n int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM model_probes WHERE status IN ('queued','running')").Scan(&n); e != nil {
			return e
		}
		if n >= MaxConcurrentProbes {
			return domain.Err(429, "ADMISSION_LIMITED", "同时最多检查 3 个模型，请等一个检查结束后再试。")
		}
		id := domain.ID()
		if _, e = tx.Exec(ctx, "INSERT INTO model_probes(id,model_id,revision_id,mode,deadline_at) VALUES($1,$2,$3,$4,now()+$5*interval '1 second')", id, model, revision, mode, int(ProbeQueueTimeout.Seconds())); e != nil {
			return e
		}
		a, e = remember(ctx, tx, scope, key, h, id, 86400)
		return e
	})
	return a, e
}
func (s *Store) Probes(ctx context.Context, model string) ([]Probe, error) {
	return many[Probe](ctx, s.Pool, "SELECT to_jsonb(p) FROM model_probes p WHERE model_id=$1 ORDER BY created_at DESC LIMIT 30", model)
}
func (s *Store) PendingProbes(ctx context.Context) ([]Probe, error) {
	// Never replay an unknown physical model outcome after a process crash.
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error { return expireProbes(ctx, tx) })
	if e != nil {
		return nil, e
	}
	return many[Probe](ctx, s.Pool, "SELECT to_jsonb(p) FROM model_probes p WHERE status='queued' ORDER BY created_at LIMIT 3")
}

func expireProbes(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE model_probes SET status=CASE WHEN submissions>0 THEN 'unknown' ELSE 'failed' END,
	 failure_code=CASE WHEN deadline_at<=now() AND status='queued' THEN 'PROBE_QUEUE_TIMEOUT'
	 WHEN deadline_at<=now() THEN 'PROBE_EXECUTION_TIMEOUT'
	 WHEN submissions>0 THEN 'PROVIDER_OUTCOME_UNKNOWN' ELSE 'PROBE_INTERRUPTED' END,updated_at=now()
	 WHERE status IN ('queued','running') AND (deadline_at<=now() OR status='running' AND updated_at<now()-interval '60 seconds')`)
	return err
}

func (s *Store) ClaimProbe(ctx context.Context, id string) (*Probe, error) {
	var p *Probe
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, e := one[Probe](ctx, tx, "UPDATE model_probes SET status='running',epoch=epoch+1,started_at=now(),deadline_at=now()+$2*interval '1 second',updated_at=now() WHERE id=$1 AND status='queued' AND deadline_at>now() RETURNING to_jsonb(model_probes)", id, int(ProbeRunTimeout.Seconds()))
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e == nil {
			p = &v
		}
		return e
	})
	return p, e
}
func probeFence(ctx context.Context, q Querier, p Probe) error {
	var ok bool
	e := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_probes WHERE id=$1 AND epoch=$2 AND status='running' AND deadline_at>now())", p.ID, p.Epoch).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return domain.ErrFenced
	}
	return nil
}
func (s *Store) ProbeProfileID(ctx context.Context, p Probe) (string, error) {
	var id string
	e := s.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE id=$1", p.RevisionID).Scan(&id)
	return id, e
}
func (s *Store) BeginProbeSubmission(ctx context.Context, p Probe) (int, error) {
	var n int
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := probeFence(ctx, tx, p); e != nil {
			return e
		}
		var available bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM catalog_models m JOIN model_providers v ON v.id=m.provider_id WHERE m.id=$1 AND m.current_revision_id=$2 AND v.enabled)", p.ModelID, p.RevisionID).Scan(&available); e != nil {
			return e
		}
		if !available {
			return domain.Err(409, "MODEL_UNAVAILABLE", "供应商已停用或模型修订已经变化。")
		}
		if e := tx.QueryRow(ctx, "UPDATE model_probes SET submissions=submissions+1,updated_at=now() WHERE id=$1 AND submissions<3 RETURNING submissions", p.ID).Scan(&n); e != nil {
			return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "本次探测已达到调用上限。")
		}
		_, e := tx.Exec(ctx, "INSERT INTO probe_operations(probe_id,submission,epoch) VALUES($1,$2,$3)", p.ID, n, p.Epoch)
		return e
	})
	return n, e
}
func (s *Store) RecordProbeSubmission(ctx context.Context, p Probe, n int, report any) error {
	_, e := s.Pool.Exec(ctx, "UPDATE probe_operations SET reporting=$4,completed=($4::jsonb->>'stream_status'='complete') WHERE probe_id=$1 AND submission=$2 AND epoch=$3", p.ID, n, p.Epoch, domain.JSON(report))
	return e
}
func (s *Store) FinishProbe(ctx context.Context, p Probe, result any, code string) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var submissions int
		var deadline time.Time
		if e := tx.QueryRow(ctx, "SELECT submissions,deadline_at FROM model_probes WHERE id=$1 AND epoch=$2 AND status='running' FOR UPDATE", p.ID, p.Epoch).Scan(&submissions, &deadline); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.ErrFenced
			}
			return e
		}
		// A terminal failure must be recorded even after the execution deadline.
		// Ownership/epoch still fence late writers, and late success never passes.
		if code == "" && !deadline.After(time.Now()) {
			code = "PROBE_EXECUTION_TIMEOUT"
		}
		status := "succeeded"
		if code != "" {
			status = "failed"
		}
		if code == "PROVIDER_OUTCOME_UNKNOWN" || submissions > 0 && (code == "PROBE_EXECUTION_TIMEOUT" || code == "PROVIDER_CONNECTION_FAILED" || code == "PROBE_INTERRUPTED") {
			status = "unknown"
		}
		_, e := tx.Exec(ctx, "UPDATE model_probes SET status=$3,result=$4,failure_code=NULLIF($5,''),updated_at=now() WHERE id=$1 AND epoch=$2", p.ID, p.Epoch, status, nullableJSON(result), code)
		return e
	})
}

func (s *Store) TouchProbe(ctx context.Context, p Probe) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := probeFence(ctx, tx, p); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE model_probes SET updated_at=now() WHERE id=$1", p.ID)
		return e
	})
}
func (s *Store) AcquireProbeResource(ctx context.Context, p Probe, resource, owner string) (bool, error) {
	var ok bool
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := probeFence(ctx, tx, p); e != nil {
			return e
		}
		available, e := s.generationResourceAvailable(ctx, tx, resource)
		if e != nil || !available {
			return e
		}
		if _, e := tx.Exec(ctx, "DELETE FROM resource_leases WHERE resource=$1 AND expires_at<now()", resource); e != nil {
			return e
		}
		t, e := tx.Exec(ctx, "INSERT INTO resource_leases(resource,slot,owner,probe_id,execution_epoch,expires_at) VALUES($1,0,$2,$3,$4,now()+interval '60 seconds') ON CONFLICT DO NOTHING", resource, owner, p.ID, p.Epoch)
		ok = e == nil && t.RowsAffected() == 1
		return e
	})
	return ok, e
}
func (s *Store) RenewProbeResource(ctx context.Context, p Probe, resource, owner string) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := probeFence(ctx, tx, p); e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, "UPDATE resource_leases SET expires_at=now()+interval '60 seconds' WHERE resource=$1 AND owner=$2 AND probe_id=$3 AND execution_epoch=$4 AND expires_at>now()", resource, owner, p.ID, p.Epoch)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrFenced
		}
		_, e = tx.Exec(ctx, "UPDATE model_probes SET updated_at=now() WHERE id=$1", p.ID)
		return e
	})
}
