package investigator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestBoundedLoopRecoveryCannotRenewCorrectionByAlternatingFailures(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for turn := 0; turn < 2; turn++ {
		if turn == 1 {
			s.cp.Calls++ // The next model response has received the correction.
		}
		for _, name := range []string{"imagined_a", "imagined_b"} {
			for attempt := 0; attempt < 2; attempt++ {
				id := fmt.Sprintf("alternating-%d-%s-%d", turn, name, attempt)
				if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, id), name, `{}`); err != nil {
					t.Fatal(err)
				}
			}
		}
		if turn == 0 && !s.loopCorrectionPending() {
			t.Fatal("the original failing batch must leave one correction opportunity")
		}
	}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("fixture")}}
	_, _, err := (&middleware{s: s}).BeforeModelRewriteState(ctx, state, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "AGENT_NO_PROGRESS" {
		t.Fatalf("alternating known failures renewed the correction: recovery=%+v stalled=%d err=%v", s.cp.Progress.Recovery, s.cp.Progress.Stalled, err)
	}
}

func TestBoundedLoopRecoveryCannotHideExhaustedCorrectionBehindAnotherFailure(t *testing.T) {
	s := deterministicDraftSession(t)
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.ExecutionContractV2, RunID: s.claim.Run.ID}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("first-a-", i)), "imagined_a", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	s.cp.Calls++
	if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, "after-correction-a"), "imagined_a", `{}`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("new-b-", i)), "imagined_b", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.SystemMessage("fixture")}}
	_, _, err := (&middleware{s: s}).BeforeModelRewriteState(ctx, state, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "AGENT_NO_PROGRESS" || !strings.Contains(problem.Detail, "imagined_a") {
		t.Fatalf("a new failure hid the exhausted correction and its cause: recovery=%+v err=%v", s.cp.Progress.Recovery, err)
	}
}
