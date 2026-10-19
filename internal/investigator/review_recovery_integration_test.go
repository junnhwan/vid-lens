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
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
)

func reviewRecoverySession(t *testing.T) *session {
	t.Helper()
	s := planSession(t)
	s.cp.HarnessRevision = 17
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	return s
}

const recoveryBody = `{"version":1,"sections":[{"id":"start","blocks":[{"type":"paragraph","origin":"video","text":"开始事实已经保存。","citation_ids":["t1"]}]},{"id":"finish","blocks":[{"type":"paragraph","origin":"video","text":"结尾的来源事实。","citation_ids":["t25"]}]},{"id":"review","blocks":[{"type":"paragraph","origin":"video","text":"回顾来源中的限制。","citation_ids":["t2"]}]}]}`

func saveReviewRecoveryDraft(t *testing.T, s *session, body string) articleDraft {
	t.Helper()
	if result, err := planCall(s, "closure-plan", "begin_article", thematicPlanFixture()); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "closure-body", "save_article_sections", body); err != nil || result["saved_section_count"] != float64(3) {
		t.Fatal(result, err)
	}
	_, draft, err := s.loadDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestReviewRecoveryInvalidReviewerNeverRequestsAuthorRepair(t *testing.T) {
	for _, validSecond := range []bool{false, true} {
		t.Run(fmt.Sprintf("valid_second=%v", validSecond), func(t *testing.T) {
			testReviewRecoveryInvalidReviewer(t, validSecond)
		})
	}
}

func testReviewRecoveryInvalidReviewer(t *testing.T, validSecond bool) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		hash := ""
		for _, message := range request.Messages {
			var content string
			var payload map[string]json.RawMessage
			if json.Unmarshal(message.Content, &content) == nil && json.Unmarshal([]byte(content), &payload) == nil && payload["candidate_hash"] != nil {
				_ = json.Unmarshal(payload["candidate_hash"], &hash)
				if payload["review_mode"] != nil && payload["review_fields"] == nil {
					t.Error("reviewer did not receive the exact existing editable fields")
				}
			}
		}
		call := calls.Add(1)
		args := map[string]any{"candidate_hash": hash, "verdict": "needs_correction", "issues": []any{map[string]any{"path": "/sections/99/blocks/0/content/0/text", "reason": "该字段与来源不一致。", "citation_ids": []string{"t1"}}}}
		if validSecond && call == 2 {
			args["verdict"], args["issues"] = "supported", []any{}
		}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "bad-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"title": "测试来源", "sections": []domain.Section{{ID: "one", Blocks: []domain.Block{{Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源事实。"}}}}}}, "limitations": []domain.Limitation{}}
	err = s.reviewContent(ctx, candidate, catalog)
	var authorRepair *candidateError
	var problem *domain.Problem
	if validSecond && err != nil || !validSecond && (errors.As(err, &authorRepair) || !errors.As(err, &problem) || problem.Code != "REVIEW_RESULT_INVALID") {
		t.Fatalf("malformed reviewer verdict was routed to author repair: %v", err)
	}
	if calls.Load() != 2 || s.cp.Repairs != 0 || s.cp.DraftIssue != nil || s.pendingEvidenceReview() != nil {
		t.Fatal("reviewer failure consumed author repairs or fabricated a content correction", calls.Load(), s.cp)
	}
	diagnostics, err := s.engine.Store.RunDiagnostics(ctx, s.claim.Run.ID)
	if err != nil || !strings.Contains(string(domain.JSON(diagnostics["review_diagnostics"])), "/sections/99/") {
		t.Fatal("review format failure lost its actual stage and offending path", diagnostics, err)
	}
}

