package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type Reference struct {
	ID            string     `json:"id"`
	RunID         string     `json:"run_id"`
	TurnID        string     `json:"turn_id"`
	SessionID     string     `json:"session_id"`
	RequestID     string     `json:"request_id"`
	URL           string     `json:"url"`
	FinalURL      *string    `json:"final_url"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Body          *string    `json:"body"`
	ContentSHA256 *string    `json:"content_sha256"`
	ReadAt        *time.Time `json:"read_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	Truncated     bool       `json:"truncated"`
}

func (r Reference) EvidenceURL() string {
	if r.FinalURL != nil {
		return *r.FinalURL
	}
	return r.URL
}
func (r Reference) Evidence() map[string]any {
	return map[string]any{"reference_id": r.ID, "kind": "external_reference", "url": r.EvidenceURL(), "title": r.Title, "status": r.Status, "body": r.Body, "content_sha256": r.ContentSHA256, "read_at": r.ReadAt, "truncated": r.Truncated}
}
func (s *Store) RegisterReference(ctx context.Context, c domain.Claim, t domain.Turn, url, title string) (Reference, error) {
	var r Reference
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if !t.AllowExternalSources || t.RunID != c.Run.ID {
			return domain.Err(403, "EXTERNAL_SOURCES_DISABLED", "本轮未允许网络补充。")
		}
		var session string
		if e := tx.QueryRow(ctx, "SELECT session_id FROM qa_conversations WHERE id=$1", t.ConversationID).Scan(&session); e != nil {
			return e
		}
		var e error
		r, e = one[Reference](ctx, tx, "INSERT INTO external_references(id,run_id,turn_id,session_id,request_id,url,title) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(run_id,url) DO UPDATE SET title=external_references.title RETURNING to_jsonb(external_references)", domain.ID(), c.Run.ID, t.ID, session, t.RequestID, url, title)
		return e
	})
	return r, e
}
func (s *Store) ReferenceForRun(ctx context.Context, run, id string) (Reference, error) {
	return one[Reference](ctx, s.Pool, "SELECT to_jsonb(r) FROM external_references r WHERE id=$1 AND run_id=$2 AND expires_at>now()", id, run)
}
func (s *Store) References(ctx context.Context, run string, readOnly bool) ([]Reference, error) {
	return many[Reference](ctx, s.Pool, "SELECT to_jsonb(r) FROM external_references r WHERE run_id=$1 AND expires_at>now() AND (NOT $2 OR status='read') ORDER BY id", run, readOnly)
}
func (s *Store) StartReferenceRead(ctx context.Context, c domain.Claim, id, key string) (Reference, Operation, error) {
	r, e := s.ReferenceForRun(ctx, c.Run.ID, id)
	if e != nil {
		return r, Operation{}, NotFound(e)
	}
	op, e := s.BeginOperation(ctx, c, key, "web.read", map[string]string{"reference_id": id}, false)
	if e != nil {
		return r, op, e
	}
	if op.Status == "completed" {
		return r, op, nil
	}
	// The physical-read budget and unknown-outcome check are reserved under
	// BeginOperation's transaction lock, including competing callers.
	op, e = s.BeginOperation(ctx, c, key, "web.read", map[string]string{"reference_id": id}, true)
	return r, op, e
}
func (s *Store) SaveReference(ctx context.Context, c domain.Claim, id, key, title, url, body, sha string, truncated bool) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE external_references SET status='read',title=$3,final_url=$4,body=$5,content_sha256=$6,read_at=now(),truncated=$7 WHERE id=$1 AND run_id=$2", id, c.Run.ID, title, url, body, sha, truncated)
		return e
	})
}
func (s *Store) FailedReference(ctx context.Context, c domain.Claim, id string) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE external_references SET status='failed' WHERE id=$1 AND run_id=$2 AND status<>'read'", id, c.Run.ID)
		return e
	})
}
func (s *Store) ReserveSearch(ctx context.Context, c domain.Claim, key string, limit int) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		var sent int
		if e := tx.QueryRow(ctx, "SELECT submissions FROM run_operations WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key).Scan(&sent); e != nil {
			return e
		}
		if sent > 0 {
			return domain.Err(409, "PROVIDER_OUTCOME_UNKNOWN", "原搜索调用结果未知，不重复消耗额度。")
		}
		var turns int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM external_search_credits WHERE run_id=$1", c.Run.ID).Scan(&turns); e != nil {
			return e
		}
		if turns >= 2 {
			return domain.Err(429, "SEARCH_QUOTA_EXHAUSTED", "本轮搜索额度已用完。")
		}
		settings, e := networkSettings(ctx, tx, config.Config{SearchMonthlyLimit: limit})
		if e != nil {
			return e
		}
		if settings.Revision > 0 && (!settings.ReferenceReadEnabled || !settings.SearchEnabled || !settings.KeyConfigured) {
			return domain.Err(403, "SEARCH_DISABLED", "搜索已关闭，继续基于视频证据回答。")
		}
		var today int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM external_search_credits
 WHERE created_at >= (date_trunc('day', now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')`).Scan(&today); e != nil {
			return e
		}
		if today >= settings.DailyLimit {
			return domain.Err(429, "SEARCH_DAILY_QUOTA_EXHAUSTED", "全站今日搜索额度已用完，北京时间零点重置；仍可根据视频证据回答。")
		}
		limit = settings.MonthlyLimit
		var month string
		if e := tx.QueryRow(ctx, "SELECT to_char(now() AT TIME ZONE 'UTC','YYYY-MM')").Scan(&month); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "INSERT INTO external_search_months(month) VALUES($1) ON CONFLICT DO NOTHING", month); e != nil {
			return e
		}
		var exhausted bool
		var total int
		if e := tx.QueryRow(ctx, "SELECT exhausted,(SELECT count(*) FROM external_search_credits WHERE month=$1) FROM external_search_months WHERE month=$1", month).Scan(&exhausted, &total); e != nil {
			return e
		}
		if exhausted || total >= min(limit, 1000) {
			return domain.Err(429, "SEARCH_QUOTA_EXHAUSTED", "免费搜索额度已用完，仍可根据视频证据回答。")
		}
		if _, e := tx.Exec(ctx, "INSERT INTO external_search_credits(run_id,operation_key,month) VALUES($1,$2,$3)", c.Run.ID, key, month); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE run_operations SET submissions=submissions+1,updated_at=now() WHERE run_id=$1 AND operation_key=$2", c.Run.ID, key)
		return e
	})
}
func (s *Store) ExhaustSearchMonth(ctx context.Context) error {
	_, e := s.Pool.Exec(ctx, "UPDATE external_search_months SET exhausted=true WHERE month=to_char(now() AT TIME ZONE 'UTC','YYYY-MM')")
	return e
}
func (s *Store) PublicReference(ctx context.Context, session, request, id string) (Reference, error) {
	v, e := one[Reference](ctx, s.Pool, "SELECT to_jsonb(r) FROM external_references r WHERE id=$1 AND session_id=$2 AND request_id=$3", id, session, request)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, NotFound(e)
	}
	if e == nil && !v.ExpiresAt.After(time.Now()) {
		return v, domain.Err(410, "REFERENCE_EXPIRED", "网页快照已过保留期限。")
	}
	return v, e
}
