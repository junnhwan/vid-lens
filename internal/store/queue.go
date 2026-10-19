package store

import (
	"context"
	"time"
)

// QueueStatus contains only aggregate execution counts. It never identifies a
// request, its owner, source, model, or position in another visitor's queue.
type QueueStatus struct {
	SchemaVersion int       `json:"schema_version"`
	MaxConcurrent int       `json:"max_concurrent"`
	Processing    int       `json:"processing"`
	Waiting       int       `json:"waiting"`
	AsOf          time.Time `json:"as_of"`
}

func (s *Store) QueueStatus(ctx context.Context) (QueueStatus, error) {
	v := QueueStatus{SchemaVersion: 1}
	err := s.Pool.QueryRow(ctx, `
	 SELECT
	 COALESCE((SELECT max_video_running FROM execution_settings WHERE singleton),$1),
	 count(*) FILTER (WHERE status IN ('running','cancel_requested') AND lease_expires_at>now()),
	 count(*) FILTER (WHERE status IN ('queued','retry_wait','waiting_dependency')
	   OR status='running' AND (lease_expires_at IS NULL OR lease_expires_at<=now())),
	 now()
	 FROM job_runs
	 WHERE kind<>'qa.answer' AND deadline_at>now()
	   AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')`, max(1, s.VideoConcurrency)).Scan(&v.MaxConcurrent, &v.Processing, &v.Waiting, &v.AsOf)
	return v, err
}
