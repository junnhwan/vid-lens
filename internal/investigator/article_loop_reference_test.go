package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopReferenceWireOnlyRequestsModelOwnedQuestionFields(t *testing.T) {
	s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{ReviewRevision: domain.ArticleConvergenceReviewV6}}}
	infos, err := s.loopTools(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		sc, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		wire := string(domain.JSON(sc))
		for _, forbidden := range []string{`"id"`, `"origin_scope"`, `"origin_candidate"`} {
			if strings.Contains(wire, forbidden) {
				t.Errorf("program-owned identity requested in model report: %s", forbidden)
			}
		}
		for _, required := range []string{`"topic"`, `"evidence_refs"`, `"checked_block_ids"`, `"required_placement"`, `"question_id"`} {
			if !strings.Contains(wire, required) {
				t.Errorf("model report lost semantic field: %s", required)
			}
		}
	}
}

func TestArticleLoopReferenceContextIncludesAllCitedOriginals(t *testing.T) {
	s := &session{transcript: []domain.Segment{{ID: "t-original"}, {ID: "context-original"}, {ID: "context-tail"}, {ID: "unrelated"}}, cp: checkpoint{ArticleLoop: &articleLoopState{}}}
	a := domain.Article{Sections: []domain.Section{
		{ID: "primary", Blocks: []domain.Block{{ID: "p1", Type: "paragraph", CitationIDs: []string{"one"}, Content: []domain.Inline{{Type: "text", Text: "Primary responsibility."}}}}},
		{ID: "other", Blocks: []domain.Block{{ID: "background", Type: "paragraph", CitationIDs: []string{"context"}, Content: []domain.Inline{{Type: "text", Text: "A related statement and its remaining context."}}}}},
	}, Citations: map[string]domain.Citation{"one": {Kind: "video_text", SegmentIDs: []string{"t-original"}}, "context": {Kind: "video_text", SegmentIDs: []string{"t-original", "context-original", "context-tail"}}}}
	got, err := s.referenceRequiredContext(context.Background(), a, domain.ArticleReviewScope{ID: "scope", SectionIDs: []string{"primary"}, SourceIDs: []string{"t-original"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.ContextBlockIDs, []string{"background"}) || !slices.Equal(got.ContextSourceIDs, []string{"context-original", "context-tail"}) {
		t.Fatalf("cross-chapter body was delivered without its actual original dependencies: %+v", got)
	}
	if !slices.Equal(got.SourceIDs, []string{"t-original"}) || !slices.Equal(got.SectionIDs, []string{"primary"}) {
		t.Fatal("context became extra primary coverage responsibility", got)
	}
}

func TestArticleLoopHistoricalReviewSchemaKeepsItsFrozenFields(t *testing.T) {
	for _, revision := range []string{"", domain.ArticleConvergenceReviewV1, domain.ArticleConvergenceReviewV2, domain.ArticleConvergenceReviewV3, domain.ArticleConvergenceReviewV4, domain.ArticleConvergenceReviewV5} {
		s := &session{cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ArticleLoopV2}, ArticleLoop: &articleLoopState{ReviewRevision: revision}}}
		infos, err := s.loopTools(true)
		if err != nil {
			t.Fatal(err)
		}
		if len(infos) != 1 || strings.Contains(string(domain.JSON(infos)), "coverage_questions") || strings.Contains(string(domain.JSON(infos)), "evidence_refs") {
			t.Fatal("new wire fields leaked into historical review", revision)
		}
	}
}

