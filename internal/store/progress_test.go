package store

import (
	"context"
	"testing"

	"vidlens/internal/domain"
)

func TestRepairKeepsRunWithCompletedASRProgress(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	accepted, err := s.CreateRequest(ctx, owner, "asr-progress", "https://www.bilibili.com/video/BV1xx411c7mD", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Request(ctx, owner, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "transcriber")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err = s.BeginOperation(ctx, *claim, "asr:1440000:1500000", "asr", map[string]int{"start_ms": 1440000, "end_ms": 1500000}, true); err != nil {
		t.Fatal(err)
	}
	// Simulate five minutes since the visible transcription phase began. The
	// worker is healthy and has just received another completed audio window.
	if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishOperation(ctx, *claim, "asr:1440000:1500000", []domain.Segment{{StartMS: 1440000, EndMS: 1500000, Text: "completed audio", Origin: "asr"}}, "", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Repair(ctx, claim.Run.ID, 1, "active", nil); err != nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("healthy ASR was terminated despite a completed chunk: status=%s failure=%+v", run.Status, run.Failure)
	}
}

func TestRepairDoesNotCountHeartbeatSubmissionOrReplayAsProgress(t *testing.T) {
	for _, activity := range []string{"heartbeat", "submission", "completed_replay"} {
		t.Run(activity, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			owner := session(t, s)
			accepted, err := s.CreateRequest(ctx, owner, activity, "https://www.bilibili.com/video/BV1xx411c7mD", "", false)
			if err != nil {
				t.Fatal(err)
			}
			req, err := s.Request(ctx, owner, accepted.ID)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := s.Claim(ctx, *req.ActiveRunID, 1, "transcriber")
			if err != nil || claim == nil {
				t.Fatalf("claim: %v", err)
			}
			args := map[string]int{"start_ms": 0, "end_ms": 60000}
			if activity == "completed_replay" {
				if _, err = s.BeginOperation(ctx, *claim, "asr:0:60000", "asr", args, true); err != nil {
					t.Fatal(err)
				}
				if err = s.FinishOperation(ctx, *claim, "asr:0:60000", []domain.Segment{}, "", nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.Pool.Exec(ctx, "UPDATE job_runs SET updated_at=now()-interval '6 minutes' WHERE id=$1", claim.Run.ID); err != nil {
				t.Fatal(err)
			}
			switch activity {
			case "heartbeat":
				err = s.Heartbeat(ctx, *claim)
			case "submission":
				_, err = s.BeginOperation(ctx, *claim, "asr:0:60000", "asr", args, true)
			case "completed_replay":
				err = s.FinishOperation(ctx, *claim, "asr:0:60000", []domain.Segment{}, "", nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Repair(ctx, claim.Run.ID, 1, "active", nil); err != nil {
				t.Fatal(err)
			}
			run, err := s.Run(ctx, claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != "failed" || run.Failure == nil || run.Failure.Message != "处理长时间没有业务进展。" {
				t.Fatalf("%s hid a stalled run: status=%s failure=%+v", activity, run.Status, run.Failure)
			}
		})
	}
}
