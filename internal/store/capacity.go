package store

import (
	"context"

	"vidlens/internal/domain"
)

const IncomingFrameBytes int64 = 20 << 20
const DefaultHostReserveBytes int64 = 3 << 30
const DefaultVideoQuota int64 = 10 << 30
const DefaultImageQuota int64 = 5 << 30

type storageLoad struct {
	usedTemp, usedImages   int64
	resTemp, resImages     int64
	exports, exportPending int64
	ownTemp, ownImages     int64
	temp, images           int64
	available, total       int64
	videoQuota, imageQuota int64
	minHostFree            int64
	sourceDownloadLimit    int64
	stop                   int
}

// DiskCapacity reports VidLens's logical usage. Accepting reflects the host
// disk check, while host size and safety-margin values never leave the server.
type DiskCapacity struct {
	Scope          string `json:"scope"`
	TotalBytes     int64  `json:"total_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	ReservedBytes  int64  `json:"reserved_bytes"`
	WorkspaceBytes int64  `json:"workspace_bytes"`
	CeilingBytes   int64  `json:"ceiling_bytes"`
	Accepting      bool   `json:"accepting"`
	Level          string `json:"level"`
}

func storageLedger(ctx context.Context, q Querier, own string) (storageLoad, error) {
	var l storageLoad
	e := q.QueryRow(ctx, `
	 SELECT
	 COALESCE((SELECT sum(bytes) FROM media_assets WHERE kind IN ('video','audio') AND state IN ('ready','deleting','unavailable')),0),
	 COALESCE((SELECT sum(bytes) FROM media_assets WHERE kind='frame' AND state IN ('ready','deleting','unavailable')),0),
	 COALESCE((SELECT sum(GREATEST(0,r.temp_bytes-COALESCE(a.temp,0))) FROM storage_reservations r LEFT JOIN (SELECT run_id,sum(bytes) FILTER(WHERE kind IN ('video','audio')) AS temp FROM media_assets WHERE state IN ('ready','deleting','unavailable') GROUP BY run_id) a ON a.run_id=r.run_id WHERE r.run_id<>$1 AND r.expires_at>now()),0),
	 COALESCE((SELECT sum(GREATEST(0,r.image_bytes-COALESCE(a.images,0))) FROM storage_reservations r LEFT JOIN (SELECT run_id,sum(bytes) FILTER(WHERE kind='frame') AS images FROM media_assets WHERE state IN ('ready','deleting','unavailable') GROUP BY run_id) a ON a.run_id=r.run_id WHERE r.run_id<>$1 AND r.expires_at>now()),0),
	 COALESCE((SELECT sum(bytes) FROM media_assets WHERE run_id=$1 AND kind IN ('video','audio') AND state IN ('ready','deleting','unavailable')),0),
	 COALESCE((SELECT sum(bytes) FROM media_assets WHERE run_id=$1 AND kind='frame' AND state IN ('ready','deleting','unavailable')),0),
	 COALESCE((SELECT sum(bytes) FROM article_exports WHERE NOT file_deleted AND storage_key IS NOT NULL),0),
	 COALESCE((SELECT sum(GREATEST(0,67108864-COALESCE(bytes,0))) FROM article_exports WHERE NOT file_deleted AND status IN ('queued','running','retry_wait')),0),
	 video_quota_bytes,image_quota_bytes,disk_stop_percent,host_min_free_bytes,source_download_limit_bytes FROM storage_settings WHERE singleton`, own).Scan(
		&l.usedTemp, &l.usedImages, &l.resTemp, &l.resImages, &l.ownTemp, &l.ownImages, &l.exports, &l.exportPending, &l.videoQuota, &l.imageQuota, &l.stop, &l.minHostFree, &l.sourceDownloadLimit)
	return l, e
}

func (s *Store) DiskCapacity(ctx context.Context, available, total int64, workspace int64) (DiskCapacity, error) {
	if total <= 0 {
		return DiskCapacity{}, domain.Err(503, "SERVICE_UNAVAILABLE", "视频存储状态暂时读不到。")
	}
	l, e := storageLedger(ctx, s.Pool, "")
	if e != nil {
		return DiskCapacity{}, e
	}
	l.available, l.total = available, total
	l.temp, l.images = workspace, IncomingFrameBytes
	return buildCapacity(l), nil
}

func admitStorage(l storageLoad) error {
	if diskSpaceBlocked(l.available, l.total, storageExtra(l), l.minHostFree) {
		return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "服务器可用空间不足；请清理磁盘，至少为服务器保留 3 GiB 空间。")
	}
	return nil
}

func storageBlocked(l storageLoad) bool {
	return diskSpaceBlocked(l.available, l.total, storageExtra(l), l.minHostFree)
}

func diskSpaceBlocked(available, total, extra, minHostFree int64) bool {
	reserve := max(DefaultHostReserveBytes, minHostFree)
	return total <= 0 || available-extra < reserve
}

// DiskSpaceAllowed applies the owner-configured reserve, with a 3 GiB floor.
// Export workers recheck at execution time because host usage can change after queue admission.
func (s *Store) DiskSpaceAllowed(ctx context.Context, available, total, extra int64) (bool, error) {
	var minHostFree int64
	if err := s.Pool.QueryRow(ctx, "SELECT host_min_free_bytes FROM storage_settings WHERE singleton").Scan(&minHostFree); err != nil {
		return false, err
	}
	return !diskSpaceBlocked(available, total, extra, minHostFree), nil
}

func videoLimit(l storageLoad) int64 {
	if l.videoQuota > 0 {
		return l.videoQuota
	}
	return DefaultVideoQuota
}
func imageLimit(l storageLoad) int64 {
	if l.imageQuota > 0 {
		return l.imageQuota
	}
	return DefaultImageQuota
}

func storageExtra(l storageLoad) int64 {
	return max(int64(0), l.temp-l.ownTemp) + max(int64(0), l.images-l.ownImages) + l.resTemp + l.resImages + l.exportPending
}

func buildStorageAdmission(l storageLoad) StorageAdmission {
	incomingTemp := max(int64(0), l.temp-l.ownTemp)
	incomingImages := max(int64(0), l.images-l.ownImages)
	sourceReserve := incomingTemp + incomingImages
	systemReserve := max(DefaultHostReserveBytes, l.minHostFree)
	extra := storageExtra(l)
	otherReservations := max(int64(0), extra-sourceReserve)
	hostRequired := otherReservations + sourceReserve + systemReserve

	videoUsed := l.usedTemp + l.exports + l.exportPending + l.resTemp
	imageUsed := l.usedImages + l.resImages
	videoAvailable := max(int64(0), videoLimit(l)-videoUsed)
	imageAvailable := max(int64(0), imageLimit(l)-imageUsed)

	hostSlots := int64(0)
	if l.total > 0 && sourceReserve > 0 {
		hostRoom := l.available - otherReservations - systemReserve
		if hostRoom > 0 {
			hostSlots = hostRoom / sourceReserve
		}
	}
	additionalSlots := hostSlots

	blockedBy := make([]string, 0, 1)
	if diskSpaceBlocked(l.available, l.total, extra, l.minHostFree) {
		blockedBy = append(blockedBy, "host_disk")
	}

	return StorageAdmission{
		CanStartSource:           additionalSlots > 0 && len(blockedBy) == 0,
		AdditionalSourceSlots:    additionalSlots,
		BlockedBy:                blockedBy,
		HostAvailableBytes:       l.available,
		HostTotalBytes:           l.total,
		HostRequiredBytes:        hostRequired,
		HostShortfallBytes:       max(int64(0), hostRequired-l.available),
		SystemReserveBytes:       systemReserve,
		SourceReserveBytes:       sourceReserve,
		DiskStopPercent:          l.stop,
		VideoQuotaAvailableBytes: videoAvailable,
		ImageQuotaAvailableBytes: imageAvailable,
	}
}

func buildCapacity(l storageLoad) DiskCapacity {
	quota := videoLimit(l)
	used, reserved := l.usedTemp+l.exports, l.resTemp+l.exportPending
	available := max(int64(0), quota-used-reserved)
	accepting := !storageBlocked(l)
	level := "open"
	if !accepting {
		level = "closed"
	}
	return DiskCapacity{Scope: "video_workspace", TotalBytes: quota, UsedBytes: used,
		ReservedBytes: reserved, AvailableBytes: available, WorkspaceBytes: l.temp,
		CeilingBytes: max(int64(0), quota-reserved-l.temp), Accepting: accepting, Level: level}
}
