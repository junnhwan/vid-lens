package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type Announcement struct {
	Revision int64  `json:"revision"`
	Enabled  bool   `json:"enabled"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Version  string `json:"version"`
}

type AnnouncementInput struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
	Title            string `json:"title"`
	Body             string `json:"body"`
}

func announcement(ctx context.Context, q Querier) (Announcement, error) {
	var v Announcement
	e := q.QueryRow(ctx, "SELECT revision,enabled,title,body,version FROM site_announcement WHERE singleton").Scan(&v.Revision, &v.Enabled, &v.Title, &v.Body, &v.Version)
	if errors.Is(e, pgx.ErrNoRows) {
		return Announcement{}, nil
	}
	return v, e
}

func (s *Store) Announcement(ctx context.Context) (Announcement, error) {
	return announcement(ctx, s.Pool)
}

func (s *Store) SaveAnnouncement(ctx context.Context, in AnnouncementInput) (Announcement, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(strings.ReplaceAll(in.Body, "\r\n", "\n"))
	if in.ExpectedRevision < 0 || utf8.RuneCountInString(in.Title) > 120 || utf8.RuneCountInString(in.Body) > 8000 || strings.ContainsRune(in.Title+in.Body, '\x00') {
		return Announcement{}, domain.Err(422, "INVALID_ARGUMENT", "公告标题最多 120 字，正文最多 8000 字，且不能含空字符。")
	}
	if in.Enabled && (in.Title == "" || in.Body == "") {
		return Announcement{}, domain.Err(422, "INVALID_ARGUMENT", "启用公告前，请填写标题和正文。")
	}
	var v Announcement
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		old, e := announcement(ctx, tx)
		if e != nil {
			return e
		}
		if old.Revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "公告已被其他页面修改，请重新读取后再保存。")
		}
		v = old
		if old.Enabled == in.Enabled && old.Title == in.Title && old.Body == in.Body {
			return nil
		}
		v.Revision++
		v.Enabled, v.Title, v.Body = in.Enabled, in.Title, in.Body
		// A published edit or re-enabling is a new notice. Identical saves keep
		// their version so returning visitors do not see the same notice again.
		if in.Enabled && (!old.Enabled || old.Title != in.Title || old.Body != in.Body) {
			v.Version = domain.ID()
		}
		_, e = tx.Exec(ctx, `INSERT INTO site_announcement(singleton,revision,enabled,title,body,version) VALUES(true,$1,$2,$3,$4,$5)
            ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,enabled=EXCLUDED.enabled,title=EXCLUDED.title,body=EXCLUDED.body,version=EXCLUDED.version,updated_at=now()`, v.Revision, v.Enabled, v.Title, v.Body, v.Version)
		return e
	})
	return v, e
}
