package store

import (
	"context"
	"testing"

	"vidlens/internal/domain"
)

func TestImmutableOperationReplayChecksArgumentsAndResult(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, claim := prepared(t, s, session(t, s))
	value := map[string]any{"report": map[string]any{"complete": true, "topics": []string{"synthetic"}}, "obligations": []string{"q1"}}
	if err := s.CompleteImmutableOperation(ctx, claim, "reference-receipt", "article_loop_review", value); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.Pool.QueryRow(ctx, "SELECT updated_at::text FROM job_runs WHERE id=$1", claim.Run.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImmutableOperation(ctx, claim, "reference-receipt", "article_loop_review", value); err != nil {
		t.Fatal("identical jsonb replay failed", err)
	}
	var after string
	_ = s.Pool.QueryRow(ctx, "SELECT updated_at::text FROM job_runs WHERE id=$1", claim.Run.ID).Scan(&after)
	if before != after {
		t.Fatal("replay refreshed business progress clock")
	}
	value["obligations"] = []string{}
	if s.CompleteImmutableOperation(ctx, claim, "reference-receipt", "article_loop_review", value) == nil {
		t.Fatal("completed key hid conflicting obligations")
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE run_operations SET result=$3 WHERE run_id=$1 AND operation_key=$2", claim.Run.ID, "reference-receipt", domain.JSON(value)); err != nil {
		t.Fatal(err)
	}
	if s.CompleteImmutableOperation(ctx, claim, "reference-receipt", "article_loop_review", value) == nil {
		t.Fatal("result tampering ignored mismatched request hash")
	}
}
