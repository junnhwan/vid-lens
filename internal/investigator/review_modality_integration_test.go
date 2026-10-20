package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestReviewV3BindsScopePixelsAndCompletePublicationCoverage(t *testing.T) {
	for _, fault := range []string{"approved", "not_assessed", "missing_visual", "scope_changed", "fenced"} {
		t.Run(fault, func(t *testing.T) {
			var calls atomic.Int32
			s, article := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				for _, m := range in.Messages {
					var text string
					var source map[string]json.RawMessage
					if json.Unmarshal(m.Content, &text) == nil && json.Unmarshal([]byte(text), &source) == nil && source["transcript"] != nil && source["execution_context"] == nil {
						t.Error("V3 review omitted host-owned execution facts")
					}
				}
				reviewWireReply(t, w, in, false)
			})
			ctx := context.Background()
			s.cp.ExecutionContract.Version = domain.ExecutionContractV3
			if err := s.reviewArticle(ctx, article); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := s.engine.Store.Pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch fault {
			case "not_assessed":
				exec("UPDATE run_operations SET result=jsonb_set(result,'{report,results,0,verdict}','\"not_assessed\"') WHERE run_id=$1 AND kind='review_batch'", s.claim.Run.ID)
			case "missing_visual":
				exec("DELETE FROM run_operations WHERE run_id=$1 AND kind='review_batch' AND result->>'mode'='visual'", s.claim.Run.ID)
			case "scope_changed":
				s.cp.OverviewIDs, s.cp.CitationFrames, s.cp.Inspections = nil, nil, nil
			case "fenced":
				persistPublicationCandidate(t, s, article)
				exec("UPDATE job_runs SET execution_epoch=execution_epoch+1 WHERE id=$1", s.claim.Run.ID)
			}
			before := calls.Load()
			candidate := map[string]any{"title": article.Title, "sections": article.Sections, "limitations": article.Limitations}
			err := s.requirePublicationReview(ctx, candidate, article.Citations)
			if fault == "fenced" {
				err = s.engine.Run(ctx, s.claim, s.profile)
			}
			if fault == "approved" && err != nil {
				t.Fatal(err)
			}
			if fault != "approved" {
				var problem *domain.Problem
				if err == nil || fault != "fenced" && (!errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE") {
					t.Fatalf("stale or partial V3 review approved: %v", err)
				}
			}
			if calls.Load() != before {
				t.Fatal("publication gate silently started another review")
			}
		})
	}
}
