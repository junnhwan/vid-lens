package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

type AnnouncementEntry struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	Version     string     `json:"version"`
	PublishedAt *time.Time `json:"published_at"`
}

type AnnouncementList struct {
	Revision int64               `json:"revision"`
	Items    []AnnouncementEntry `json:"items"`
}

type AnnouncementCommand struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"action"`
	ID               string `json:"id"`
	Title            string `json:"title"`
	Body             string `json:"body"`
}

type PublicNotice struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

type PublicFeed struct {
	Announcement *PublicNotice  `json:"announcement"`
	Timeline     []PublicNotice `json:"timeline"`
}

func normalizeNotice(title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if utf8.RuneCountInString(title) > 120 || utf8.RuneCountInString(body) > 8000 || strings.ContainsRune(title+body, '\x00') {
		return "", "", domain.Err(422, "INVALID_ARGUMENT", "公告标题最多 120 字，正文最多 8000 字，且不能含空字符。")
	}
	return title, body, nil
}

func (s *Store) Announcements(ctx context.Context) (AnnouncementList, error) {
	return announcements(ctx, s.Pool)
}

func announcements(ctx context.Context, q Querier) (AnnouncementList, error) {
	var list AnnouncementList
	err := q.QueryRow(ctx, "SELECT COALESCE((SELECT revision FROM site_announcement_state WHERE singleton), 0)").Scan(&list.Revision)
	if err != nil {
		return list, err
	}
	rows, err := q.Query(ctx, `SELECT id, title, body, status, version, published_at FROM site_announcement_entries
		ORDER BY COALESCE(published_at, created_at) DESC, created_at DESC, id`)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	list.Items = []AnnouncementEntry{}
	for rows.Next() {
		var item AnnouncementEntry
		if err = rows.Scan(&item.ID, &item.Title, &item.Body, &item.Status, &item.Version, &item.PublishedAt); err != nil {
			return list, err
		}
		list.Items = append(list.Items, item)
	}
	return list, rows.Err()
}

func (s *Store) PublicAnnouncements(ctx context.Context) (PublicFeed, error) {
	var feed PublicFeed
	feed.Timeline = []PublicNotice{}
	rows, err := s.Pool.Query(ctx, `SELECT id, title, body, version, published_at, status FROM site_announcement_entries
		WHERE status IN ('current', 'published') ORDER BY published_at DESC, id`)
	if err != nil {
		return feed, err
	}
	defer rows.Close()
	for rows.Next() {
		var item PublicNotice
		var status string
		if err = rows.Scan(&item.ID, &item.Title, &item.Body, &item.Version, &item.PublishedAt, &status); err != nil {
			return feed, err
		}
		feed.Timeline = append(feed.Timeline, item)
		if status == "current" {
			notice := item
			feed.Announcement = &notice
		}
	}
	return feed, rows.Err()
}

func (s *Store) ApplyAnnouncement(ctx context.Context, in AnnouncementCommand) (AnnouncementList, error) {
	title, body, err := normalizeNotice(in.Title, in.Body)
	if err != nil {
		return AnnouncementList{}, err
	}
	switch in.Action {
	case "publish":
		if title == "" || body == "" {
			return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "发布公告前，请填写标题和正文。")
		}
	case "correct":
		if in.ID == "" {
			return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "请选择要改正的公告。")
		}
	case "withdraw":
		if in.ID == "" {
			return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "请选择要撤回的公告。")
		}
	case "archive":
		if in.ID == "" {
			return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "请选择要结束展示的公告。")
		}
	default:
		return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "不认识的公告操作。")
	}
	if in.ExpectedRevision < 0 {
		return AnnouncementList{}, domain.Err(422, "INVALID_ARGUMENT", "不认识的公告操作。")
	}
	err = s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		scanErr := tx.QueryRow(ctx, "SELECT revision FROM site_announcement_state WHERE singleton FOR UPDATE").Scan(&revision)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			revision = 0
		} else if scanErr != nil {
			return scanErr
		}
		if revision != in.ExpectedRevision {
			return domain.Err(409, "REVISION_CONFLICT", "公告已被其他页面修改，请重新读取后再保存。")
		}
		changed, err := applyAnnouncement(ctx, tx, in, title, body)
		if err != nil || !changed {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO site_announcement_state(singleton, revision) VALUES (true, $1)
			ON CONFLICT (singleton) DO UPDATE SET revision=EXCLUDED.revision`, revision+1)
		return err
	})
	if err != nil {
		return AnnouncementList{}, err
	}
	return s.Announcements(ctx)
}

