package store

import (
	"context"
	"time"
)

type RequestSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Platform     string    `json:"platform"`
	CanonicalURL string    `json:"canonical_url"`
	Status       string    `json:"status"`
	Phase        *string   `json:"phase"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RequestHistoryPage struct {
	SchemaVersion int              `json:"schema_version"`
	Items         []RequestSummary `json:"items"`
	Limit         int              `json:"limit"`
	Offset        int              `json:"offset"`
	HasMore       bool             `json:"has_more"`
}

func (s *Store) RequestHistory(ctx context.Context, session string, limit, offset int) (RequestHistoryPage, error) {
	page := RequestHistoryPage{
		SchemaVersion: 1,
		Items:         []RequestSummary{},
		Limit:         limit,
		Offset:        offset,
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT r.id,
		       COALESCE(NULLIF(a.content->>'title', ''), NULLIF(ss.source->>'title', ''), s.canonical_url),
		       s.platform, s.canonical_url, r.status, jr.phase, r.created_at, r.updated_at
		FROM generation_requests r
		JOIN video_sources s ON s.id=r.source_id
		LEFT JOIN source_snapshots ss ON ss.id=r.snapshot_id
		LEFT JOIN articles a ON a.id=r.article_id
		LEFT JOIN job_runs jr ON jr.id=r.active_run_id
		WHERE r.session_id=$1 AND r.reuse<>'example_question'
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $2 OFFSET $3`, session, limit+1, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RequestSummary
		if err = rows.Scan(&item.ID, &item.Title, &item.Platform, &item.CanonicalURL, &item.Status, &item.Phase, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
	}
	return page, nil
}
