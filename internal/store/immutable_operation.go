package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// CompleteImmutableOperation atomically commits a receipt and all obligations
// it owns in one journal value. Identical replay is read-only; a completed key
// with different arguments OR output is a conflict, never an implicit success.
func (s *Store) CompleteImmutableOperation(ctx context.Context, c domain.Claim, key, kind string, value any) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		h := domain.ArticleJSONHash(value)
		inserted, err := tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,attempt_id,kind,args_hash,status,result,completed_at)
		 VALUES($1,$2,$3,$4,$5,'completed',$6,now()) ON CONFLICT DO NOTHING`, c.Run.ID, key, c.AttemptID, kind, h, domain.JSON(value))
		if err != nil {
			return err
		}
		if inserted.RowsAffected() == 0 {
			op, err := one[Operation](ctx, tx, "SELECT to_jsonb(o) FROM run_operations o WHERE run_id=$1 AND operation_key=$2 FOR UPDATE", c.Run.ID, key)
			if err != nil {
				return err
			}
			if op.Status != "completed" || op.Kind != kind || op.ArgsHash != h || domain.ArticleJSONHash(op.Result) != h {
				return errors.New("immutable operation replay conflict")
			}
			return nil
		}
		_, err = tx.Exec(ctx, "UPDATE job_runs SET updated_at=now() WHERE id=$1", c.Run.ID)
		return err
	})
}
