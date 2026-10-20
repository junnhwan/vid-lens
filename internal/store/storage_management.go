package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type StorageSettings struct {
	Revision                 int64 `json:"revision"`
	VideoQuotaBytes          int64 `json:"video_quota_bytes"`
	ImageQuotaBytes          int64 `json:"image_quota_bytes"`
	DiskStopPercent          int   `json:"disk_stop_percent"`
	HostMinFreeBytes         int64 `json:"host_min_free_bytes"`
	SourceDownloadLimitBytes int64 `json:"source_download_limit_bytes"`
}
type StorageInput struct {
	ExpectedRevision         int64 `json:"expected_revision"`
	VideoQuotaBytes          int64 `json:"video_quota_bytes"`
	ImageQuotaBytes          int64 `json:"image_quota_bytes"`
	DiskStopPercent          int   `json:"disk_stop_percent"`
	HostMinFreeBytes         int64 `json:"host_min_free_bytes"`
	SourceDownloadLimitBytes int64 `json:"source_download_limit_bytes"`
	MaxVideoMS               int64 `json:"max_video_ms"`
}
type StorageAdmission struct {
	CanStartSource           bool     `json:"can_start_source"`
	AdditionalSourceSlots    int64    `json:"additional_source_slots"`
	BlockedBy                []string `json:"blocked_by"`
	HostAvailableBytes       int64    `json:"host_available_bytes"`
	HostTotalBytes           int64    `json:"host_total_bytes"`
	HostRequiredBytes        int64    `json:"host_required_bytes"`
	HostShortfallBytes       int64    `json:"host_shortfall_bytes"`
	SystemReserveBytes       int64    `json:"system_reserve_bytes"`
	SourceReserveBytes       int64    `json:"source_reserve_bytes"`
	DiskStopPercent          int      `json:"disk_stop_percent"`
	VideoQuotaAvailableBytes int64    `json:"video_quota_available_bytes"`
	ImageQuotaAvailableBytes int64    `json:"image_quota_available_bytes"`
}
type StorageOverview struct {
	Settings           StorageSettings   `json:"settings"`
	MaxVideoMS         int64             `json:"max_video_ms"`
	OriginalBytes      int64             `json:"original_bytes"`
	ImageBytes         int64             `json:"image_bytes"`
	ExportBytes        int64             `json:"export_bytes"`
	ReservedVideoBytes int64             `json:"reserved_video_bytes"`
	ReservedImageBytes int64             `json:"reserved_image_bytes"`
	Retention          string            `json:"retention"`
	Admission          *StorageAdmission `json:"admission,omitempty"`
}

func (s *Store) StorageOverview(ctx context.Context) (StorageOverview, error) {
	return s.storageOverview(ctx, nil)
}

// StorageOverviewWithAdmission adds host filesystem state for the owner-only
// management view. Public capacity remains limited to the logical workspace.
func (s *Store) StorageOverviewWithAdmission(ctx context.Context, available, total, workspace int64) (StorageOverview, error) {
	return s.storageOverview(ctx, &storageAdmissionInput{available: available, total: total, workspace: workspace})
}

type storageAdmissionInput struct {
	available int64
	total     int64
	workspace int64
}

func (s *Store) storageOverview(ctx context.Context, admissionInput *storageAdmissionInput) (StorageOverview, error) {
	var v StorageOverview
	// One read-only snapshot keeps settings, counts and reservations aligned
	// without holding the lock used by admission, claims and heartbeats.
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, "SELECT revision,video_quota_bytes,image_quota_bytes,disk_stop_percent,host_min_free_bytes,source_download_limit_bytes FROM storage_settings WHERE singleton").Scan(&v.Settings.Revision, &v.Settings.VideoQuotaBytes, &v.Settings.ImageQuotaBytes, &v.Settings.DiskStopPercent, &v.Settings.HostMinFreeBytes, &v.Settings.SourceDownloadLimitBytes); e != nil {
			return e
		}
		l, e := storageLedger(ctx, tx, "")
		if e != nil {
			return e
		}
		v.OriginalBytes, v.ImageBytes, v.ExportBytes = l.usedTemp, l.usedImages, l.exports
		v.ReservedVideoBytes, v.ReservedImageBytes = l.resTemp+l.exportPending, l.resImages
		v.Retention = "manual"
		if admissionInput != nil {
			l.available, l.total = admissionInput.available, admissionInput.total
			l.temp, l.images = admissionInput.workspace, IncomingFrameBytes
			admission := buildStorageAdmission(l)
			v.Admission = &admission
		}
		return nil
	})
	return v, e
}

