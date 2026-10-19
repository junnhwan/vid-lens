package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
	"vidlens/internal/domain"
)

func (s *Store) AcquireResource(ctx context.Context, c domain.Claim, resource, owner string, slots int) (bool, error) {
	ok := false
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		available, e := s.generationResourceAvailable(ctx, tx, resource)
		if e != nil || !available {
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
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
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
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		l, e := storageLedger(ctx, tx, c.Run.ID)
		if e != nil {
			return e
		}
		l.temp, l.images, l.available, l.total, l.stop = temp, images, available, total, stop
		if e := admitStorage(l); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO storage_reservations(run_id,temp_bytes,image_bytes,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(run_id) DO UPDATE SET temp_bytes=GREATEST(storage_reservations.temp_bytes,EXCLUDED.temp_bytes),image_bytes=GREATEST(storage_reservations.image_bytes,EXCLUDED.image_bytes)", c.Run.ID, temp, images, c.Run.DeadlineAt.Add(5*time.Minute))
		return e
	})
}
