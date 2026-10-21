package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestCurrentReviewKeepsAllFindingsWithinBoundedFieldGroups(t *testing.T) {
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var in reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		payload := reviewWirePayload(t, in)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
		for _, unit := range units {
			count := 1
			if unit.ID == "u1" {
				count = 4
			}
			if unit.ID == "u2" {
				count = 3
			}
			issues := []reviewUnitIssue{}
			for _, quote := range []string{"来源事实0", "术语A", "术语B", "术语C"}[:count] {
				issues = append(issues, reviewUnitIssue{Claim: quote, Reason: "本项引用与实际原文不对应。", CitationIDs: []string{"t1"}})
			}
			report.Results = append(report.Results, reviewUnitResult{UnitID: unit.ID, Verdict: "contradicted", Issues: issues})
		}
		writeReviewWireArgs(w, reviewUnitsTool, report)
	})
	s.cp.ExecutionContract.Version = domain.CurrentExecutionContract
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blocks := []any{}
	for i := 0; i < 14; i++ {
		blocks = append(blocks, map[string]any{"text": fmt.Sprint("来源事实0；术语A；术语B；术语C，第", i, "项"), "citation_ids": []string{"t1"}})
	}
	err = s.reviewContent(context.Background(), map[string]any{"blocks": blocks}, catalog)
	var correction *candidateError
	if !errors.As(err, &correction) {
		t.Fatal(err)
	}
	issues, _ := s.pendingEvidenceReview()["issues"].([]map[string]any)
	paths := map[any]bool{}
	for _, issue := range issues {
		paths[issue["path"]] = true
	}
	if len(issues) != 19 || len(paths) != 14 {
		t.Fatalf("a raw issue-count cutoff discarded findings for still-bounded fields: issues=%d fields=%d", len(issues), len(paths))
	}
}

func TestCurrentReviewFieldGroupingKeepsBoundsAndLegacyCeiling(t *testing.T) {
	issues := []map[string]any{}
	for field := 0; field < 20; field++ {
		for finding := 0; finding < 5; finding++ {
			issues = append(issues, map[string]any{"path": fmt.Sprintf("/blocks/%d/text", field), "finding": finding})
		}
	}
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.CurrentExecutionContract}}}
	selected := s.reviewCorrectionIssues(issues)
	paths := map[string]int{}
	for _, issue := range selected {
		paths[issue["path"].(string)]++
		if issue["finding"] == 4 {
			t.Fatal("the per-field feedback bound was exceeded")
		}
	}
	if len(selected) != 64 || len(paths) != 16 {
		t.Fatalf("unbounded repair feedback: %d issues, %d fields", len(selected), len(paths))
	}
	for path, count := range paths {
		if count != 4 {
			t.Fatalf("field %s has %d findings", path, count)
		}
	}
	s.cp.ExecutionContract.Version = domain.ExecutionContractV8
	if legacy := s.reviewCorrectionIssues(issues); len(legacy) != 16 || legacy[15]["path"] != "/blocks/3/text" {
		t.Fatal("an existing execution silently adopted grouped feedback")
	}
}

