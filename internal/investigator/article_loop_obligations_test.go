package investigator

import (
	"context"
	"encoding/json"
	"github.com/cloudwego/eino/schema"
	"net/http"
	"testing"
	"vidlens/internal/domain"
)

func TestArticleLoopCandidateFailureSurvivesReadOnlyState(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("no model expected") })
	ctx := context.Background()
	frames, err := s.engine.Store.RunFrames(ctx, s.claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop = &articleLoopState{ReviewRevision: domain.ArticleConvergenceReviewV4, Frames: []string{frames[0].ID}, SeenFrames: []string{frames[0].ID}, ReadSources: s.loopAllSourceAliases(), Failures: map[string]int{}}
	in := loopGoodWrite()
	in.Sections[0].Content = in.Sections[0].Content[:1]
	in.Sections[0].Content[0].Sources = []string{"t1", "f1"}
	if _, _, err = s.saveLoopSections(ctx, "seed", in); err != nil {
		t.Fatal(err)
	}
	if ok, e := s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: 1}); e != nil || ok {
		t.Fatal("unobserved visual claim passed", ok, e)
	}
	msg := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "read_article_state", Arguments: "{}"}}}}
	if _, err = s.applyLoopResponse(ctx, articleLoopCall{Mode: "author"}, msg); err != nil {
		t.Fatal(err)
	}
	messages, err := s.loopInput(ctx, articleLoopCall{Mode: "author", Sources: s.loopAllSourceAliases()})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		var p map[string]json.RawMessage
		if json.Unmarshal([]byte(message.Content), &p) == nil && p["candidate_validation"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("read-only state erased the unsatisfied candidate validation condition")
	}
	saved, before, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inPatch := loopPatchInput{ExpectedVersion: saved.Version, SectionObservations: []loopObservationReplacement{{SectionID: before.Article.Sections[0].ID, Observations: []loopPixelObservation{{Frame: "f1", VisibleFacts: []string{"实际交付的合成画面包含一个可见标记"}}}}}}
	if _, progress, e := s.patchLoopBlocks(ctx, "observe", inPatch); e != nil || !progress {
		t.Fatal("could not record text-citation observation without rewriting chapter", progress, e)
	}
	saved, after, err := s.loadLoopDraft(ctx)
	if err != nil || domain.ArticleHash(before.Article) != domain.ArticleHash(after.Article) {
		t.Fatal("observation-only repair changed prose", err)
	}
	if err = s.validateLoopArticle(ctx, after.Article, after); err != nil {
		t.Fatal("delivered observation failed validation", err)
	}
	s.cp.ArticleLoop.Budget.Calls = 20
	s.cp.ArticleLoop.OutputTokens = 2048
	if _, err = s.freezeLoopCandidate(ctx, loopFinishInput{ExpectedVersion: saved.Version}); err != nil {
		t.Fatal(err)
	}
	if len(s.cp.ArticleLoop.CandidateValidation) > 0 {
		t.Fatal("successful validation did not close the program condition")
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopReadOnlyEvidenceDoesNotConsumeRepairAttempts(t *testing.T) {
	calls, reviews, reads := 0, 0, 0
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls++
		if p["scope"] != nil {
			reviews++
			report := loopGoodReport(t, p)
			if reviews == 1 {
				var a domain.ArticleReading
				_ = json.Unmarshal(p["candidate"], &a)
				report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: a.Sections[0].Blocks[0].ID, EvidenceIDs: []string{"t1"}, Problem: "合成事实待修正", Correction: "按原文修改"}}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		var v int
		_ = json.Unmarshal(p["draft_version"], &v)
		if v == 0 {
			in := loopGoodWrite()
			in.Sections[0].Content = in.Sections[0].Content[:1]
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		reads++
		switch reads {
		case 1:
			writeReviewWireArgs(w, "read_article_state", loopStateInput{})
			return
		case 2:
			writeReviewWireArgs(w, "read_evidence", loopReadInput{SourceIDs: []string{"t1"}})
			return
		case 3:
			writeReviewWireArgs(w, "view_frames", loopViewInput{FrameIDs: []string{"f1"}, Question: "核对已交付帧中的实际文字"})
			return
		}
		var a loopSaveInput
		_ = json.Unmarshal(p["current_article"], &a)
		var locations map[string][]string
		_ = json.Unmarshal(p["canonical_locations"], &locations)
		text := "已经根据原文修正的合成事实，并保留结论的适用条件。"
		writeReviewWireArgs(w, loopPatchTool, loopPatchInput{ExpectedVersion: v, Finish: true, Replacements: []loopBlockReplacement{{BlockID: locations[a.Sections[0].SectionID][0], Content: loopContent{Text: &text, Sources: []string{"t1"}}}}})
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	if calls != 7 || reviews != 2 {
		t.Fatal("calls were not charged or review repeated", calls, reviews)
	}
}
