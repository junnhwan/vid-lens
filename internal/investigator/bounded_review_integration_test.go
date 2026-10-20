package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/media"
)

type reviewWireRequest struct {
	MaxTokens int `json:"max_tokens"`
	Tools     []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Messages []struct {
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func reviewWirePayload(t *testing.T, in reviewWireRequest) map[string]json.RawMessage {
	t.Helper()
	for _, m := range in.Messages {
		var text string
		var payload map[string]json.RawMessage
		if json.Unmarshal(m.Content, &text) == nil && json.Unmarshal([]byte(text), &payload) == nil && (payload["request_id"] != nil || payload["candidate_hash"] != nil) {
			return payload
		}
	}
	t.Error("missing review request identity")
	return nil
}

func reviewWireReply(t *testing.T, w http.ResponseWriter, in reviewWireRequest, omitCoverage bool) {
	t.Helper()
	payload := reviewWirePayload(t, in)
	var identity string
	var args any
	if json.Unmarshal(payload["request_id"], &identity) == nil && identity != "" {
		var units []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(payload["units"], &units); err != nil {
			t.Error(err)
		}
		results := []any{}
		if !omitCoverage {
			for _, u := range units {
				results = append(results, map[string]any{"unit_id": u.ID, "verdict": "supported", "issues": []any{}})
			}
		}
		args = map[string]any{"request_id": identity, "results": results}
	} else {
		_ = json.Unmarshal(payload["candidate_hash"], &identity)
		args = map[string]any{"candidate_hash": identity, "verdict": "supported", "issues": []any{}}
	}
	writeReviewWireArgs(w, in.Tools[0].Function.Name, args)
}

func writeReviewWireArgs(w http.ResponseWriter, name string, args any) {
	chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "review-result", "type": "function", "function": map[string]any{"name": name, "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
}

func boundedReviewSession(t *testing.T, handler http.HandlerFunc) *session {
	t.Helper()
	s := planSession(t)
	s.cp.HarnessRevision = 16
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: "vidlens-execution-2026-10-20-v2", RunID: s.claim.Run.ID}
	s.profile.MaxOutputTokens, s.profile.ContextTokens, s.profile.InputCharsLimit = 393216, 1048576, 2000000
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBoundedReviewRequiresExplicitCoverage(t *testing.T) {
	calls := 0
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		reviewWireReply(t, w, in, true)
	})
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(context.Background(), map[string]any{"summary": "来源事实。"}, catalog)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID" {
		t.Fatalf("omitted review coverage was accepted: %v", err)
	}
	if calls != 2 || s.cp.Repairs != 0 || s.pendingEvidenceReview() != nil {
		t.Fatalf("review protocol failure consumed author repairs or unbounded calls: calls=%d repairs=%d", calls, s.cp.Repairs)
	}
}

func TestBoundedReviewGrowsWithinCapacityAndReplays(t *testing.T) {
	var mu sync.Mutex
	budgets := []int{}
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		mu.Lock()
		budgets = append(budgets, in.MaxTokens)
		mu.Unlock()
		if in.MaxTokens <= 32768 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", in.MaxTokens)
			return
		}
		reviewWireReply(t, w, in, false)
	})
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"summary": "来源事实。"}
	if err = s.reviewContent(context.Background(), candidate, catalog); err != nil {
		t.Fatalf("review still stops at a hidden fixed cap: %v", err)
	}
	mu.Lock()
	count := len(budgets)
	copied := append([]int{}, budgets...)
	mu.Unlock()
	if count < 2 || copied[count-1] <= 32768 {
		t.Fatal("fixture did not exercise capacity recovery", copied)
	}
	for i, n := range copied {
		if n > 393216 || i > 0 && n <= copied[i-1] {
			t.Fatal("recovery repeated or exceeded the configured capacity", copied)
		}
	}
	if err = s.reviewContent(context.Background(), candidate, catalog); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(budgets) != count || s.cp.Repairs != 0 {
		t.Fatal("completed review was resent or charged as author correction", budgets, s.cp.Repairs)
	}
}

func reviewWireTruncated(w http.ResponseWriter, tokens int) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", tokens)
}

func TestBoundedReviewSplitResumesCommittedUnitsAndKnownTruncation(t *testing.T) {
	var allowSecond atomic.Bool
	var calls atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		payload := reviewWirePayload(t, in)
		var units []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(payload["units"], &units)
		if payload["candidate_context"] == nil {
			t.Error("smaller review lost cross-chapter context")
		}
		if len(units) > 1 {
			reviewWireTruncated(w, in.MaxTokens)
			return
		}
		if len(units) == 1 && units[0].ID == "u2" && !allowSecond.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reviewWireReply(t, w, in, false)
	})
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"summary": "A source statement.", "title": "A second source statement."}
	if err = s.reviewContent(ctx, candidate, catalog); err == nil {
		t.Fatal("fixture did not interrupt the second child")
	}
	if calls.Load() != 3 || s.cp.Calls != 3 {
		t.Fatal("wrong pre-interruption accounting", calls.Load(), s.cp.Calls)
	}
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(run.Checkpoint, &s.cp); err != nil {
		t.Fatal(err)
	}
	allowSecond.Store(true)
	if err = s.reviewContent(ctx, candidate, catalog); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || s.cp.Calls != 3 || s.cp.Repairs != 0 {
		t.Fatal("recovery resent the parent or completed child, or reset budgets", calls.Load(), s.cp.Calls, s.cp.Repairs)
	}
	// Model reporting survives a crash before the split decision is committed.
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE run_operations SET status='started',result=NULL,completed_at=NULL WHERE run_id=$1 AND kind='review_batch' AND result->>'state'='split'", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.reviewContent(ctx, candidate, catalog); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatal("known complete truncation was physically resent", calls.Load())
	}
	// Even an unchanged local field must be rechecked when another premise changes.
	candidate["title"] = "A changed premise."
	if err = s.reviewContent(ctx, candidate, catalog); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 7 {
		t.Fatal("cross-candidate approval was reused", calls.Load())
	}
}

