package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func frozenModelTest(t *testing.T, s *Store, c domain.Claim, key string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.BeginOperation(ctx, c, "input:"+key, "model_input", key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishOperation(ctx, c, "input:"+key, map[string]any{"messages": "frozen complete input"}, "", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.BeginModelSubmission(ctx, c, key, "unchanged", c.Run.ConfigID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModelRecoveryKeepsOneRequestFrozenInputAndFiniteSends(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r, c := prepared(t, s, session(t, s))
	frozenModelTest(t, s, c, "model:0")
	delay, yes, err := s.ScheduleModelRecovery(ctx, c, "model:0")
	if err != nil || !yes || delay != 30*time.Second {
		t.Fatal("automatic checkpoint recovery not scheduled", delay, yes, err)
	}
	if _, yes, err = s.ScheduleModelRecovery(ctx, c, "model:0"); err != nil || yes {
		t.Fatal("replay granted another recovery", yes, err)
	}
	diagnostic, err := s.RunDiagnostics(ctx, c.Run.ID)
	if err != nil || len(diagnostic["automatic_recoveries"].([]modelRecovery)) != 1 || strings.Contains(string(domain.JSON(diagnostic)), "frozen complete input") {
		t.Fatal("recovery audit missing or leaked model input", diagnostic, err)
	}
	if _, err = s.BeginModelSubmission(ctx, c, "model:0", "unchanged", c.Run.ConfigID); err == nil {
		t.Fatal("cooldown bypassed")
	}
	f := domain.Failure{Code: "PROVIDER_UNAVAILABLE", Message: "模型响应未完整结束", CanRetry: true}
	if retry, err := s.Fail(ctx, c, f, true, delay); err != nil || !retry {
		t.Fatal(retry, err)
	}
	view := execution(func() *domain.Run { v, _ := s.Run(ctx, c.Run.ID); return &v }()).(map[string]any)
	if view["retry_reason"] != f.Message || view["retry_at"] == nil || view["retry_attempt"] != 1 {
		t.Fatal("recovery has no reason, time or attempt", view)
	}
	if _, err = s.Claim(ctx, c.Run.ID, 1, "too-early"); err == nil {
		t.Fatal("early queue delivery spent an attempt")
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET retry_not_before=now()-interval '1 second' WHERE id=$1", c.Run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE run_operations SET result=jsonb_set(result,'{not_before}',to_jsonb(now()-interval '1 second')) WHERE run_id=$1 AND kind='model_recovery'", c.Run.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.Claim(ctx, c.Run.ID, 1, "after-cooldown")
	if err != nil || next == nil {
		t.Fatal(err)
	}
	if _, err = s.BeginModelSubmission(ctx, c, "model:0", "unchanged", c.Run.ConfigID); !errors.Is(err, domain.ErrFenced) {
		t.Fatal("old worker escaped fencing", err)
	}
	op, err := s.BeginModelSubmission(ctx, *next, "model:0", "unchanged", next.Run.ConfigID)
	if err != nil || op.Submissions != 4 {
		t.Fatal("reserved send missing", op, err)
	}
	if _, err = s.BeginModelSubmission(ctx, *next, "model:0", "unchanged", next.Run.ConfigID); err == nil {
		t.Fatal("recovery reset the original send ledger")
	}
	if _, yes, err = s.ScheduleModelRecovery(ctx, *next, "model:0"); err != nil || yes {
		t.Fatal("second recovery for same call", yes, err)
	}
	request, _ := s.Request(ctx, r.SessionID, r.ID)
	if request.ID != r.ID || *request.ActiveRunID != c.Run.ID || request.GenerationSpec.DetailPolicyRevision != r.GenerationSpec.DetailPolicyRevision {
		t.Fatal("recovery changed task identity or policy")
	}
	if err = s.CancelRequest(ctx, r.SessionID, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ScheduleModelRecovery(ctx, *next, "model:1"); !errors.Is(err, domain.ErrFenced) {
		t.Fatal("canceled request recovered", err)
	}
}

func TestModelRecoveryDoesNotUpgradeLegacyPolicyOrExceedRunLimit(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(map[bool]string{true: "legacy", false: "run_limit"}[legacy], func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			r, c := prepared(t, s, session(t, s))
			if legacy {
				if _, err := s.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=generation_spec #- '{detail_policy,provider_recovery_limit}' WHERE id=$1", r.ID); err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range []string{"model:0", "model:1", "model:2"} {
				frozenModelTest(t, s, c, key)
				_, reserved, err := s.ScheduleModelRecovery(ctx, c, key)
				if err != nil || reserved != (!legacy && key != "model:2") {
					t.Fatal(key, reserved, err)
				}
			}
		})
	}
}
