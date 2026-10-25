package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestArticleLoopCompleteAuthorPacketShowsExecutableBudgetAndTools(t *testing.T) {
	checked := false
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
			return
		}
		checked = true
		var wire reviewWireRequest
		_ = json.Unmarshal(raw, &wire)
		for _, tinfo := range wire.Tools {
			if slices.Contains([]string{"read_article_state", "read_evidence", loopChallengeTool}, tinfo.Function.Name) {
				t.Error("complete author packet invites redundant/unavailable action", tinfo.Function.Name)
			}
		}
		var budget struct {
			Author int `json:"remaining_logical_author_calls"`
			Review int `json:"review_calls_reserved"`
			Repair int `json:"repair_and_review_calls_reserved"`
		}
		_ = json.Unmarshal(p["completion_budget"], &budget)
		var calls int
		_ = json.Unmarshal(p["calls_reserved"], &calls)
		if budget.Repair == 0 || calls+budget.Author+budget.Review+budget.Repair != 20 {
			t.Error("visible author budget hides the first-pass repair/review reserve", calls, budget)
		}
		writeReviewWireArgs(w, "stop_article", map[string]any{"reason": "synthetic interface probe finished", "evidence_ids": []string{"t1"}})
	}, domain.ArticleConvergenceReviewV13)
	_ = s.engine.Run(context.Background(), s.claim, s.profile)
	if !checked {
		t.Fatal("actual author call site not reached")
	}
}

func TestArticleLoopAuthorReadsRemainAvailableForOmittedMaterialsAndV11(t *testing.T) {
	for _, revision := range []string{domain.ArticleConvergenceReviewV11, domain.ArticleConvergenceReviewV12, domain.ArticleConvergenceReviewV13} {
		s := &session{transcript: []domain.Segment{{ID: "a"}, {ID: "b"}}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{ReviewRevision: revision}}}
		p := articleLoopCall{Mode: "author", Sources: []string{"t1"}, Sections: []string{"one"}}
		if revision == domain.ArticleConvergenceReviewV11 {
			p.Sources, p.Sections = []string{"t1", "t2"}, nil
		}
		infos, err := s.loopCallTools(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"read_evidence", "read_article_state"} {
			if !slices.ContainsFunc(infos, func(info *schema.ToolInfo) bool { return info.Name == name }) {
				t.Fatal("omitted materials or old contract lost its read capability", revision, name)
			}
		}
	}
}

