package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
)

// A private, provisional view; it never sets qa_turns.answer or completes a run.
func (s *Store) SaveQuestionPreview(ctx context.Context, c domain.Claim, a domain.Answer) error {
	if c.Run.Kind != "qa.answer" || a.Evidence == nil || a.Evidence.Review != "pending" {
		return domain.ErrFenced
	}
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fence(ctx, tx, c); err != nil {
			return err
		}
		key := "question-preview:" + Hash(a)
		tag, err := tx.Exec(ctx, `INSERT INTO run_operations(run_id,operation_key,kind,args_hash,status,result,completed_at) VALUES($1,$2,'question_preview',$3,'completed',$4,now()) ON CONFLICT(run_id,operation_key) DO NOTHING`, c.Run.ID, key, Hash(a), domain.JSON(a))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return event(ctx, tx, c.Run.ID, nil)
	})
}
