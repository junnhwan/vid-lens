package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestBoundedProgressDoesNotCountAlternatingSavedDraftsAsNewWork(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	for i, text := range []string{"改写后的开始事实。", "开始事实。", "改写后的开始事实。"} {
		body := map[string]any{"version": 2 + i, "sections": []any{map[string]any{"id": "start", "blocks": []any{map[string]any{"type": "paragraph", "origin": "video", "text": text, "citation_ids": []string{"t1"}}}}}}
		result, err := planCall(s, fmt.Sprint("alternate-", i), "save_article_sections", body)
		if err != nil || result["error"] != nil {
			t.Fatal(result, err)
		}
		if s.cp.Progress.Stalled != i {
			t.Fatalf("returning to a previously saved draft reset progress: iteration=%d stalled=%d want=%d", i, s.cp.Progress.Stalled, i)
		}
	}
}

func TestBoundedLoopRecoverySurvivesReplayAndStopsRepeatedFailure(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("bad-tool-", i)), "imagined_patch", `{"activity_title":"retry"}`); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := s.readiness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovery, ok := ready["loop_recovery"].(map[string]any); !ok || recovery["tool"] != "imagined_patch" || !strings.Contains(fmt.Sprint(recovery["next_action"]), "save_article_sections") {
		t.Fatalf("repeated failure has no executable correction: %s", domain.JSON(ready))
	}
	var raw []byte
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT checkpoint FROM job_runs WHERE id=$1", s.claim.Run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &s.cp); err != nil {
		t.Fatal(err)
	}
	// Replaying the last completed action must not spend another correction.
	if _, err = s.unknownTool(context.WithValue(ctx, callIDKey{}, "bad-tool-1"), "imagined_patch", `{"activity_title":"retry"}`); err != nil {
		t.Fatal(err)
	}
	if s.cp.Progress.Stalled != 2 {
		t.Fatal("replayed feedback advanced loop state", s.cp.Progress)
	}
	// The next model response has consumed the directed recovery. More tool
	// calls in the original response alone must not spend that opportunity.
	s.cp.Calls++
	if _, err = s.unknownTool(context.WithValue(ctx, callIDKey{}, "bad-tool-2"), "imagined_patch", `{"activity_title":"different title"}`); err != nil {
		t.Fatal(err)
	}
	_, _, err = (&middleware{s: s}).BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{}, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "AGENT_NO_PROGRESS" || !strings.Contains(problem.Detail, "imagined_patch") {
		t.Fatalf("same failure after correction did not stop with its concrete cause: %v", err)
	}
}

func TestBoundedLoopRecoveryGivesOneModelTurnAfterARejectedBatch(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("batch-error-", i)), "imagined_patch", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("fixture")}}
	if _, _, err := (&middleware{s: s}).BeforeModelRewriteState(ctx, state, nil); err != nil {
		t.Fatalf("batch failures stopped before the model could consume its correction: %v", err)
	}
	s.cp.Calls++
	if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, "after-batch-correction"), "imagined_patch", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&middleware{s: s}).BeforeModelRewriteState(ctx, state, nil); err == nil {
		t.Fatal("same batch failure repeated after the correction did not stop")
	}
}

func TestBoundedLoopRecoveryAllowsAChangedAttemptAfterNewSourceInformation(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("before-evidence-", i)), "imagined_patch", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := planCall(s, "new-chapter-information", "read_article_sections", map[string]any{"section_ids": []string{"start"}}); err != nil || result["error"] != nil {
		t.Fatal(result, err)
	}
	if s.cp.Progress.Stalled != 0 || s.cp.Progress.Recovery != nil {
		t.Fatal("newly delivered repair material did not clear the stale correction", s.cp.Progress)
	}
	s.cp.Calls++
	if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, "after-new-information"), "imagined_patch", `{}`); err != nil {
		t.Fatal(err)
	}
	if s.cp.Progress.Recovery != nil || s.cp.Progress.Stalled != 1 {
		t.Fatal("old failure signature killed a new attempt after information gain", s.cp.Progress)
	}
}

func TestBoundedLoopCorrectionCannotExtendPhysicalBudgets(t *testing.T) {
	for _, boundary := range []string{"tools", "model_calls"} {
		t.Run(boundary, func(t *testing.T) {
			s := &session{profile: config.DefaultProfile(), cp: checkpoint{HarnessRevision: 16, ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV2}, Progress: progressState{Recovery: &loopRecovery{Attempts: 3, NudgeCall: 0}}}}
			if boundary == "tools" {
				s.cp.Progress.Total = 4 * s.profile.MaxIterations
			} else {
				s.cp.Calls = s.profile.MaxIterations
				s.cp.Progress.Recovery.NudgeCall = s.cp.Calls
			}
			_, _, err := (&middleware{s: s}).BeforeModelRewriteState(context.Background(), &adk.ChatModelAgentState{}, nil)
			var problem *domain.Problem
			if !errors.As(err, &problem) || problem.Code != "RESOURCE_LIMIT_EXCEEDED" {
				t.Fatalf("recovery extended %s boundary: %v", boundary, err)
			}
		})
	}
}

func TestBoundedLoopCorrectionCannotRenewStallAllowanceWithNewFailures(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for round := 0; round < 3; round++ {
		for i := 0; i < 2; i++ {
			id := fmt.Sprintf("new-failure-%d-%d", round, i)
			if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, id), fmt.Sprintf("imagined_%d", round), `{}`); err != nil {
				t.Fatal(err)
			}
		}
		if round < 2 {
			s.cp.Calls++
		}
	}
	_, _, err := (&middleware{s: s}).BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{}, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "AGENT_NO_PROGRESS" {
		t.Fatalf("new failure signatures renewed the exhausted stall allowance: stalled=%d err=%v", s.cp.Progress.Stalled, err)
	}
}