func TestCurrentReviewNormalizesOnlyEmptyAnnotationsAndExactContextEcho(t *testing.T) {
	for _, fault := range []string{"null_note", "exact_context", "root_echo", "finding_with_echo", "wrong_context", "invented_semantics", "missing_coverage", "missing_issues", "missing_identity", "duplicate_identity", "conflicting_verdict"} {
		t.Run(fault, func(t *testing.T) {
			var calls atomic.Int32
			s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				_ = json.NewDecoder(r.Body).Decode(&in)
				payload := reviewWirePayload(t, in)
				var id string
				var units []reviewUnit
				_ = json.Unmarshal(payload["request_id"], &id)
				_ = json.Unmarshal(payload["units"], &units)
				results := []any{}
				for _, unit := range units {
					result := map[string]any{"unit_id": unit.ID, "verdict": "supported", "issues": []any{}}
					switch fault {
					case "null_note":
						result["issues_note"] = nil
					case "exact_context":
						result["section_title"] = unit.Context
					case "finding_with_echo":
						result["section_title"], result["issues_note"] = unit.Context, nil
						if unit.ID == "u2" {
							result["verdict"] = "contradicted"
							result["issues"] = []any{map[string]any{"claim": "来源事实0", "reason": "当前引用与这段原文不对应。", "citation_ids": []string{"t1"}}}
						}
					case "wrong_context":
						result["section_title"] = "另一个单元的标题"
					case "invented_semantics":
						result["approved_anyway"] = true
					case "missing_issues":
						delete(result, "issues")
					case "missing_identity":
						delete(result, "unit_id")
						result["id"] = unit.ID
					case "duplicate_identity":
						result["unit_id"] = "u1"
					case "conflicting_verdict":
						result["issues"] = []any{map[string]any{"claim": "来源事实0", "reason": "原文不对应。", "citation_ids": []string{"t1"}}}
					}
					if fault != "missing_coverage" {
						results = append(results, result)
					}
				}
				report := map[string]any{"request_id": id, "results": results}
				if fault == "root_echo" {
					report["review_mode"] = "source_text"
					report["units"] = units
				}
				writeReviewWireArgs(w, reviewUnitsTool, report)
			})
			s.cp.ExecutionContract.Version = domain.CurrentExecutionContract
			ctx := context.Background()
			catalog, err := s.citationCatalog(ctx)
			if err != nil {
				t.Fatal(err)
			}
			candidate := map[string]any{"sections": []any{map[string]any{"title": "共享标题", "blocks": []any{map[string]any{"text": "来源事实0", "citation_ids": []string{"t1"}}}}}}
			err = s.reviewContent(ctx, candidate, catalog)
			accepted := fault == "null_note" || fault == "exact_context" || fault == "root_echo"
			if accepted {
				if err != nil || calls.Load() != 1 {
					t.Fatalf("empty annotation or exact server context discarded a complete report: %v sends=%d", err, calls.Load())
				}
				if err = s.requirePublicationReview(ctx, candidate, catalog); err != nil {
					t.Fatal("canonical coverage was not persisted", err)
				}
				var normalized, raw int
				if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FILTER (WHERE kind='review_batch' AND jsonb_array_length(COALESCE(result->'ignored_context_fields','[]'::jsonb))>0),count(*) FILTER (WHERE kind='model' AND result::text LIKE $2) FROM run_operations WHERE run_id=$1", s.claim.Run.ID, "%"+map[string]string{"null_note": "issues_note", "exact_context": "section_title", "root_echo": "review_mode"}[fault]+"%").Scan(&normalized, &raw); err != nil || normalized != 1 || raw != 1 {
					t.Fatal("normalization was unaudited or rewrote raw provider data", normalized, raw, err)
				}
			} else if fault == "finding_with_echo" {
				var correction *candidateError
				if !errors.As(err, &correction) || calls.Load() != 1 || s.pendingEvidenceReview() == nil {
					t.Fatal("normalization discarded a real finding or treated it as approval", err)
				}
				if s.requirePublicationReview(ctx, candidate, catalog) == nil {
					t.Fatal("contradicted evidence became publishable after context projection")
				}
			} else {
				var problem *domain.Problem
				if !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" || calls.Load() != 2 || s.cp.Repairs != 0 {
					t.Fatalf("unsafe, incomplete or conflicting result became approval: %v sends=%d", err, calls.Load())
				}
			}
		})
	}
}

func TestReviewV8KeepsItsFrozenStrictResponseContract(t *testing.T) {
	var calls atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		payload := reviewWirePayload(t, in)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		results := []any{}
		for _, unit := range units {
			results = append(results, map[string]any{"unit_id": unit.ID, "verdict": "supported", "issues": []any{}, "issues_note": nil})
		}
		writeReviewWireArgs(w, reviewUnitsTool, map[string]any{"request_id": id, "results": results})
	})
	s.cp.ExecutionContract.Version = domain.ExecutionContractV8
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(context.Background(), map[string]any{"summary": "来源事实0"}, catalog)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" || calls.Load() != 2 {
		t.Fatal("existing V8 run silently adopted a new parser", err, calls.Load())
	}
}
