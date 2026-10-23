package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopScopedReviewDeliversEveryCitedOriginal(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	})
	ctx := context.Background()
	if err := s.engine.Run(ctx, s.claim, s.profile); err == nil {
		t.Fatal("预期在独立复核中中断")
	}
	s = reloadLoopSession(t, s)
	whole := s.cp.ArticleLoop.Scopes[0]
	if err := s.recoverLoopFailure(ctx, articleLoopCall{Mode: "review", Scope: &whole}, &domain.WorkError{Code: "MODEL_RESPONSE_BUDGET_EXHAUSTED"}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range s.cp.ArticleLoop.Scopes {
		if scope.Global {
			continue
		}
		call := articleLoopCall{Mode: "review", Scope: &scope, Frames: scope.FrameIDs}
		for i, original := range s.transcript {
			if slices.Contains(scope.SourceIDs, original.ID) {
				call.Sources = append(call.Sources, s.loopAllSourceAliases()[i])
			}
		}
		messages, err := s.loopInput(ctx, call)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Candidate domain.ArticleReading `json:"candidate"`
			Originals []loopOriginal        `json:"originals"`
			Bindings  map[string]struct {
				Originals []string `json:"originals"`
			} `json:"citation_bindings"`
		}
		for _, message := range messages {
			_ = json.Unmarshal([]byte(message.Content), &p)
		}
		if len(p.Candidate.Sections) == 0 {
			t.Fatal("缺少实际正文")
		}
		delivered := map[string]bool{}
		for _, original := range p.Originals {
			delivered[original.Alias] = true
		}
		for citation, binding := range p.Bindings {
			for _, original := range binding.Originals {
				if !delivered[original] {
					t.Errorf("分段复核展示引用 %s，却未交付其原文 %s", citation, original)
				}
			}
		}
	}
}
