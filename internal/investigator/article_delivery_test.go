package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/exporting"
)

func TestDeliveryPublishesAndExportsBeforeReview(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil || p["material_role"] != nil {
			t.Error("delivery must not depend on a semantic source plan or review")
		}
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	}, "delivery-first-2026-10-25-v1")
	ctx := context.Background()
	s.cp.ArticleDeliveryPolicy = domain.ArticleDeliveryV1
	if err := s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("article needed a blocking review: %d provider calls", calls.Load())
	}
	var request, owner string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&request, &owner); err != nil {
		t.Fatal(err)
	}
	view, err := s.engine.Store.Article(ctx, owner, request)
	if err != nil || view["article"] == nil {
		t.Fatalf("article not readable before review: %v", err)
	}
	for _, format := range []string{"markdown", "markdown_zip"} {
		accepted, _, err := s.engine.Store.CreateExport(ctx, owner, request, format, domain.ID())
		if err != nil {
			t.Fatal(err)
		}
		job, err := s.engine.Store.ClaimExport(ctx, accepted.ID)
		if err != nil || job == nil {
			t.Fatalf("export claim: %v", err)
		}
		exporter := exporting.Service{Store: s.engine.Store, Config: s.engine.Config}
		if err = exporter.Run(ctx, *job); err != nil {
			t.Fatal(err)
		}
		ready, err := s.engine.Store.Export(ctx, owner, request, job.ID)
		if err != nil || ready.Status != "ready" {
			t.Fatalf("export not delivered before review: %+v %v", ready, err)
		}
	}
}

