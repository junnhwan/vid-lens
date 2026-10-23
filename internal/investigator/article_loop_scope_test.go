package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopReviewSplitNarrowsChaptersAndRetainsLearnedWorkSize(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		in := loopGoodWrite()
		in.Sections[0].Content[0].Sources = []string{"t1"}
		text := "后半段的原文包含另一项独立的来源事实，这一章保留它的解释与适用条件，避免把前半段的说明误当成全部来源内容。"
		background := "这里是明确标注的背景解释，不能冒充原视频事实；没有原文引用的背景段落仍必须出现在独立核验中，不能因为按来源拆分而丢失。"
		in.Sections = append(in.Sections,
			loopSection{Key: "later", Title: "后半段", Content: []loopContent{{Text: &text, Sources: []string{"t25"}}}},
			loopSection{Key: "background", Title: "背景说明", Content: []loopContent{{Background: &background}}})
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("expected interrupted review")
	}
	s = reloadLoopSession(t, s)
	l := s.cp.ArticleLoop
	// 保留既有空修订检查点按原文数量学习范围的兼容行为。
	l.ReviewRevision = ""
	whole := l.Scopes[0]
	hash, calls := l.CandidateHash, s.cp.Calls
	if err := s.recoverLoopFailure(ctx, articleLoopCall{Mode: "review", Scope: &whole}, &domain.WorkError{Code: "MODEL_RESPONSE_BUDGET_EXHAUSTED"}); err != nil {
		t.Fatal(err)
	}
	if len(l.Scopes) != 3 || !l.Scopes[2].Global || l.CandidateHash != hash || s.cp.Calls != calls {
		t.Fatal("split changed the candidate, spent a call, or lost the global obligation")
	}
	left, right := l.Scopes[0], l.Scopes[1]
	if len(left.SectionIDs) != 2 || len(right.SectionIDs) != 2 || len(left.FrameIDs) != 1 || len(right.FrameIDs) != 0 {
		t.Fatalf("source split still carries unrelated full chapters/pixels: left=%+v right=%+v", left, right)
	}
	p := articleLoopCall{Mode: "review", Scope: &right, Frames: right.FrameIDs}
	for i, original := range s.transcript {
		if slices.Contains(right.SourceIDs, original.ID) {
			p.Sources = append(p.Sources, s.loopAllSourceAliases()[i])
		}
	}
	input, err := s.loopInput(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range input {
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(message.Content), &payload) == nil && payload["candidate"] != nil {
			var candidate domain.ArticleReading
			if err := json.Unmarshal(payload["candidate"], &candidate); err != nil {
				t.Fatal(err)
			}
			found = true
			if len(candidate.Sections) != 2 || len(candidate.Limitations) != 0 {
				t.Fatal("actual review input retained unrelated chapters or limitations")
			}
		}
	}
	if !found {
		t.Fatal("actual scoped reading input missing")
	}
	for _, ids := range [][3][]string{{whole.SourceIDs, left.SourceIDs, right.SourceIDs}, {whole.SectionIDs, left.SectionIDs, right.SectionIDs}, {whole.FrameIDs, left.FrameIDs, right.FrameIDs}} {
		for _, id := range ids[0] {
			if !slices.Contains(ids[1], id) && !slices.Contains(ids[2], id) {
				t.Fatalf("split lost required identity %s", id)
			}
		}
	}
	// Persist/reload the learned work size. New candidate planning may reuse a
	// range size, but never a previous candidate's approval or coverage receipt.
	var restored articleLoopState
	if err := json.Unmarshal(domain.JSON(l), &restored); err != nil {
		t.Fatal(err)
	}
	s.cp.ArticleLoop = &restored
	_, draft, err := s.loadLoopDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := s.planLoopReview(ctx, draft.Article)
	if err != nil || len(planned) < 3 {
		t.Fatalf("next revision retried the oversized whole review: scopes=%+v err=%v", planned, err)
	}
	for _, scope := range planned {
		if len(scope.SourceIDs) > (len(whole.SourceIDs)+1)/2 {
			t.Fatal("lost measured review work limit")
		}
	}
}
