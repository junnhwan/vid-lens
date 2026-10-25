package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleFrozenResponsePolicyCannotChangeOnReplay(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("policy freeze must not send") }, domain.ArticleConvergenceReviewV6)
	ctx := context.Background()
	p, input, infos, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	infos, err = s.loopCallTools(*p)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"messages": stripPixels(input), "budget": s.articleCallBudget, "tools_hash": digest(domain.JSON(infos)), "policy_revision": domain.ArticleLoopPolicy, "parallel_tool_calls": false}
	first, err := s.freezeModelInput(ctx, "model:0", input, args, s.articleCallBudget, infos)
	if err != nil || first.ResponsePolicy == nil || first.ResponsePolicy.TotalMS != 600000 {
		t.Fatal("response budget not frozen", first.ResponsePolicy, err)
	}
	if _, err = s.freezeModelInput(ctx, "model:0", input, args, s.articleCallBudget, infos); err != nil {
		t.Fatal("identical input did not replay", err)
	}
	s.cp.ArticleLoop.Budget.ResponsePolicy = nil
	if _, err = s.freezeModelInput(ctx, "model:0", input, args, s.articleCallBudget, infos); err == nil {
		t.Fatal("frozen policy was silently downgraded")
	}
	_ = p
}

func TestArticleKnownCallBudgetNeverResendsTheFrozenInput(t *testing.T) {
	s := loopFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("known budget outcome was sent again") }, domain.ArticleConvergenceReviewV6)
	ctx := context.Background()
	p, input, infos, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	infos, err = s.loopCallTools(*p)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"messages": stripPixels(input), "budget": s.articleCallBudget, "tools_hash": digest(domain.JSON(infos)), "policy_revision": domain.ArticleLoopPolicy, "parallel_tool_calls": false}
	if _, err = s.freezeModelInput(ctx, "model:0", input, args, s.articleCallBudget, infos); err != nil {
		t.Fatal(err)
	}
	var replayArgs any
	if err = json.Unmarshal(domain.JSON(args), &replayArgs); err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, "model:0", "model", replayArgs, false); err != nil {
		t.Fatal(err)
	}
	op, err := s.engine.Store.BeginModelSubmission(ctx, s.claim, "model:0", replayArgs, s.claim.Run.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.RecordSubmission(ctx, s.claim, "model:0", op.Submissions, map[string]any{"failure_code": "MODEL_CALL_BUDGET_EXHAUSTED"}); err != nil {
		t.Fatal(err)
	}
	s.articleCall = p
	bound, err := (&meteredModel{session: s}).WithTools(infos)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = bound.Generate(ctx, nil)
		var work *domain.WorkError
		if !errors.As(err, &work) || work.Code != "MODEL_CALL_BUDGET_EXHAUSTED" || work.Transient {
			t.Fatal("known budget did not survive replay", err)
		}
	}
	stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if err != nil || stats.Submissions != 1 {
		t.Fatal("unknown send was paid again", stats, err)
	}
}

func TestArticlePhysicalSubmissionHonorsTheFrozenSendCeiling(t *testing.T) {
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		loopPayload(t, r)
		writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
	}, domain.ArticleConvergenceReviewV6)
	ctx := context.Background()
	p, input, infos, err := s.prepareLoopCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	infos, err = s.loopCallTools(*p)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"messages": stripPixels(input), "budget": s.articleCallBudget, "tools_hash": digest(domain.JSON(infos)), "policy_revision": domain.ArticleLoopPolicy, "parallel_tool_calls": false}
	s.cp.ArticleLoop.Budget.Calls, s.cp.ArticleLoop.Budget.Sends = 1, 1
	for slot := 0; slot < 2; slot++ {
		s.cp.Calls = slot
		p.Slot = slot
		s.articleCall = p
		if _, err = s.freezeModelInput(ctx, fmt.Sprint("model:", slot), input, args, s.articleCallBudget, infos); err != nil {
			t.Fatal(err)
		}
		bound, e := (&meteredModel{session: s}).WithTools(infos)
		if e != nil {
			t.Fatal(e)
		}
		_, err = bound.Generate(ctx, nil)
		if slot == 0 && err != nil {
			t.Fatal(err)
		}
		if slot == 1 {
			var problem *domain.Problem
			if !errors.As(err, &problem) || problem.Code != "ARTICLE_BUDGET_EXHAUSTED" {
				t.Fatal("extra physical submission not rejected at transport boundary", err)
			}
		}
	}
	stats, e := s.engine.Store.ModelExecution(context.Background(), s.claim.Run.ID)
	if e != nil || stats.Submissions != 1 {
		t.Fatal("frozen send ceiling ignored", stats, e, err)
	}
	assertNoLoopPublication(t, s)
}
