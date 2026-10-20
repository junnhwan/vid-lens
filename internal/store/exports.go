package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
	"vidlens/internal/domain"
)

const ExportMaxBytes int64 = 64 << 20
const ExportMaxEntries = 128
const ExportStorageBytes int64 = 256 << 20

type Export struct {
	ID              string          `json:"id"`
	SessionID       string          `json:"session_id"`
	RequestID       string          `json:"request_id"`
	ArticleID       string          `json:"article_id"`
	Format          string          `json:"format"`
	RendererVersion int             `json:"renderer_version"`
	Revision        int64           `json:"revision"`
	Status          string          `json:"status"`
	Epoch           int64           `json:"epoch"`
	Attempts        int             `json:"attempts"`
	ActiveMS        int64           `json:"active_ms"`
	LeaseUntil      *time.Time      `json:"lease_until"`
	DeadlineAt      time.Time       `json:"deadline_at"`
	StorageKey      *string         `json:"storage_key"`
	Bytes           *int64          `json:"bytes"`
	SHA256          *string         `json:"sha256"`
	Failure         *domain.Failure `json:"failure"`
	RetryAt         *time.Time      `json:"retry_at"`
	CreatedAt       time.Time       `json:"created_at"`
	ReadyAt         *time.Time      `json:"ready_at"`
	ExpiresAt       *time.Time      `json:"expires_at"`
}

type DiskSpace struct {
	Available, Total int64
}

