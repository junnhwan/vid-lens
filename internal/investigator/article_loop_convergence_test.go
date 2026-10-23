package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopRepairBudgetDoesNotResetOnSavedWording(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		n := calls.Add(1)
		if p["scope"] != nil {
			report := loopGoodReport(t, p)
			var scope domain.ArticleReviewScope
			_ = json.Unmarshal(p["scope"], &scope)
			report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: scope.SectionIDs[0] + "_b1", EvidenceIDs: []string{"t1"}, Problem: "同一实质错误仍未解决", Correction: "按实际原文修正"}}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		in := loopGoodWrite()
		text := *in.Sections[0].Content[0].Text + fmt.Sprint(n)
		in.Sections[0].Content[0].Text = &text
		_ = json.Unmarshal(p["draft_version"], &in.ExpectedVersion)
		if in.ExpectedVersion > 0 {
			var current loopSaveInput
			_ = json.Unmarshal(p["current_article"], &current)
			in.Sections[0].Key = ""
			in.Sections[0].SectionID = current.Sections[0].SectionID
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_REPAIR_LIMIT" || calls.Load() != 6 {
		t.Fatalf("反复保存措辞未按三轮停止：calls=%d err=%v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopInvalidReviewStopsAfterOneCorrection(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		report := loopGoodReport(t, p)
		report.Complete = false
		report.Unassessed = []string{"仍未完成"}
		writeReviewWireArgs(w, loopReviewTool, report)
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_REVIEW_PROTOCOL_FAILED" || calls.Load() != 3 {
		t.Fatalf("同一无效报告反复核验：calls=%d err=%v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopWrongReviewToolCannotAvoidProtocolStop(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		writeReviewWireArgs(w, "not_a_review_tool", loopGoodReport(t, p))
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_REVIEW_PROTOCOL_FAILED" || calls.Load() != 3 {
		t.Fatalf("错误工具绕过无效报告停止边界：calls=%d err=%v", calls.Load(), err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopBlockRevisionIsAtomicAndReplayable(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if loopPayload(t, r)["scope"] != nil {
			w.WriteHeader(503)
			return
		}
		in := loopGoodWrite()
		text := "另外一章独立事实，完整保留原文关系。"
		in.Sections = append(in.Sections, loopSection{Key: "untouched", Title: "未改章节", Content: []loopContent{{Text: &text, Sources: []string{"t2"}}}})
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	ctx := context.Background()
	if s.engine.Run(ctx, s.claim, s.profile) == nil {
		t.Fatal("预期复核中断")
	}
	s = reloadLoopSession(t, s)
	_, before, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	text := "根据真实来源修正的局部正文，不修改其它段落或章节。"
	in := loopPatchInput{ExpectedVersion: 1, Replacements: []loopBlockReplacement{{BlockID: before.Article.Sections[0].Blocks[0].ID, Content: loopContent{Text: &text, Sources: []string{"t1", "t25"}}}}}
	for i := 0; i < 2; i++ {
		result, _, err := s.patchLoopBlocks(ctx, "patch-test", in)
		if err != nil {
			t.Fatal(err)
		}
		if result.(map[string]any)["version"] != 2 {
			t.Fatalf("重放产生重复版本：%v", result)
		}
	}
	_, after, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if domain.ContentHash(before.Article.Sections[1]) != domain.ContentHash(after.Article.Sections[1]) || domain.BlockText(after.Article.Sections[0].Blocks[0]) != text {
		t.Fatal("局部修稿丢失未改正文")
	}
	bad := loopPatchInput{ExpectedVersion: 2, Replacements: []loopBlockReplacement{{BlockID: after.Article.Sections[0].Blocks[0].ID, Content: loopContent{Text: &text, Sources: []string{"t1"}}}, {BlockID: after.Article.Sections[1].Blocks[0].ID, Content: loopContent{Text: &text, Sources: []string{"t999"}}}}}
	result, progress, err := s.patchLoopBlocks(ctx, "bad-patch", bad)
	if err != nil || progress || result.(map[string]any)["error"] == nil {
		t.Fatalf("非法局部修稿未整体拒绝：%v %v", result, err)
	}
	saved, unchanged, err := s.loadLoopDraft(ctx)
	if err != nil || saved.Version != 2 || domain.ArticleHash(unchanged.Article) != domain.ArticleHash(after.Article) {
		t.Fatal("非法局部修稿部分落库")
	}
}

func TestArticleLoopReusesOnlyUnchangedReviewMaterialAndChecksCurrentWhole(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) { testLoopReviewReuse(t, corrupt) })
	}
}

func testLoopReviewReuse(t *testing.T, corrupt bool) {
	var calls atomic.Int32
	var secondReviews atomic.Int32
	var firstReviews atomic.Int32
	var s *session
	s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil {
			report := loopGoodReport(t, p)
			if p["revision_delta"] != nil {
				secondReviews.Add(1)
				var delta domain.ArticleReviewDelta
				_ = json.Unmarshal(p["revision_delta"], &delta)
				if corrupt {
					_, err := s.engine.Store.Pool.Exec(context.Background(), `UPDATE run_operations SET result=jsonb_set(result,'{report,topics,0,topic}',to_jsonb($3::text)) WHERE run_id=$1 AND operation_key=$2`, s.claim.Run.ID, delta.ReviewKey, "未由原核验产生的伪造主题")
					if err != nil {
						t.Error(err)
					}
				}
				var reading domain.ArticleReading
				_ = json.Unmarshal(p["candidate"], &reading)
				if len(reading.Sections) != 2 {
					t.Error("全文关系核对没有实际完整正文")
				}
				var originals []loopOriginal
				_ = json.Unmarshal(p["originals"], &originals)
				if len(originals) != 25 || len(delta.AffectedBlockIDs) != 1 || delta.AffectedBlockIDs[0] != reading.Sections[1].Blocks[0].ID {
					t.Errorf("差异复核丢失完整来源或重审未改事实：%+v originals=%d", delta, len(originals))
				}
			} else {
				var scope domain.ArticleReviewScope
				_ = json.Unmarshal(p["scope"], &scope)
				var reading domain.ArticleReading
				_ = json.Unmarshal(p["candidate"], &reading)
				if scope.Global {
					firstReviews.Add(1)
					if len(reading.Sections) != 2 {
						t.Fatal("综合核验未收到完整正文")
					}
					report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: reading.Sections[1].Blocks[0].ID, EvidenceIDs: []string{"t13"}, Problem: "第二章的合成事实错误", Correction: "只修正第二章正文"}}
				} else {
					t.Error("容量允许时仍拆成会重复审问旧事实的局部复核")
				}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		var version int
		_ = json.Unmarshal(p["draft_version"], &version)
		if version == 0 {
			in := loopGoodWrite()
			in.Sections[0].Content[0].Sources = nil
			for i := 1; i <= 12; i++ {
				in.Sections[0].Content[0].Sources = append(in.Sections[0].Content[0].Sources, fmt.Sprint("t", i))
			}
			text := "后半段独立合成事实，保留全部必要条件与结论。"
			sources := []string{}
			for i := 13; i <= 25; i++ {
				sources = append(sources, fmt.Sprint("t", i))
			}
			in.Sections = append(in.Sections, loopSection{Key: "second", Title: "第二部分", Content: []loopContent{{Text: &text, Sources: sources}}})
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		var locations map[string][]string
		_ = json.Unmarshal(p["canonical_locations"], &locations)
		var current loopSaveInput
		_ = json.Unmarshal(p["current_article"], &current)
		text := "后半段已按原文修正的独立合成事实，保留全部必要条件与结论。"
		sources := []string{}
		for i := 13; i <= 25; i++ {
			sources = append(sources, fmt.Sprint("t", i))
		}
		writeReviewWireArgs(w, loopPatchTool, loopPatchInput{ExpectedVersion: version, Finish: true, Replacements: []loopBlockReplacement{{BlockID: locations[current.Sections[0].SectionID][0], Content: loopContent{Text: &text, Sources: sources}}}})
	})
	ctx := context.Background()
	for _, original := range s.transcript {
		if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE transcript_segments SET text=$2 WHERE id=$1", original.ID, strings.Repeat("这是独立的合成原文事实与必要条件。", 55)); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	s.transcript, err = s.engine.Store.Transcript(ctx, s.snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = s.engine.Run(ctx, s.claim, s.profile)
	if corrupt {
		var problem *domain.Problem
		if !errors.As(err, &problem) || problem.Code != "ARTICLE_APPROVAL_REQUIRED" {
			t.Fatalf("发布事务未拒绝篡改的局部复用报告：%v", err)
		}
		assertNoLoopPublication(t, s)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if calls.Load() != 4 || firstReviews.Load() != 1 || secondReviews.Load() != 1 {
		t.Fatalf("局部修改重复全文事实核验：calls=%d first=%d second=%d", calls.Load(), firstReviews.Load(), secondReviews.Load())
	}
	var reused int
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='article_loop_review' AND result->'delta' IS NOT NULL", s.claim.Run.ID).Scan(&reused); err != nil || reused != 1 {
		t.Fatalf("缺少绑定新稿的复用证明：%d %v", reused, err)
	}
	// 成功发布已经经过真实 Store 事务；不能只依据 fake reviewer 的完成标签。
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || run.Status != "succeeded" || run.ResultID == nil {
		t.Fatal("缺少不可变文章与真实终态")
	}
	if !slices.ContainsFunc(s.cp.ArticleLoop.ReviewKeys, func(key string) bool { return key != "" }) {
		t.Fatal("核验记录丢失")
	}
}

func TestArticleLoopContextNeighborsDoNotExpandOnReplay(t *testing.T) {
	s := &session{transcript: []domain.Segment{{ID: "one"}, {ID: "two"}, {ID: "three"}, {ID: "four"}}, cp: checkpoint{ArticleLoop: &articleLoopState{ReviewRevision: domain.ArticleConvergenceReviewV1}}}
	a := domain.Article{Sections: []domain.Section{{ID: "chapter", Blocks: []domain.Block{{CitationIDs: []string{"ref"}}}}}, Citations: map[string]domain.Citation{"ref": {SegmentIDs: []string{"two"}}}}
	scope := domain.ArticleReviewScope{SectionIDs: []string{"chapter"}, SourceIDs: []string{"two"}}
	first := s.loopScopeContext(a, scope)
	second := s.loopScopeContext(a, first)
	if domain.ContentHash(first) != domain.ContentHash(second) || !slices.Equal(first.ContextSourceIDs, []string{"one", "three"}) {
		t.Fatal("恢复或预检把相邻上下文递归扩大", first, second)
	}
}

func TestArticleLoopCannotRepeatAnUnshrinkableReview(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if loopPayload(t, r)["scope"] != nil {
			w.WriteHeader(503)
			return
		}
		in := loopGoodWrite()
		in.Sections[0].Content[0].Sources = nil
		for i := 1; i <= 25; i++ {
			in.Sections[0].Content[0].Sources = append(in.Sections[0].Content[0].Sources, fmt.Sprint("t", i))
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	ctx := context.Background()
	if s.engine.Run(ctx, s.claim, s.profile) == nil {
		t.Fatal("预期复核中断")
	}
	s = reloadLoopSession(t, s)
	scope := s.cp.ArticleLoop.Scopes[0]
	calls := s.cp.Calls
	err := s.recoverLoopFailure(ctx, articleLoopCall{Mode: "review", Scope: &scope}, &domain.WorkError{Code: "MODEL_RESPONSE_BUDGET_EXHAUSTED"})
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_CAPACITY_EXCEEDED" || s.cp.Calls != calls {
		t.Fatalf("无法缩小的完整章节仍在重复审核：%v", err)
	}
	assertNoLoopPublication(t, s)
}