func TestBoundedReviewExhaustionKeepsWriterBudget(t *testing.T) {
	for _, limit := range []string{"capacity", "calls"} {
		t.Run(limit, func(t *testing.T) {
			var calls atomic.Int32
			s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var in reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				reviewWireTruncated(w, in.MaxTokens)
			})
			if limit == "capacity" {
				s.profile.MaxOutputTokens = 4096
			} else {
				s.cp.Calls = s.profile.MaxIterations - 1
			}
			catalog, err := s.citationCatalog(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			candidate := map[string]any{"summary": "A source statement."}
			err = s.reviewContent(context.Background(), candidate, catalog)
			var problem *domain.Problem
			want := "REVIEW_BUDGET_EXHAUSTED"
			if limit == "calls" {
				want = "RESOURCE_LIMIT_EXCEEDED"
			}
			if !errors.As(err, &problem) || problem.Code != want || calls.Load() != 1 || s.cp.Repairs != 0 || s.pendingEvidenceReview() != nil {
				t.Fatalf("review exhausted the wrong quota: calls=%d repairs=%d err=%v", calls.Load(), s.cp.Repairs, err)
			}
			if err = s.reviewContent(context.Background(), candidate, catalog); err == nil || calls.Load() != 1 {
				t.Fatal("exhausted review was resent", err, calls.Load())
			}
		})
	}
}

func TestBoundedReviewCannotCommitAfterFencing(t *testing.T) {
	var current *session
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if _, err := current.engine.Store.Pool.Exec(context.Background(), "UPDATE job_runs SET execution_epoch=execution_epoch+1 WHERE id=$1", current.claim.Run.ID); err != nil {
			t.Error(err)
		}
		reviewWireReply(t, w, in, false)
	})
	current = s
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(context.Background(), map[string]any{"summary": "A source statement."}, catalog)
	if !errors.Is(err, domain.ErrFenced) {
		t.Fatal("stale review was accepted", err)
	}
	var completed int
	if err = s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='review_batch' AND status='completed'", s.claim.Run.ID).Scan(&completed); err != nil || completed != 0 {
		t.Fatal("stale review committed coverage", completed, err)
	}
}

func TestBoundedReviewDeliversAllPixelsForOneComparison(t *testing.T) {
	var visualCalls atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		var in reviewWireRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		payload := reviewWirePayload(t, in)
		var mode string
		_ = json.Unmarshal(payload["review_mode"], &mode)
		if mode == "visual" {
			visualCalls.Add(1)
			body := string(domain.JSON(in))
			if strings.Count(body, "data:image/jpeg;") != 4 {
				t.Error("comparison did not receive its four actual pixel inputs")
			}
			for _, alias := range []string{"f1", "f2", "f3", "f4"} {
				if !strings.Contains(body, "source_citation_ids="+alias) {
					t.Error("pixel lost its citation identity", alias)
				}
			}
		}
		reviewWireReply(t, w, in, false)
	})
	ctx := context.Background()
	var err error
	s.engine.Media, err = media.New(s.engine.Store, s.engine.Resources, s.engine.Config)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		var pixels bytes.Buffer
		if err = jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
			t.Fatal(err)
		}
		id := domain.ID()
		hash := sha256.Sum256(pixels.Bytes())
		if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg", Bytes: int64(pixels.Len()), TimeMS: domain.Ptr(int64(i * 100)), RequestedTimeMS: domain.Ptr(int64(i * 100))}
		if err = s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
			t.Fatal(err)
		}
		alias := fmt.Sprint("f", i)
		catalog[alias] = domain.Citation{ID: alias, Kind: "video_frame", SnapshotID: s.snapshot.ID, FrameID: id, TimeMS: asset.TimeMS}
	}
	candidate := map[string]any{"blocks": []any{map[string]any{"text": "One comparison across four supplied frames.", "citation_ids": []string{"f1", "f2", "f3", "f4"}}}}
	if err = s.reviewContent(ctx, candidate, catalog); err != nil {
		t.Fatal(err)
	}
	if visualCalls.Load() != 1 {
		t.Fatal("one comparison was fragmented across visual calls", visualCalls.Load())
	}
}

func TestBoundedReviewNotAssessedCannotApproveContent(t *testing.T) {
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
		for _, unit := range units {
			report.Results = append(report.Results, reviewUnitResult{UnitID: unit.ID, Verdict: "not_assessed", Issues: []reviewUnitIssue{}})
		}
		writeReviewWireArgs(w, reviewUnitsTool, report)
	})
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(context.Background(), map[string]any{"summary": "A source statement."}, catalog)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "REVIEW_COVERAGE_INCOMPLETE" || s.pendingEvidenceReview() != nil || s.cp.Repairs != 0 {
		t.Fatal("unassessed content was approved or charged to the author", err)
	}
}
