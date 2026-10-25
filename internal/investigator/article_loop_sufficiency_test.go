package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

// Synthetic contracts, not a claim that the fixture model understands a video.
func sufficiencyPlan(t *testing.T, p map[string]json.RawMessage) map[string]any {
	t.Helper()
	var refs []string
	_ = json.Unmarshal(p["primary_source_ids"], &refs)
	return map[string]any{"complete": true, "requirements": []any{map[string]any{"requirement": "保留主要概念、操作前提与结尾条件", "evidence_refs": refs}}, "incidental": []any{}}
}

func sufficiencyReport(t *testing.T, p map[string]json.RawMessage) domain.ArticleReferenceReport {
	t.Helper()
	r := loopGoodReferenceReport(t, p)
	var questions []domain.ArticleCoverageQuestion
	var a domain.ArticleReading
	_ = json.Unmarshal(p["pending_coverage_questions"], &questions)
	_ = json.Unmarshal(p["candidate"], &a)
	for _, q := range questions {
		r.CoverageDecisions = append(r.CoverageDecisions, domain.ArticleCoverageDecision{QuestionID: q.ID, Outcome: "covered", EvidenceRefs: q.EvidenceRefs, BlockIDs: []string{a.Sections[0].Blocks[0].ID}, Explanation: "合成材料的必要条件在该正文块中，语义质量另做真实验收"})
	}
	return r
}

func freshSufficiencyFixture(t *testing.T, h http.HandlerFunc, revisions ...string) *session {
	s := loopFixture(t, h)
	s.initializeArticleLoop()
	// Frozen v10 fixtures keep their admitted integrated-review behavior.
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV10
	s.cp.ArticleLoop.LimitedReviewContext = false
	if len(revisions) > 0 {
		s.cp.ArticleLoop.ReviewRevision = revisions[0]
	}
	if err := s.persist(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return reloadLoopSession(t, s)
}

func TestArticleLoopSourceFirstRequirementsReachPublication(t *testing.T) {
	plans, reviews := 0, 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		var authority map[string]any
		_ = json.Unmarshal(p["evidence_authority"], &authority)
		if authority["external_fact_check"] != false || authority["continuous_video_observed"] != false || authority["unobserved_video_absence_claims"] != false {
			t.Error("call acquired unsupported evidence authority", authority)
		}
		if string(p["material_role"]) == `"source_requirements"` {
			plans++
			for _, key := range []string{"candidate", "current_article", "navigation", "prior_complete_review", "review_findings", "draft_version"} {
				if p[key] != nil {
					t.Errorf("source-first extraction saw candidate data: %s", key)
				}
			}
			writeReviewWireArgs(w, "plan_source_requirements", sufficiencyPlan(t, p))
			return
		}
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		reviews++
		if plans != 1 || string(p["pending_coverage_questions"]) == "[]" {
			t.Error("first review lacks source-derived obligations")
		}
		writeReviewWireArgs(w, loopReviewTool, sufficiencyReport(t, p))
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err, string(s.cp.ArticleLoop.Feedback))
	}
	if plans != 1 || reviews != 1 {
		t.Fatalf("source obligations were bypassed or regenerated: %d/%d", plans, reviews)
	}
}

func TestArticleLoopIncompleteSourceInventoryNeverReachesAuthor(t *testing.T) {
	calls := 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls++
		if string(p["material_role"]) != `"source_requirements"` {
			t.Error("invalid source inventory reached author or review")
		}
		report := sufficiencyPlan(t, p)
		var refs []string
		_ = json.Unmarshal(p["primary_source_ids"], &refs)
		report["requirements"].([]any)[0].(map[string]any)["evidence_refs"] = refs[:len(refs)-1]
		writeReviewWireArgs(w, loopSourceTool, report)
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_SOURCE_PLAN_FAILED" || calls != 2 {
		t.Fatalf("incomplete inventory escaped its stop: %d %v", calls, err)
	}
	assertNoLoopPublication(t, s)
}

func TestArticleLoopV7RetainsFrozenSourceReviewInput(t *testing.T) {
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
			return
		}
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		if p["source_plans"] == nil || p["source_inventory_receipts"] != nil {
			t.Error("v8 projection changed a frozen v7 input")
		}
		writeReviewWireArgs(w, loopReviewTool, sufficiencyReport(t, p))
	})
	s.cp.ArticleLoop.ReviewRevision = domain.ArticleConvergenceReviewV7
	if err := s.persist(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
}

