package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *Store) PendingProbe(ctx context.Context, id string) (bool, error) {
	var pending bool
	err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM model_probes WHERE id=$1 AND status IN ('queued','running') AND deadline_at>now())", id).Scan(&pending)
	return pending, err
}

func (s *Store) PendingExport(ctx context.Context, id string) (bool, error) {
	var pending bool
	err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE id=$1 AND status IN ('queued','running','retry_wait') AND deadline_at>now())", id).Scan(&pending)
	return pending, err
}

// OperationCount reads the same durable ledger used by BeginOperation. The
// caller owns which operation kinds count towards its frozen execution budget.
func (s *Store) OperationCount(ctx context.Context, run string, kinds ...string) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind=ANY($2)", run, kinds).Scan(&count)
	return count, err
}

type SearchUsage struct {
	DailyUsed      int  `json:"daily_used"`
	MonthlyUsed    int  `json:"monthly_used"`
	MonthExhausted bool `json:"month_exhausted"`
}

func (s *Store) SearchUsage(ctx context.Context) (SearchUsage, error) {
	var v SearchUsage
	err := s.Pool.QueryRow(ctx, `SELECT
        count(*) FILTER (WHERE created_at >= (date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')),
        count(*) FILTER (WHERE month=to_char(now() AT TIME ZONE 'UTC','YYYY-MM')),
        COALESCE((SELECT exhausted FROM external_search_months WHERE month=to_char(now() AT TIME ZONE 'UTC','YYYY-MM')),false)
        FROM external_search_credits`).Scan(&v.DailyUsed, &v.MonthlyUsed, &v.MonthExhausted)
	return v, err
}

type RuntimeStatus struct {
	SchemaVersion int              `json:"schema_version"`
	Runs          map[string]int64 `json:"runs"`
	Storage       struct {
		MediaBytes int64 `json:"media_bytes"`
		FrameBytes int64 `json:"frame_bytes"`
	} `json:"storage"`
}

func (s *Store) RuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	v := RuntimeStatus{SchemaVersion: 1, Runs: map[string]int64{}}
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT status,count(*) FROM job_runs GROUP BY status")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int64
			if err := rows.Scan(&status, &count); err != nil {
				return err
			}
			v.Runs[status] = count
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT COALESCE(sum(bytes) FILTER(WHERE kind IN ('video','audio')),0),COALESCE(sum(bytes) FILTER(WHERE kind='frame'),0) FROM media_assets WHERE state IN ('ready','deleting')").Scan(&v.Storage.MediaBytes, &v.Storage.FrameBytes)
	})
	return v, err
}
