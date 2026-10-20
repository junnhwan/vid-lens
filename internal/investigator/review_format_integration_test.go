package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestInvalidReviewerResponseDoesNotBecomeAnAuthorCorrection(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 16
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		args := `{"candidate_hash":"invalid","verdict":"supported","issues":[],"unexpected":true}`
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "malformed-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": args}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(context.Background(), map[string]any{"title": "合成来源", "sections": []domain.Section{{ID: "one", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源事实。"}}, CitationIDs: []string{"t1"}}}}}}, catalog)
	var authorCorrection *candidateError
	var problem *domain.Problem
	if errors.As(err, &authorCorrection) || !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" {
		t.Fatalf("malformed reviewer response became author content feedback: %v", err)
	}
	if calls.Load() != 2 || s.cp.Repairs != 0 || s.pendingEvidenceReview() != nil {
		t.Fatal("review format recovery changed writer quota or exceeded its two-call allowance", calls.Load(), s.cp.Repairs)
	}
}
