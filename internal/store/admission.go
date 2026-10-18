package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// Waiting work does not consume running slots. This separate admission bound
// prevents an unbounded backlog without tying it to a small worker setting.
const maxPendingVideoTasks = 100

// The caller holds the control transaction lock. Include distinct article
// work waiting on a shared source, so finishing that source cannot fan out
// beyond the bound. Its first article replaces the source's existing slot.
func admitVideoTask(ctx context.Context, tx pgx.Tx) error {
	var count int
	err := tx.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM job_runs WHERE kind<>'qa.answer' AND status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested'))
	 + COALESCE((SELECT sum(GREATEST(0,articles-1)) FROM (
	   SELECT count(DISTINCT COALESCE(r.generation_spec,jsonb_build_object('config_id',r.config_id))) AS articles
	   FROM generation_requests r JOIN job_runs j ON j.id=r.active_run_id
	   WHERE r.status='waiting_source' AND j.kind='source.prepare'
	     AND j.status IN ('queued','running','retry_wait','waiting_dependency','cancel_requested')
	   GROUP BY j.id
	 ) pending),0)`).Scan(&count)
	if err != nil {
		return err
	}
	if count >= maxPendingVideoTasks {
		return domain.Err(429, "ADMISSION_LIMITED", "当前视频处理容量已满，请稍后再试。")
	}
	return nil
}

func admitSourceSubscriber(ctx context.Context, tx pgx.Tx, run string, spec domain.GenerationSpec) error {
	var waiting, same bool
	err := tx.QueryRow(ctx, `SELECT count(*)>0,COALESCE(bool_or(config_id=$2 AND generation_spec=$3::jsonb),false)
	 FROM generation_requests WHERE active_run_id=$1 AND status='waiting_source'`, run, spec.ConfigID, domain.JSON(spec)).Scan(&waiting, &same)
	if err != nil {
		return err
	}
	if waiting && !same {
		return admitVideoTask(ctx, tx)
	}
	return nil
}
