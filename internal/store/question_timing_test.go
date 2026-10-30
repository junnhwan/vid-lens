package store

import (
	"context"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestQuestionTimingAdmissionClaimAndRetryUseTheSameFrozenRound(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		s, owner, _, conversation := conversationFixture(t)
		ctx := context.Background()
		accepted, err := s.CreateTurn(ctx, owner, conversation.ID, "timing", domain.QuestionInput{Question: "合成时间预算问题"}, "")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.Turn(ctx, owner, conversation.ID, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.Run(ctx, turn.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.DeadlineAt.Sub(run.CreatedAt) < 24*time.Minute || run.DeadlineAt.Sub(run.CreatedAt) > 26*time.Minute {
			t.Fatal("admission still has the old wall deadline")
		}
		if legacy {
			if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET checkpoint=checkpoint-'question_timing' WHERE id=$1", run.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET active_ms=400000 WHERE id=$1", run.ID); err != nil {
			t.Fatal(err)
		}
		claim, err := s.Claim(ctx, run.ID, run.DispatchGeneration, "timing-fixture")
		if err != nil {
			t.Fatal(err)
		}
		if legacy {
			if claim != nil {
				t.Fatal("old question budget silently expanded")
			}
			continue
		}
		if claim == nil {
			t.Fatal("new question stopped at old active budget")
		}
		p, err := s.RunProfile(ctx, claim.Run)
		if err != nil || p.ActiveBudget("qa.answer") != 20*time.Minute {
			t.Fatal("worker profile is stale", err)
		}
		retry, err := s.Fail(ctx, *claim, domain.Failure{Code: "PROVIDER_UNAVAILABLE", CanRetry: true}, true, time.Millisecond)
		if err != nil || !retry {
			t.Fatal("retry still uses old budget", retry, err)
		}
		again, err := s.Run(ctx, run.ID)
		if err != nil || Hash(again.Checkpoint) != Hash(run.Checkpoint) || !again.DeadlineAt.Equal(run.DeadlineAt) {
			t.Fatal("retry replaced frozen timing", err)
		}
	}
}
