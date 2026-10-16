package store

import (
	"context"

	"vidlens/internal/domain"
)

// IncomingFrameBytes is reserved with the download workspace before a video is fetched.
const IncomingFrameBytes int64 = 20 << 20

type storageLoad struct {
	usedTemp, usedImages   int64
	resTemp, resImages     int64
	exports, exportPending int64
	ownTemp, ownImages     int64
	temp, images           int64
	available, total       int64
	stop                   int
}

// DiskCapacity is the public snapshot of the volume that holds runtime data.
// CeilingBytes is the used-byte mark at which another download would be refused.
type DiskCapacity struct {
	TotalBytes     int64  `json:"total_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	ReserveBytes   int64  `json:"reserve_bytes"`
	CeilingBytes   int64  `json:"ceiling_bytes"`
	StopPercent    int    `json:"stop_percent"`
	Accepting      bool   `json:"accepting"`
	Level          string `json:"level"`
	BlockedBy      string `json:"blocked_by,omitempty"`
}

func (s *Store) DiskCapacity(ctx context.Context, available, total int64, stop int, workspace int64) (DiskCapacity, error) {
	if total <= 0 {
		return DiskCapacity{}, domain.Err(503, "SERVICE_UNAVAILABLE", "磁盘状态暂时读不到。")
	}
	var l storageLoad
	if e := s.Pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT sum(bytes) FILTER (WHERE kind IN ('video','audio')) FROM media_assets WHERE state IN ('ready','deleting')),0),
			COALESCE((SELECT sum(bytes) FILTER (WHERE kind='frame') FROM media_assets WHERE state IN ('ready','deleting')),0),
			COALESCE((SELECT sum(GREATEST(0,r.temp_bytes-COALESCE(a.temp,0))) FROM storage_reservations r LEFT JOIN (SELECT run_id,sum(bytes) FILTER (WHERE kind IN ('video','audio')) AS temp,sum(bytes) FILTER (WHERE kind='frame') AS images FROM media_assets WHERE state IN ('ready','deleting') GROUP BY run_id) a ON a.run_id=r.run_id WHERE r.expires_at>now()),0),
			COALESCE((SELECT sum(GREATEST(0,r.image_bytes-COALESCE(a.images,0))) FROM storage_reservations r LEFT JOIN (SELECT run_id,sum(bytes) FILTER (WHERE kind IN ('video','audio')) AS temp,sum(bytes) FILTER (WHERE kind='frame') AS images FROM media_assets WHERE state IN ('ready','deleting') GROUP BY run_id) a ON a.run_id=r.run_id WHERE r.expires_at>now()),0),
			COALESCE((SELECT sum(bytes) FILTER (WHERE storage_key IS NOT NULL) FROM article_exports WHERE NOT file_deleted),0),
			COALESCE((SELECT sum(67108864) FILTER (WHERE status IN ('queued','running','retry_wait')) FROM article_exports WHERE NOT file_deleted),0)`).Scan(&l.usedTemp, &l.usedImages, &l.resTemp, &l.resImages, &l.exports, &l.exportPending); e != nil {
		return DiskCapacity{}, e
	}
	l.available, l.total, l.stop = available, total, stop
	l.temp, l.images = workspace, IncomingFrameBytes
	return buildCapacity(l), nil
}

func admitStorage(l storageLoad) error {
	if storageBlocked(l) {
		return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "磁盘可用容量不足，暂不能准备新素材。")
	}
	return nil
}

func storageBlocked(l storageLoad) bool {
	disk, media := storageLimits(l)
	return disk || media
}

func storageLimits(l storageLoad) (disk, media bool) {
	extra := storageExtra(l)
	reserve := diskFloor(l.total, l.stop)
	incomingTemp := max(int64(0), l.temp-l.ownTemp)
	incomingImages := max(int64(0), l.images-l.ownImages)
	media = l.usedTemp+l.exports+l.exportPending+l.resTemp+incomingTemp > 10<<30 || l.usedImages+l.resImages+incomingImages > 5<<30
	disk = l.available-extra < reserve || (l.total-l.available+extra)*100 > l.total*int64(l.stop)
	return disk, media
}

func storageExtra(l storageLoad) int64 {
	return max(int64(0), l.temp-l.ownTemp) + max(int64(0), l.images-l.ownImages) + l.resTemp + l.resImages + l.exportPending
}

func diskFloor(total int64, stop int) int64 {
	reserve := max(int64(5<<30), total*15/100)
	if stop > 85 {
		reserve = 5 << 30
	}
	return reserve
}

func buildCapacity(l storageLoad) DiskCapacity {
	extra := storageExtra(l)
	reserve := diskFloor(l.total, l.stop)
	ceiling := min(l.total-extra-reserve, l.total*int64(l.stop)/100-extra)
	if ceiling < 0 {
		ceiling = 0
	}
	if l.total > 0 && ceiling > l.total {
		ceiling = l.total
	}
	used := l.total - l.available
	if used < 0 {
		used = 0
	}
	disk, media := storageLimits(l)
	accepting := !disk && !media && l.total > 0
	level := "open"
	incoming := max(int64(0), l.temp-l.ownTemp) + max(int64(0), l.images-l.ownImages)
	if !accepting {
		level = "closed"
	} else if ceiling-used < incoming {
		level = "tight"
	}
	blocked := ""
	if !accepting {
		if disk {
			blocked = "disk"
		} else if media {
			blocked = "media"
		}
	}
	available := l.available
	if available < 0 {
		available = 0
	}
	return DiskCapacity{
		TotalBytes:     l.total,
		AvailableBytes: available,
		UsedBytes:      used,
		ReserveBytes:   reserve,
		CeilingBytes:   ceiling,
		StopPercent:    l.stop,
		Accepting:      accepting,
		Level:          level,
		BlockedBy:      blocked,
	}
}
