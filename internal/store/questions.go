package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

func (s *Store) Question(ctx context.Context, c domain.Claim, id string, data any) error {
	if id == "" {
		return errors.New("question id missing")
	}
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if e := fence(ctx, tx, c); e != nil {
			return e
		}
		key := c.Run.ID + ":" + id
		_, e := tx.Exec(ctx, "INSERT INTO evidence_questions(id,run_id,snapshot_id,data) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET data=EXCLUDED.data || jsonb_build_object('critical',COALESCE((evidence_questions.data->>'critical')::boolean,false) OR COALESCE((EXCLUDED.data->>'critical')::boolean,false))", key, c.Run.ID, c.Run.SnapshotID, domain.JSON(data))
		return e
	})
}
func (s *Store) Questions(ctx context.Context, run string) ([]json.RawMessage, error) {
	return many[json.RawMessage](ctx, s.Pool, "SELECT data FROM evidence_questions WHERE run_id=$1 ORDER BY id", run)
}
