package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
	"vidlens/internal/store"
)

// A useful transcript must survive publication without decorative overview
// images. Exercise freeze, independent review, Publish and the immutable read.
func TestArticleLoopTextOnlyPublication(t *testing.T) {
	testArticleLoopTextOnlyPublication(t, false)
}

func TestArticleLoopToolCommentaryDoesNotReplaceValidation(t *testing.T) {
	testArticleLoopTextOnlyPublication(t, true)
}

func testArticleLoopTextOnlyPublication(t *testing.T, commentary bool) {
	t.Helper()
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		n := calls.Add(1)
		if commentary {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", domain.JSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "旁白声称已完成；不能以此跳过实际写入和独立核验。"}}}}))
		}
		if p["scope"] != nil {
			report := loopGoodReport(t, p)
			if len(report.VisualObservations) != 0 {
				t.Error("unused overview images reached text-only review")
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		if n > 1 {
			t.Errorf("text-only article rejected: %s", p["feedback"])
			writeReviewWireArgs(w, "stop_article", loopCannotCompleteInput{Reason: "test: text-only completion rejected", EvidenceIDs: []string{"t1"}})
			return
		}
		in := loopGoodWrite()
		in.Sections[0].Content = in.Sections[0].Content[:1]
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	run, err := s.engine.Store.Run(context.Background(), s.claim.Run.ID)
	if err != nil || run.Status != "succeeded" || run.ResultID == nil {
		t.Fatalf("not published: %+v %v", run, err)
	}
	a, err := s.engine.Store.PublishedArticle(context.Background(), *run.ResultID)
	if err != nil || len(a.Sections) != 1 || len(a.Sections[0].Blocks) != 1 || a.Sections[0].Blocks[0].Type != "paragraph" || len(a.Citations) < 2 || calls.Load() != 2 {
		t.Fatalf("text-only publication changed content or call count: %+v calls=%d err=%v", a, calls.Load(), err)
	}
}

func TestArticleLoopVisualSearchIsBoundedAndReloadable(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no provider expected") })
	ctx := context.Background()
	frames, err := s.engine.Store.RunFrames(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop = &articleLoopState{Budget: articleLoopBudget{Calls: 20}, ReviewReserve: 1, Frames: []string{frames[0].ID}}
	view := func(in loopViewInput) {
		t.Helper()
		msg := &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "view_frames", Arguments: string(domain.JSON(in))}}}}
		if _, e := s.applyLoopResponse(ctx, articleLoopCall{Mode: "author"}, msg); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		window := media.Window{StartMS: int64(i * 1000), EndMS: int64((i + 1) * 1000), Count: 1, Mode: "targeted"}
		key := "article-loop:inspect:" + store.Hash(window)
		if _, err = s.engine.Store.BeginOperation(ctx, s.claim, key, "inspect", nil, false); err != nil {
			t.Fatal(err)
		}
		if err = s.engine.Store.FinishOperation(ctx, s.claim, key, media.Inspection{FrameIDs: []string{frames[0].ID}}, "", nil); err != nil {
			t.Fatal(err)
		}
		view(loopViewInput{Question: "核对来源提及的板书", StartMS: window.StartMS, EndMS: window.EndMS, Count: 1})
	}
	// Persisted checkpoints must retain the cap, not start another search loop.
	raw := domain.JSON(s.cp.ArticleLoop)
	s.cp.ArticleLoop = &articleLoopState{}
	if err = json.Unmarshal(raw, s.cp.ArticleLoop); err != nil {
		t.Fatal(err)
	}
	view(loopViewInput{Question: "再找一张配图", StartMS: 2000, EndMS: 3000, Count: 1})
	if !strings.Contains(string(s.cp.ArticleLoop.Feedback), "visual_search_limit") {
		t.Fatalf("unbounded visual search: %s", s.cp.ArticleLoop.Feedback)
	}
	view(loopViewInput{Question: "核对已有画面", FrameIDs: []string{"f1"}})
	if strings.Contains(string(s.cp.ArticleLoop.Feedback), "error") || len(s.cp.ArticleLoop.NextFrames) != 1 {
		t.Fatal("cap blocked existing pixels", string(s.cp.ArticleLoop.Feedback))
	}
	view(loopViewInput{Question: "重复已有窗口", StartMS: 0, EndMS: 1000, Count: 1})
	if len(s.cp.ArticleLoop.InspectionKeys) != 2 || strings.Contains(string(s.cp.ArticleLoop.Feedback), "error") {
		t.Fatal("cached window consumed another search slot")
	}
}

func TestArticleLoopCombinedRevisionIsAtomic(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("no provider expected") })
	ctx := context.Background()
	s.cp.ArticleLoop = &articleLoopState{ReadSources: s.loopAllSourceAliases()}
	in := loopGoodWrite()
	in.Finish = false
	in.Sections[0].Content = in.Sections[0].Content[:1]
	second, third := in.Sections[0], in.Sections[0]
	second.Key, third.Key = "second", "third"
	in.Sections = append(in.Sections, second, third)
	if _, _, err := s.saveLoopSections(ctx, "seed", in); err != nil {
		t.Fatal(err)
	}
	saved, draft, err := s.loadLoopDraft(ctx)
	if err != nil || len(draft.Article.Sections) != 3 {
		t.Fatal("seed", err)
	}
	ids := []string{draft.Article.Sections[0].ID, draft.Article.Sections[1].ID, draft.Article.Sections[2].ID}
	replace := second
	replace.Key, replace.SectionID, replace.Title = "", ids[1], "修订后的章节"
	base := loopSaveInput{ExpectedVersion: saved.Version, Sections: []loopSection{replace}, DeleteIDs: []string{ids[0]}, Order: []string{ids[2], ids[1]}}
	for _, kind := range []string{"unknown_delete", "duplicate_order", "invalid_section", "delete_and_replace", "empty_title"} {
		bad := base
		switch kind {
		case "unknown_delete":
			bad.DeleteIDs = []string{"missing"}
		case "duplicate_order":
			bad.Order = []string{ids[1], ids[1]}
		case "invalid_section":
			bad.Sections = []loopSection{{SectionID: ids[1], Title: "broken"}}
		case "delete_and_replace":
			bad.DeleteIDs = []string{ids[1]}
			bad.Order = []string{ids[0], ids[2]}
		case "empty_title":
			bad.Title = domain.Ptr("")
		}
		result, progress, e := s.saveLoopSections(ctx, kind, bad)
		if e != nil || progress || result.(map[string]any)["error"] == nil {
			t.Fatalf("%s was not atomically rejected: %v %v", kind, result, e)
		}
		after, _, e := s.loadLoopDraft(ctx)
		if e != nil || after.Version != saved.Version || store.Hash(after.Data) != store.Hash(saved.Data) {
			t.Fatalf("%s partially saved", kind)
		}
	}
	result, progress, err := s.saveLoopSections(ctx, "combined", base)
	if err != nil || !progress || result.(map[string]any)["error"] != nil {
		t.Fatalf("valid combined revision rejected: %v %v", result, err)
	}
	after, final, err := s.loadLoopDraft(ctx)
	if err != nil || after.Version != saved.Version+1 || len(final.Article.Sections) != 2 || final.Article.Sections[0].ID != ids[2] || final.Article.Sections[1].Title != replace.Title {
		t.Fatalf("bad atomic result: %+v %v", final.Article, err)
	}
}
