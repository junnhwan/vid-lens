package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestJointReviewBindsTextPixelsAndPublicationCoverage(t *testing.T) {
	for _, fault := range []string{"approved", "missing_joint", "not_assessed", "old_protocol", "text_changed", "incomplete_response", "text_correction"} {
		t.Run(fault, func(t *testing.T) {
			var mixedCalls, calls atomic.Int32
			s, article := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				payload := reviewWirePayload(t, in)
				var mode string
				var units []reviewUnit
				_ = json.Unmarshal(payload["review_mode"], &mode)
				_ = json.Unmarshal(payload["units"], &units)
				for _, unit := range units {
					if strings.Contains(string(domain.JSON(unit.Value)), "联合证据局限") {
						mixedCalls.Add(1)
						body := string(domain.JSON(in))
						if mode != "multimodal" || strings.Count(body, "data:image/jpeg;") != 1 || !strings.Contains(body, "来源事实0") || !strings.Contains(body, "source_citation_ids=f1") || !strings.Contains(body, "execution_context") {
							t.Error("one atomic obligation did not receive both original text and owned pixels", mode)
						}
						if fault == "incomplete_response" || fault == "text_correction" {
							var id string
							_ = json.Unmarshal(payload["request_id"], &id)
							report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
							for _, supplied := range units {
								result := reviewUnitResult{UnitID: supplied.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
								if supplied.ID == unit.ID {
									result.Verdict = "not_assessed"
									if fault == "text_correction" {
										result.Verdict = "contradicted"
										result.Issues = []reviewUnitIssue{{Claim: "来源事实0", Reason: "原文与本句所归因的来源事实不同。", CitationIDs: []string{"t1"}}}
									}
								}
								report.Results = append(report.Results, result)
							}
							writeReviewWireArgs(w, reviewUnitsTool, report)
							return
						}
					}
				}
				reviewWireReply(t, w, in, false)
			})
			ctx := context.Background()
			s.cp.ExecutionContract.Version = domain.ExecutionContractV6
			catalog, err := s.citationCatalog(ctx)
			if err != nil {
				t.Fatal(err)
			}
			article.Citations["t1"] = catalog["t1"]
			article.Limitations = []domain.Limitation{{Code: "visual_unclear", Message: "联合证据局限：图片是黑色，原文提供来源事实0，不能据此确认具体页面。", CitationIDs: []string{"f1", "t1"}}}
			err = s.reviewArticle(ctx, article)
			if fault == "incomplete_response" || fault == "text_correction" {
				var correction *candidateError
				if errors.As(err, &correction) {
					err = correction.err
				}
				var problem *domain.Problem
				want := "REVIEW_COVERAGE_INCOMPLETE"
				if fault == "text_correction" {
					want = "MODEL_RESULT_INVALID"
				}
				if !errors.As(err, &problem) || problem.Code != want || s.cp.Repairs != 0 || (s.pendingEvidenceReview() != nil) != (fault == "text_correction") {
					t.Fatalf("joint review confused incomplete coverage with author correction: %v", err)
				}
				return
			}
			if err != nil || mixedCalls.Load() != 1 {
				t.Fatalf("mixed limitation never reached its complete review: calls=%d err=%v", mixedCalls.Load(), err)
			}
			before := calls.Load()
			switch fault {
			case "missing_joint":
				_, err = s.engine.Store.Pool.Exec(ctx, "DELETE FROM run_operations WHERE run_id=$1 AND kind='review_batch' AND result->>'mode'='multimodal'", s.claim.Run.ID)
			case "not_assessed":
				_, err = s.engine.Store.Pool.Exec(ctx, "UPDATE run_operations SET result=jsonb_set(result,'{report,results,0,verdict}','\"not_assessed\"') WHERE run_id=$1 AND kind='review_batch' AND result->>'mode'='multimodal'", s.claim.Run.ID)
			case "old_protocol":
				s.cp.ExecutionContract.Version = domain.ExecutionContractV5
			case "text_changed":
				s.snapshot.TextHash = "changed-source"
			}
			if err != nil {
				t.Fatal(err)
			}
			candidate := map[string]any{"title": article.Title, "sections": article.Sections, "limitations": article.Limitations}
			err = s.requirePublicationReview(ctx, candidate, article.Citations)
			var problem *domain.Problem
			if fault == "approved" && err != nil || fault != "approved" && (!errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE") {
				t.Fatalf("publication did not enforce exact joint obligations: %s %v", fault, err)
			}
			if calls.Load() != before {
				t.Fatal("publication guard spent another model call")
			}
			if fault == "approved" {
				if err = s.reviewArticle(ctx, article); err != nil || calls.Load() != before {
					t.Fatal("same-candidate recovery repeated completed review", err)
				}
			}
		})
	}
}

func TestCurrentReviewKeepsFindingsAndRoutesUnassessedUnitToEvidenceRepair(t *testing.T) {
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		payload := reviewWirePayload(t, in)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
		for i, unit := range units {
			result := reviewUnitResult{UnitID: unit.ID, Verdict: "not_assessed", Issues: []reviewUnitIssue{}}
			if i == 0 {
				result.Verdict = "contradicted"
				result.Issues = []reviewUnitIssue{{Claim: "来源事实0", Reason: "该引用与当前断言不对应。", CitationIDs: []string{"t1"}}}
			}
			report.Results = append(report.Results, result)
		}
		writeReviewWireArgs(w, reviewUnitsTool, report)
	})
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"summary": "来源事实0", "title": "待补读的另一项原文事实"}
	err = s.reviewContent(ctx, candidate, catalog)
	var correction *candidateError
	meta := s.pendingEvidenceReview()
	if !errors.As(err, &correction) || meta == nil {
		t.Fatalf("one unassessed unit discarded actionable findings and blocked evidence acquisition: %v", err)
	}
	issues, _ := meta["issues"].([]map[string]any)
	if len(issues) != 2 || issues[0]["code"] != "evidence_not_supported" || issues[1]["code"] != "review_not_assessed" || issues[1]["path"] != "/title" || s.cp.Repairs != 0 {
		t.Fatal("coverage gap became approval, a fabricated contradiction, or lost the real finding", meta)
	}
	if err = s.requirePublicationReview(ctx, candidate, catalog); err == nil {
		t.Fatal("an unassessed unit was allowed through publication")
	}
}
