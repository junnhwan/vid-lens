package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func receipt(ctx context.Context, tx pgx.Tx, session, article string) error {
	t, e := tx.Exec(ctx, "INSERT INTO completed_article_receipts(session_id,article_id) VALUES($1,$2) ON CONFLICT DO NOTHING", session, article)
	if e != nil {
		return e
	}
	if t.RowsAffected() == 0 {
		return nil
	}
	_, e = tx.Exec(ctx, "UPDATE site_metric_totals SET completed_readings=completed_readings+1,updated_at=now()")
	return e
}
func (s *Store) SiteStats(ctx context.Context) (map[string]any, error) {
	return one[map[string]any](ctx, s.Pool, "SELECT jsonb_build_object('completed_readings',completed_readings,'likes',likes,'as_of',updated_at,'metric_version',1) FROM site_metric_totals")
}
func (s *Store) SiteLike(ctx context.Context, session string, set bool) (map[string]any, error) {
	var v map[string]any
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if set {
			tag, e := tx.Exec(ctx, "INSERT INTO site_likes(session_id) VALUES($1) ON CONFLICT DO NOTHING", session)
			if e != nil {
				return e
			}
			if tag.RowsAffected() > 0 {
				if _, e = tx.Exec(ctx, "UPDATE site_metric_totals SET likes=likes+1,updated_at=now()"); e != nil {
					return e
				}
			}
		}
		var e error
		v, e = one[map[string]any](ctx, tx, "SELECT jsonb_build_object('liked',EXISTS(SELECT 1 FROM site_likes WHERE session_id=$1),'count',likes) FROM site_metric_totals", session)
		return e
	})
	return v, e
}