func (s *Store) SaveStorageSettings(ctx context.Context, in StorageInput, workspace int64) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var currentID string
		if e := tx.QueryRow(ctx, "SELECT config_id FROM runtime_settings WHERE singleton FOR UPDATE").Scan(&currentID); e != nil {
			return e
		}
		current, e := one[config.Profile](ctx, tx, "SELECT profile FROM runtime_config_revisions WHERE id=$1", currentID)
		if e != nil {
			return e
		}
		l, e := storageLedger(ctx, tx, "")
		if e != nil {
			return e
		}
		// Keep the legacy percentage field stable for older admin clients; disk
		// admission no longer uses it.
		if in.DiskStopPercent == 0 {
			in.DiskStopPercent = l.stop
		}
		if in.HostMinFreeBytes == 0 {
			in.HostMinFreeBytes = l.minHostFree
		}
		if in.SourceDownloadLimitBytes == 0 {
			in.SourceDownloadLimitBytes = l.sourceDownloadLimit
		}
		if in.MaxVideoMS == 0 {
			in.MaxVideoMS = current.MaxVideoMS
		}
		if in.SourceDownloadLimitBytes < 64<<20 || in.SourceDownloadLimitBytes > 2<<30 {
			return domain.Err(422, "INVALID_ARGUMENT", "单条来源下载上限须在 64 MiB 到 2 GiB 之间。")
		}
		if in.MaxVideoMS < 60_000 || in.MaxVideoMS > 24*60*60*1000 {
			return domain.Err(422, "INVALID_ARGUMENT", "单条视频时长上限须在 1 分钟到 24 小时之间。")
		}
		if in.HostMinFreeBytes < DefaultHostReserveBytes || in.HostMinFreeBytes > 20<<30 {
			return domain.Err(422, "INVALID_ARGUMENT", "主机最低空闲空间不能少于 3 GiB，且不能超过 20 GiB。")
		}
		effectiveWorkspace := max(workspace, in.SourceDownloadLimitBytes+(64<<20))
		if in.VideoQuotaBytes < max(int64(1<<30), effectiveWorkspace) || in.VideoQuotaBytes > 10<<40 || in.ImageQuotaBytes < 1<<30 || in.ImageQuotaBytes > 10<<40 {
			return domain.Err(422, "INVALID_ARGUMENT", "视频配额不能小于单次工作区，配图配额至少 1 GiB；两项均不能超过 10 TiB。")
		}
		tag, e := tx.Exec(ctx, "UPDATE storage_settings SET video_quota_bytes=$1,image_quota_bytes=$2,disk_stop_percent=$3,host_min_free_bytes=$4,source_download_limit_bytes=$5,revision=revision+1 WHERE singleton AND revision=$6", in.VideoQuotaBytes, in.ImageQuotaBytes, in.DiskStopPercent, in.HostMinFreeBytes, in.SourceDownloadLimitBytes, in.ExpectedRevision)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return domain.Err(409, "REVISION_CONFLICT", "存储设置已被更新，请重新读取后保存。")
		}
		if in.MaxVideoMS != current.MaxVideoMS {
			current.MaxVideoMS = in.MaxVideoMS
			newID := domain.ID()
			if _, e := tx.Exec(ctx, "INSERT INTO runtime_config_revisions(id,profile) VALUES($1,$2)", newID, domain.JSON(current)); e != nil {
				return e
			}
			tag, e := tx.Exec(ctx, "UPDATE runtime_settings SET config_id=$1 WHERE config_id=$2", newID, currentID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return domain.Err(409, "REVISION_CONFLICT", "运行配置已被更新，请刷新后重试。")
			}
		}
		return nil
	})
}