func TestArticleLoopSourceInventoryCannotBeDroppedOrTamperedBeforePublish(t *testing.T) {
	for _, revision := range []string{domain.ArticleConvergenceReviewV10, domain.ArticleConvergenceReviewV11, domain.ArticleConvergenceReviewV12, domain.ArticleConvergenceReviewV13} {
		t.Run(revision, func(t *testing.T) {
			for _, corruption := range []string{"plan", "input", "decision"} {
				t.Run(corruption, func(t *testing.T) {
					var s *session
					s = freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
						p := loopPayload(t, r)
						if string(p["material_role"]) == `"source_requirements"` {
							writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
							return
						}
						if p["scope"] == nil {
							writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
							return
						}
						report := sufficiencyReport(t, p)
						var err error
						switch corruption {
						case "plan":
							_, err = s.engine.Store.Pool.Exec(context.Background(), `UPDATE run_operations SET result=jsonb_set(result,'{report,requirements,0,requirement}','"篡改来源义务"') WHERE run_id=$1 AND kind='article_source_plan'`, s.claim.Run.ID)
						case "input":
							_, err = s.engine.Store.Pool.Exec(context.Background(), `UPDATE run_operations SET result=jsonb_set(result,'{messages,1,content}','"{}"') WHERE run_id=$1 AND operation_key='input:model:0'`, s.claim.Run.ID)
						case "decision":
							report.CoverageDecisions = []domain.ArticleCoverageDecision{}
						}
						if err != nil {
							t.Error(err)
						}
						writeReviewWireArgs(w, loopReviewTool, report)
					}, revision)
					if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
						t.Fatal("corrupt source inventory approved")
					}
					assertNoLoopPublication(t, s)
				})
			}
		})
	}
}

func TestArticleLoopConfirmedClosureCanContinuePastThreeReviews(t *testing.T) {
	reviews := 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			writeReviewWireArgs(w, "plan_source_requirements", sufficiencyPlan(t, p))
			return
		}
		if p["scope"] != nil {
			reviews++
			report := sufficiencyReport(t, p)
			for i := range report.Resolutions {
				report.Resolutions[i].Outcome = "fixed"
				report.Resolutions[i].EvidenceRefs = nil
			}
			if reviews <= 3 {
				var a domain.ArticleReading
				_ = json.Unmarshal(p["candidate"], &a)
				report.Issues = []domain.ArticleReferenceIssue{{Severity: "blocking", AC: "AC04", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{fmt.Sprint("t", reviews)}, Problem: "独立复审确认上次问题已闭合，又发现不同引用缺口", Correction: "按实际原文补足对应引用"}}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		in := loopGoodWrite()
		_ = json.Unmarshal(p["draft_version"], &in.ExpectedVersion)
		if in.ExpectedVersion > 0 {
			var current loopSaveInput
			_ = json.Unmarshal(p["current_article"], &current)
			in.Sections[0].Key = ""
			in.Sections[0].SectionID = current.Sections[0].SectionID
			text := *in.Sections[0].Content[0].Text + strings.Repeat("已补充对应引用。", reviews)
			in.Sections[0].Content[0].Text = &text
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatalf("confirmed closure stopped after %d reviews: %v %s", reviews, err, s.cp.ArticleLoop.Feedback)
	}
	s = reloadLoopSession(t, s)
	if reviews != 4 || s.cp.Calls > 10 {
		t.Fatalf("unbounded convergence: %d/%d", reviews, s.cp.Calls)
	}
}

func TestArticleLoopSameDebtStopsDespiteChangedWording(t *testing.T) {
	reviews := 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			writeReviewWireArgs(w, "plan_source_requirements", sufficiencyPlan(t, p))
			return
		}
		if p["scope"] != nil {
			reviews++
			report := sufficiencyReport(t, p)
			for i := range report.Resolutions {
				report.Resolutions[i].Outcome = "remaining"
			}
			var a domain.ArticleReading
			_ = json.Unmarshal(p["candidate"], &a)
			report.Issues = []domain.ArticleReferenceIssue{{Severity: "blocking", AC: "AC03", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t1"}, Problem: fmt.Sprint("相同来源冲突仅换说法", reviews), Correction: "修复同一实际来源冲突"}}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		in := loopGoodWrite()
		_ = json.Unmarshal(p["draft_version"], &in.ExpectedVersion)
		if in.ExpectedVersion > 0 {
			var current loopSaveInput
			_ = json.Unmarshal(p["current_article"], &current)
			in.Sections[0].Key = ""
			in.Sections[0].SectionID = current.Sections[0].SectionID
			text := *in.Sections[0].Content[0].Text + fmt.Sprint("换措辞", reviews)
			in.Sections[0].Content[0].Text = &text
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	err := s.engine.Run(context.Background(), s.claim, s.profile)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "ARTICLE_NO_PROGRESS" || reviews != 3 {
		t.Fatalf("same debt not stopped: reviews=%d err=%v %s", reviews, err, s.cp.ArticleLoop.Feedback)
	}
	assertNoLoopPublication(t, s)
}
