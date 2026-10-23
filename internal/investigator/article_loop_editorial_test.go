package investigator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

// Reproduce the captured replace+delete conflict. Rejection must preserve all
// edits, so correcting that action can save a repair and deletion together.
func TestArticleLoopConflictingQuoteRemovalRejectsWholeRepair(t *testing.T) {
	for _, revision := range []string{domain.ArticleConvergenceReviewV4, domain.ArticleConvergenceReviewV5} {
		t.Run(revision, func(t *testing.T) {
			s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("no model call expected") })
			ctx := context.Background()
			s.cp.ArticleLoop = &articleLoopState{ReviewRevision: revision, ReadSources: s.loopAllSourceAliases(), Failures: map[string]int{}}
			in := loopGoodWrite()
			in.Finish = false
			quote := s.transcript[0].Text
			in.Sections[0].Content = []loopContent{{Text: &quote, Sources: []string{"t1"}}}
			contextText := "另一主题的合成说明，删除重复引语时必须保留。"
			in.Sections = append(in.Sections, loopSection{Key: "context", Title: "另一主题", Content: []loopContent{
				{Text: &contextText, Sources: []string{"t2"}}, {Quote: &quote, Sources: []string{"t1"}},
			}})
			if _, _, err := s.saveLoopSections(ctx, "seed", in); err != nil {
				t.Fatal(err)
			}
			_, before, err := s.loadLoopDraft(ctx)
			if err != nil {
				t.Fatal(err)
			}
			first := before.Article.Sections[0].Blocks[0].ID
			removed := before.Article.Sections[1].Blocks[1].ID
			fixed := quote + " 修订后保留完整原意。"
			bad := loopPatchInput{ExpectedVersion: 1, RemoveBlockIDs: []string{removed}, Replacements: []loopBlockReplacement{
				{BlockID: first, Content: loopContent{Text: &fixed, Sources: []string{"t1", "t25"}}},
				{BlockID: removed, Content: loopContent{Quote: &quote, Sources: []string{"t1"}}},
			}}
			result, progress, err := s.patchLoopBlocks(ctx, "conflicting", bad)
			if err != nil || progress || !strings.Contains(fmt.Sprint(result), "conflicting block edit") {
				t.Fatalf("conflicting removal was not rejected: %v %v", result, err)
			}
			saved, unchanged, err := s.loadLoopDraft(ctx)
			if err != nil || saved.Version != 1 || domain.ArticleHash(unchanged.Article) != domain.ArticleHash(before.Article) {
				t.Fatal("rejected action partially saved a different repair", err)
			}
			good := bad
			good.Replacements = good.Replacements[:1]
			if _, progress, err = s.patchLoopBlocks(ctx, "corrected", good); err != nil || !progress {
				t.Fatal("corrected repair did not save", progress, err)
			}
			saved, after, err := s.loadLoopDraft(ctx)
			if err != nil || saved.Version != 2 || len(after.Article.Sections[1].Blocks) != 1 || domain.BlockText(after.Article.Sections[0].Blocks[0]) != fixed || domain.ContentHash(after.Article.Sections[1].Blocks[0]) != domain.ContentHash(before.Article.Sections[1].Blocks[0]) {
				t.Fatal("repair lost retained content or repeated the write", err)
			}
			// Guidance is not an approval, and the historical policy stays distinct.
			assertNoLoopPublication(t, s)
			if s.cp.ArticleLoop.ReviewRevision != revision {
				t.Fatal("old checkpoint silently adopted a new review revision")
			}
			if domain.ArticleReviewPolicyIdentity(s.spec, domain.ArticleLoopV2, domain.ArticleConvergenceReviewV4) == domain.ArticleReviewPolicyIdentity(s.spec, domain.ArticleLoopV2, domain.ArticleConvergenceReviewV5) {
				t.Fatal("new guidance reused the historical approval identity")
			}
		})
	}
}