func TestArticleLoopReferenceOutstandingFindingsMatchResolutionContract(t *testing.T) {
	blocker := domain.ArticleReviewIssue{Severity: "blocking", AC: "AC03", Location: "b1", Problem: "incorrect subject"}
	advisory := domain.ArticleReviewIssue{Severity: "advisory", AC: "AC06", Location: "b2", Problem: "optional ordering"}
	for _, issues := range [][]domain.ArticleReviewIssue{{blocker, advisory}, {advisory, blocker}, {advisory}} {
		prior := domain.ArticleReviewRecord{CandidateHash: "before", Report: domain.ArticleReviewReport{Issues: issues}}
		var pending []struct {
			ID string `json:"issue_id"`
		}
		if err := json.Unmarshal([]byte(domain.JSON(loopReferenceUnresolvedFindings(issues))), &pending); err != nil {
			t.Fatal(err)
		}
		// Follow exactly the list delivered to the model, then use the independent
		// publication validator as the oracle for the required resolution set.
		report := domain.ArticleReviewReport{}
		for _, finding := range pending {
			report.Resolutions = append(report.Resolutions, domain.ArticleReviewResolution{IssueID: finding.ID, Outcome: "fixed", Explanation: "current candidate repaired the blocking claim"})
		}
		if err := domain.ValidateArticleReviewResolutions(report, prior, "after", domain.ArticleReviewScope{}, nil); err != nil {
			t.Fatalf("following delivered outstanding findings contradicts the validator: %v", err)
		}
	}
}

func loopGoodReferenceReport(t *testing.T, p map[string]json.RawMessage) domain.ArticleReferenceReport {
	g := loopGoodReport(t, p)
	r := domain.ArticleReferenceReport{Complete: true, Unassessed: []string{}, Topics: g.Topics, Issues: []domain.ArticleReferenceIssue{}, Resolutions: []domain.ArticleReferenceResolution{}, VisualObservations: g.VisualObservations, CoverageQuestions: []domain.ArticleCoverageQuestion{}, CoverageDecisions: []domain.ArticleCoverageDecision{}}
	for _, resolution := range g.Resolutions {
		refs := []string{}
		for _, q := range resolution.EvidenceQuotes {
			refs = append(refs, q.SourceID)
		}
		r.Resolutions = append(r.Resolutions, domain.ArticleReferenceResolution{IssueID: resolution.IssueID, Outcome: resolution.Outcome, Explanation: resolution.Explanation, EvidenceRefs: refs})
	}
	return r
}

func TestArticleLoopReferenceRepairIsOneFieldAndDoesNotWriteDraft(t *testing.T) {
	for _, badRepair := range []bool{false, true} {
		t.Run(fmt.Sprint(badRepair), func(t *testing.T) {
			var calls, reviews, repairs atomic.Int32
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				calls.Add(1)
				if p["ticket"] != nil {
					repairs.Add(1)
					var ticket domain.ArticleReferenceTicket
					_ = json.Unmarshal(p["ticket"], &ticket)
					if len(ticket.Errors) != 1 || ticket.Errors[0].Path != "/issues/0/evidence_refs/0" {
						t.Errorf("unexpected repair authority: %s", p["ticket"])
					}
					ref := "t25"
					if badRepair {
						ref = "t888"
					}
					writeReviewWireArgs(w, loopReferenceRepairTool, domain.ArticleReferenceRepair{TicketID: ticket.ID, Patches: []domain.ArticleReferencePatch{{Path: ticket.Errors[0].Path, Ref: ref}}})
					return
				}
				if p["scope"] != nil {
					n := reviews.Add(1)
					report := loopGoodReferenceReport(t, p)
					var a domain.ArticleReading
					_ = json.Unmarshal(p["candidate"], &a)
					if n == 1 {
						report.Issues = []domain.ArticleReferenceIssue{{Severity: "blocking", AC: "AC04", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t999", "t1"}, Problem: "合成的引用缺口主张", Correction: "核对真实引用，不改其他正文"}}
					}
					writeReviewWireArgs(w, loopReviewTool, report)
					return
				}
				var version int
				_ = json.Unmarshal(p["draft_version"], &version)
				if version == 0 {
					writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
					return
				}
				var findings []struct {
					ID string `json:"issue_id"`
				}
				_ = json.Unmarshal(p["review_findings"], &findings)
				if len(findings) != 1 {
					t.Errorf("lost staged finding: %s", p["review_findings"])
					return
				}
				writeReviewWireArgs(w, loopChallengeTool, loopReferenceChallengeInput{ExpectedVersion: version, Responses: []loopReferenceChallenge{{IssueID: findings[0].ID, Reason: "合成独立反证，保留原有有效正文", EvidenceRefs: []string{"t1"}}}})
			}, domain.ArticleConvergenceReviewV6)
			err := s.engine.Run(context.Background(), s.claim, s.profile)
			if badRepair {
				if err == nil {
					t.Fatal("second invalid reference approved")
				}
				assertNoLoopPublication(t, s)
				if calls.Load() != 3 {
					t.Fatalf("repair escaped two-submission limit: %d", calls.Load())
				}
			} else if err != nil {
				t.Fatal(err, string(s.cp.ArticleLoop.Feedback))
			} else if calls.Load() != 5 || reviews.Load() != 2 {
				t.Fatalf("semantic review was regenerated for mechanical repair: %d/%d", calls.Load(), reviews.Load())
			}
			if repairs.Load() != 1 {
				t.Fatal("repair did not get exactly one call")
			}
			d, e := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
			if e != nil || d.Version != 1 {
				t.Fatal("reference repair wrote article", d.Version, e)
			}
		})
	}
}