func TestReviewRecoveryPatchClosesLoopThroughFreshTextAndPixelReview(t *testing.T) {
	s := reviewRecoverySession(t)
	s.snapshot.Source.TextProvenance = &domain.Provenance{Coverage: "complete"}
	ctx := context.Background()
	ms, err := media.New(s.engine.Store, s.engine.Resources, s.engine.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.engine.Media = ms
	var pixels bytes.Buffer
	if err = jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	id := domain.ID()
	hash := sha256.Sum256(pixels.Bytes())
	if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg", Bytes: int64(pixels.Len()), TimeMS: domain.Ptr(int64(1500)), RequestedTimeMS: domain.Ptr(int64(1500))}
	if err = s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
		t.Fatal(err)
	}
	s.cp.OverviewIDs, s.cp.CitationFrames = []string{id}, []string{id}
	if err = s.engine.Store.RecordObservation(ctx, s.claim, "closure-observation", "overview", observation{InspectionID: "overview", FrameIDs: []string{id}, VisibleFacts: []string{"合成测试画面。"}, Status: "supported"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var payload struct {
			Hash   string            `json:"candidate_hash"`
			Mode   string            `json:"review_mode"`
			Fields map[string]string `json:"review_fields"`
		}
		for _, message := range request.Messages {
			var content string
			if json.Unmarshal(message.Content, &content) != nil {
				continue // The visual review also receives actual image parts.
			}
			var next struct {
				Hash   string            `json:"candidate_hash"`
				Mode   string            `json:"review_mode"`
				Fields map[string]string `json:"review_fields"`
			}
			if json.Unmarshal([]byte(content), &next) == nil && next.Mode != "" {
				payload = next
			}
		}
		args := evidenceReview{CandidateHash: payload.Hash, Verdict: "supported", Issues: []evidenceReviewIssue{}}
		if payload.Mode == "source_text" && payload.Fields["/sections/1/blocks/0/content/0/text"] == "结尾保留来源的明确条件。" && (len(payload.Fields) != 2 || payload.Fields["/title"] == "") {
			t.Error("a correction must recheck its changed chapter and dependent global title without resubmitting independent chapters", payload.Fields)
		}
		if payload.Mode == "source_text" && payload.Fields["/sections/1/blocks/0/content/0/text"] == "结尾的来源事实。" {
			args.Verdict = "needs_correction"
			args.Issues = []evidenceReviewIssue{{Path: "/sections/1/blocks/0/content/0/text", Reason: "保留来源中的明确条件。", CitationIDs: []string{"t25"}}}
		}
		if payload.Mode == "visual" && payload.Fields["/sections/1/blocks/1/caption"] == "错误的测试画面。" {
			args.Verdict = "needs_correction"
			args.Issues = []evidenceReviewIssue{{Path: "/sections/1/blocks/1/caption", Reason: "图注必须对应真实测试像素。", CitationIDs: []string{"f1"}}}
		}
		if payload.Mode == "visual" && payload.Fields["/sections/1/blocks/1/caption"] == "合成测试画面。" && len(payload.Fields) != 2 {
			t.Error("visual correction must recheck the affected chapter heading and caption only", payload.Fields)
		}
		calls.Add(1)
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "closure-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(recoveryBody, `"citation_ids":["t25"]}]`, `"citation_ids":["t25"]},{"type":"figure","origin":"video","frame_ids":["f1"],"caption":"错误的测试画面。"}]`, 1)
	draft := saveReviewRecoveryDraft(t, s, body)
	err = s.autoFinalize(ctx)
	var rejected *candidateError
	if !errors.As(err, &rejected) || calls.Load() != 4 {
		t.Fatal("the review stopped at the first text issue instead of covering the actual pixels in the same correction round", err, calls.Load())
	}
	if !strings.Contains(string(domain.JSON(s.pendingEvidenceReview())), "/sections/1/blocks/1/caption") {
		t.Fatal("the text issue hid the actionable visual correction from the same round")
	}
	if err = s.repair(ctx, rejected.err); err != nil {
		t.Fatal(err)
	}
	patch := draftPatchInput{Version: 2, Patches: []fieldPatch{
		{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON("结尾保留来源的明确条件。")},
		{Op: "replace", Path: "/sections/finish/blocks/1/caption", Value: domain.JSON("合成测试画面。")},
	}}
	if result, err := planCall(s, "closure-patch", "patch_article_draft", patch); err != nil || result["version"] != float64(3) {
		t.Fatal(result, err)
	}
	if err = s.autoFinalize(ctx); !errors.Is(err, errDraftReady) || calls.Load() != 6 || s.pendingEvidenceReview() != nil {
		t.Fatal("corrected text did not finish fresh source and real-pixel reviews", err, calls.Load())
	}
	if _, after, err := s.loadDraft(ctx); err != nil || domain.BlockText(after.Sections["start"].Blocks[0]) != domain.BlockText(draft.Sections["start"].Blocks[0]) {
		t.Fatal("correction changed an unaffected chapter", err)
	}
	if err = s.autoFinalize(ctx); !errors.Is(err, errDraftReady) || calls.Load() != 6 {
		t.Fatal("replay sent the already approved review again", err, calls.Load())
	}
}

func TestReviewRecoveryWithdrawnFindingNeverReachesTheAuthor(t *testing.T) {
	s := reviewRecoverySession(t)
	s.cp.EvidenceReviewIssue = map[string]any{"candidate_hash": "previous-untrusted-opinion", "code": "semantic_evidence", "issues": []any{map[string]any{"path": "/summary", "reason": "先前的暂定意见。", "citation_ids": []string{"t1"}}}}
	ctx := context.Background()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls.Add(1) == 2 {
			if !strings.Contains(string(domain.JSON(body)), "provisional_review") || !strings.Contains(string(domain.JSON(body)), "previous-untrusted-opinion") {
				t.Error("tentative findings were not rechecked as untrusted source claims")
			}
			respondSupportedEvidenceReview(t, w, body)
			return
		}
		var messages []struct{ Content json.RawMessage }
		_ = json.Unmarshal(body["messages"], &messages)
		hash := ""
		for _, message := range messages {
			var text string
			var payload struct {
				Hash string `json:"candidate_hash"`
			}
			if json.Unmarshal(message.Content, &text) == nil && json.Unmarshal([]byte(text), &payload) == nil && payload.Hash != "" {
				hash = payload.Hash
			}
		}
		args := evidenceReview{CandidateHash: hash, Verdict: "needs_correction", Issues: []evidenceReviewIssue{{Path: "/summary", Reason: "这一点与原文一致，复核后应当撤回。", CitationIDs: []string{"t1"}}}}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "tentative-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.reviewContent(ctx, map[string]any{"summary": "原文支持的事实。"}, catalog); err != nil || calls.Load() != 2 || s.cp.Repairs != 0 || s.pendingEvidenceReview() != nil || s.cp.LastCandidate != nil {
		t.Fatal("a withdrawn provisional finding consumed author corrections or blocked the supported candidate", err, calls.Load(), s.cp)
	}
}

func TestReviewRecoveryServerBindsCompactFieldIdentity(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		args := map[string]any{"issues": []any{}}
		if calls.Add(1) <= 2 {
			args["issues"] = []any{map[string]any{"field": "F1", "reason": "这一确定断言与给定原文不符。", "citation_ids": []string{"t1"}}}
		}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "compact-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = s.reviewContent(ctx, map[string]any{"summary": "错误的具体事实。"}, catalog)
	var correction *candidateError
	if !errors.As(err, &correction) || calls.Load() != 2 || !strings.Contains(string(domain.JSON(s.pendingEvidenceReview())), `"path":"/summary"`) {
		t.Fatal("the server failed to bind a valid compact field ID to its exact candidate path", err, calls.Load())
	}
	if err = s.reviewContent(ctx, map[string]any{"summary": "已按原文纠正的事实。"}, catalog); err != nil || calls.Load() != 3 {
		t.Fatal("a corrected compact verdict required the model to copy server hashes or a redundant verdict", err, calls.Load())
	}
}

func TestReviewRecoveryAdjudicationCannotInventAnotherCorrection(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		call := calls.Add(1)
		args := map[string]any{"issues": []any{}}
		if call <= 2 {
			field := "F1"
			if call == 2 {
				field = "F2"
			}
			args["issues"] = []any{map[string]any{"field": field, "reason": "这是待独立核对的意见。", "citation_ids": []string{"t1"}}}
		}
		if call >= 2 && !strings.Contains(string(body["messages"]), "出版阻断意见的裁决器") {
			t.Error("adjudication was another open-ended critic instead of a bounded decision")
		}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "bounded-adjudication", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.reviewContent(ctx, map[string]any{"summary": "已有来源事实。", "title": "另一个字段。"}, catalog); err != nil || calls.Load() != 3 || s.pendingEvidenceReview() != nil || s.cp.Repairs != 0 {
		t.Fatal("an adjudicator introduced an unrelated author correction or escaped its fixed call budget", err, calls.Load())
	}
}

func TestReviewRecoveryExplicitRetryFreezesACompatibleNewHarness(t *testing.T) {
	for _, tc := range []struct{ target, calls, want int }{{0, 0, 16}, {17, 0, 17}, {17, 1, 16}} {
		t.Run(fmt.Sprintf("target=%d_calls=%d", tc.target, tc.calls), func(t *testing.T) {
			s := reviewRecoverySession(t)
			saveReviewRecoveryDraft(t, s, recoveryBody)
			ctx := context.Background()
			frozen, err := s.engine.Store.GenerationForRun(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			frozen.DetailPolicy.Revision, frozen.DetailPolicyRevision = reviewBudgetPolicy, reviewBudgetPolicy
			before := domain.JSON(frozen)
			if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE generation_requests SET generation_spec=$2 WHERE active_run_id=$1", s.claim.Run.ID, before); err != nil {
				t.Fatal(err)
			}
			s.cp.HarnessRevision, s.cp.DraftResume, s.cp.Calls = 16, true, tc.calls
			var cp map[string]any
			_ = json.Unmarshal(domain.JSON(s.cp), &cp)
			cp["retry_harness_revision"] = tc.target
			_ = json.Unmarshal(domain.JSON(cp), &s.cp)
			if err = s.resumeDraft(ctx); err != nil {
				t.Fatal(err)
			}
			if s.cp.HarnessRevision != tc.want || !strings.Contains(s.cp.Messages[0].Content, reviewAuthorInstructions) || !strings.Contains(s.cp.Messages[0].Content, evidenceGroundingInstructions) || !strings.Contains(s.cp.Messages[0].Content, repairProgressInstructions) {
				t.Fatal("retry lost recorded recovery/review instructions or changed an already-started execution", s.cp.HarnessRevision)
			}
			if strings.Contains(s.cp.Messages[0].Content, reviewRecoveryInstructions) != (tc.want == 17) {
				t.Fatal("retry tools and instructions use different Harness revisions")
			}
			after, err := s.engine.Store.GenerationForRun(ctx, s.claim.Run.ID)
			if err != nil || string(domain.JSON(after)) != string(before) {
				t.Fatal("execution refresh changed the immutable user/model/content specification", err)
			}
		})
	}
}

func TestReviewRecoveryUnchangedRevisionPreservesVersionAndCorrection(t *testing.T) {
	s := reviewRecoverySession(t)
	draft := saveReviewRecoveryDraft(t, s, recoveryBody)
	article, err := s.assembleArticle(draft)
	if err != nil {
		t.Fatal(err)
	}
	issue := evidenceReviewCorrection(map[string]any{"sections": article.Sections}, "test-review", []map[string]any{{"code": "evidence_not_supported", "path": "/sections/1/blocks/0/content/0/text", "reason": "需要保留结尾的实际条件。", "citation_ids": []string{"t25"}}})
	s.cp.EvidenceReviewIssue, s.cp.DraftIssue = issue, issue
	s.cp.DraftCorrection, s.cp.DraftRepairVersion = true, 2
	same := `{"version":2,"sections":[{"id":"finish","blocks":[{"type":"paragraph","origin":"video","text":"结尾的来源事实。","citation_ids":["t25"]}]}]}`
	result, err := planCall(s, "closure-unchanged", "save_article_sections", same)
	version, after, readErr := s.loadDraft(context.Background())
	if err != nil || readErr != nil || result["error"] == nil || version != 2 || domain.BlockText(after.Sections["finish"].Blocks[0]) != domain.BlockText(draft.Sections["finish"].Blocks[0]) {
		t.Fatal("unchanged body advanced the draft or failed without actionable feedback", result, version, err, readErr)
	}
	ready, err := s.readiness(context.Background())
	if err != nil || !strings.Contains(string(domain.JSON(ready["evidence_review_issues"])), "finish") {
		t.Fatal("unchanged submission lost the exact outstanding correction", ready, err)
	}
	fixed := strings.Replace(same, "结尾的来源事实。", "结尾保留原始来源中的实际条件。", 1)
	result, err = planCall(s, "closure-changed", "save_article_sections", fixed)
	if err != nil || result["version"] != float64(3) {
		t.Fatal("writer could not recover after unchanged-submission feedback", result, err)
	}
	_, after, err = s.loadDraft(context.Background())
	if err != nil || domain.BlockText(after.Sections["start"].Blocks[0]) != domain.BlockText(draft.Sections["start"].Blocks[0]) || s.pendingEvidenceReview() == nil {
		t.Fatal("correction modified other chapters or bypassed fresh source review", err)
	}
}

func TestReviewRecoveryLimitationMessageUsesTheReviewersExactPatchPath(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	plan := thematicPlanFixture()
	plan.Limitations = []domain.Limitation{{Code: "not_observed", Message: "错误的具体限制。", CitationIDs: []string{"t25"}}}
	if result, err := planCall(s, "limitation-plan", "begin_article", plan); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "limitation-body", "save_article_sections", recoveryBody); err != nil || result["version"] != float64(2) {
		t.Fatal(result, err)
	}
	_, draft, err := s.loadDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	issue := evidenceReviewCorrection(map[string]any{"limitations": draft.Limitations}, "limitation-review", []map[string]any{{"path": "/limitations/0/message", "reason": "只保留原文不能确定的实际范围。", "citation_ids": []string{"t25"}}})
	s.cp.EvidenceReviewIssue, s.cp.DraftIssue, s.cp.DraftCorrection = issue, issue, true
	s.cp.DraftRepairVersion = 2
	result, err := planCall(s, "limitation-correction", "patch_article_draft", draftPatchInput{Version: 2, Patches: []fieldPatch{
		{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON("修正后的结尾事实。")},
		{Op: "replace", Path: "/limitations/0/message", Value: domain.JSON("来源没有解释这个结果的原因。")},
	}})
	if err != nil || result["error"] != nil || result["version"] != float64(3) {
		t.Fatal("a real review-issued limitation path rejected the whole valid correction", result, err)
	}
	_, next, err := s.loadDraft(ctx)
	if err != nil || next.Limitations[0].Message != "来源没有解释这个结果的原因。" || next.Limitations[0].Code != draft.Limitations[0].Code || draftContentHash(next) == draftContentHash(draft) || domain.BlockText(next.Sections["start"].Blocks[0]) != domain.BlockText(draft.Sections["start"].Blocks[0]) {
		t.Fatal("limitation correction lost content or changed an unaffected chapter", next, err)
	}
}

