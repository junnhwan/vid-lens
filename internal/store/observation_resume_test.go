package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"vidlens/internal/domain"
)

func TestExplicitRetryReusesObservedFramesBeforeArticlePlan(t *testing.T) {
	for _, mode := range []string{"ready", "unavailable", "observation_only", "unavailable_observation_only", "protocol_changed"} {
		t.Run(mode, func(t *testing.T) {
			missing := mode == "unavailable" || mode == "unavailable_observation_only" || mode == "protocol_changed"
			s := testStore(t)
			ctx := context.Background()
			owner := session(t, s)
			req, c := prepared(t, s, owner)
			frame := domain.Asset{ID: domain.ID(), SnapshotID: req.SnapshotID, SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "frame", StorageKey: domain.ID() + ".jpg", SHA256: "observed", MIME: "image/jpeg", Bytes: 42, TimeMS: domain.Ptr(int64(200)), ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
			if err := s.AddAsset(ctx, c, frame); err != nil {
				t.Fatal(err)
			}
			cp := map[string]any{"schema_version": 1, "harness_revision": 3, "layout_version": "source-context-v3", "protocol": domain.ArticleDraftProtocol, "ready": true, "calls": 7, "repairs": 2, "draft_issue": map[string]any{"code": "plan_invalid"}, "overview_ids": []string{frame.ID}, "citation_frames": []string{frame.ID}, "inspections": map[string][]string{"overview": {frame.ID}}}
			if mode == "observation_only" || mode == "unavailable_observation_only" {
				cp["inspections"] = map[string][]string{}
			}
			if mode == "protocol_changed" {
				cp["protocol"] = "unknown-future-protocol"
			}
			if err := s.Checkpoint(ctx, c, cp); err != nil {
				t.Fatal(err)
			}
			obs := map[string]any{"inspection_id": "overview", "question_id": "q", "frame_ids": []string{frame.ID}, "visible_facts": []string{"actual saved observation"}, "status": "supported"}
			if err := s.RecordObservation(ctx, c, "obs", "overview", obs); err != nil {
				t.Fatal(err)
			}
			if err := s.Question(ctx, c, "q", map[string]any{"id": "q", "critical": true, "status": "supported"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.BeginOperation(ctx, c, "model:old", "model", nil, false); err != nil {
				t.Fatal(err)
			}
			if err := s.FinishOperation(ctx, c, "model:old", map[string]any{"private_old_response": true}, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Fail(ctx, c, domain.Failure{Code: "MODEL_RESULT_INVALID", Message: "plan rejected", CanRetry: true}, false, 0); err != nil {
				t.Fatal(err)
			}
			if mode == "unavailable" || mode == "unavailable_observation_only" {
				if _, err := s.Pool.Exec(ctx, "UPDATE media_assets SET state='unavailable' WHERE id=$1", frame.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.Run(ctx, c.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			a, err := s.CreateRequest(ctx, owner, domain.ID(), "", req.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.Request(ctx, owner, a.ID)
			if err != nil || next.ActiveRunID == nil {
				t.Fatal(err)
			}
			observations, err := s.Observations(ctx, *next.ActiveRunID)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if len(observations) != 0 {
					t.Fatal("missing pixels reused a historical observation")
				}
				return
			}
			if len(observations) != 1 {
				t.Fatalf("pre-plan observation was lost: %d", len(observations))
			}
			questions, err := s.Questions(ctx, *next.ActiveRunID)
			if err != nil || len(questions) != 1 {
				t.Fatal("observation question identity lost", err)
			}
			var oldCalls int
			if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='model'", *next.ActiveRunID).Scan(&oldCalls); err != nil || oldCalls != 0 {
				t.Fatal("historical model submissions copied into retry", err)
			}
			draft, err := s.ArticleDraft(ctx, *next.ActiveRunID)
			if err != nil || draft.Version != 0 {
				t.Fatal("a nonexistent article draft was fabricated", draft, err)
			}
			run, err := s.Run(ctx, *next.ActiveRunID)
			if err != nil {
				t.Fatal(err)
			}
			var restored map[string]any
			if err = json.Unmarshal(run.Checkpoint, &restored); err != nil {
				t.Fatal(err)
			}
			if restored["draft_resume"] != true || restored["draft_issue"] != nil || restored["calls"] != float64(0) {
				t.Fatalf("bad pre-plan resume: %s", run.Checkpoint)
			}
			claim, err := s.Claim(ctx, *next.ActiveRunID, 1, "resume")
			if err != nil || claim == nil {
				t.Fatal(err)
			}
			if _, err = s.InternalFrame(ctx, *claim, frame.ID); err != nil {
				t.Fatal("resumed observed frame is not pinned", err)
			}
			original, err := s.Run(ctx, c.Run.ID)
			if err != nil || original.Status != "failed" || Hash(original.Checkpoint) != Hash(before.Checkpoint) {
				t.Fatal("historical failure was changed")
			}
		})
	}
}