func applyAnnouncement(ctx context.Context, tx pgx.Tx, in AnnouncementCommand, title, body string) (bool, error) {
	switch in.Action {
	case "publish":
		if title == "" || body == "" {
			return false, domain.Err(422, "INVALID_ARGUMENT", "发布公告前，请填写标题和正文。")
		}
		if in.ID != "" {
			var status string
			err := tx.QueryRow(ctx, "SELECT status FROM site_announcement_entries WHERE id=$1 FOR UPDATE", in.ID).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, domain.Err(404, "NOT_FOUND", "这条公告已经不在了，请重新读取。")
			}
			if err != nil {
				return false, err
			}
			if status != "draft" {
				return false, domain.Err(422, "INVALID_ARGUMENT", "只有尚未发布的草稿可以发布成新公告。改正已有公告不会再次提醒访客。")
			}
		}
		var same bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM site_announcement_entries WHERE status='current' AND title=$1 AND body=$2)", title, body).Scan(&same); err != nil {
			return false, err
		}
		if same {
			if in.ID == "" {
				return false, nil
			}
			if _, err := tx.Exec(ctx, "DELETE FROM site_announcement_entries WHERE id=$1 AND status='draft'", in.ID); err != nil {
				return false, err
			}
			return true, nil
		}
		if _, err := tx.Exec(ctx, "UPDATE site_announcement_entries SET status='published' WHERE status='current'"); err != nil {
			return false, err
		}
		if in.ID != "" {
			_, err := tx.Exec(ctx, `UPDATE site_announcement_entries
				SET title=$2, body=$3, status='current', version=$4, published_at=now() WHERE id=$1 AND status='draft'`, in.ID, title, body, domain.ID())
			return true, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO site_announcement_entries(id, title, body, status, version, published_at)
			VALUES ($1,$2,$3,'current',$4,now())`, domain.ID(), title, body, domain.ID())
		return true, err
	case "correct":
		if in.ID == "" {
			return false, domain.Err(422, "INVALID_ARGUMENT", "请选择要改正的公告。")
		}
		var status, oldTitle, oldBody string
		err := tx.QueryRow(ctx, "SELECT status, title, body FROM site_announcement_entries WHERE id=$1 FOR UPDATE", in.ID).Scan(&status, &oldTitle, &oldBody)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, domain.Err(404, "NOT_FOUND", "这条公告已经不在了，请重新读取。")
		}
		if err != nil {
			return false, err
		}
		if oldTitle == title && oldBody == body {
			return false, nil
		}
		if status != "draft" && (title == "" || body == "") {
			return false, domain.Err(422, "INVALID_ARGUMENT", "已发布的公告需要保留标题和正文。")
		}
		_, err = tx.Exec(ctx, "UPDATE site_announcement_entries SET title=$2, body=$3 WHERE id=$1", in.ID, title, body)
		return true, err
	case "withdraw":
		if in.ID == "" {
			return false, domain.Err(422, "INVALID_ARGUMENT", "请选择要撤回的公告。")
		}
		tag, err := tx.Exec(ctx, "UPDATE site_announcement_entries SET status='withdrawn' WHERE id=$1 AND status<>'withdrawn'", in.ID)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			return true, nil
		}
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM site_announcement_entries WHERE id=$1)", in.ID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, domain.Err(404, "NOT_FOUND", "这条公告已经不在了，请重新读取。")
		}
		return false, nil
	default:
		if in.ID == "" {
			return false, domain.Err(422, "INVALID_ARGUMENT", "请选择要结束展示的公告。")
		}
		tag, err := tx.Exec(ctx, "UPDATE site_announcement_entries SET status='published' WHERE id=$1 AND status='current'", in.ID)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			return true, nil
		}
		var status string
		err = tx.QueryRow(ctx, "SELECT status FROM site_announcement_entries WHERE id=$1", in.ID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, domain.Err(404, "NOT_FOUND", "这条公告已经不在了，请重新读取。")
		}
		if err != nil {
			return false, err
		}
		return false, domain.Err(422, "INVALID_ARGUMENT", "只有正在展示的公告可以结束展示。")
	}
}
