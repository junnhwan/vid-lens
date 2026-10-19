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

	"github.com/jackc/pgx/v5"
	"vidlens/internal/domain"
	"vidlens/internal/media"
)

// Existing generator/recovery fixtures intentionally have no semantic defects.
// Answer only the independent review tool without advancing their writer state.
func respondSupportedEvidenceReview(t *testing.T, w http.ResponseWriter, value any) bool {
	t.Helper()
	var request struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(domain.JSON(value), &request); err != nil {
		t.Error(err)
		return false
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "report_evidence_review" {
		return false
	}
	hash := ""
	for _, message := range request.Messages {
		var content string
		var payload struct {
			CandidateHash string `json:"candidate_hash"`
		}
		if json.Unmarshal(message.Content, &content) == nil && json.Unmarshal([]byte(content), &payload) == nil && payload.CandidateHash != "" {
			hash = payload.CandidateHash
		}
	}
	if hash == "" {
		t.Error("independent review lost candidate identity")
		w.WriteHeader(400)
		return true
	}
	args := evidenceReview{CandidateHash: hash, Verdict: "supported", Issues: []evidenceReviewIssue{}}
	chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "supported-review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	return true
}

func TestPublicationWaitsForIndependentEvidenceReview(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	s.cp.HarnessRevision = 16
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
	if err = s.engine.Store.RecordObservation(ctx, s.claim, "writer-observation", "overview", observation{InspectionID: "overview", FrameIDs: []string{id}, VisibleFacts: []string{"WRITER_OBSERVATION_NOT_PRIMARY: 金属认证。"}, Status: "supported"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var malformedText atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input struct {
			Messages  []struct{ Content json.RawMessage } `json:"messages"`
			Tools     []json.RawMessage                   `json:"tools"`
			MaxTokens int                                 `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		body := string(domain.JSON(input))
		if len(input.Tools) != 1 || strings.Contains(body, "WRITER_OBSERVATION_NOT_PRIMARY") {
			t.Error("review reused writer beliefs or lost source pixels instead of a fresh evidence context")
		}
		if input.MaxTokens != min(s.profile.MaxOutputTokens, 32768) {
			t.Error("actual independent review request lost its reasoning reservation")
		}
		expected := ""
		mode := ""
		for _, m := range input.Messages {
			var content string
			var payload map[string]json.RawMessage
			if json.Unmarshal(m.Content, &content) == nil && json.Unmarshal([]byte(content), &payload) == nil {
				_ = json.Unmarshal(payload["candidate_hash"], &expected)
				if expected != "" {
					_ = json.Unmarshal(payload["review_mode"], &mode)
					break
				}
			}
		}
		if expected == "" {
			t.Error("review was not bound to its exact candidate")
		}
		args := map[string]any{"candidate_hash": expected, "verdict": "supported", "issues": []any{}}
		switch mode {
		case "source_text":
			if strings.Contains(body, "image_url") || !strings.Contains(body, `\"candidate\"`) {
				t.Error("text conditions were mixed with visual review")
			}
			if !malformedText.Swap(true) {
				args["extra_field"] = "deliberately invalid reviewer output"
			} else if calls.Load() == 2 && !strings.Contains(body, "review_format_error") {
				t.Error("review format retry did not receive the actual schema error")
			}
		case "visual":
			if !strings.Contains(body, "image_url") || !strings.Contains(body, "source_citation_ids=f1") || !strings.Contains(body, "candidate_fields") || strings.Contains(body, "开始事实已经保存") {
				t.Error("visual review lost pixels or included unrelated chapters")
			}
		default:
			t.Error("source text and matching visual fields were not reviewed separately")
		}
		if mode == "visual" && strings.Contains(body, "金属认证") {
			args["verdict"] = "needs_correction"
			args["issues"] = []any{map[string]any{"path": "/sections/1/blocks/0/caption", "reason": "黑色像素不能证明材质或认证。", "citation_ids": []string{"f1"}}}
		}
		chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "review", "type": "function", "function": map[string]any{"name": "report_evidence_review", "arguments": string(domain.JSON(args))}}}}, "finish_reason": "tool_calls"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", domain.JSON(chunk))
	}))
	defer server.Close()
	// This fixture has not called a provider yet. Bind its isolated config row
	// to the local mock; production revisions are never edited by this test.
	s.profile.ModelBaseURL = server.URL
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	plan := thematicPlanFixture()
	plan.Limitations = []domain.Limitation{{Code: "partial_text", Message: "合成夹具只包含测试文字。"}}
	if body, err := planCall(s, "review-plan", "begin_article", plan); err != nil || body["version"] != float64(1) {
		t.Fatal(body, err)
	}
	body := `{"version":1,"sections":[{"id":"start","blocks":[{"type":"paragraph","origin":"video","text":"开始事实已经保存，不应因另一个章节被纠正而改写。","citation_ids":["t1"]}]},{"id":"finish","blocks":[{"type":"figure","origin":"video","frame_ids":["f1"],"caption":"金属认证：这张图证明材质与认证结果。"}]},{"id":"review","blocks":[{"type":"paragraph","origin":"video","text":"回顾由开头的来源支持。","citation_ids":["t2"]}]}]}`
	if result, err := planCall(s, "review-body", "save_article_sections", body); err != nil || result["saved_section_count"] != float64(3) {
		t.Fatal(result, err)
	}
	err = s.autoFinalize(ctx)
	if calls.Load() != 3 || err == nil {
		t.Fatalf("structurally valid but unsupported claim crossed publication without review: calls=%d err=%v", calls.Load(), err)
	}
	if _, e := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate"); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("rejected semantic candidate entered the publishable operation", e)
	}
	var rejected *candidateError
	if !errors.As(err, &rejected) {
		t.Fatal("review rejection was not repairable", err)
	}
	firstErr := rejected.err
	var reviewProblem *domain.Problem
	if !errors.As(firstErr, &reviewProblem) {
		t.Fatal("source review did not retain a structured problem")
	}
	issues, _ := reviewProblem.Meta["issues"].([]map[string]any)
	if len(issues) != 1 || issues[0]["section_id"] != "finish" || issues[0]["draft_path"] != "/sections/finish/blocks/0/caption" || issues[0]["expected_type"] != "string" {
		t.Fatal("review array offsets were not bound to the saved chapter identity", reviewProblem.Meta)
	}
	if err = s.autoFinalize(ctx); err == nil || calls.Load() != 3 {
		t.Fatal("the same rejected content was re-sent instead of replaying the review", calls.Load(), err)
	}
	// Simulate a crash after model:N completed but before its review verdict
	// committed. The reservation and frozen model input must still prevent a
	// second physical submission when the publication boundary resumes.
	frozen, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.Store.Pool.Exec(ctx, "DELETE FROM run_operations WHERE run_id=$1 AND kind='content_review'", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.autoFinalize(ctx); err == nil || calls.Load() != 3 || s.cp.Calls != 3 {
		t.Fatal("review takeover duplicated a provider call or lost its reserved model slot", calls.Load(), s.cp.Calls, err)
	}
	replayed, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:0")
	if err != nil || !bytes.Equal(frozen.Result, replayed.Result) {
		t.Fatal("review takeover replaced a frozen model input", err)
	}
	if err = s.repair(ctx, firstErr); err != nil {
		t.Fatal(err)
	}
	if s.cp.DraftIssue["code"] != "semantic_evidence" {
		t.Fatal("writer did not receive the exact review correction", s.cp.DraftIssue)
	}
	// A tool/schema failure must not erase the still-unfixed source verdict.
	// Round-trip the actual checkpoint shape as a worker takeover does.
	s.cp.DraftIssue = map[string]any{"code": "candidate_schema", "detail": "unrelated failed patch"}
	var restored checkpoint
	if err = json.Unmarshal(domain.JSON(s.cp), &restored); err != nil {
		t.Fatal(err)
	}
	s.cp = restored
	ready, err := s.readiness(ctx)
	if err != nil || !strings.Contains(string(domain.JSON(ready["evidence_review_issues"])), "finish") || !strings.Contains(fmt.Sprint(ready["next_action"]), "save_article_sections") {
		t.Fatal("a later tool failure hid the unresolved semantic correction", ready, err)
	}
	s.turn = &domain.Turn{}
	ready, err = s.readiness(ctx)
	if err != nil || !strings.Contains(fmt.Sprint(ready["next_action"]), "submit_answer") || strings.Contains(fmt.Sprint(ready["next_action"]), "read_article_sections") {
		t.Fatal("answer review correction was directed to unavailable article tools", ready, err)
	}
	s.turn = nil
	revised := fmt.Sprintf(`{"version":2,"sections":[{"id":"finish","blocks":[{"type":"figure","origin":"video","frame_ids":["f1"],"citation_ids":[%q],"caption":"实际交付的黑色合成测试画面。"}]}]}`, id)
	if result, err := planCall(s, "review-revision", "save_article_sections", revised); err != nil || result["version"] != float64(3) {
		t.Fatal(result, err)
	}
	if err = s.autoFinalize(ctx); !errors.Is(err, errDraftReady) || calls.Load() != 5 {
		t.Fatal("corrected content did not pass a fresh source review", calls.Load(), err)
	}
	if strings.Contains(string(domain.JSON(s.cp)), `"evidence_review_issue"`) {
		t.Fatal("a passed fresh review left an obsolete correction pending")
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate")
	var article domain.Article
	if err != nil || json.Unmarshal(op.Result, &article) != nil || domain.BlockText(article.Sections[0].Blocks[0]) != "开始事实已经保存，不应因另一个章节被纠正而改写。" {
		t.Fatal("review correction changed unaffected saved prose", err)
	}
	if err = s.autoFinalize(ctx); !errors.Is(err, errDraftReady) || calls.Load() != 5 {
		t.Fatal("approved candidate review was not replayable", calls.Load(), err)
	}
}
