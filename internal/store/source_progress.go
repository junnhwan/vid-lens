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