func TestDeliveryReviewFailureAndManualRetryAreIndependent(t *testing.T) {
	for _, mode := range []string{"http_failure", "malformed_report"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					_ = loopPayload(t, r)
					writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
					return
				}
				if mode == "http_failure" {
					http.Error(w, "unavailable", 502)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"bad","type":"function","function":{"name":"report_article_advice","arguments":"{"}}]}}]}`))
			}, domain.ArticleDeliveryV1)
			ctx := context.Background()
			if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
				t.Fatal(err)
			}
			var request, owner string
			if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&request, &owner); err != nil {
				t.Fatal(err)
			}
			run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil || run.ResultID == nil {
				t.Fatal(err)
			}
			before, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := s.engine.Store.PendingArticleReviews(ctx)
			if err != nil || len(pending) != 1 {
				t.Fatalf("automatic review count %d %v", len(pending), err)
			}
			review, err := s.engine.Store.ClaimArticleReview(ctx, pending[0].ID)
			if err != nil || review == nil {
				t.Fatal(err)
			}
			if err = s.engine.RunArticleReview(ctx, *review); err != nil {
				t.Fatal(err)
			}
			failed, err := s.engine.Store.ArticleReview(ctx, review.ID)
			if err != nil || failed.Status != "incomplete" || failed.Submissions != 1 {
				t.Fatalf("review failure was not independent: %+v %v", failed, err)
			}
			if err = s.engine.RunArticleReview(ctx, *review); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("physical review repeated after failure: %d", calls.Load())
			}
			after, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
			if err != nil || domain.ArticleHash(before) != domain.ArticleHash(after) {
				t.Fatal("advice modified immutable article", err)
			}
			if _, err = s.engine.Store.Article(ctx, owner, request); err != nil {
				t.Fatal("failed advice blocked reading", err)
			}
			if _, _, err = s.engine.Store.CreateExport(ctx, owner, request, "markdown", domain.ID()); err != nil {
				t.Fatal("failed advice blocked export", err)
			}
			key := domain.ID()
			manual, err := s.engine.Store.CreateArticleReview(ctx, owner, request, key)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := s.engine.Store.CreateArticleReview(ctx, owner, request, key)
			if err != nil || !replay.Replayed || manual.ID != replay.ID || manual.ID == review.ID {
				t.Fatalf("manual idempotency failed: %+v %+v %v", manual, replay, err)
			}
			if _, err = s.engine.Store.LatestArticleReview(ctx, "wrong-session", request); err == nil {
				t.Fatal("review leaked across sessions")
			}
			rows, err := s.engine.Store.PendingArticleReviews(ctx)
			if err != nil || len(rows) != 1 || rows[0].Trigger != "manual" {
				t.Fatalf("automatic retry was queued %+v %v", rows, err)
			}
			var result string
			if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT status FROM generation_requests WHERE id=$1", request).Scan(&result); err != nil || result != "completed" {
				t.Fatal("review changed delivery state", result, err)
			}
		})
	}
}

func TestDeliveryReviewReturnsAdviceWithoutChangingArticle(t *testing.T) {
	var calls atomic.Int32
	var section string
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_ = loopPayload(t, r)
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		writeReviewWireArgs(w, articleAdviceTool, domain.ArticleAdvice{Summary: "合成审核意见。", Findings: []domain.ArticleFinding{{SectionID: section, Problem: "测试建议核对。", Suggestion: "用户可以自行决定是否修订。"}}})
	}, domain.ArticleDeliveryV1)
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(ctx, s.claim.Run.ID)
	a, err := s.engine.Store.PublishedArticle(ctx, *run.ResultID)
	if err != nil {
		t.Fatal(err)
	}
	section = a.Sections[0].ID
	rows, err := s.engine.Store.PendingArticleReviews(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	r, err := s.engine.Store.ClaimArticleReview(ctx, rows[0].ID)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	if err = s.engine.RunArticleReview(ctx, *r); err != nil {
		t.Fatal(err)
	}
	v, err := s.engine.Store.ArticleReview(ctx, r.ID)
	if err != nil || v.Status != "completed" || v.Result == nil || !strings.Contains(v.Result.Summary, "合成") {
		t.Fatalf("advice missing %+v %v", v, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("review used multiple calls %d", calls.Load())
	}
}

func TestDeliveryInterruptedReviewIsNotAutomaticallySubmittedAgain(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	}, domain.ArticleDeliveryV1)
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	rows, err := s.engine.Store.PendingArticleReviews(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	r, err := s.engine.Store.ClaimArticleReview(ctx, rows[0].ID)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.BeginArticleReviewSubmission(ctx, *r, "frozen-input"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE article_reviews SET deadline_at=now()-interval '1 second' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.engine.Store.PendingArticleReviews(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal("interrupted review requeued", err)
	}
	v, err := s.engine.Store.ArticleReview(ctx, r.ID)
	if err != nil || v.Status != "incomplete" || v.Submissions != 1 {
		t.Fatalf("unknown outcome lost: %+v %v", v, err)
	}
	if duplicate, err := s.engine.Store.ClaimArticleReview(ctx, r.ID); err != nil || duplicate != nil {
		t.Fatal("interrupted review reclaimed", err)
	}
}

func TestDeliveryPartialDraftRemainsReadable(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := loopGoodWrite()
		in.Finish = false
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleDeliveryV1)
	ctx := context.Background()
	s.cp.ArticleLoop.Budget.Calls = 1
	s.cp.ArticleLoop.Budget.Sends = 1
	if err := s.persist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var err error
	s.claim.Run, err = s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("unfinished article was labelled complete")
	}
	var owner, request string
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT session_id,id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&owner, &request); err != nil {
		t.Fatal(err)
	}
	v, err := s.engine.Store.RequestDraft(ctx, owner, request)
	if err != nil || v["draft"] == nil {
		t.Fatal("accepted chapter lost after interruption", err)
	}
	if _, err = s.engine.Store.RequestDraft(ctx, "other", request); err == nil {
		t.Fatal("draft leaked across sessions")
	}
	assertNoLoopPublication(t, s)
}

func TestDeliveryCreatesAndOrdersChaptersInOneSave(t *testing.T) {
	var calls atomic.Int32
	var s *session
	s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := loopGoodWrite()
		if calls.Add(1) == 1 {
			in.Finish = false
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		saved, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
		if err != nil {
			t.Error(err)
			return
		}
		var draft articleLoopDraft
		_ = json.Unmarshal(saved.Data, &draft)
		in.ExpectedVersion = saved.Version
		in.Title = nil
		in.Sections[0].Key = "next"
		in.Order = []string{draft.Article.Sections[0].ID, "next"}
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleDeliveryV2)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("atomic creation/reorder required repairs: %d", calls.Load())
	}
}

func TestDeliveryUsesAvailableInputAndAvoidsRedundantReads(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model call expected") }, domain.CurrentArticleDelivery)
	s.profile.InputCharsLimit, s.profile.ContextTokens = 140000, 0
	s.cp.ArticleLoop.SourceChars = 70000
	s.cp.ArticleLoop.Budget.InputCharsLimit, s.cp.ArticleLoop.Budget.ContextTokens = 140000, 0
	s.transcript = nil
	for i := range 800 {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("dense-", i), Text: "Unchanged original.", StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000), Origin: "subtitle"})
	}
	p, _, tools, err := s.prepareLoopCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sources) != len(s.transcript) {
		t.Fatalf("needless source pagination despite available input: delivered %d of %d", len(p.Sources), len(s.transcript))
	}
	for _, info := range tools {
		if info.Name == "read_evidence" || info.Name == "read_article_state" {
			t.Fatalf("redundant %s still consumes author calls when complete input is present", info.Name)
		}
	}
}

func TestDeliveryDenseSourceStillRespectsCapacity(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model call expected") }, domain.CurrentArticleDelivery)
	s.profile.InputCharsLimit, s.profile.ContextTokens = 50000, 0
	s.cp.ArticleLoop.Budget.InputCharsLimit, s.cp.ArticleLoop.Budget.ContextTokens = 50000, 0
	s.transcript = nil
	for i := range 800 {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("dense-", i), Text: strings.Repeat("unchanged original ", 5), StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000), Origin: "subtitle"})
	}
	p, _, _, err := s.prepareLoopCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sources) == 0 || len(p.Sources) >= len(s.transcript) {
		t.Fatalf("actual input capacity was ignored: %d sources", len(p.Sources))
	}
	if len(s.cp.ArticleLoop.ReadSources) != 0 {
		t.Fatal("preparation fabricated source delivery")
	}
}

func TestDeliveryPartialSourceInputKeepsSavedDraftAndAvoidsEmptyReads(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model call expected") }, domain.CurrentArticleDelivery)
	s.profile.InputCharsLimit, s.profile.ContextTokens = 50000, 0
	ctx := context.Background()
	saved, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft.Article.Sections = []domain.Section{}
	for i := range 2 {
		draft.Article.Sections = append(draft.Article.Sections, domain.Section{ID: fmt.Sprint("chapter-", i), Title: fmt.Sprint("saved heading ", i), Blocks: []domain.Block{{ID: fmt.Sprint("block-", i), Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: strings.Repeat("saved body ", 1200)}}}}})
	}
	if _, err = s.engine.Store.SaveArticleDraft(ctx, s.claim, "large-saved-draft", nil, saved.Version, draft); err != nil {
		t.Fatal(err)
	}
	p := articleLoopCall{Mode: "author", Sources: []string{"t25"}}
	messages, err := s.loopInput(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	input := ""
	for _, message := range messages {
		input += message.Content
	}
	for _, expected := range []string{"saved heading 0", "saved heading 1", `"draft_context_complete":true`} {
		if !strings.Contains(input, expected) {
			t.Fatalf("saved draft was hidden by a partial source range: %s", expected)
		}
	}
	s.cp.ArticleLoop.ReadSources = s.loopAllSourceAliases()
	tools, err := s.loopCallTools(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range tools {
		if info.Name == "read_evidence" || info.Name == "read_article_state" {
			t.Fatalf("redundant %s offered after complete source delivery and full saved draft input", info.Name)
		}
	}
}

func TestDeliveryContinuationFollowsSavedCitationRange(t *testing.T) {
	s := &session{transcript: []domain.Segment{}}
	for i := range 30 {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("source-", i)})
	}
	a := domain.Article{Citations: map[string]domain.Citation{"c": {SegmentIDs: []string{"source-9"}}}, Sections: []domain.Section{{Blocks: []domain.Block{{Type: "paragraph", Content: []domain.Inline{{Type: "citation", CitationID: "c"}}}}}}}
	got := s.deliveryContinuation(a)
	if len(got) != 23 || got[0] != "t8" || got[len(got)-1] != "t30" {
		t.Fatalf("continuation lost its overlap or tail: %v", got)
	}
}

func TestDeliveryMalformedJSONRecoveryNarrowsWorkWithoutDroppingSavedDraft(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model call expected") }, domain.CurrentArticleDelivery)
	s.profile.InputCharsLimit, s.profile.ContextTokens = 140000, 0
	s.cp.ArticleLoop.Budget.InputCharsLimit, s.cp.ArticleLoop.Budget.ContextTokens = 140000, 0
	s.transcript = nil
	for i := range 800 {
		s.transcript = append(s.transcript, domain.Segment{ID: fmt.Sprint("dense-", i), Text: "Unchanged original.", StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000), Origin: "subtitle"})
	}
	ctx := context.Background()
	saved, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft.Article.Sections = []domain.Section{{ID: "saved", Title: "Keep this complete chapter", Blocks: []domain.Block{{ID: "saved-body", Type: "paragraph", Content: []domain.Inline{{Type: "text", Text: "Keep this complete body."}}}}}}
	if _, err = s.engine.Store.SaveArticleDraft(ctx, s.claim, "recovery-existing", nil, saved.Version, draft); err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop.Failures["author_envelope"] = 1
	s.cp.ArticleLoop.Feedback = loopAuthorFailureFeedback("envelope", fmt.Errorf("unexpected EOF"), loopSaveTool, []byte(`{"sections":[{"content":[{"text":"damaged old template`))
	p, messages, _, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sources) == 0 || len(p.Sources) > 96 || len(p.Frames) != 0 {
		t.Fatalf("malformed output repeated the same large source/pixel workload: sources=%d pixels=%d", len(p.Sources), len(p.Frames))
	}
	input := ""
	for _, message := range messages {
		input += message.Content
	}
	if !strings.Contains(input, "Keep this complete body.") || strings.Contains(input, "damaged old template") || !strings.Contains(input, "delivery_recovery") {
		t.Fatal("recovery lost the authoritative body or replayed damaged JSON")
	}
	if s.cp.Calls != 0 || len(s.cp.ArticleLoop.ReadSources) != 0 || p.OutputTokens != 8192 || len(s.transcript) != 800 {
		t.Fatal("recovery changed budgets or invented/dropped source delivery")
	}
}

func TestDeliveryQuoteWordingDoesNotDiscardChapter(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := loopGoodWrite()
		in.Sections[0].Content[0].Quote = domain.Ptr("An approximate rendering that is not a verbatim source quote.")
		in.Sections[0].Content[0].Text = nil
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.CurrentArticleDelivery)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil || a.Sections[0].Blocks[0].Type != "paragraph" || !strings.Contains(domain.BlockText(a.Sections[0].Blocks[0]), "approximate rendering") {
		t.Fatal("approximate wording was discarded or certified as a quote", err)
	}
	if len(a.Limitations) < 2 {
		t.Fatal("advisory wording limitation missing")
	}
}

func TestDeliveryOptionalFigureDoesNotDiscardProse(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = loopPayload(t, r)
		in := loopGoodWrite()
		in.Sections[0].Content[1].Figure.VisibleFacts = nil
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.CurrentArticleDelivery)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, _ := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil || len(a.Sections[0].Blocks) != 1 || a.Sections[0].Blocks[0].Type != "paragraph" || len(a.Limitations) < 2 {
		t.Fatal("optional image blocked supported prose or produced fictional pixel facts", err)
	}
}
