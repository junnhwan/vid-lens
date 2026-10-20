package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"vidlens/internal/domain"
)

func TestBoundedReviewFindingsMustQuoteReadableContent(t *testing.T) {
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}}
	for _, tc := range []struct {
		name  string
		path  string
		value any
		claim string
	}{
		{"whitespace", "/summary", "A source statement.", " "},
		{"block_identity", "/blocks/0", map[string]any{"id": "block-source-ref", "content": []any{map[string]any{"text": "A source statement."}}}, "block-source-ref"},
		{"block_type", "/blocks/0", map[string]any{"type": "paragraph", "content": []any{map[string]any{"text": "A source statement."}}}, "paragraph"},
		{"citation_alias", "/blocks/0", map[string]any{"content": []any{map[string]any{"type": "citation", "citation_id": "t1"}}}, "t1"},
		{"frame_identity", "/blocks/0", map[string]any{"frame_ids": []any{"frame-identity"}, "caption": "A source statement."}, "frame-identity"},
		{"section_identity", "/limitations/0", map[string]any{"section_ids": []any{"section-identity"}, "message": "A source statement."}, "section-identity"},
		{"limitation_code", "/limitations/0", map[string]any{"code": "evidence_missing", "message": "A source statement."}, "evidence_missing"},
		{"answer_kind", "/kind", "source_fact", "source_fact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := reviewTask{Mode: "source_text", Units: []reviewUnit{{ID: "u1", Path: tc.path, Value: tc.value}}}
			report := reviewUnitReport{RequestID: "review", Results: []reviewUnitResult{{UnitID: "u1", Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: tc.claim, Reason: "The supplied source says something materially different.", CitationIDs: []string{"t1"}}}}}}
			if err := validateReviewUnits(report, "review", task, catalog); err == nil {
				t.Fatal("non-content quote was allowed to request an author correction")
			}
		})
	}
	for _, tc := range []struct {
		name  string
		value any
		claim string
	}{
		{"title", "A readable title.", "readable title"},
		{"paragraph", map[string]any{"content": []any{map[string]any{"text": "A source statement."}}}, "source statement"},
		{"code", map[string]any{"type": "code", "language": "go", "text": "return count + 1"}, "count + 1"},
		{"formula", map[string]any{"type": "math", "text": "E = mc^2"}, "mc^2"},
		{"table_header", map[string]any{"type": "table", "columns": []any{"Revenue (USD)"}}, "Revenue (USD)"},
		{"table_cell", map[string]any{"rows": []any{map[string]any{"cells": []any{"42 million"}}}}, "42 million"},
		{"figure_caption", map[string]any{"caption": "A caption describes the source frame."}, "source frame"},
		{"limitation", map[string]any{"code": "evidence_missing", "message": "The material is unknown."}, "material is unknown"},
		{"map_label", map[string]any{"nodes": []any{map[string]any{"id": "node-1", "label": "A source concept."}}}, "source concept"},
		{"identity_as_prose", map[string]any{"id": "block-01", "text": "The variable is called block-01."}, "block-01"},
	} {
		t.Run("readable_"+tc.name, func(t *testing.T) {
			task := reviewTask{Mode: "source_text", Units: []reviewUnit{{ID: "u1", Path: "/blocks/0", Value: tc.value}}}
			report := reviewUnitReport{RequestID: "review", Results: []reviewUnitResult{{UnitID: "u1", Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: tc.claim, Reason: "The supplied source says something materially different.", CitationIDs: []string{"t1"}}}}}}
			if err := validateReviewUnits(report, "review", task, catalog); err != nil {
				t.Fatal("readable content cannot be reviewed", err)
			}
		})
	}
}

func TestBoundedReviewMetadataFindingIsProtocolFailureWithoutAuthorRepair(t *testing.T) {
	calls := 0
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
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
		for _, unit := range units {
			report.Results = append(report.Results, reviewUnitResult{UnitID: unit.ID, Verdict: "contradicted", Issues: []reviewUnitIssue{{Claim: "internal-block-identity", Reason: "The source does not establish this identifier.", CitationIDs: []string{"t1"}}}})
		}
		writeReviewWireArgs(w, reviewUnitsTool, report)
	})
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"blocks": []any{map[string]any{"id": "internal-block-identity", "type": "paragraph", "content": []any{map[string]any{"text": "A source statement."}}}}}
	err = s.reviewContent(context.Background(), candidate, catalog)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" || calls != 2 || s.pendingEvidenceReview() != nil || s.cp.Repairs != 0 {
		t.Fatalf("metadata was treated as a content defect: calls=%d repairs=%d issue=%v err=%v", calls, s.cp.Repairs, s.pendingEvidenceReview(), err)
	}
	// A takeover must replay both known protocol failures rather than submit the
	// same invalid review again or turn it into an author repair.
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || json.Unmarshal(run.Checkpoint, &s.cp) != nil {
		t.Fatal("cannot reload the durable review checkpoint", err)
	}
	err = s.reviewContent(context.Background(), candidate, catalog)
	if !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" || calls != 2 || s.cp.Calls != 2 || s.pendingEvidenceReview() != nil || s.cp.Repairs != 0 {
		t.Fatalf("known invalid reviews were resent or charged after takeover: calls=%d checkpoint_calls=%d repairs=%d err=%v", calls, s.cp.Calls, s.cp.Repairs, err)
	}
}
