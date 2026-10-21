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

func TestCurrentReviewCollectsAllTextBatchesBeforeOneWriterRepair(t *testing.T) {
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
		report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
		for _, unit := range units {
			result := reviewUnitResult{UnitID: unit.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
			if unit.ID == "u1" || unit.ID == "u9" {
				result.Verdict = "contradicted"
				result.Issues = []reviewUnitIssue{{Claim: "来源事实0", Reason: "本项的引用没有对应这段原文。", CitationIDs: []string{"t1"}}}
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
	blocks := []any{}
	for i := 0; i < 9; i++ {
		blocks = append(blocks, map[string]any{"text": fmt.Sprint("来源事实0，第", i, "项"), "citation_ids": []string{"t1"}})
	}
	err = s.reviewContent(ctx, map[string]any{"blocks": blocks}, catalog)
	var correction *candidateError
	if !errors.As(err, &correction) || calls.Load() != 2 {
		t.Fatalf("first rejected text batch prevented remaining chapters being assessed: calls=%d err=%v", calls.Load(), err)
	}
	issues, _ := s.pendingEvidenceReview()["issues"].([]map[string]any)
	if len(issues) != 2 || issues[0]["path"] != "/blocks/0" || issues[1]["path"] != "/blocks/8" || s.cp.Repairs != 0 {
		t.Fatal("independent chapter findings did not share one bounded correction", s.pendingEvidenceReview())
	}
}

func TestCurrentReviewGrowsBatchOnceBeforeSplittingAndReplays(t *testing.T) {
	var calls atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.MaxTokens <= 8192 {
			reviewWireTruncated(w, in.MaxTokens)
			return
		}
		reviewWireReply(t, w, in, false)
	})
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"summary": "来源事实0", "title": "另一项来源事实"}
	if err = s.reviewContent(ctx, candidate, catalog); err != nil || calls.Load() != 2 {
		t.Fatalf("batch capacity was reset to the same small budget in recursive children: sends=%d err=%v", calls.Load(), err)
	}
	if err = s.reviewContent(ctx, candidate, catalog); err != nil || calls.Load() != 2 {
		t.Fatal("completed growth/review was resent", err, calls.Load())
	}
}

func TestCurrentReviewSeparatesBoundCitationsFromContextNeighbors(t *testing.T) {
	s := boundedReviewSession(t, func(http.ResponseWriter, *http.Request) { t.Error("input projection must not send a model request") })
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	units, err := candidateReviewUnits(map[string]any{"blocks": []any{map[string]any{"text": "来源事实0", "citation_ids": []string{"t1"}}}}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	in, err := s.reviewUnitInput(ctx, "projection", map[string]any{}, map[string]any{}, catalog, reviewTask{Mode: "source_text", Units: units})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Bindings map[string]struct {
			Cited   []string `json:"cited_text_aliases"`
			Context []string `json:"context_only_text_aliases"`
		} `json:"evidence_bindings"`
	}
	if err = json.Unmarshal([]byte(in[2].Content), &payload); err != nil || len(payload.Bindings["u1"].Cited) != 1 || payload.Bindings["u1"].Cited[0] != "t1" || len(payload.Bindings["u1"].Context) == 0 {
		t.Fatalf("delivered neighbors are indistinguishable from the claim's actual citation: %s %v", in[2].Content, err)
	}
}

func TestCurrentReviewCollectsVisualBatchesWithoutConsumingOneRepairEach(t *testing.T) {
	var calls atomic.Int32
	s, article := publicationReviewFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		payload := reviewWirePayload(t, in)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		report := reviewUnitReport{RequestID: id, Results: []reviewUnitResult{}}
		for _, unit := range units {
			result := reviewUnitResult{UnitID: unit.ID, Verdict: "supported", Issues: []reviewUnitIssue{}}
			if unit.ID == "u1" || unit.ID == "u9" {
				result.Verdict = "contradicted"
				result.Issues = []reviewUnitIssue{{Claim: "合成画面", Reason: "该具体描述与所提供的像素不同。", CitationIDs: []string{"f1"}}}
			}
			report.Results = append(report.Results, result)
		}
		writeReviewWireArgs(w, reviewUnitsTool, report)
	})
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	blocks := []any{}
	for i := 0; i < 9; i++ {
		blocks = append(blocks, map[string]any{"text": fmt.Sprint("合成画面", i), "citation_ids": []string{"f1"}})
	}
	err := s.reviewContent(context.Background(), map[string]any{"blocks": blocks}, article.Citations)
	var correction *candidateError
	if !errors.As(err, &correction) || calls.Load() != 2 {
		t.Fatalf("visual chapters were still stopped at the first finding: calls=%d err=%v", calls.Load(), err)
	}
	issues, _ := s.pendingEvidenceReview()["issues"].([]map[string]any)
	if len(issues) != 2 {
		t.Fatal("visual findings were discarded", s.pendingEvidenceReview())
	}
}

func TestCurrentReviewGrowthFallsBackAtConfiguredCapacity(t *testing.T) {
	var calls atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.MaxTokens > 8192 {
			t.Error("review exceeded the frozen configured output capacity")
		}
		var units []reviewUnit
		_ = json.Unmarshal(reviewWirePayload(t, in)["units"], &units)
		if len(units) > 1 {
			reviewWireTruncated(w, in.MaxTokens)
			return
		}
		reviewWireReply(t, w, in, false)
	})
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	s.profile.MaxOutputTokens = 8192
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.reviewContent(context.Background(), map[string]any{"summary": "来源事实0", "title": "另一项来源事实"}, catalog); err != nil || calls.Load() != 3 {
		t.Fatal("review did not split when growth had no capacity", calls.Load(), err)
	}
}
