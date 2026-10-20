package store

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestNewExecutionAndExplicitRetryHaveSeparateFrozenContracts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	req, claim := prepared(t, s, owner)
	decode := func(raw json.RawMessage) map[string]any {
		t.Helper()
		var cp map[string]any
		if err := json.Unmarshal(raw, &cp); err != nil {
			t.Fatal(err)
		}
		return cp
	}
	first := decode(claim.Run.Checkpoint)
	contract, ok := first["execution_contract"].(map[string]any)
	if !ok || contract["version"] != domain.CurrentExecutionContract || contract["run_id"] != claim.Run.ID {
		t.Fatalf("new article execution did not freeze its own contract: %s", claim.Run.Checkpoint)
	}
	first["ready"], first["layout_version"], first["protocol"], first["harness_revision"] = true, "source-context-v3", domain.ArticleDraftProtocol, 14
	first["calls"], first["repairs"] = 8, 2
	first["messages"] = []map[string]string{{"role": "system", "content": "ancestor frozen messages"}}
	if err := s.Checkpoint(ctx, claim, first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveArticleDraft(ctx, claim, "saved", nil, 0, map[string]any{"title": "keep this draft", "sections": map[string]string{"one": "saved text"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fail(ctx, claim, domain.Failure{Code: "MODEL_RESULT_INVALID", Message: "fixture", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	before, err := s.Run(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, "retry-once", "", req.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateRequest(ctx, owner, "retry-once", "", req.ID, false)
	if err != nil || !again.Replayed || again.ID != retry.ID {
		t.Fatal("retry replay created a different execution", err)
	}
	next, err := s.Request(ctx, owner, retry.ID)
	if err != nil || next.ActiveRunID == nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, *next.ActiveRunID)
	if err != nil {
		t.Fatal(err)
	}
	cp := decode(run.Checkpoint)
	nextContract, ok := cp["execution_contract"].(map[string]any)
	if !ok || nextContract["version"] != domain.CurrentExecutionContract || nextContract["run_id"] != run.ID || run.ID == claim.Run.ID {
		t.Fatalf("explicit retry inherited another run's execution contract: %s", run.Checkpoint)
	}
	if cp["draft_resume"] != true || cp["calls"] != float64(0) || cp["repairs"] != float64(0) || cp["layout_version"] != nil || cp["harness_revision"] != nil || cp["protocol"] != nil {
		t.Fatalf("retry did not separate inherited artifacts from execution state: %s", run.Checkpoint)
	}
	if Hash(next.GenerationSpec) != Hash(req.GenerationSpec) {
		t.Fatal("new execution changed immutable user intent")
	}
	draft, err := s.ArticleDraft(ctx, run.ID)
	if err != nil || draft.Version != 1 {
		t.Fatal("new contract lost the inherited draft", err)
	}
	ancestor, err := s.Run(ctx, claim.Run.ID)
	if err != nil || Hash(ancestor.Checkpoint) != Hash(before.Checkpoint) {
		t.Fatal("retry rewrote the ancestor contract", err)
	}
}