// Synthetic fixture: this is a new wire submission, never a captured model report.
func TestArticleLoopReferenceSelectionDoesNotRecopyOriginals(t *testing.T) {
	var calls atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] == nil {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		good := loopGoodReport(t, p)
		wire := map[string]any{}
		_ = json.Unmarshal(domain.JSON(good), &wire)
		wire["coverage_questions"] = []any{}
		wire["coverage_decisions"] = []any{}
		wire["resolutions"] = []any{}
		writeReviewWireArgs(w, loopReviewTool, wire)
	}, domain.ArticleConvergenceReviewV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err, string(s.cp.ArticleLoop.Feedback))
	}
	if calls.Load() != 2 {
		t.Fatalf("valid reference report regenerated: %d calls", calls.Load())
	}
}

func TestArticleLoopReferenceCoverageWaitsForActualFullBody(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) { testReferenceCoverage(t, batch) })
	}
}

func testReferenceCoverage(t *testing.T, batch bool) {
	var calls, reviews, globals atomic.Int32
	var contextSeen atomic.Bool
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] == nil {
			in := loopGoodWrite()
			in.Sections[0].Content = in.Sections[0].Content[:1]
			background := "这里完整保留了合成主题的方法、工具和前置条件。"
			kitchen := "这一章引用相关主题的简短引语，具体背景可能在前章。"
			in.Sections[0].Title = "主题起点"
			in.Sections[0].Content[0] = loopContent{Text: &background, Sources: []string{"t14"}}
			in.Sections = append(in.Sections, loopSection{Key: "kitchen", Title: "厨房局部", Content: []loopContent{{Text: &kitchen, Sources: []string{"t14", "t25"}}}})
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		if reviews.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":32768}}\n\ndata: [DONE]\n\n")
			return
		}
		var a domain.ArticleReading
		_ = json.Unmarshal(p["candidate"], &a)
		report := loopGoodReferenceReport(t, p)
		if p["global_relations_only"] != nil {
			globals.Add(1)
			var pending []domain.ArticleCoverageQuestion
			_ = json.Unmarshal(p["pending_coverage_questions"], &pending)
			if len(a.Sections) != 2 || !batch && len(pending) != 1 || p["full_view_manifest"] == nil {
				t.Errorf("full-body coverage material missing: %s", p["pending_coverage_questions"])
				return
			}
			var scope domain.ArticleReviewScope
			_ = json.Unmarshal(p["scope"], &scope)
			if batch && !scope.CoverageBatch {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
				return
			}
			for _, q := range pending {
				report.CoverageDecisions = append(report.CoverageDecisions, domain.ArticleCoverageDecision{QuestionID: q.ID, Outcome: "covered", EvidenceRefs: []string{"t14"}, BlockIDs: []string{a.Sections[0].Blocks[0].ID}, Explanation: "实际完整正文起点章保留了必要方法、工具和条件，并非仅一条引语"})
			}
		} else if a.Sections[0].Title == "厨房局部" {
			var contextBlocks []domain.ArticleContextBlock
			_ = json.Unmarshal(p["context_blocks"], &contextBlocks)
			for _, b := range contextBlocks {
				if b.Block.Text == "这里完整保留了合成主题的方法、工具和前置条件。" {
					contextSeen.Store(true)
				}
			}
			report.CoverageQuestions = []domain.ArticleCoverageQuestion{{Topic: "方法和工具的必要背景是否仍有实质覆盖", EvidenceRefs: []string{"t14"}, CheckedBlockIDs: []string{a.Sections[0].Blocks[0].ID}, RequiredPlacement: "anywhere"}}
			if batch {
				q := report.CoverageQuestions[0]
				q.Topic = "必要前提是否仍有实质覆盖"
				report.CoverageQuestions = append(report.CoverageQuestions, q)
			}
		}
		writeReviewWireArgs(w, loopReviewTool, report)
	}, domain.ArticleConvergenceReviewV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err, string(s.cp.ArticleLoop.Feedback))
	}
	wantCalls, wantGlobals := int32(5), int32(1)
	if batch {
		wantCalls, wantGlobals = 8, 4
	}
	if calls.Load() != wantCalls || globals.Load() != wantGlobals || !contextSeen.Load() {
		t.Fatalf("coverage skipped actual body: calls=%d globals=%d context=%v", calls.Load(), globals.Load(), contextSeen.Load())
	}
}

