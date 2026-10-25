package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopReferencePublicationChecksActualFrozenPacket(t *testing.T) {
	for _, tamper := range []string{"full_view_manifest", "originals", "primary_source_ids", "evidence_directory"} {
		t.Run(tamper, func(t *testing.T) {
			var s *session
			s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				if p["scope"] == nil {
					writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
					return
				}
				op, err := s.engine.Store.Operation(context.Background(), s.claim.Run.ID, "input:model:1")
				if err != nil {
					t.Error(err)
					return
				}
				var frozen frozenModelInput
				_ = json.Unmarshal(op.Result, &frozen)
				changed := false
				for _, m := range frozen.Messages {
					var fields map[string]json.RawMessage
					if json.Unmarshal([]byte(m.Content), &fields) != nil || fields[tamper] == nil {
						continue
					}
					if tamper == "full_view_manifest" {
						delete(fields, tamper)
					} else {
						fields[tamper] = json.RawMessage(`[]`)
					}
					m.Content = string(domain.JSON(fields))
					changed = true
				}
				if !changed {
					t.Errorf("test did not reach actual frozen %s", tamper)
				}
				if _, err = s.engine.Store.Pool.Exec(context.Background(), "UPDATE run_operations SET result=$3 WHERE run_id=$1 AND operation_key=$2", s.claim.Run.ID, op.Key, domain.JSON(frozen)); err != nil {
					t.Error(err)
				}
				writeReviewWireArgs(w, loopReviewTool, loopGoodReferenceReport(t, p))
			}, domain.ArticleConvergenceReviewV6)
			if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
				t.Fatal("forged material approved", tamper)
			}
			assertNoLoopPublication(t, s)
			_, draft, err := s.loadLoopDraft(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err = s.engine.Store.Publish(context.Background(), s.claim, draft.Article); err == nil {
				t.Fatal("direct Store.Publish bypassed frozen material verification")
			}
		})
	}
}
