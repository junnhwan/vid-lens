package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool    *pgxpool.Pool
	aead    cipher.AEAD
	signing []byte
}

func Open(ctx context.Context, dsn, secret string) (*Store, error) {
	pc, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return nil, errors.New("invalid database URL")
	}
	pc.MaxConns = 10
	pc.MinConns = 0
	pc.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "5000"
	pc.ConnConfig.RuntimeParams["max_parallel_workers_per_gather"] = "0"
	p, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		return nil, e
	}
	k := sha256.Sum256([]byte(secret))
	b, _ := aes.NewCipher(k[:])
	g, _ := cipher.NewGCM(b)
	s := &Store{Pool: p, aead: g, signing: k[:]}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, errors.New("database unavailable")
	}
	return s, nil
}

// Transactions are short control operations. A database-wide lock fixes join/cancel/publish ordering.
func (s *Store) Tx(ctx context.Context, f func(pgx.Tx) error) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, e := s.Pool.Begin(c)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(c, "SELECT pg_advisory_xact_lock(867914031)"); e != nil {
		return e
	}
	if e = f(tx); e != nil {
		return e
	}
	return tx.Commit(c)
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(867914030)"); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())")
	if e != nil {
		return e
	}

	for version, name := range []string{"migrations/001_init.sql", "migrations/002_evidence_order.sql", "migrations/003_strip_provider_thoughts.sql", "migrations/004_model_reporting.sql", "migrations/005_model_catalog.sql", "migrations/006_site_metrics.sql", "migrations/007_article_exports.sql", "migrations/008_external_references.sql", "migrations/009_provider_policies.sql", "migrations/010_reference_snapshots.sql", "migrations/011_network_settings.sql", "migrations/012_diagnostics.sql", "migrations/013_storage_management.sql", "migrations/014_model_route_priorities.sql", "migrations/015_request_history_index.sql", "migrations/016_example_showcases.sql", "migrations/017_request_shares.sql"} {
		n := version + 1
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", n).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			b, err := migrations.ReadFile(name)
			if err != nil {
				return err
			}
			if _, e = tx.Exec(ctx, string(b)); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", n); e != nil {
				return e
			}
		}
	}

	return tx.Commit(ctx)
}

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func one[T any](ctx context.Context, q Querier, sql string, args ...any) (T, error) {
	var v T
	var b []byte
	e := q.QueryRow(ctx, sql, args...).Scan(&b)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v)
	return v, e
}
func many[T any](ctx context.Context, q Querier, sql string, args ...any) ([]T, error) {
	rows, e := q.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []T{}
	for rows.Next() {
		var b []byte
		var x T
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &x); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}
func Hash(v any) string { b := sha256.Sum256(domain.JSON(v)); return hex.EncodeToString(b[:]) }
func (s *Store) Bootstrap(ctx context.Context, p config.Profile, modelKey, asrKey string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM runtime_settings)").Scan(&exists); e != nil {
			return e
		}
		if exists {
			return nil
		}
		if e := s.putCredential(ctx, tx, "model", modelKey); e != nil {
			return e
		}
		if e := s.putCredential(ctx, tx, "asr", asrKey); e != nil {
			return e
		}
		id := domain.ID()
		if _, e := tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", id, domain.JSON(p)); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO runtime_settings(config_id) VALUES($1)", id)
		return e
	})
}
func (s *Store) Profile(ctx context.Context, id string) (config.Profile, error) {
	return one[config.Profile](ctx, s.Pool, "SELECT profile FROM runtime_config_revisions WHERE id=$1", id)
}
func (s *Store) CurrentConfig(ctx context.Context) (string, config.Profile, error) {
	var id string
	e := s.Pool.QueryRow(ctx, "SELECT config_id FROM runtime_settings").Scan(&id)
	if e != nil {
		return "", config.Profile{}, e
	}
	p, e := s.Profile(ctx, id)
	return id, p, e
}
func (s *Store) putCredential(ctx context.Context, tx pgx.Tx, name, value string) error {
	if value == "" {
		return nil
	}
	n := make([]byte, s.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return e
	}
	b := s.aead.Seal(n, n, []byte(value), []byte(name))
	_, e := tx.Exec(ctx, "INSERT INTO credentials(name,ciphertext) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,updated_at=now()", name, b)
	return e
}
func (s *Store) Credential(ctx context.Context, name string) (string, error) {
	var b []byte
	e := s.Pool.QueryRow(ctx, "SELECT ciphertext FROM credentials WHERE name=$1", name).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	n := s.aead.NonceSize()
	if len(b) < n {
		return "", errors.New("invalid credential")
	}
	v, e := s.aead.Open(nil, b[:n], b[n:], []byte(name))
	if e != nil {
		return "", errors.New("cannot decrypt credential; check secret key")
	}
	return string(v), nil
}
func (s *Store) SaveConfig(ctx context.Context, p config.Profile, modelKey, asrKey string) (string, error) {
	if e := p.Validate(); e != nil {
		return "", e
	}
	id := domain.ID()
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		current, e := one[config.Profile](ctx, tx, "SELECT c.profile FROM runtime_settings s JOIN runtime_config_revisions c ON c.id=s.config_id")
		if e != nil {
			return e
		}
		// Credential references belong to the server. Replacements receive new
		// identities so already accepted work retains its original authentication.
		p.ModelKeyRef = current.ModelKeyRef
		p.ASRKeyRef = current.ASRKeyRef
		if modelKey != "" {
			p.ModelKeyRef = "model:" + id
		}
		if asrKey != "" {
			p.ASRKeyRef = "asr:" + id
		}
		if e := s.putCredential(ctx, tx, p.ModelKeyRef, modelKey); e != nil {
			return e
		}
		if e := s.putCredential(ctx, tx, p.ASRKeyRef, asrKey); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", id, domain.JSON(p)); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE runtime_settings SET config_id=$1", id)
		return e
	})
	return id, e
}
func loadRun(ctx context.Context, q Querier, id string) (domain.Run, error) {
	return one[domain.Run](ctx, q, "SELECT to_jsonb(r) FROM job_runs r WHERE id=$1", id)
}
func (s *Store) Run(ctx context.Context, id string) (domain.Run, error) {
	return loadRun(ctx, s.Pool, id)
}
func (s *Store) Request(ctx context.Context, session, id string) (domain.Request, error) {
	v, e := one[domain.Request](ctx, s.Pool, "SELECT to_jsonb(r) FROM generation_requests r WHERE id=$1 AND session_id=$2", id, session)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	return v, e
}
func loadRequest(ctx context.Context, q Querier, id string) (domain.Request, error) {
	return one[domain.Request](ctx, q, "SELECT to_jsonb(r) FROM generation_requests r WHERE id=$1", id)
}
func event(ctx context.Context, tx pgx.Tx, run string, step *domain.Step) error {
	var seq int64
	if e := tx.QueryRow(ctx, "UPDATE job_runs SET last_event_seq=last_event_seq+1,updated_at=now() WHERE id=$1 RETURNING last_event_seq", run).Scan(&seq); e != nil {
		return e
	}
	var sid *string
	var b []byte
	typ := "view.snapshot"
	if step != nil {
		step.LastSeq = seq
		if step.FirstSeq == 0 {
			step.FirstSeq = seq
		}
		sid = &step.ID
		b = domain.JSON(step)
		typ = "activity.upsert"
	}
	_, e := tx.Exec(ctx, "INSERT INTO run_events(run_id,seq,event_type,step_id,step) VALUES($1,$2,$3,$4,$5)", run, seq, typ, sid, b)
	return e
}
func newRun(ctx context.Context, tx pgx.Tx, kind, key, source, configID string, snapshot *string, deadline time.Time) (string, error) {
	id := domain.ID()
	_, e := tx.Exec(ctx, "INSERT INTO job_runs(id,kind,work_key,source_id,snapshot_id,config_id,status,deadline_at) VALUES($1,$2,$3,$4,$5,$6,'queued',$7)", id, kind, key, source, snapshot, configID, deadline)
	if e != nil {
		return "", e
	}
	queue := "generation"
	if kind == "qa.answer" {
		queue = "interactive"
	}
	_, e = tx.Exec(ctx, "INSERT INTO task_outbox(run_id,dispatch_generation,queue) VALUES($1,1,$2)", id, queue)
	if e == nil {
		e = event(ctx, tx, id, nil)
	}
	return id, e
}
func fence(ctx context.Context, tx pgx.Tx, c domain.Claim) error {
	var ok bool
	e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM job_runs WHERE id=$1 AND status='running' AND execution_epoch=$2 AND lease_owner=$3 AND lease_expires_at>clock_timestamp() AND deadline_at>clock_timestamp())", c.Run.ID, c.Epoch, c.Owner).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return domain.ErrFenced
	}
	return nil
}
func NotFound(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
	}
	return e
}
func require(v bool, code, msg string) error {
	if !v {
		return domain.Err(409, code, msg)
	}
	return nil
}
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }
func (s *Store) DBTime(ctx context.Context) time.Time {
	var t time.Time
	_ = s.Pool.QueryRow(ctx, "SELECT now()").Scan(&t)
	return t
}
func checkErr(e error) error {
	if e != nil {
		return fmt.Errorf("store: %w", e)
	}
	return nil
}