type ManagedVideo struct {
	ID             string    `json:"id"`
	Platform       string    `json:"platform"`
	URL            string    `json:"url"`
	Title          string    `json:"title"`
	DurationMS     *int64    `json:"duration_ms"`
	RequestCount   int64     `json:"request_count"`
	ArticleCount   int64     `json:"article_count"`
	LatestStatus   string    `json:"latest_status"`
	LastRequested  time.Time `json:"last_requested_at"`
	OriginalBytes  int64     `json:"original_bytes"`
	ImageBytes     int64     `json:"image_bytes"`
	AssetIDs       []string  `json:"asset_ids"`
	Active         bool      `json:"active"`
	Protected      bool      `json:"protected"`
	Deleting       bool      `json:"deleting"`
	DeleteFailures int64     `json:"delete_failures"`
	Cleanup        string    `json:"cleanup"`
}

type VideoCleanupCounts struct {
	Ready int64 `json:"ready"`
	Held  int64 `json:"held"`
	Gone  int64 `json:"gone"`
}

// Never return session identifiers, private questions, prompts or storage paths.
// cleanup is "", "ready" (originals can be removed), "held" (in use), or "gone" (nothing left to remove).
func (s *Store) ManagedVideos(ctx context.Context, search, cleanup string, limit, offset int) ([]ManagedVideo, int64, VideoCleanupCounts, error) {
	var total int64
	var counts VideoCleanupCounts
	var out []ManagedVideo
	switch cleanup {
	case "", "ready", "held", "gone":
	default:
		return nil, 0, counts, domain.Err(422, "INVALID_ARGUMENT", "清理筛选只能是可清理、使用中或已清理。")
	}
	e := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := `WITH videos AS (
		 SELECT v.id,v.platform,v.canonical_url AS url,
		 COALESCE(NULLIF(s.source->>'title',''),NULLIF(o.result->>'title',''),v.canonical_id) AS title,
		 COALESCE(s.duration_ms,(o.result->>'duration_ms')::bigint) AS duration_ms,
		 r.status AS latest_status,r.created_at AS last_requested_at,
		 (SELECT count(*) FROM generation_requests WHERE source_id=v.id) AS request_count,
		 (SELECT count(*) FROM articles a JOIN source_snapshots ss ON ss.id=a.snapshot_id WHERE ss.source_id=v.id) AS article_count,
		 COALESCE(m.original_bytes,0) AS original_bytes,COALESCE(m.image_bytes,0) AS image_bytes,
		 COALESCE(m.asset_ids,'[]'::jsonb) AS asset_ids,COALESCE(m.deleting,false) AS deleting,
		 COALESCE(m.delete_failures,0) AS delete_failures,
		 EXISTS(SELECT 1 FROM job_runs j WHERE j.source_id=v.id AND j.status NOT IN ('succeeded','failed','canceled')) AS active,
		 EXISTS(SELECT 1 FROM media_assets a JOIN asset_refs ar ON ar.asset_id=a.id WHERE a.source_id=v.id AND a.kind IN ('video','audio') AND a.state<>'deleted' AND (ar.expires_at IS NULL OR ar.expires_at>now())) AS protected
		 FROM video_sources v
		 JOIN LATERAL (SELECT status,created_at FROM generation_requests WHERE source_id=v.id ORDER BY created_at DESC,id DESC LIMIT 1) r ON true
		 LEFT JOIN LATERAL (SELECT source,duration_ms FROM source_snapshots WHERE source_id=v.id ORDER BY created_at DESC LIMIT 1) s ON true
		 LEFT JOIN LATERAL (SELECT ro.result FROM run_operations ro JOIN job_runs j ON j.id=ro.run_id WHERE j.source_id=v.id AND ro.operation_key='source-metadata' AND ro.status='completed' ORDER BY ro.updated_at DESC LIMIT 1) o ON true
		 LEFT JOIN LATERAL (SELECT sum(bytes) FILTER(WHERE kind IN ('video','audio')) AS original_bytes,
		 sum(bytes) FILTER(WHERE kind='frame') AS image_bytes,
		 jsonb_agg(id ORDER BY id) FILTER(WHERE kind IN ('video','audio')) AS asset_ids,
		 bool_or(state='deleting' AND kind IN ('video','audio')) AS deleting,
		 sum(delete_failures) FILTER(WHERE kind IN ('video','audio')) AS delete_failures
		 FROM media_assets WHERE source_id=v.id AND state<>'deleted') m ON true
		), classified AS (
		 SELECT *, CASE WHEN active OR protected THEN 'held' WHEN COALESCE(jsonb_array_length(asset_ids),0)=0 THEN 'gone' ELSE 'ready' END AS cleanup FROM videos
		), filtered AS (SELECT * FROM classified WHERE $1='' OR strpos(lower(title||' '||url||' '||platform),lower($1))>0) `
		if e := tx.QueryRow(ctx, q+`SELECT count(*) FILTER (WHERE $2='' OR cleanup=$2), count(*) FILTER (WHERE cleanup='ready'), count(*) FILTER (WHERE cleanup='held'), count(*) FILTER (WHERE cleanup='gone') FROM filtered`, search, cleanup).Scan(&total, &counts.Ready, &counts.Held, &counts.Gone); e != nil {
			return e
		}
		var e error
		out, e = many[ManagedVideo](ctx, tx, q+"SELECT to_jsonb(x) FROM (SELECT * FROM filtered WHERE $4='' OR cleanup=$4 ORDER BY last_requested_at DESC,id DESC LIMIT $2 OFFSET $3) x", search, limit, offset, cleanup)
		return e
	})
	return out, total, counts, e
}

