package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

func (s *Store) RequestDraft(ctx context.Context, session, request string) (map[string]any, error) {
	out := map[string]any{"draft": nil}
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := loadRequest(ctx, tx, request)
		if err != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		if r.ArticleID != nil || r.ActiveRunID == nil {
			return nil
		}
		d, err := one[Draft](ctx, tx, `SELECT result FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1`, *r.ActiveRunID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var data struct {
			Article domain.Article `json:"article"`
		}
		if json.Unmarshal(d.Data, &data) != nil || len(data.Article.Sections) == 0 {
			return nil
		}
		f, err := frames(ctx, tx, request, ContentFrameIDs(data.Article.Sections, data.Article.Citations))
		if err != nil {
			return err
		}
		out["draft"] = map[string]any{"version": d.Version, "article": data.Article, "frames": f, "complete": false}
		return nil
	})
	return out, err
}
