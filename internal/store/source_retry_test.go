package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestAutomaticSourceRetryClosesPreviousAttempt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	accepted, err := s.CreateRequest(ctx, owner, "source-retry", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Request(ctx, owner, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "source-worker")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	for _, step := range []domain.Step{
		{ID: "subtitles", Phase: "prepare_source", Title: "正在检查可用的平台字幕", Status: "completed"},
		{ID: "audio", Phase: "prepare_source", Title: "正在获取转写所需音轨", Status: "running"},
		{ID: "video", Phase: "prepare_source", Title: "正在获取用于观察的完整视频", Status: "running"},
	} {
		step.StartedAt = domain.Ptr(time.Now().UTC())
		if _, err = s.Step(ctx, *claim, step); err != nil {
			t.Fatal(err)
		}
	}
	reason := "下载长时间没有字节进展。"
	retry, err := s.Fail(ctx, *claim, domain.Failure{Code: "SOURCE_UNAVAILABLE", Message: reason, CanRetry: true}, true, time.Second)
	if err != nil || !retry {
		t.Fatalf("retry: %v %v", retry, err)
	}
	check := func() {
		t.Helper()
		view, _, err := s.RequestView(ctx, owner, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		activity := view["activity"].(map[string]any)
		steps := activity["recent_steps"].([]domain.Step)
		explained := false
		for _, step := range steps {
			if step.ID == "audio" || step.ID == "video" {
				if step.Status != "failed" || step.FinishedAt == nil || step.Outcome == nil || step.Outcome.Message != reason {
					t.Errorf("ended download still appears active or lost its reason: %+v", step)
				}
			}
			if step.ID == "subtitles" && step.Status != "completed" {
				t.Error("completed subtitle check was changed")
			}
			if step.Kind == "retry" && strings.Contains(step.Title, "重试") && step.Outcome != nil && step.Outcome.Message == reason {
				explained = true
			}
		}
		if !explained {
			t.Error("automatic retry has no visible explanation")
		}
	}
	check()
	claim, err = s.Claim(ctx, claim.Run.ID, 1, "next-source-worker")
	if err != nil || claim == nil {
		t.Fatalf("second claim: %v", err)
	}
	if _, err = s.Step(ctx, *claim, domain.Step{ID: "next-video", Phase: "prepare_source", Title: "正在获取用于观察的完整视频", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestInterruptedSourceAttemptClosesParallelSteps(t *testing.T) {
	for _, mode := range []string{"shutdown", "lease_expired", "final_failure"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			owner := session(t, s)
			accepted, err := s.CreateRequest(ctx, owner, mode, "https://youtube.com/watch?v=abcdefghijk", "", false)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := s.Request(ctx, owner, accepted.ID)
			claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "source-worker")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			old := *claim
			if _, err = s.Step(ctx, old, domain.Step{ID: "video", Phase: "prepare_source", Status: "running"}); err != nil {
				t.Fatal(err)
			}
			wantStatus := "canceled"
			switch mode {
			case "shutdown":
				err = s.Suspend(ctx, old)
			case "lease_expired":
				_, err = s.Pool.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1", old.Run.ID)
				if err == nil {
					claim, err = s.Claim(ctx, old.Run.ID, 1, "replacement-worker")
					if err == nil && claim == nil {
						t.Fatal("replacement worker did not acquire source")
					}
				}
			case "final_failure":
				wantStatus = "failed"
				_, err = s.Fail(ctx, old, domain.Failure{Code: "SOURCE_UNAVAILABLE", Message: "来源暂不可用。"}, false, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			var step domain.Step
			if err = s.Pool.QueryRow(ctx, "SELECT step FROM run_events WHERE run_id=$1 AND step_id='video' ORDER BY seq DESC LIMIT 1", old.Run.ID).Scan(&step); err != nil {
				t.Fatal(err)
			}
			if step.Status != wantStatus || step.FinishedAt == nil || step.Outcome == nil {
				t.Fatalf("unfinished step survived %s: %+v", mode, step)
			}
			if _, err = s.Step(ctx, old, domain.Step{ID: "video", Status: "running"}); err != domain.ErrFenced {
				t.Fatalf("old worker revived its step after %s: %v", mode, err)
			}
		})
	}
}