// Select the exact originals reviewed by the owner. A stale retry cannot delete
// a newer download. Pins and new work share this transaction's control lock.
func (s *Store) MarkVideoDeleting(ctx context.Context, source string, expected []string) ([]domain.Asset, error) {
	var assets []domain.Asset
	if len(expected) == 0 || len(expected) > 1000 {
		return nil, domain.Err(422, "INVALID_ARGUMENT", "请先重新读取要清理的素材。")
	}
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var exists, active, stale, protected bool
		if e := tx.QueryRow(ctx, `SELECT
		 EXISTS(SELECT 1 FROM video_sources WHERE id=$1),
		 EXISTS(SELECT 1 FROM job_runs WHERE source_id=$1 AND status NOT IN ('succeeded','failed','canceled')),
		 EXISTS(SELECT 1 FROM media_assets WHERE source_id=$1 AND kind IN ('video','audio') AND state<>'deleted' AND NOT(id=ANY($2))),
		 EXISTS(SELECT 1 FROM media_assets a JOIN asset_refs ar ON ar.asset_id=a.id WHERE a.source_id=$1 AND a.id=ANY($2) AND a.kind IN ('video','audio') AND a.state<>'deleted' AND (ar.expires_at IS NULL OR ar.expires_at>now()))`, source, expected).Scan(&exists, &active, &stale, &protected); e != nil {
			return e
		}
		if !exists {
			return domain.Err(404, "RESOURCE_NOT_FOUND", "视频记录不存在。")
		}
		if active || protected {
			return domain.Err(409, "STORAGE_IN_USE", "视频正在被任务使用，请等处理结束后再清理。")
		}
		if stale {
			return domain.Err(409, "REVISION_CONFLICT", "视频素材已有变化，请刷新列表后重新确认。")
		}
		var e error
		assets, e = many[domain.Asset](ctx, tx, `UPDATE media_assets SET state='deleting',delete_not_before=NULL WHERE source_id=$1 AND id=ANY($2) AND kind IN ('video','audio') AND state<>'deleted' RETURNING to_jsonb(media_assets)`, source, expected)
		return e
	})
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.Err(404, "RESOURCE_NOT_FOUND", "视频记录不存在。")
	}
	return assets, e
}
