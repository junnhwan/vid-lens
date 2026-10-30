package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleQuestionsPublishFirstAndSubmitOnlyOnce(t *testing.T) {
	for _, mode := range []string{"ready", "unavailable", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var sectionID string
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var input reviewWireRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					return
				}
				calls.Add(1)
				if len(input.Tools) != 1 || input.Tools[0].Function.Name != articleQuestionsTool {
					writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
					return
				}
				if mode == "unavailable" {
					http.Error(w, "fixture unavailable", 502)
					return
				}
				if sectionID == "" {
					t.Error("questions blocked article publication")
				}
				if !strings.Contains(string(domain.JSON(input.Messages)), sectionID) {
					t.Error("questions lack article-specific navigation")
				}
				if input.MaxTokens > 2048 {
					t.Error("unbounded auxiliary output")
				}
				writeReviewWireArgs(w, articleQuestionsTool, domain.SuggestedQuestions{Items: []domain.SuggestedQuestion{
					{Question: "这段示例如何区分可见事实与作者的推断？", SectionID: sectionID},
					{Question: "解释图中信息时，需要保留哪些适用条件？", SectionID: sectionID},
					{Question: "哪些关键说法值得回到视频中进一步核对？"},
				}})
			}, domain.ArticleDeliveryV1)
			ctx := context.Background()
			if mode != "legacy" {
				s.cp.ArticleQuestionsPolicy = domain.ArticleQuestionsPolicy
			}
			if err := s.persist(ctx, nil); err != nil {
				t.Fatal(err)
			}
			frozen, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.claim.Run = frozen
			if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
				t.Fatal(err)
			}
			var articleID, request, owner string
			if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT article_id,id,session_id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&articleID, &request, &owner); err != nil {
				t.Fatal(err)
			}
			a, err := s.engine.Store.PublishedArticle(ctx, articleID)
			if err != nil {
				t.Fatal(err)
			}
			sectionID = a.Sections[0].ID
			before := domain.ContentHash(a)
			pending, err := s.engine.Store.PendingArticleQuestions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "legacy" {
				if len(pending) != 0 {
					t.Fatal("old contract silently scheduled an extra paid call")
				}
				return
			}
			if len(pending) != 1 || calls.Load() != 1 {
				t.Fatal("article awaited suggestions or queued duplicates", pending, calls.Load())
			}
			if _, err = s.engine.Store.ArticleQuestionSuggestions(ctx, "other-browser", request); err == nil {
				t.Fatal("suggestions bypassed private request access")
			}
			q, err := s.engine.Store.ClaimArticleQuestions(ctx, articleID)
			if err != nil || q == nil {
				t.Fatal(err)
			}
			if err = s.engine.RunArticleQuestions(ctx, *q); err != nil {
				t.Fatal(err)
			}
			view, err := s.engine.Store.ArticleQuestionSuggestions(ctx, owner, request)
			if err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if mode == "unavailable" {
				want = "incomplete"
			}
			if view["status"] != want {
				t.Fatal("incorrect question preparation state", view)
			}
			if err = s.engine.RunArticleQuestions(ctx, *q); err != nil {
				t.Fatal(err)
			}
			if _, err = s.engine.Store.Article(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			if _, err = s.engine.Store.ArticleQuestionSuggestions(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatal("read/redelivery started another model submission", calls.Load())
			}
			a, err = s.engine.Store.PublishedArticle(ctx, articleID)
			if err != nil || domain.ContentHash(a) != before {
				t.Fatal("suggestions changed the immutable article", err)
			}
		})
	}
}

func TestArticleQuestionsRejectDuplicateOrForeignSections(t *testing.T) {
	a := domain.Article{Sections: []domain.Section{{ID: "owned"}}}
	valid := domain.SuggestedQuestions{Items: []domain.SuggestedQuestion{{Question: "为什么应当区分观察事实与推断？", SectionID: "owned"}, {Question: "这段方法有哪些需要注意的适用条件？"}, {Question: "哪些信息还需要回到原片进一步核对？"}}}
	if err := validateSuggestedQuestions(valid, a); err != nil {
		t.Fatal(err)
	}
	valid.Items[1] = valid.Items[0]
	if validateSuggestedQuestions(valid, a) == nil {
		t.Fatal("duplicate questions accepted")
	}
	valid.Items[1] = domain.SuggestedQuestion{Question: "这段方法有哪些需要注意的适用条件？", SectionID: "another-video"}
	if validateSuggestedQuestions(valid, a) == nil {
		t.Fatal("foreign section accepted")
	}
}
