package store

// Resource waits have their own bounded clock. They never update updated_at,
// complete a business operation, or pretend that a heartbeat made progress.
import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type resourceWaitRecord struct {
	Resource      string     `json:"resource"`
	Slots         int        `json:"slots"`
	VerifiedUntil time.Time  `json:"verified_until"`
	LimitMS       int64      `json:"limit_ms"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

func (s *Store) RecordResourceWait(ctx context.Context, c domain.Claim, owner, resource string, slots int) (bool, error) {
	logged := false
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		if slots < 1 || slots > config.MaxVideoConcurrency {
			return errors.New("invalid resource capacity")
		}
		if resource == "ai:generation" {
			if err := tx.QueryRow(ctx, "SELECT max_video_running FROM execution_settings WHERE singleton").Scan(&slots); err != nil {
				return err
			}
		}
		var count int
		var until *time.Time
		err := tx.QueryRow(ctx, `SELECT count(*),min(LEAST(l.expires_at,r.lease_expires_at))
 FROM resource_leases l JOIN job_runs r ON r.id=l.run_id AND r.execution_epoch=l.execution_epoch
 WHERE (l.resource=$1 OR ($1 IN ('asr','ai:generation') AND left(l.resource,length($1)+1)=$1||':'))
 AND l.owner<>$2 AND l.expires_at>clock_timestamp() AND r.status='running' AND r.lease_expires_at>clock_timestamp()`, resource, owner).Scan(&count, &until)
		if err != nil || count < slots || until == nil {
			return err
		}
		key := "resource-wait:" + owner
		op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key)
		var record resourceWaitRecord
		if errors.Is(err, pgx.ErrNoRows) {
			profile, err := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", c.Run.ConfigID)
			if err != nil {
				return err
			}
			limit := profile.QueueSeconds
			if c.Run.Kind == "qa.answer" {
				limit = profile.AnswerQueueSeconds
			}
			record = resourceWaitRecord{Resource: resource, Slots: slots, VerifiedUntil: *until, LimitMS: limit * 1000}
			_, err = tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result,started_at)
 VALUES($1,$2,$3,'resource_wait',$4,'planned',$5,clock_timestamp())`, c.Run.ID, key, c.AttemptID, Hash(map[string]string{"resource": resource}), domain.JSON(record))
			logged = err == nil
			return err
		}
		if err != nil {
			return err
		}
		if op.Kind != "resource_wait" || op.Status != "planned" || json.Unmarshal(op.Result, &record) != nil || record.Resource != resource {
			return errors.New("resource wait identity changed")
		}
		var elapsed int64
		if err := tx.QueryRow(ctx, "SELECT GREATEST(0,EXTRACT(EPOCH FROM(clock_timestamp()-started_at))*1000)::bigint FROM run_operations WHERE run_id=$1 AND operation_key=$2 AND attempt_id=$3", c.Run.ID, key, c.AttemptID).Scan(&elapsed); err != nil {
			return err
		}
		if elapsed > record.LimitMS {
			return resourceWaitTimeout()
		}
		record.VerifiedUntil, record.Slots = *until, slots
		_, err = tx.Exec(ctx, "UPDATE run_operations SET result=$3,updated_at=now() WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key, domain.JSON(record))
		logged = err == nil
		return err
	})
	return logged, err
}

func (s *Store) EndResourceWait(ctx context.Context, c domain.Claim, owner string) error {
	timedOut := false
	err := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE run_operations SET status='completed',completed_at=clock_timestamp(),updated_at=now()
 WHERE run_id=$1 AND operation_key=$2 AND attempt_id=$3 AND kind='resource_wait' AND status='planned'
 RETURNING EXTRACT(EPOCH FROM(completed_at-started_at))*1000>COALESCE((result->>'limit_ms')::bigint,0)`, c.Run.ID, "resource-wait:"+owner, c.AttemptID).Scan(&timedOut)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err == nil && timedOut {
		return resourceWaitTimeout()
	}
	return err
}

func resourceWaitTimeout() error {
	return domain.Err(422, "QUEUE_WAIT_TIMEOUT", "等待处理资源已达到本次任务的排队时限；来源和草稿已保留。")
}

// Union overlapping parallel waits, clamp them to the business-idle window,
// and stop credit at the last verified holder/caller lease. A stale marker
// cannot suppress a genuinely stalled execution indefinitely.
func resourceWaitDuration(ctx context.Context, tx pgx.Tx, run domain.Run, now time.Time) (time.Duration, error) {
	rows, err := many[resourceWaitRecord](ctx, tx, `SELECT o.result||jsonb_build_object('started_at',o.started_at,'completed_at',o.completed_at)
 FROM run_operations o JOIN job_attempts a ON a.id=o.attempt_id
 WHERE o.run_id=$1 AND o.kind='resource_wait' AND a.execution_epoch=$2
 AND o.started_at<$4 AND (o.completed_at IS NULL OR o.completed_at>$3)`, run.ID, run.ExecutionEpoch, run.UpdatedAt, now)
	if err != nil {
		return 0, err
	}
	type interval struct{ start, end time.Time }
	intervals := []interval{}
	for _, row := range rows {
		if row.LimitMS <= 0 || row.VerifiedUntil.IsZero() {
			continue
		}
		end := minTime(now, row.VerifiedUntil)
		if run.LeaseExpiresAt != nil {
			end = minTime(end, *run.LeaseExpiresAt)
		}
		if row.CompletedAt != nil {
			end = minTime(end, *row.CompletedAt)
		}
		if row.LimitMS > 0 && end.Sub(row.StartedAt).Milliseconds() > row.LimitMS {
			return 0, resourceWaitTimeout()
		}
		start := row.StartedAt
		if start.Before(run.UpdatedAt) {
			start = run.UpdatedAt
		}
		if end.After(start) {
			intervals = append(intervals, interval{start, end})
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	var total time.Duration
	var start, end time.Time
	for _, span := range intervals {
		if end.IsZero() {
			start, end = span.start, span.end
			continue
		}
		if !span.start.After(end) {
			if span.end.After(end) {
				end = span.end
			}
			continue
		}
		total += end.Sub(start)
		start, end = span.start, span.end
	}
	if !end.IsZero() {
		total += end.Sub(start)
	}
	return total, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