func TestReviewRecoveryExtraChapterDoesNotPoisonCompletePlannedDraft(t *testing.T) {
	s := reviewRecoverySession(t)
	extra := strings.TrimSuffix(recoveryBody, "]}") + `,{"id":"unplanned","blocks":[{"type":"paragraph","origin":"video","text":"不属于冻结计划的额外章节。","citation_ids":["t1"]}]}]}`
	draft := saveReviewRecoveryDraft(t, s, extra)
	state := s.draftState(2, draft)
	if state["error"] != nil || s.cp.LastCandidate != nil || s.cp.DraftIssue != nil || len(draft.Sections) != len(draft.Plan) {
		t.Fatal("an extra model chapter forced repair of a nonexistent planned identity", state, s.cp.LastCandidate, s.cp.DraftIssue)
	}
	if !strings.Contains(string(domain.JSON(state)), "unplanned") {
		t.Fatal("ignored unplanned chapter was not acknowledged to the writer", state)
	}
}

func TestReviewRecoveryRepeatedPlanCannotPoisonSavedOutline(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	if result, err := planCall(s, "accepted-outline", "begin_article", thematicPlanFixture()); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	_, before, err := s.loadDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A compatible model repeated an obsolete planning action with a different
	// schema after repair_article_plan had already durably saved version 1.
	for _, name := range []string{"begin_article", "repair_article_plan"} {
		result, err := planCall(s, "obsolete-"+name, name, `{"concept_map":{"root":"invented"},"sections":"invalid"}`)
		if err != nil || result["error"] != nil || result["version"] != float64(1) || result["action_already_completed"] != true || s.cp.LastCandidate != nil || s.cp.DraftIssue != nil {
			t.Fatal("obsolete planning arguments created an impossible repair instead of acknowledging the saved outline", name, result, err)
		}
	}
	_, after, err := s.loadDraft(ctx)
	if err != nil || draftContentHash(before) != draftContentHash(after) {
		t.Fatal("obsolete planning changed the frozen outline", err)
	}
	if result, err := planCall(s, "continue-saved-outline", "save_article_sections", recoveryBody); err != nil || result["version"] != float64(2) {
		t.Fatal("writer could not continue with the accepted outline", result, err)
	}
	if s.cp.DraftWriteRequired {
		t.Fatal("the next-write directive survived its actual accepted write")
	}
}

