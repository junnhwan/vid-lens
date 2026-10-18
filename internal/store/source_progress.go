package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// Source preparation restarts from its beginning on another attempt. Close its
// parallel actions before releasing ownership so they cannot stay live forever.
func closeSourceSteps(ctx context.Context, tx pgx.Tx, r domain.Run, status, reason string) error {
	if r.Kind != "source.prepare" && r.Kind != "source.restore" {
		return nil
	}
	steps, err := many[domain.Step](ctx, tx, `SELECT step FROM (
		SELECT DISTINCT ON(step_id) step,seq FROM run_events
		WHERE run_id=$1 AND step_id IS NOT NULL ORDER BY step_id,seq DESC
	) latest WHERE step->>'status' IN ('running','queued') ORDER BY seq`, r.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, step := range steps {
		step.Status = status
		step.FinishedAt = &now
		step.Outcome = &domain.Outcome{Message: reason, EvidenceStatus: "not_assessed"}
		if err = event(ctx, tx, r.ID, &step); err != nil {
			return err
		}
	}
	return nil
}

func sourceRetryNotice(ctx context.Context, tx pgx.Tx, r domain.Run, reason string) error {
	if r.Kind != "source.prepare" && r.Kind != "source.restore" {
		return nil
	}
	now := time.Now().UTC()
	step := domain.Step{
		ID: domain.ID(), RunID: r.ID, Kind: "retry", Phase: "prepare_source",
		Title:  fmt.Sprintf("第 %d 次来源准备中断，将自动重试", r.AttemptNo),
		Status: "completed", StartedAt: &now, FinishedAt: &now,
		FrameIDs: []string{}, Frames: []domain.Frame{},
		Outcome: &domain.Outcome{Message: reason, EvidenceStatus: "not_assessed"},
	}
	return event(ctx, tx, r.ID, &step)
}

// Only newly read bytes advance the source business clock. A heartbeat,
// duplicate report or takeover from an old epoch cannot conceal a stalled
// download. One bounded ledger row is retained per transfer and attempt.
func (s *Store) SourceDownloadProgress(ctx context.Context, c domain.Claim, transfer string, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		if c.Run.Kind != "source.prepare" && c.Run.Kind != "source.restore" {
			return domain.ErrFenced
		}
		key := "source-download:" + c.AttemptID + ":" + Hash(transfer)
		tag, err := tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result,completed_at)
		 VALUES($1,$2,$3,'source_download_progress',$4,'completed',$5,now())
		 ON CONFLICT(run_id,operation_key) DO UPDATE SET result=EXCLUDED.result,updated_at=now(),completed_at=now()
		 WHERE run_operations.kind='source_download_progress' AND COALESCE((run_operations.result->>'bytes')::bigint,0)<$6`, c.Run.ID, key, c.AttemptID, Hash(transfer), domain.JSON(map[string]int64{"bytes": bytes}), bytes)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		tag, err = tx.Exec(ctx, "UPDATE job_runs SET updated_at=now() WHERE id=$1 AND kind IN ('source.prepare','source.restore')", c.Run.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrFenced
		}
		return nil
	})
}