// This verifies scheduling and immutable admissions with a synthetic model;
// understanding and prose quality still require the separate real acceptance.
func TestArticleLoopPlannedWorkSettlesNewQuestionsAndIndependentlyReviewsRepair(t *testing.T) {
	locals, globals, authors := 0, 0, 0
	bodies := map[string]int{}
	firstBody := ""
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if string(p["material_role"]) == `"source_requirements"` {
			if p["major_concept_limit"] == nil {
				t.Error("source granularity not frozen before send")
			}
			writeReviewWireArgs(w, loopSourceTool, sufficiencyPlan(t, p))
			return
		}
		if p["scope"] != nil {
			var scope domain.ArticleReviewScope
			var body domain.ArticleReading
			_ = json.Unmarshal(p["scope"], &scope)
			_ = json.Unmarshal(p["candidate"], &body)
			report := sufficiencyReport(t, p)
			if !scope.Global {
				locals++
				if locals == 1 {
					report.Issues = []domain.ArticleReferenceIssue{{Severity: "blocking", AC: "AC03", Location: body.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t1"}, Problem: "合成首次冲突", Correction: "在原块修正合成冲突"}}
					report.CoverageQuestions = []domain.ArticleCoverageQuestion{{Topic: "来源清单之外新发现的合成末尾条件", EvidenceRefs: []string{"t25"}, CheckedBlockIDs: []string{body.Sections[0].Blocks[0].ID}, RequiredPlacement: "anywhere"}}
				} else {
					if len(report.Resolutions) != 1 {
						t.Error("original blocking debt disappeared on changed candidate", report.Resolutions)
					}
					for i := range report.Resolutions {
						report.Resolutions[i].Outcome = "fixed"
						report.Resolutions[i].EvidenceRefs = nil
					}
				}
			} else {
				globals++
				if p["full_view_manifest"] == nil || len(report.CoverageDecisions) > domain.ArticleCoverageDecisionUnits {
					t.Error("batch lacks actual complete current body or exceeds work bound")
				}
				hash := domain.ContentHash(body)
				bodies[hash]++
				if firstBody == "" {
					firstBody = hash
				}
				if authors > 1 && hash == firstBody {
					t.Error("old complete body reused after repair")
				}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		authors++
		if p["source_requirements_to_preserve"] == nil {
			t.Error("author lost source-first requirements")
		}
		in := loopGoodWrite()
		_ = json.Unmarshal(p["draft_version"], &in.ExpectedVersion)
		if in.ExpectedVersion > 0 {
			if globals != 2 {
				t.Error("repair preceded first complete coverage, including new question", globals)
			}
			var current loopSaveInput
			_ = json.Unmarshal(p["current_article"], &current)
			in.Sections[0].Key = ""
			in.Sections[0].SectionID = current.Sections[0].SectionID
			text := *in.Sections[0].Content[0].Text + "合成冲突已按原文修正。"
			in.Sections[0].Content[0].Text = &text
		}
		writeReviewWireArgs(w, loopSaveTool, in)
	}, domain.ArticleConvergenceReviewV13)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	if locals != 2 || globals != 4 || len(bodies) != 2 || len(s.cp.ArticleLoop.CoverageQuestions) != 2 {
		t.Fatalf("incomplete independent review: locals=%d full=%d bodies=%d questions=%d", locals, globals, len(bodies), len(s.cp.ArticleLoop.CoverageQuestions))
	}
}

func TestArticleLoopPlannedCoveragePreservesDebtBatchAcrossCandidates(t *testing.T) {
	s := &session{snapshot: domain.Snapshot{ID: "snap"}, transcript: []domain.Segment{{ID: "one", Text: "必要前提"}, {ID: "two", Text: "执行与验证"}}, cp: checkpoint{ArticleLoop: &articleLoopState{ReviewRevision: domain.ArticleConvergenceReviewV11}}}
	a := domain.Article{Sections: []domain.Section{{ID: "chapter"}}}
	for i := range 10 {
		s.cp.ArticleLoop.CoverageQuestions = append(s.cp.ArticleLoop.CoverageQuestions, domain.ArticleCoverageQuestion{ID: strings.Repeat("q", i+1), Topic: "合成主题条件", EvidenceRefs: []string{"t1"}})
	}
	first, err := s.planLoopCoverageWork(a, nil)
	if err != nil || len(first) != 2 || !first[1].FullRelations {
		t.Fatal(first, err)
	}
	for _, scope := range first {
		if len(s.loopReferenceQuestions(scope)) > 8 {
			t.Fatal("unbounded initial coverage task")
		}
	}
	s.cp.ArticleLoop.CoverageQuestions = append(s.cp.ArticleLoop.CoverageQuestions, domain.ArticleCoverageQuestion{ID: "new", Topic: "新发现的必要条件", EvidenceRefs: []string{"t2"}})
	next, err := s.planLoopCoverageWork(a, first)
	if err != nil || len(next) != 3 {
		t.Fatal(next, err)
	}
	if next[0].ID != first[0].ID || next[2].ID != first[1].ID || !slices.Equal(next[2].CoverageQuestionIDs, first[1].CoverageQuestionIDs) || !next[2].FullRelations {
		t.Fatal("established blocking responsibility regrouped or final relations lost", first, next)
	}
}

func TestArticleLoopSemanticWorkCountsMeaningAndCompleteOriginals(t *testing.T) {
	for _, revision := range []string{domain.ArticleConvergenceReviewV12, domain.ArticleConvergenceReviewV13} {
		s := &session{snapshot: domain.Snapshot{ID: "snapshot"}, transcript: []domain.Segment{{ID: "original", Text: strings.Repeat("原文", 5000)}}, cp: checkpoint{ArticleLoop: &articleLoopState{ReviewRevision: revision, CoverageQuestions: []domain.ArticleCoverageQuestion{{ID: "q", OriginScope: "receipt", Topic: "先核实条件，再执行并验证，失败时停止", EvidenceRefs: []string{"t1"}}}}}}
		scope := domain.ArticleReviewScope{Global: true, CoverageBatch: true, CoverageQuestionIDs: []string{"q"}}
		before := s.loopCoverageWork(scope)
		if revision == domain.ArticleConvergenceReviewV13 && before != 10000+len([]rune(s.cp.ArticleLoop.CoverageQuestions[0].Topic)) {
			t.Fatal("semantic work omitted originals or necessary conditions", before)
		}
		s.cp.ArticleLoop.CoverageQuestions[0].ID = strings.Repeat("opaque", 200)
		s.cp.ArticleLoop.CoverageQuestions[0].OriginScope = strings.Repeat("receipt", 200)
		scope.CoverageQuestionIDs = []string{s.cp.ArticleLoop.CoverageQuestions[0].ID}
		after := s.loopCoverageWork(scope)
		if revision == domain.ArticleConvergenceReviewV13 && before != after || revision == domain.ArticleConvergenceReviewV12 && before == after {
			t.Fatal("opaque metadata changed semantic work or old policy changed", revision, before, after)
		}
		if revision == domain.ArticleConvergenceReviewV13 {
			s.transcript[0].Text = strings.Repeat("全", domain.ArticleCoverageWorkRunes+1)
			if _, err := s.planLoopCoverageWork(domain.Article{}, nil); err == nil {
				t.Fatal("oversized necessary original was ignored or clipped")
			}
		}
	}
}
