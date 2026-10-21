package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func TestExplicitRetryKeepsSourceOnlyCoverageAndPinsUndeliveredOverview(t *testing.T) {
	for _, version := range []string{domain.ExecutionContractV4, domain.ExecutionContractV5, domain.ExecutionContractV6, domain.ExecutionContractV7} {
		t.Run(version, func(t *testing.T) { testSourceOnlyCoverageRetry(t, version) })
	}
}

func testSourceOnlyCoverageRetry(t *testing.T, version string) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	req, claim := prepared(t, s, owner)
	frame := domain.Asset{ID: domain.ID(), SnapshotID: req.SnapshotID, SourceID: claim.Run.SourceID, RunID: claim.Run.ID, Kind: "frame", StorageKey: domain.ID() + ".jpg", SHA256: "pending-fixture", MIME: "image/jpeg", Bytes: 42, TimeMS: domain.Ptr(int64(200)), ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err := s.AddAsset(ctx, claim, frame); err != nil {
		t.Fatal(err)
	}
	checkpoint := map[string]any{"ready": true, "schema_version": 1, "layout_version": "source-context-v3", "protocol": domain.ArticleDraftProtocol, "execution_contract": domain.ExecutionContract{Version: version, RunID: claim.Run.ID}, "source_index_protocol": "source-index-2026-10-20-v1", "source_notes": []any{map[string]any{"chunk_id": "frozen-chunk", "summary": "derived navigation only", "evidence_ids": []string{"t1"}}}, "pending_overview": []string{frame.ID}, "inspections": map[string]any{}}
	if err := s.Checkpoint(ctx, claim, checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fail(ctx, claim, domain.Failure{Code: "PROVIDER_UNAVAILABLE", Message: "fixture", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, domain.ID(), "", req.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owner, retry.ID)
	if err != nil || next.ActiveRunID == nil {
		t.Fatal(err)
	}
	run, err := s.Run(ctx, *next.ActiveRunID)
	if err != nil {
		t.Fatal(err)
	}
	var copied map[string]any
	if err = json.Unmarshal(run.Checkpoint, &copied); err != nil || copied["draft_resume"] != true || Hash(copied["source_notes"]) != Hash(checkpoint["source_notes"]) || Hash(copied["pending_overview"]) != Hash(checkpoint["pending_overview"]) {
		t.Fatal("source-only progress lost on explicit retry", err, string(run.Checkpoint))
	}
	var pinned bool
	if err = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=$1 AND owner_type='run' AND owner_id=$2 AND expires_at>now())", frame.ID, run.ID).Scan(&pinned); err != nil || !pinned {
		t.Fatal("undelivered overview was not pinned for the new claim", err)
	}
}
