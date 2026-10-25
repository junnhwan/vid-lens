package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

func loopPayload(t *testing.T, r *http.Request) map[string]json.RawMessage {
	t.Helper()
	var req struct {
		reviewWireRequest
		Parallel *bool `json:"parallel_tool_calls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
	}
	if req.Parallel == nil || *req.Parallel {
		t.Error("article provider request must disable parallel actions")
	}
	payload := map[string]json.RawMessage{}
	for _, msg := range req.Messages {
		var raw string
		var p map[string]json.RawMessage
		if json.Unmarshal(msg.Content, &raw) == nil && json.Unmarshal([]byte(raw), &p) == nil {
			for key, value := range p {
				payload[key] = value
			}
		}
	}
	p := payload
	if p["snapshot_id"] != nil {
		var limits struct {
			Policy     string `json:"response_policy"`
			Output     int    `json:"output_tokens"`
			Timeout    int64  `json:"response_timeout_ms"`
			WriteBytes int    `json:"max_write_bytes"`
		}
		err := json.Unmarshal(p["current_call"], &limits)
		wantTimeout := int64(300000)
		if limits.Policy == streamProgressPolicy {
			wantTimeout = 600000
		}
		if err != nil || limits.Output != req.MaxTokens || limits.Timeout != wantTimeout || limits.WriteBytes != loopWriteBytes {
			t.Error("visible call limits differ from actual provider/write limits", limits, req.MaxTokens, err)
		}
		if p["scope"] == nil && p["frame_index_navigation_only"] == nil && string(p["material_role"]) != `"source_requirements"` {
			t.Error("author cannot reload prior pixels without a frame navigation ledger")
		}
		return p
	}
	t.Error("loop payload missing")
	return nil
}
func loopGoodWrite() loopSaveInput {
	title := "合成来源的完整图文"
	text := "这段合成来源介绍多个来源事实，并在后半段提供最后一项事实。正文把这些事实组织为可以独立阅读的说明，测试只验证交付契约与恢复，不声称真实模型已经理解这些事实。"
	return loopSaveInput{ExpectedVersion: 0, Title: &title, Finish: true, Sections: []loopSection{{Key: "main", Title: "完整来源与结尾", Content: []loopContent{{Text: &text, Sources: []string{"t1", "t25"}}, {Figure: &loopFigure{Frames: []string{"f1"}, Caption: "用于测试发布链的合成黑色画面。", VisibleFacts: []string{"黑色画面"}, Uncertainty: []string{"合成图片没有实际教学内容"}}}}, Limitations: []loopSectionLimitation{{Code: "partial_text", Message: "这是合成字幕与图片的离线夹具，不代表真实视频内容。", Sources: []string{"t1"}}}}}}
}
func loopGoodReport(t *testing.T, p map[string]json.RawMessage) domain.ArticleGroundedReviewReport {
	t.Helper()
	var candidate domain.Article
	var scope domain.ArticleReviewScope
	var originals []loopOriginal
	if err := json.Unmarshal(p["candidate"], &candidate); err != nil {
		t.Error(err)
	}
	if err := json.Unmarshal(p["scope"], &scope); err != nil {
		t.Error(err)
	}
	if err := json.Unmarshal(p["originals"], &originals); err != nil {
		t.Error(err)
	}
	refs := []string{}
	for _, o := range originals {
		refs = append(refs, o.Alias)
	}
	observations := []domain.ArticleVisualObservation{}
	for _, id := range scope.FrameIDs {
		observations = append(observations, domain.ArticleVisualObservation{FrameID: id, VisibleFacts: []string{"合成黑色画面"}, Uncertainty: []string{"合成画面不能证明其他事实"}})
	}
	var pending []struct {
		ID string `json:"issue_id"`
	}
	_ = json.Unmarshal(p["unresolved_findings"], &pending)
	resolutions := []domain.ArticleReviewResolution{}
	for _, issue := range pending {
		quotes := []domain.ArticleEvidenceQuote{}
		if len(originals) > 0 {
			quotes = append(quotes, domain.ArticleEvidenceQuote{SourceID: originals[0].Alias, Quote: originals[0].Segment.Text})
		}
		resolutions = append(resolutions, domain.ArticleReviewResolution{IssueID: issue.ID, Outcome: "withdrawn", Explanation: "合成测试中的独立复核关闭意见；真实语义另行验证", EvidenceQuotes: quotes})
	}
	return domain.ArticleGroundedReviewReport{VisualObservations: observations, ArticleReviewReport: domain.ArticleReviewReport{Resolutions: resolutions, Complete: true, Unassessed: []string{}, Topics: []domain.ArticleTopic{{Topic: "合成来源事实与后半段结尾", SourceIDs: refs, SectionIDs: scope.SectionIDs}}, Issues: []domain.ArticleReviewIssue{}}}
}
func loopFixture(t *testing.T, handler http.HandlerFunc, revisions ...string) *session {
	t.Helper()
	s, _ := publicationReviewFixture(t, handler)
	// Populate a real read/export source shape; the older publication-only
	// fixture intentionally omitted navigation metadata.
	duration := s.snapshot.DurationMS
	title := "合成来源测试视频"
	source := domain.Source{SourceID: s.claim.Run.SourceID, Platform: "youtube", CanonicalURL: "https://youtube.com/watch?v=abcdefghijk", Title: &title, DurationMS: &duration}
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE source_snapshots SET source=$2 WHERE id=$1", s.snapshot.ID, domain.JSON(source)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.engine.Store.Pool.Exec(context.Background(), "DELETE FROM observations WHERE run_id=$1", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	s.compositionReady = false
	s.cp = checkpoint{SchemaVersion: 1, ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2, RunID: s.claim.Run.ID}}
	s.profile.MaxOutputTokens = 32768
	s.profile.ArticleHarness = domain.ArticleLoopV2
	// Existing fixtures exercise the frozen v5 wire. New revision tests opt in;
	// admission itself defaults to v6 through initializeArticleLoop.
	s.initializeArticleLoop()
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV5
	if len(revisions) > 0 {
		s.cp.ArticleLoop.ReviewRevision = revisions[0]
		if domain.ArticleDeliveryPolicy(revisions[0]) {
			s.cp.ArticleDeliveryPolicy = revisions[0]
			s.cp.ArticleLoop.OutputTokens = min(s.profile.MaxOutputTokens, 8192)
			s.cp.ArticleLoop.OutputSections = 2
		}
	}
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	var err error
	s.snapshot, err = s.engine.Store.Snapshot(context.Background(), s.snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.transcript, err = s.engine.Store.Transcript(context.Background(), s.snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.Checkpoint(context.Background(), s.claim, s.cp); err != nil {
		t.Fatal(err)
	}
	s.claim.Run, err = s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func reloadLoopSession(t *testing.T, s *session) *session {
	t.Helper()
	r, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := *s
	next.claim.Run = r
	if err = json.Unmarshal(r.Checkpoint, &next.cp); err != nil {
		t.Fatal(err)
	}
	return &next
}
func assertNoLoopPublication(t *testing.T, s *session) {
	t.Helper()
	var n int
	if err := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unexpected publication %d %v", n, err)
	}
}
func TestArticleLoopT01TwoCallsAndRealPublication(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil {
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
		} else {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
		}
	})
	// An unused, valid overview frame must not create another observation or
	// a per-frame review obligation. Only the selected figure is published.
	frames, e := s.engine.Store.RunFrames(context.Background(), s.claim.Run.ID)
	if e != nil {
		t.Fatal(e)
	}
	unused := frames[0]
	unused.ID = domain.ID()
	unused.StorageKey = unused.ID + ".jpg"
	unused.TimeMS = domain.Ptr(int64(1500))
	unused.RequestedTimeMS = unused.TimeMS
	pixels, e := os.ReadFile(filepath.Join(s.engine.Config.DataDir, frames[0].StorageKey))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.engine.Config.DataDir, unused.StorageKey), pixels, 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.engine.Store.AddAsset(context.Background(), s.claim, unused); e != nil {
		t.Fatal(e)
	}

	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("simple article needed hidden plan or extra review: %d", calls.Load())
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || run.Status != "succeeded" || run.ResultID == nil {
		t.Fatalf("terminal state: %+v %v", run, err)
	}
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil || len(a.Sections) != 1 || len(a.Citations) < 2 {
		t.Fatalf("unreadable publication %+v %v", a, err)
	}
	var old int
	if err = s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND (operation_key LIKE 'workflow:%' OR operation_key LIKE 'review-units:%' OR operation_key='candidate')", s.claim.Run.ID).Scan(&old); err != nil || old != 0 {
		t.Fatalf("legacy main chain reached: %d %v", old, err)
	}
}
func TestArticleLoopT02T04PartialReceiptAndReplay(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no provider expected") })
	l := &articleLoopState{ReadSources: s.loopAllSourceAliases(), Frames: []string{}, SeenFrames: []string{}}
	frames, err := s.engine.Store.RunFrames(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	l.Frames = []string{frames[0].ID}
	l.SeenFrames = l.Frames
	s.cp.ArticleLoop = l
	input := loopGoodWrite()
	bad := input.Sections[0]
	bad.Key = "bad"
	bad.SectionID = ""
	bad.rejected = "unknown /content/0/citations field"
	input.Sections = append(input.Sections, bad)
	result, progress, err := s.saveLoopSections(context.Background(), "mixed", input)
	if err != nil || !progress {
		t.Fatalf("valid chapter lost %v %v", result, err)
	}
	saved, d, err := s.loadLoopDraft(context.Background())
	if err != nil || saved.Version != 1 || len(d.Article.Sections) != 1 || len(d.BatchReceipt.Rejected) != 1 {
		t.Fatalf("partial receipt %+v %+v %v", saved, d.BatchReceipt, err)
	}
	// Simulate a crash after committed write, before returning receipt/checkpoint.
	again, p, err := s.saveLoopSections(context.Background(), "mixed", input)
	if err != nil || !p || store.Hash(again) != store.Hash(result) {
		t.Fatalf("replay changed original receipt %v %v", again, err)
	}
	latest, _, _ := s.loadLoopDraft(context.Background())
	if latest.Version != saved.Version {
		t.Fatal("replay duplicated version")
	}
	input.ExpectedVersion = 1
	input.Sections = input.Sections[:1]
	input.Sections[0].SectionID = d.Article.Sections[0].ID
	input.Sections[0].Key = ""
	_, progress, err = s.saveLoopSections(context.Background(), "same", input)
	if err != nil || progress {
		t.Fatalf("no-op counted as progress %v %v", progress, err)
	}
}
func TestArticleLoopT03StrictEnvelopeAndSemanticForms(t *testing.T) {
	for _, raw := range []string{`{"expected_version":0,"sections":[`, `{"expected_version":0,"expected_version":1,"sections":[]}`, `{"sections":[{"content":[{"text":"first","text":"second"}]}]}`, `{"sections":[]}}`} {
		var in loopSaveInput
		if loopDecode([]byte(raw), &in) == nil {
			t.Fatalf("invalid envelope accepted %s", raw)
		}
	}
	var in loopSaveInput
	if err := loopDecode([]byte(`{"expected_version":0,"sections":[{"key":"good","title":"合法","content":[{"text":"事实","sources":["t1"]}]},{"key":"bad","title":"错误","content":[{"text":"事实","citations":["t1"]}]}]}`), &in); err != nil || len(in.Sections) != 2 || in.Sections[0].rejected != "" || in.Sections[1].rejected == "" {
		t.Fatalf("bad chapter rejected entire envelope or silently ignored unknown field: %+v %v", in, err)
	}
	cat := map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", Label: "原文", SnapshotID: "s", SegmentIDs: []string{"x"}, Range: &domain.Range{StartMS: 0, EndMS: 1}}}
	code := loopContent{Code: &loopCode{Text: "x := 1", Language: "go"}, Sources: []string{"t1"}}
	list := loopContent{List: &loopList{Ordered: true, Items: []loopSourcedText{{Text: "第一步", Sources: []string{"t1"}}}}}
	table := loopContent{Table: &loopTable{Columns: []string{"列"}, Rows: []loopRow{{Cells: []string{"值"}, Sources: []string{"t1"}}}}}
	sec, _, _, _, err := loopCanonicalSection(loopSection{Title: "语义", Content: []loopContent{code, list, table}}, "stable", cat, nil)
	if err != nil || sec.Blocks[0].Type != "code" || sec.Blocks[1].Type != "list" || sec.Blocks[2].Type != "table" {
		t.Fatalf("semantic forms lost %+v %v", sec, err)
	}
	text := "事实"
	both := loopContent{Text: &text, Code: &loopCode{Text: "x"}, Sources: []string{"t1"}}
	if _, _, _, _, err = loopCanonicalSection(loopSection{Title: "坏", Content: []loopContent{both}}, "stable", cat, nil); err == nil {
		t.Fatal("multiple constructors accepted")
	}
}
func TestArticleLoopT15BlockingAndIncompleteReviewDoNotPublish(t *testing.T) {
	for _, variant := range []string{"incomplete", "wrong_citation", "missing_end", "false_caption"} {
		t.Run(variant, func(t *testing.T) {
			var calls atomic.Int32
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				n := calls.Add(1)
				if p["scope"] == nil {
					if n == 1 {
						writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
					} else {
						writeReviewWireArgs(w, loopFinishTool, loopFinishInput{ExpectedVersion: 1})
					}
					return
				}
				report := loopGoodReport(t, p)
				if variant == "incomplete" {
					report.Complete = false
					report.Unassessed = []string{"后半段"}
				} else {
					ac := "AC04"
					if variant == "missing_end" {
						ac = "AC02"
					}
					if variant == "false_caption" {
						ac = "AC05"
					}
					report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: ac, Location: "main claim", EvidenceIDs: []string{"t25"}, Problem: "合成注入的错引/遗漏/假图注", Correction: "按原文与真实像素修正"}}
				}
				writeReviewWireArgs(w, loopReviewTool, report)
			})
			err := s.engine.Run(context.Background(), s.claim, s.profile)
			var problem *domain.Problem
			if !errors.As(err, &problem) || !slices.Contains([]string{"ARTICLE_NO_PROGRESS", "ARTICLE_BUDGET_EXHAUSTED", "ARTICLE_REVIEW_PROTOCOL_FAILED", "ARTICLE_REPAIR_LIMIT"}, problem.Code) {
				t.Fatalf("unexpected termination %v", err)
			}
			assertNoLoopPublication(t, s)
			draft, _ := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
			if draft.Version != 1 {
				t.Fatal("normal body rewritten after reviewer failure")
			}
		})
	}
}
func TestArticleLoopT09T10UndeliveredEvidenceCannotFinish(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no provider expected") })
	s.cp.ArticleLoop = &articleLoopState{ReadSources: []string{"t1"}, SeenFrames: []string{}, Frames: []string{}}
	input := loopGoodWrite()
	result, _, err := s.saveLoopSections(context.Background(), "not-delivered", input)
	if err != nil {
		t.Fatal(err)
	}
	saved, d, err := s.loadLoopDraft(context.Background())
	if err != nil || len(d.Article.Sections) != 0 || len(d.BatchReceipt.Rejected) != 1 {
		t.Fatalf("undelivered pixels/source saved %v %+v %v", result, d.BatchReceipt, err)
	}
	ok, err := s.freezeLoopCandidate(context.Background(), loopFinishInput{ExpectedVersion: saved.Version})
	if err != nil || ok || !strings.Contains(string(s.cp.ArticleLoop.Feedback), "source_coverage") {
		t.Fatalf("unread tail accepted %v %v", ok, err)
	}
	assertNoLoopPublication(t, s)
}
func TestArticleLoopT11OrdinaryTextDoesNotComplete(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"写完了\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var p *domain.Problem
	if !errors.As(err, &p) || p.Code != "ARTICLE_NO_PROGRESS" || calls.Load() > 7 {
		t.Fatalf("plain text bypass/unbounded loop %d %v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}
func TestArticleLoopT07ReviewTransportFailureResumesOnlyReview(t *testing.T) {
	var author, review atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			author.Add(1)
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		review.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	if err == nil {
		t.Fatal("failed review published")
	}
	assertNoLoopPublication(t, s)
	before, _ := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	fail.Store(false)
	s = reloadLoopSession(t, s)
	if err = s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	after, _ := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if author.Load() != 1 || review.Load() != 2 || before.Version != after.Version {
		t.Fatalf("review failure rewrote author or lost budget: %d/%d %+v %+v", author.Load(), review.Load(), before, after)
	}
}

func TestArticleLoopT03TruncationNarrowsAuthorAndReview(t *testing.T) {
	for _, mode := range []string{"author", "review"} {
		t.Run(mode, func(t *testing.T) {
			var calls, author, review atomic.Int32
			truncated := false
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				calls.Add(1)
				isReview := p["scope"] != nil
				if isReview {
					review.Add(1)
				} else {
					author.Add(1)
				}
				if !truncated && (mode == "review" && isReview || mode == "author" && !isReview) {
					truncated = true
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":32768}}\n\ndata: [DONE]\n\n")
					return
				}
				if isReview {
					writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
				} else {
					if author.Load() > 1 {
						var max int
						_ = json.Unmarshal(p["max_sections_this_action"], &max)
						if p["recommended_sections_this_action"] != nil {
							_ = json.Unmarshal(p["recommended_sections_this_action"], &max)
						}
						if max >= 40 {
							t.Error("truncated author repeated original output scope")
						}
					}
					writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
				}
			})
			if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
				t.Fatal(err)
			}
			if mode == "review" && (author.Load() != 1 || review.Load() != 4) {
				t.Fatalf("review truncation rewrote author or skipped coverage: %d/%d", author.Load(), review.Load())
			}
			if mode == "author" && calls.Load() != 3 {
				t.Fatalf("unexpected truncated author recovery: %d", calls.Load())
			}
		})
	}
}
func TestArticleLoopT05UnknownSendKeepsSameInputAndCounts(t *testing.T) {
	var calls atomic.Int32
	inputs := []string{}
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		n := calls.Add(1)
		if n <= 2 {
			inputs = append(inputs, store.Hash(string(body)))
		}
		if n == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		p := loopPayload(t, r)
		if p["scope"] != nil {
			writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
		} else {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
		}
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
		t.Fatal("unknown response treated as success")
	}
	before := reloadLoopSession(t, s)
	if before.cp.Calls != 1 || before.cp.ArticleLoop.Pending == nil {
		t.Fatal("unknown send lost reserved logical slot")
	}
	if err := before.engine.Run(context.Background(), before.claim, before.profile); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0] != inputs[1] || calls.Load() != 3 {
		t.Fatalf("takeover changed frozen input or hid sends: %v %d", inputs, calls.Load())
	}
	stats, err := s.engine.Store.ModelExecution(context.Background(), s.claim.Run.ID)
	if err != nil || stats.Calls != 2 || stats.Submissions != 3 {
		t.Fatalf("accounting lost unknown send: %+v %v", stats, err)
	}
	var usage string
	if err = s.engine.Store.Pool.QueryRow(context.Background(), "SELECT reporting->'input_tokens'->>'status' FROM model_submissions WHERE run_id=$1 AND operation_key='model:0' AND submission=1", s.claim.Run.ID).Scan(&usage); err == nil && usage == "reported_zero" {
		t.Fatal("unknown billing became zero")
	}
}
func TestArticleLoopT06LateResponseCannotWriteAfterCancelOrTakeover(t *testing.T) {
	for _, mode := range []string{"cancel", "takeover"} {
		t.Run(mode, func(t *testing.T) {
			var s *session
			var calls atomic.Int32
			s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = loopPayload(t, r)
				ctx := context.Background()
				if mode == "cancel" {
					var req, owner string
					if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&req, &owner); err != nil {
						t.Error(err)
					}
					if err := s.engine.Store.CancelRequest(ctx, owner, req); err != nil {
						t.Error(err)
					}
				} else {
					if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1", s.claim.Run.ID); err != nil {
						t.Error(err)
					}
					next, err := s.engine.Store.Claim(ctx, s.claim.Run.ID, s.claim.Run.DispatchGeneration, "new-worker")
					if err != nil || next == nil || next.Epoch <= s.claim.Epoch {
						t.Errorf("takeover failed %+v %v", next, err)
					}
				}
				writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			})
			if err := s.engine.Run(context.Background(), s.claim, s.profile); !errors.Is(err, domain.ErrFenced) {
				t.Fatalf("late response not fenced: %v", err)
			}
			draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
			if err != nil || draft.Version != 0 || calls.Load() != 1 {
				t.Fatalf("late write or extra send: %+v calls=%d %v", draft, calls.Load(), err)
			}
			assertNoLoopPublication(t, s)
		})
	}
}
func TestArticleLoopT08ChangedTitleInvalidatesApprovalInBothEntrances(t *testing.T) {
	var s *session
	var original domain.Article
	var calls atomic.Int32
	s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		saved, d, err := s.loadLoopDraft(context.Background())
		if err != nil {
			t.Error(err)
		}
		original = d.Article
		d.Article.Title = "核验之后修改的标题"
		if _, err = s.engine.Store.SaveArticleDraft(context.Background(), s.claim, "concurrent-title", nil, saved.Version, d); err != nil {
			t.Error(err)
		}
		writeReviewWireArgs(w, loopReviewTool, loopGoodReport(t, p))
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_APPROVAL_REQUIRED" || calls.Load() != 2 {
		t.Fatalf("changed candidate published or generated secretly: %v calls=%d", err, calls.Load())
	}
	if err = s.engine.Store.Publish(context.Background(), s.claim, original); !errors.As(err, &problem) || problem.Code != "ARTICLE_APPROVAL_REQUIRED" {
		t.Fatalf("transaction bypass accepted old candidate: %v", err)
	}
	assertNoLoopPublication(t, s)
	// The historical candidate shortcut also cannot send a model request.
	if _, err = s.engine.Store.BeginOperation(context.Background(), s.claim, "candidate", "article_candidate", original, false); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.FinishOperation(context.Background(), s.claim, "candidate", original, "", nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	err = s.engine.Run(context.Background(), s.claim, s.profile)
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_APPROVAL_REQUIRED" || calls.Load() != 2 {
		t.Fatal("legacy candidate shortcut bypassed new gate", err)
	}
}
func TestArticleLoopT12TooSmallCapacityStopsBeforeSending(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	s.profile.InputCharsLimit = 1000
	s.initializeArticleLoop()
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV5
	if err := s.engine.Store.Checkpoint(context.Background(), s.claim, s.cp); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE runtime_config_revisions SET profile=$2 WHERE id=$1", s.claim.Run.ConfigID, domain.JSON(s.profile)); err != nil {
		t.Fatal(err)
	}
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_CAPACITY_EXCEEDED" || calls.Load() != 0 {
		t.Fatalf("capacity silently reduced source/quality: calls=%d %v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}
func TestArticleLoopProviderContextRejectionNarrowsNextInput(t *testing.T) {
	var calls atomic.Int32
	counts := []int{}
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		var originals []loopOriginal
		_ = json.Unmarshal(p["originals"], &originals)
		counts = append(counts, len(originals))
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"code":"context_length_exceeded","message":"prompt is too long"}}`)
			return
		}
		// Stop after observing the changed input; do not permit this scripted model
		// to claim it has read the omitted source or produce a complete article.
		w.WriteHeader(503)
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	if err == nil || len(counts) != 2 || counts[1] >= counts[0] || counts[1] == 0 {
		t.Fatalf("context failure repeated unchanged scope: %v %v", counts, err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopT13ExplicitRetryImportsArtifactsAndLineageOnly(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil {
			w.WriteHeader(503)
			return
		}
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
		t.Fatal("failed review published")
	}
	s = reloadLoopSession(t, s)
	before, _, err := s.loadLoopDraft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var reqID, owner string
	if err = s.engine.Store.Pool.QueryRow(context.Background(), "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&reqID, &owner); err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.Store.Fail(context.Background(), s.claim, domain.Failure{Code: "PROVIDER_UNAVAILABLE", CanRetry: true}, false, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := s.engine.Store.CreateRequest(context.Background(), owner, domain.ID(), "", reqID, false)
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.engine.Store.Request(context.Background(), owner, retry.ID)
	if err != nil || request.ActiveRunID == nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(context.Background(), *request.ActiveRunID)
	if err != nil {
		t.Fatal(err)
	}
	var cp checkpoint
	if err = json.Unmarshal(run.Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.ExecutionContract == nil || cp.ExecutionContract.Version != domain.ArticleLoopV2 || cp.ExecutionContract.RunID != run.ID || cp.ArticleLoop != nil || cp.Calls != 0 || len(cp.ArticleLoopParent) == 0 {
		t.Fatalf("retry imported execution or lost parent lineage: %s", run.Checkpoint)
	}
	draft, err := s.engine.Store.ArticleDraft(context.Background(), run.ID)
	if err != nil || draft.Version != 1 || store.Hash(json.RawMessage(draft.Data)) != store.Hash(json.RawMessage(before.Data)) {
		t.Fatal("retry lost immutable valid draft", err)
	}
	var approvals int
	if err = s.engine.Store.Pool.QueryRow(context.Background(), "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind IN ('article_loop_approval','article_loop_review','model','model_input')", run.ID).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatal("retry copied approval or call slots", approvals, err)
	}
	ancestor, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || store.Hash(ancestor.Checkpoint) != store.Hash(s.claim.Run.Checkpoint) {
		t.Fatal("retry rewrote ancestor checkpoint", err)
	}
}

func TestArticleLoopT11EquivalentViewUsesExistingPixels(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		calls.Add(1)
		writeReviewWireArgs(w, "view_frames", loopViewInput{Question: "同一个画面问题", StartMS: 0, EndMS: 1000, Count: 1})
	})
	frames, err := s.engine.Store.RunFrames(context.Background(), s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	window := media.Window{StartMS: 0, EndMS: 1000, Count: 1, Mode: "targeted"}
	key := "article-loop:inspect:" + store.Hash(window)
	if _, err = s.engine.Store.BeginOperation(context.Background(), s.claim, key, "inspect", nil, false); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.FinishOperation(context.Background(), s.claim, key, media.Inspection{FrameIDs: []string{frames[0].ID}}, "", nil); err != nil {
		t.Fatal(err)
	}
	err = s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_NO_PROGRESS" || calls.Load() > 7 {
		t.Fatalf("equivalent views became fake progress: calls=%d %v", calls.Load(), err)
	}
	after, e := s.engine.Store.RunFrames(context.Background(), s.claim.Run.ID)
	if e != nil || len(after) != len(frames) {
		t.Fatal("equivalent view rematerialized frames", e)
	}
}

func TestArticleLoopCannotCompleteIsExplicitFailure(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		calls.Add(1)
		writeReviewWireArgs(w, "stop_article", loopCannotCompleteInput{Reason: "关键示例未出现在可用音画中，无法忠实交付完整图文。", EvidenceIDs: []string{"t1"}})
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_CANNOT_COMPLETE" || calls.Load() != 1 {
		t.Fatalf("missing evidence was hidden as success or repeated calls: %d %v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopReviewFailureRetainsDiagnosticReportWithoutRewritingDraft(t *testing.T) {
	var review atomic.Int32
	var previous string
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		report := loopGoodReport(t, p)
		if review.Add(1) == 1 {
			report.Complete = false
			report.Unassessed = []string{"the end needs review"}
			previous = string(domain.JSON(report))
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		var feedback struct {
			Previous string `json:"previous_report"`
		}
		if err := json.Unmarshal(p["execution_feedback"], &feedback); err != nil || feedback.Previous != previous {
			t.Error("reviewer lost the actual failed report", err)
		}
		writeReviewWireArgs(w, loopReviewTool, report)
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil || draft.Version != 1 || review.Load() != 2 {
		t.Fatalf("review recovery rewrote accepted body: v%d reviews%d %v", draft.Version, review.Load(), err)
	}
}
