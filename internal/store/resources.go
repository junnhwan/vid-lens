package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
	"vidlens/internal/domain"
)

func (s *Store) AcquireResource(ctx context.Context, c domain.Claim, resource, owner string, slots int) (bool, error) {
	ok := false
	e := s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "DELETE FROM resource_leases WHERE resource=$1 AND expires_at<now()", resource); e != nil {
			return e
		}
		for i := 0; i < slots; i++ {
			tag, e := tx.Exec(ctx, "INSERT INTO resource_leases(resource,slot,owner,run_id,execution_epoch,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '60 seconds') ON CONFLICT DO NOTHING", resource, i, owner, c.Run.ID, c.Epoch)
			if e != nil {
				return e
			}
			if tag.RowsAffected() > 0 {
				ok = true
				break
			}
		}
		return nil
	})
	return ok, e
}
func (s *Store) RenewResource(ctx context.Context, c domain.Claim, resource, owner string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, "UPDATE resource_leases SET expires_at=now()+interval '60 seconds' WHERE resource=$1 AND owner=$2 AND expires_at>now()", resource, owner)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrFenced
		}
		return nil
	})
}
func (s *Store) ReleaseResource(ctx context.Context, resource, owner string) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM resource_leases WHERE resource=$1 AND owner=$2", resource, owner)
	return e
}
func (s *Store) ReserveStorage(ctx context.Context, c domain.Claim, temp, images, available, total int64, stop int) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		var usedTemp, usedImages, resTemp, resImages int64
		if e := tx.QueryRow(ctx, "SELECT COALESCE(sum(bytes) FILTER(WHERE kind IN ('video','audio')),0),COALESCE(sum(bytes) FILTER(WHERE kind='frame'),0) FROM media_assets WHERE state IN ('ready','deleting')").Scan(&usedTemp, &usedImages); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, "SELECT COALESCE(sum(GREATEST(0,r.temp_bytes-COALESCE(a.temp,0))),0),COALESCE(sum(GREATEST(0,r.image_bytes-COALESCE(a.images,0))),0) FROM storage_reservations r LEFT JOIN (SELECT run_id,sum(bytes) FILTER(WHERE kind IN ('video','audio')) AS temp,sum(bytes) FILTER(WHERE kind='frame') AS images FROM media_assets WHERE state IN ('ready','deleting') GROUP BY run_id) a ON a.run_id=r.run_id WHERE r.run_id<>$1 AND r.expires_at>now()", c.Run.ID).Scan(&resTemp, &resImages); e != nil {
			return e
		}
		var ownTemp, ownImages int64
		if e := tx.QueryRow(ctx, "SELECT COALESCE(sum(bytes) FILTER(WHERE kind IN ('video','audio')),0),COALESCE(sum(bytes) FILTER(WHERE kind='frame'),0) FROM media_assets WHERE run_id=$1 AND state IN ('ready','deleting')", c.Run.ID).Scan(&ownTemp, &ownImages); e != nil {
			return e
		}
		extra := max(int64(0), temp-ownTemp) + max(int64(0), images-ownImages) + resTemp + resImages
		reserve := max(int64(5<<30), total*15/100)
		if stop > 85 {
			reserve = 5 << 30
		}
		if usedTemp+resTemp+max(int64(0), temp-ownTemp) > 10<<30 || usedImages+resImages+max(int64(0), images-ownImages) > 5<<30 || available-extra < reserve || (total-available+extra)*100 > total*int64(stop) {
			return domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "磁盘可用容量不足，暂不能准备新素材。")
		}
		_, e := tx.Exec(ctx, "INSERT INTO storage_reservations(run_id,temp_bytes,image_bytes,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(run_id) DO UPDATE SET temp_bytes=GREATEST(storage_reservations.temp_bytes,EXCLUDED.temp_bytes),image_bytes=GREATEST(storage_reservations.image_bytes,EXCLUDED.image_bytes)", c.Run.ID, temp, images, c.Run.DeadlineAt.Add(5*time.Minute))
		return e
	})
}
