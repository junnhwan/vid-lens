package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

const sourcePreparationSlots = 2

var ErrExecutionBusy = errors.New("execution capacity is occupied")

type ExecutionSettings struct {
	Revision        int64 `json:"revision"`
	MaxVideoRunning int   `json:"max_video_running"`
}

type ExecutionInput struct {
	ExpectedRevision int64 `json:"expected_revision"`
	MaxVideoRunning  int   `json:"max_video_running"`
}

func (s *Store) executionSettings(ctx context.Context, q Querier) (ExecutionSettings, error) {
	var v ExecutionSettings
	e := q.QueryRow(ctx, "SELECT revision,max_video_running FROM execution_settings WHERE singleton").Scan(&v.Revision, &v.MaxVideoRunning)
	if errors.Is(e, pgx.ErrNoRows) {
		return ExecutionSettings{MaxVideoRunning: max(1, s.VideoConcurrency)}, nil
	}
	return v, e
}

func (s *Store) ExecutionSettings(ctx context.Context) (ExecutionSettings, error) {
	return s.executionSettings(ctx, s.Pool)
}

func (s *Store) SaveExecutionSettings(ctx context.Context, in ExecutionInput) (ExecutionSettings, error) {
	if in.ExpectedRevision < 0 || in.MaxVideoRunning < 1 || in.MaxVideoRunning > config.MaxVideoConcurrency {
		return ExecutionSettings{}, domain.Err(422, "INVALID_ARGUMENT", "最大同时运行数需为 1–64 之间的整数。")
	}
	var v ExecutionSettings
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		old, e := s.executionSettings(ctx, tx)
		if e != nil {
			return e
		}
		if old.Revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "队列配置已被其他页面修改，请重新读取后再保存。")
		}
		v = ExecutionSettings{Revision: old.Revision + 1, MaxVideoRunning: in.MaxVideoRunning}
		_, e = tx.Exec(ctx, `INSERT INTO execution_settings(singleton,revision,max_video_running) VALUES(true,$1,$2)
            ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,max_video_running=EXCLUDED.max_video_running,updated_at=now()`, v.Revision, v.MaxVideoRunning)
		return e
	})
	return v, e
}

// Called under the same transaction lock as a claim and an owner save. Expired
// leases do not occupy capacity; canceling executions retain it until stopped.
func (s *Store) admitExecution(ctx context.Context, tx pgx.Tx, r domain.Run) error {
	if r.Kind == "qa.answer" {
		return nil
	}
	v, e := s.executionSettings(ctx, tx)
	if e != nil {
		return e
	}
	var running, preparing int
	e = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE kind IN ('source.prepare','source.restore'))
        FROM job_runs WHERE kind<>'qa.answer' AND status IN ('running','cancel_requested') AND lease_expires_at>now()`).Scan(&running, &preparing)
	if e != nil {
		return e
	}
	if running >= v.MaxVideoRunning || (strings.HasPrefix(r.Kind, "source.") && preparing >= sourcePreparationSlots) {
		return ErrExecutionBusy
	}
	return nil
}

// Count every generation resource, including slots above a newly lowered limit.
// Their leases keep renewing while old calls drain, so no new call slips in.
func (s *Store) generationResourceAvailable(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	if name != "ai:generation" && !strings.HasPrefix(name, "ai:generation:") {
		return true, nil
	}
	v, e := s.executionSettings(ctx, tx)
	if e != nil {
		return false, e
	}
	var n int
	e = tx.QueryRow(ctx, "SELECT count(*) FROM resource_leases WHERE (resource='ai:generation' OR resource LIKE 'ai:generation:%') AND expires_at>now()").Scan(&n)
	return n < v.MaxVideoRunning, e
}
