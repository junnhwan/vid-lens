package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestDraftRevisionReplayConflictAndCancellation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	req, c := prepared(t, s, owner)
	first, err := s.SaveArticleDraft(ctx, c, "one", map[string]string{"id": "one"}, 0, map[string]any{"sections": map[string]string{"one": "preserved"}})
	if err != nil || first.Version != 1 {
		t.Fatalf("first draft %+v %v", first, err)
	}
	second, err := s.SaveArticleDraft(ctx, c, "two", map[string]string{"id": "two"}, 1, map[string]any{"sections": map[string]string{"one": "preserved", "two": "new"}})
	if err != nil || second.Version != 2 {
		t.Fatalf("second draft %+v %v", second, err)
	}
	replay, err := s.SaveArticleDraft(ctx, c, "one", map[string]string{"id": "one"}, 0, map[string]any{"ignored": "replay"})
	if err != nil || replay.Version != 1 {
		t.Fatalf("replay lost original revision %+v %v", replay, err)
	}
	if _, err = s.SaveArticleDraft(ctx, c, "stale", map[string]string{}, 1, map[string]string{}); err == nil {
		t.Fatal("stale revision overwrote chapter")
	}
	if _, err = s.SaveArticleDraft(ctx, c, "one", map[string]string{"id": "different"}, 0, map[string]string{}); err == nil {
		t.Fatal("same operation accepted different arguments")
	}
	if err = s.RecordCandidate(ctx, c, "bad", "save_article_sections", `{"sections":[]}, {"extra":true}`, "invalid", map[string]string{"code": "json_syntax"}); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordCandidate(ctx, c, "bad", "save_article_sections", `{"sections":[]}, {"extra":true}`, "invalid", map[string]string{"code": "json_syntax", "extra": "later validation detail"}); err != nil {
		t.Fatalf("candidate replay depended on mutable error detail: %v", err)
	}
	if _, err = s.Operation(ctx, c.Run.ID, "candidate"); err == nil {
		t.Fatal("invalid draft was placed in publish slot")
	}
	if err = s.CancelRequest(ctx, owner, req.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveArticleDraft(ctx, c, "late", nil, 2, map[string]string{}); !errors.Is(err, domain.ErrFenced) {
		t.Fatalf("cancellation did not fence draft: %v", err)
	}
	saved, err := s.ArticleDraft(ctx, c.Run.ID)
	if err != nil || saved.Version != 2 || draftJSONEqual(saved.Data, second.Data) == false {
		t.Fatalf("failed write changed draft: %+v %v", saved, err)
	}
}

func TestExplicitRetryKeepsDraftAndEvidenceUnderNewRun(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	req, c := prepared(t, s, owner)
	frame := domain.Asset{ID: domain.ID(), SnapshotID: req.SnapshotID, SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "frame", StorageKey: domain.ID() + ".jpg", SHA256: "frame", MIME: "image/jpeg", Bytes: 42, TimeMS: domain.Ptr(int64(200)), ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err := s.AddAsset(ctx, c, frame); err != nil {
		t.Fatal(err)
	}
	cp := map[string]any{"schema_version": 1, "layout_version": "source-context-v3", "protocol": domain.ArticleDraftProtocol, "ready": true, "inspections": map[string][]string{"unobserved": {frame.ID}}, "calls": 8, "repairs": 1,
		"evidence_review_issue": map[string]any{"candidate_hash": "ancestor-candidate", "code": "semantic_evidence"}}
	if err := s.Checkpoint(ctx, c, cp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveArticleDraft(ctx, c, "first", nil, 0, map[string]any{"title": "preserved", "sections": map[string]string{"one": "accepted"}}); err != nil {
		t.Fatal(err)
	}
	obs := map[string]any{"inspection_id": "overview", "frame_ids": []string{}, "visible_facts": []string{"saved fact"}, "status": "supported"}
	if err := s.RecordObservation(ctx, c, "obs", "overview", obs); err != nil {
		t.Fatal(err)
	}
	if err := s.Question(ctx, c, "q", map[string]any{"id": "q", "critical": true, "status": "supported"}); err != nil {
		t.Fatal(err)
	}
	unfinished := domain.Step{ID: "unfinished", Title: "尚未完成的动作", Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
	if _, err := s.Step(ctx, c, unfinished); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fail(ctx, c, domain.Failure{Code: "MODEL_RESULT_INVALID", Message: "invalid batch", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRequest(ctx, owner, domain.ID(), "", req.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Request(ctx, owner, retry.ID)
	if err != nil || next.ActiveRunID == nil || *next.ActiveRunID == c.Run.ID {
		t.Fatalf("retry did not create separate run: %+v %v", next, err)
	}
	draft, err := s.ArticleDraft(ctx, *next.ActiveRunID)
	if err != nil || draft.Version != 1 {
		t.Fatalf("draft not reused %+v %v", draft, err)
	}
	observations, err := s.Observations(ctx, *next.ActiveRunID)
	if err != nil || len(observations) != 1 {
		t.Fatalf("evidence lost %d %v", len(observations), err)
	}
	questions, err := s.Questions(ctx, *next.ActiveRunID)
	if err != nil || len(questions) != 1 {
		t.Fatalf("question scope lost %d %v", len(questions), err)
	}
	run, err := s.Run(ctx, *next.ActiveRunID)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]any
	if err = json.Unmarshal(run.Checkpoint, &restored); err != nil {
		t.Fatal(err)
	}
	if restored["draft_resume"] != true || restored["calls"] != float64(0) {
		t.Fatalf("explicit retry identity not reset: %s", run.Checkpoint)
	}
	if restored["evidence_review_issue"] != nil {
		t.Fatal("a new run inherited its ancestor's candidate-bound review", restored["evidence_review_issue"])
	}
	claim, err := s.Claim(ctx, *next.ActiveRunID, 1, "resumed")
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	if _, err = s.InternalFrame(ctx, *claim, frame.ID); err != nil {
		t.Fatalf("unobserved frame was not pinned for resume: %v", err)
	}
	old, err := s.Run(ctx, c.Run.ID)
	if err != nil || old.Status != "failed" {
		t.Fatal("old failure was rewritten")
	}
	var ancestor map[string]any
	if err = json.Unmarshal(old.Checkpoint, &ancestor); err != nil || ancestor["evidence_review_issue"] == nil {
		t.Fatal("clearing a new run's review erased its ancestor's failure evidence", err)
	}
	view, pos, err := s.RequestView(ctx, owner, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempts := view["activity"].(map[string]any)["attempts"].([]map[string]any)
	if len(attempts) != 2 || attempts[0]["saved_sections"] != 1 || attempts[0]["current"] != false || attempts[1]["inherited_sections"] != 1 || attempts[1]["current"] != true || pos.RunID == nil || *pos.RunID != claim.Run.ID {
		t.Fatalf("retry ledger or current stream identity wrong: %+v", attempts)
	}
	if _, _, err = s.RequestView(ctx, session(t, s), next.ID); err == nil {
		t.Fatal("retry history leaked to another visitor")
	}
	page, err := s.ActivityPage(ctx, owner, "request", req.ID, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range page["items"].([]domain.Step) {
		if step.ID == unfinished.ID {
			found = true
			if step.Status != "failed" {
				t.Fatal("failed ancestor rendered active", step)
			}
		}
	}
	if !found {
		t.Fatal("earlier action not retained")
	}
}

func draftJSONEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return Hash(x) == Hash(y)
}