func TestArticleLoopReferenceRevisionRequiresIndependentCurrentReview(t *testing.T) {
	var calls, reviews atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil {
			report := loopGoodReferenceReport(t, p)
			var a domain.ArticleReading
			_ = json.Unmarshal(p["candidate"], &a)
			if reviews.Add(1) == 1 {
				report.Issues = []domain.ArticleReferenceIssue{
					{AC: "AC03", Severity: "blocking", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t1"}, Problem: "合成的主体归属缺陷", Correction: "只修改这一语义块并保留全部来源"},
					{AC: "AC06", Severity: "advisory", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t1"}, Problem: "合成的可选编排建议", Correction: "可选调整，不应成为阻塞问题关闭义务"},
				}
			} else {
				if p["revision_delta"] == nil || len(report.Resolutions) != 1 {
					t.Error("new candidate lost delta or unresolved finding")
				}
				var prior struct {
					Issues []struct {
						Finding domain.ArticleReferenceIssue `json:"finding"`
					} `json:"issues"`
				}
				if json.Unmarshal(p["prior_complete_review"], &prior) != nil || len(prior.Issues) != 2 || prior.Issues[1].Finding.Severity != "advisory" {
					t.Error("optional advice disappeared from the prior review record")
				}
				for i := range report.Resolutions {
					report.Resolutions[i].Outcome = "fixed"
				}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		var version int
		_ = json.Unmarshal(p["draft_version"], &version)
		if version == 0 {
			writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			return
		}
		var locations map[string][]string
		_ = json.Unmarshal(p["canonical_locations"], &locations)
		var current loopSaveInput
		_ = json.Unmarshal(p["current_article"], &current)
		corrected := "合成作者对照收到的原文修正主体，并保留原来的必要条件和结尾事实。"
		writeReviewWireArgs(w, loopPatchTool, loopPatchInput{ExpectedVersion: version, Finish: true, Replacements: []loopBlockReplacement{{BlockID: locations[current.Sections[0].SectionID][0], Content: loopContent{Text: &corrected, Sources: []string{"t1", "t25"}}}}})
	}, domain.ArticleConvergenceReviewV6)
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	d, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
	if err != nil || d.Version != 2 || calls.Load() != 4 || reviews.Load() != 2 {
		t.Fatalf("revision bypassed independent current review: version=%d calls=%d reviews=%d %v", d.Version, calls.Load(), reviews.Load(), err)
	}
}