func TestReviewRecoveryObsoletePlanForcesTheNextWriteOnTheRealWire(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	if result, err := planCall(s, "wire-outline", "begin_article", thematicPlanFixture()); err != nil || result["version"] != float64(1) {
		t.Fatal(result, err)
	}
	if result, err := planCall(s, "wire-obsolete-plan", "begin_article", `{"mind_map":{}}`); err != nil || result["error"] != nil {
		t.Fatal(result, err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Tools  []struct{ Function struct{ Name string } } `json:"tools"`
			Choice struct {
				Type     string
				Function struct{ Name string }
			} `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if len(input.Tools) != 1 || input.Tools[0].Function.Name != "save_article_sections" || input.Choice.Type != "function" || input.Choice.Function.Name != "save_article_sections" {
			t.Error("a repeated obsolete planning action did not force the actual next writer tool on the wire", input)
		}
		calls.Add(1)
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "next-real-write", "type": "function", "function": map[string]any{"name": "save_article_sections", "arguments": recoveryBody}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	s.profile.ModelBaseURL = server.URL
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	tools, err := s.makeTools()
	if err != nil {
		t.Fatal(err)
	}
	infos := []*schema.ToolInfo{}
	for _, candidate := range tools {
		info, err := candidate.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	model, err := (&meteredModel{session: s}).WithTools(infos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.Generate(ctx, []*schema.Message{schema.UserMessage("按已保存提纲继续正文。")})
	if err != nil || calls.Load() != 1 || len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Name != "save_article_sections" {
		t.Fatal(result, calls.Load(), err)
	}
}

func TestReviewRecoveryPlanCountFeedbackHasAnExecutableWholeArrayPatch(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	plan := thematicPlanFixture()
	points := []string{}
	for i := 0; i < 13; i++ {
		points = append(points, fmt.Sprint("需要保留的来源要点", i))
	}
	plan.Sections[0].KeyPoints = points
	if result, err := planCall(s, "point-overflow", "begin_article", plan); err != nil || result["error"] == nil {
		t.Fatal("oversized point list was not rejected", result, err)
	}
	merged := append([]string{points[0] + "；" + points[1]}, points[2:]...)
	result, err := planCall(s, "merge-overflow-points", "repair_article_plan", candidatePatchInput{
		CandidateID: s.cp.LastCandidate["candidate_id"].(string), ExpectedHash: s.cp.LastCandidate["expected_hash"].(string),
		Patches: []fieldPatch{{Op: "replace", Path: "/sections/0/key_points", Value: domain.JSON(merged)}},
	})
	if err != nil || result["error"] != nil || result["version"] != float64(1) {
		t.Fatal("the validator required reducing the count but the repair tool rejected its exact field path", result, err)
	}
	_, draft, err := s.loadDraft(ctx)
	if err != nil || len(draft.Plan[0].KeyPoints) != 12 || draft.Plan[0].KeyPoints[0] != merged[0] || len(draft.Plan) != len(plan.Sections) || draft.Plan[0].ID != plan.Sections[0].ID {
		t.Fatal("count correction changed the outline identity or lost the merged facts", draft, err)
	}
}

func TestReviewRecoveryEachVerdictAndItsPatchShareOneRepairBudget(t *testing.T) {
	s := reviewRecoverySession(t)
	ctx := context.Background()
	draft := saveReviewRecoveryDraft(t, s, recoveryBody)
	for round := 0; round < s.repairLimit(); round++ {
		article, err := s.assembleArticle(draft)
		if err != nil {
			t.Fatal(err)
		}
		issue := evidenceReviewCorrection(map[string]any{"sections": article.Sections}, fmt.Sprint("round-", round), []map[string]any{{"path": "/sections/1/blocks/0/content/0/text", "reason": "保留来源明确指出的条件。", "citation_ids": []string{"t25"}}})
		s.cp.EvidenceReviewIssue = issue
		if err = s.repair(ctx, &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "独立来源复核要求修订。", Meta: issue}); err != nil {
			t.Fatal("a valid new review lost its original logical correction budget", round, err)
		}
		result, err := planCall(s, fmt.Sprint("round-patch-", round), "patch_article_draft", draftPatchInput{Version: 2 + round, Patches: []fieldPatch{{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON(fmt.Sprintf("来源明确保留的第%d项条件。", round+1))}}})
		if err != nil || result["error"] != nil || result["version"] != float64(3+round) {
			t.Fatal("the same correction was counted once for rejection and again for its patch", round, result, err)
		}
		_, draft, err = s.loadDraft(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if used, err := s.repairUsage(ctx); err != nil || used != s.repairLimit() {
		t.Fatal("logical review corrections lost their bounded accounting", used, err)
	}
	if err := s.repair(ctx, invalid("第四轮仍然需要修改")); err == nil {
		t.Fatal("the original correction limit was expanded")
	}
}

func TestReviewRecoveryEncodedPatchArrayRetainsLiteralText(t *testing.T) {
	s := reviewRecoverySession(t)
	saveReviewRecoveryDraft(t, s, recoveryBody)
	s.cp.DraftIssue = map[string]any{"code": "candidate_schema"}
	s.cp.DraftCorrection, s.cp.DraftRepairVersion = true, 2
	text := "来源中的字面字符串 [1,2] 不应被解释成数组。"
	patches := string(domain.JSON([]fieldPatch{{Op: "replace", Path: "/sections/finish/blocks/0/content/0/text", Value: domain.JSON(text)}}))
	raw := string(domain.JSON(map[string]any{"version": 2, "patches": patches}))
	result, err := planCall(s, "encoded-field-patches", "patch_article_draft", raw)
	if err != nil || result["version"] != float64(3) {
		t.Fatal("a complete encoded patch array did not reach the ordinary validator", result, err)
	}
	_, draft, err := s.loadDraft(context.Background())
	if err != nil || domain.BlockText(draft.Sections["finish"].Blocks[0]) != text {
		t.Fatal("container normalization interpreted literal prose or changed its value", draft, err)
	}
	for _, raw := range []string{`{"patches":"[{invalid]"}`, `{"patches":"{}"}`, `{"patches":null}`} {
		if got := normalizeRecoveryRepairContainers("patch_article_draft", raw); got != raw {
			t.Fatal("invalid or wrongly typed repair data was guessed", raw, got)
		}
	}
}