func (s *Store) CreateExport(ctx context.Context, session, request, format, key string, disk ...DiskSpace) (Accepted, bool, error) {
	var a Accepted
	reused := false
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, e := loadRequest(ctx, tx, request)
		if e != nil || r.SessionID != session {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "资源不存在。")
		}
		var found bool
		scope := "export:" + session + ":" + request
		h := Hash(map[string]string{"format": format})
		if a, found, e = replay(ctx, tx, scope, key, h); e != nil || found {
			return e
		}
		if r.ArticleID == nil {
			return domain.Err(409, "ARTICLE_NOT_READY", "文章尚未就绪。")
		}
		if format != "markdown" && format != "markdown_zip" {
			return domain.Err(400, "INVALID_ARGUMENT", "不支持这种导出格式。")
		}
		var id string
		e = tx.QueryRow(ctx, `SELECT id FROM article_exports WHERE session_id=$1 AND request_id=$2 AND article_id=$3 AND format=$4 AND renderer_version=$5 AND (status IN ('queued','running','retry_wait') AND deadline_at>now() OR status='ready' AND expires_at>now()) ORDER BY created_at DESC LIMIT 1`, session, request, *r.ArticleID, format, domain.ExportRendererVersion).Scan(&id)
		if e == nil {
			reused = true
			a, e = remember(ctx, tx, scope, key, h, id, 86400)
			return e
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		var total, own int
		var reserved int64
		if e = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE session_id=$1) FROM article_exports WHERE status IN ('queued','running','retry_wait') AND deadline_at>now()`, session).Scan(&total, &own); e != nil {
			return e
		}
		if total >= 5 || own >= 1 {
			return domain.Err(429, "ADMISSION_LIMITED", "导出容量暂时已满，请稍后重试。")
		}
		if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN status IN ('queued','running','retry_wait') THEN 67108864 ELSE COALESCE(bytes,0) END),0) FROM article_exports WHERE NOT file_deleted AND (storage_key IS NOT NULL OR status IN ('queued','running','retry_wait'))`).Scan(&reserved); e != nil {
			return e
		}
		if reserved+ExportMaxBytes > ExportStorageBytes {
			return domain.Err(429, "ADMISSION_LIMITED", "导出副本空间暂时已满。")
		}
		load, e := storageLedger(ctx, tx, "")
		if e != nil {
			return e
		}
		if load.usedTemp+load.exports+load.resTemp+load.exportPending+ExportMaxBytes > videoLimit(load) {
			return domain.Err(429, "ADMISSION_LIMITED", "临时素材与导出空间暂时已满。")
		}
		if len(disk) > 0 {
			d := disk[0]
			extra := load.resTemp + load.resImages + load.exportPending + ExportMaxBytes
			if diskSpaceBlocked(d.Available, d.Total, extra, load.minHostFree) {
				return domain.Err(429, "ADMISSION_LIMITED", "磁盘余量不足，暂时无法准备导出。")
			}
		}
		art, e := article(ctx, tx, *r.ArticleID)
		if e != nil {
			return e
		}
		ids := ContentFrameIDs(art.Sections, art.Citations)
		if format == "markdown_zip" {
			if len(ids)+3 > ExportMaxEntries {
				return domain.Err(422, "EXPORT_LIMIT_EXCEEDED", "文章配图数量超过导出限制。")
			}
			sort.Strings(ids)
			for _, fid := range ids {
				var state string
				if e = tx.QueryRow(ctx, "SELECT state FROM media_assets WHERE id=$1 FOR UPDATE", fid).Scan(&state); e != nil {
					return e
				}
				if state != "ready" {
					return domain.Err(409, "EXPORT_ASSET_UNAVAILABLE", "正式配图暂不可读。")
				}
			}
		}
		id = domain.ID()
		if _, e = tx.Exec(ctx, "INSERT INTO article_exports(id,session_id,request_id,article_id,format,renderer_version) VALUES($1,$2,$3,$4,$5,$6)", id, session, request, *r.ArticleID, format, domain.ExportRendererVersion); e != nil {
			return e
		}
		if format == "markdown_zip" {
			for _, fid := range ids {
				if _, e = tx.Exec(ctx, "INSERT INTO asset_refs(asset_id,owner_type,owner_id,expires_at) VALUES($1,'export',$2,now()+interval '12 minutes')", fid, id); e != nil {
					return e
				}
			}
		}
		a, e = remember(ctx, tx, scope, key, h, id, 86400)
		return e
	})
	return a, reused, e
}
func (s *Store) Export(ctx context.Context, session, request, id string) (Export, error) {
	v, e := one[Export](ctx, s.Pool, "SELECT to_jsonb(e) FROM article_exports e WHERE id=$1 AND session_id=$2 AND request_id=$3", id, session, request)
	if e != nil {
		return v, NotFound(e)
	}
	if v.Status == "ready" && v.ExpiresAt != nil && !v.ExpiresAt.After(time.Now()) {
		_, e = s.Pool.Exec(ctx, "UPDATE article_exports SET status='expired',revision=revision+1 WHERE id=$1 AND status='ready'", id)
		v.Status = "expired"
		v.Revision++
	}
	return v, e
}
func ExportView(v *Export) map[string]any {
	var exported any
	var failure *domain.Failure
	var retry *time.Time
	download := false
	if v != nil {
		download = v.Status == "ready"
		var url *string
		if download {
			url = domain.Ptr("/api/v1/requests/" + v.RequestID + "/exports/" + v.ID + "/content")
		}
		exported = map[string]any{"id": v.ID, "request_id": v.RequestID, "article_id": v.ArticleID, "format": v.Format, "revision": v.Revision, "status": v.Status, "created_at": v.CreatedAt, "ready_at": v.ReadyAt, "expires_at": v.ExpiresAt, "bytes": v.Bytes, "sha256": v.SHA256, "download_url": url}
		failure = v.Failure
		retry = v.RetryAt
	}
	return map[string]any{"schema_version": 1, "export": exported, "failure": failure, "retry_at": retry, "actions": map[string]bool{"can_download": download, "can_create_export": v == nil || v.Status == "ready" || v.Status == "expired" || v.Status == "unavailable" || v.Status == "failed"}}
}
func (s *Store) LatestExport(ctx context.Context, session, request, format string) (*Export, error) {
	r, e := s.Request(ctx, session, request)
	if e != nil {
		return nil, e
	}
	_ = r
	var id string
	e = s.Pool.QueryRow(ctx, "SELECT id FROM article_exports WHERE session_id=$1 AND request_id=$2 AND ($3='' OR format=$3) AND renderer_version=$4 ORDER BY created_at DESC LIMIT 1", session, request, format, domain.ExportRendererVersion).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := s.Export(ctx, session, request, id)
	return &v, e
}
func (s *Store) PendingExports(ctx context.Context) ([]Export, error) {
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE article_exports SET status='failed',revision=revision+1,failure='{"code":"TASK_DEADLINE_EXCEEDED","message":"导出准备已达到期限。","can_retry":true,"retry_at":null}' WHERE status IN ('queued','running','retry_wait') AND (deadline_at<now() OR active_ms>=120000 OR attempts>=2 AND lease_until<now())`); e != nil {
			return e
		}
		// A crashed worker may have used its full reserved active allowance.
		// Charge it conservatively rather than silently resetting the budget.
		_, e := tx.Exec(ctx, `UPDATE article_exports SET status='failed',revision=revision+1,active_ms=120000,failure='{"code":"TASK_DEADLINE_EXCEEDED","message":"导出执行中断，可重新创建。","can_retry":true,"retry_at":null}' WHERE status='running' AND lease_until<now()`)
		return e
	})
	if e != nil {
		return nil, e
	}
	return many[Export](ctx, s.Pool, "SELECT to_jsonb(e) FROM article_exports e WHERE (status='queued' OR status='retry_wait' AND retry_at<=now()) AND deadline_at>now() ORDER BY created_at LIMIT 5")
}
func (s *Store) ClaimExport(ctx context.Context, id string) (*Export, error) {
	var v *Export
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var running bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE status='running' AND lease_until>now())").Scan(&running); e != nil {
			return e
		}
		if running {
			return nil
		}
		x, e := one[Export](ctx, tx, "UPDATE article_exports SET status='running',epoch=epoch+1,attempts=attempts+1,revision=revision+1,lease_until=LEAST(deadline_at,now()+interval '120 seconds'),retry_at=NULL WHERE id=$1 AND (status='queued' OR status='retry_wait' AND retry_at<=now()) AND deadline_at>now() AND active_ms<120000 AND attempts<2 RETURNING to_jsonb(article_exports)", id)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e == nil {
			v = &x
		}
		return e
	})
	return v, e
}
func exportFence(ctx context.Context, q Querier, v Export) error {
	var ok bool
	e := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE id=$1 AND epoch=$2 AND status='running' AND lease_until>now() AND deadline_at>now())", v.ID, v.Epoch).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return domain.ErrFenced
	}
	return nil
}
func (s *Store) FinishExport(ctx context.Context, v Export, key, sha string, bytes int64, failure *domain.Failure, retry bool, elapsed time.Duration) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := exportFence(ctx, tx, v); e != nil {
			return e
		}
		status := "ready"
		if failure != nil {
			status = "failed"
			if retry && v.Attempts < 2 && v.ActiveMS+elapsed.Milliseconds() < 120000 {
				status = "retry_wait"
			}
		}
		if _, e := tx.Exec(ctx, "UPDATE article_exports SET status=$3,storage_key=NULLIF($4,''),sha256=NULLIF($5,''),bytes=$6,failure=$7,active_ms=active_ms+$8,ready_at=CASE WHEN $3='ready' THEN now() END,expires_at=CASE WHEN $3='ready' THEN now()+interval '24 hours' END,retry_at=CASE WHEN $3='retry_wait' THEN now()+interval '5 seconds' END,revision=revision+1,lease_until=NULL WHERE id=$1 AND epoch=$2", v.ID, v.Epoch, status, key, sha, bytes, nullableJSON(failure), elapsed.Milliseconds()); e != nil {
			return e
		}
		if status != "retry_wait" {
			_, e := tx.Exec(ctx, "DELETE FROM asset_refs WHERE owner_type='export' AND owner_id=$1", v.ID)
			return e
		}
		return nil
	})
}
func (s *Store) ExportDownload(ctx context.Context, v Export) (string, error) {
	id := domain.ID()
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "DELETE FROM export_download_leases WHERE expires_at<now()"); e != nil {
			return e
		}
		var ready bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE id=$1 AND status='ready' AND expires_at>now())", v.ID).Scan(&ready); e != nil {
			return e
		}
		if !ready {
			return domain.Err(410, "EXPORT_EXPIRED", "导出已过期，请重新创建。")
		}
		var n, own int
		if e := tx.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE session_id=$1) FROM export_download_leases", v.SessionID).Scan(&n, &own); e != nil {
			return e
		}
		if n >= 2 || own >= 1 {
			return domain.Err(429, "ADMISSION_LIMITED", "下载连接已满，请稍后再试。")
		}
		_, e := tx.Exec(ctx, "INSERT INTO export_download_leases(id,export_id,session_id) VALUES($1,$2,$3)", id, v.ID, v.SessionID)
		return e
	})
	return id, e
}
func (s *Store) ReleaseDownload(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM export_download_leases WHERE id=$1", id)
	return e
}
func (s *Store) ExportUnavailable(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, `UPDATE article_exports SET status='unavailable',revision=revision+1,failure='{"code":"EXPORT_FILE_UNAVAILABLE","message":"导出副本暂不可读，请重新创建。","can_retry":true,"retry_at":null}' WHERE id=$1 AND status='ready'`, id)
	return e
}
func (s *Store) ExpiredExportFiles(ctx context.Context) ([]Export, error) {
	if _, e := s.Pool.Exec(ctx, "UPDATE article_exports SET status='expired',revision=revision+1 WHERE status='ready' AND expires_at<now()"); e != nil {
		return nil, e
	}
	return many[Export](ctx, s.Pool, "SELECT to_jsonb(e) FROM article_exports e WHERE status IN ('expired','unavailable') AND storage_key IS NOT NULL AND NOT file_deleted AND NOT EXISTS(SELECT 1 FROM export_download_leases d WHERE d.export_id=e.id AND d.expires_at>now()) LIMIT 20")
}
func (s *Store) ExportFileDeleted(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, "UPDATE article_exports SET file_deleted=true WHERE id=$1 AND status IN ('expired','unavailable')", id)
	return e
}

// An uncommitted epoch file is removable only after its worker's deadline.
// A committed file remains protected until the normal download-aware cleanup.
func (s *Store) ExportFileProtected(ctx context.Context, key, id string) (bool, error) {
	var protected bool
	e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM article_exports WHERE (storage_key=$1 AND NOT file_deleted) OR (id=$2 AND status='running' AND deadline_at>now()))", key, id).Scan(&protected)
	return protected, e
}
