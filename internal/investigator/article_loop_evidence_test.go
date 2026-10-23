package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"vidlens/internal/domain"
)

func TestArticleLoopEvidenceChallengeRequiresIndependentWithdrawal(t *testing.T) {
	for _, withdraw := range []bool{true, false} {
		t.Run(fmt.Sprint(withdraw), func(t *testing.T) {
			var calls, reviews atomic.Int32
			var s *session
			s = loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				calls.Add(1)
				if p["scope"] != nil {
					n := reviews.Add(1)
					report := loopGoodReport(t, p)
					var a domain.ArticleReading
					_ = json.Unmarshal(p["candidate"], &a)
					if n == 1 || !withdraw {
						report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: a.Sections[0].Blocks[0].ID, EvidenceIDs: []string{"t1"}, Problem: "待核实的合成意见", Correction: "对照原文确认"}}
					}
					if n == 2 && (p["review_challenges"] == nil || p["revision_delta"] == nil) {
						t.Error("independent reviewer did not receive dispute and exact baseline")
					}
					writeReviewWireArgs(w, loopReviewTool, report)
					return
				}
				var version int
				_ = json.Unmarshal(p["draft_version"], &version)
				if version == 0 {
					in := loopGoodWrite()
					in.Sections[0].Content = in.Sections[0].Content[:1]
					writeReviewWireArgs(w, loopSaveTool, in)
					return
				}
				var findings []struct {
					ID string `json:"issue_id"`
				}
				_ = json.Unmarshal(p["review_findings"], &findings)
				if len(findings) != 1 {
					t.Errorf("findings=%s", p["review_findings"])
					return
				}
				quote := s.transcript[0].Text
				if calls.Load() > 4 {
					writeReviewWireArgs(w, "stop_article", loopCannotCompleteInput{Reason: "独立核验仍有争议，不能发布", EvidenceIDs: []string{"t1"}})
					return
				}
				writeReviewWireArgs(w, loopChallengeTool, loopChallengeInput{ExpectedVersion: version, Responses: []loopReviewChallenge{{IssueID: findings[0].ID, Reason: "此意见与来源上下文不一致，请独立核对", EvidenceQuotes: []domain.ArticleEvidenceQuote{{SourceID: "t1", Quote: quote}}}}})
			})
			err := s.engine.Run(context.Background(), s.claim, s.profile)
			if withdraw {
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 4 {
					t.Fatalf("unchanged valid article did not converge in 4 calls: %d", calls.Load())
				}
				d, e := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
				if e != nil || d.Version != 1 {
					t.Fatal("dispute unnecessarily changed article", d.Version, e)
				}
			} else {
				if err == nil {
					t.Fatal("author dispute approved itself")
				}
				assertNoLoopPublication(t, s)
			}
		})
	}
}

func TestArticleLoopSplitRepairRetainsCompletedWork(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) { testSplitRepair(t, resume) })
	}
}
func testSplitRepair(t *testing.T, resume bool) {
	var interrupted atomic.Bool
	var calls, reviews, deltas, relations atomic.Int32
	s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		calls.Add(1)
		if p["scope"] != nil {
			n := reviews.Add(1)
			if n == 1 {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":32768}}\n\ndata: [DONE]\n\n")
				return
			}
			report := loopGoodReport(t, p)
			var a domain.ArticleReading
			_ = json.Unmarshal(p["candidate"], &a)
			if p["global_relations_only"] != nil {
				relations.Add(1)
			} else if p["revision_delta"] != nil {
				if resume && !interrupted.Swap(true) {
					w.WriteHeader(503)
					return
				}
				deltas.Add(1)
			} else if n == 3 {
				report.Issues = []domain.ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: a.Sections[0].Blocks[0].ID, EvidenceIDs: []string{"t25"}, Problem: "后半段事实需修正", Correction: "修改这一段"}}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		var version int
		_ = json.Unmarshal(p["draft_version"], &version)
		if version == 0 {
			in := loopGoodWrite()
			in.Sections[0].Content = in.Sections[0].Content[:1]
			in.Sections[0].Content[0].Sources = []string{"t1"}
			text := "后半段的合成事实说明，用于核验范围保持测试。"
			in.Sections = append(in.Sections, loopSection{Key: "last", Title: "后半段", Content: []loopContent{{Text: &text, Sources: []string{"t25"}}}})
			writeReviewWireArgs(w, loopSaveTool, in)
			return
		}
		var locations map[string][]string
		_ = json.Unmarshal(p["canonical_locations"], &locations)
		var a loopSaveInput
		_ = json.Unmarshal(p["current_article"], &a)
		text := "已根据来源修正的后半段事实，并保留全部必要条件。"
		writeReviewWireArgs(w, loopPatchTool, loopPatchInput{ExpectedVersion: version, Finish: true, Replacements: []loopBlockReplacement{{BlockID: locations[a.Sections[0].SectionID][0], Content: loopContent{Text: &text, Sources: []string{"t25"}}}}})
	})
	if resume {
		if err := s.engine.Run(context.Background(), s.claim, s.profile); err == nil {
			t.Fatal("expected interrupted delta review")
		}
		s = reloadLoopSession(t, s)
	}
	if err := s.engine.Run(context.Background(), s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	expected := int32(7)
	if resume {
		expected++
	}
	if calls.Load() != expected || deltas.Load() != 1 || relations.Load() != 1 {
		t.Fatalf("completed scope reopened: calls=%d reviews=%d deltas=%d global=%d", calls.Load(), reviews.Load(), deltas.Load(), relations.Load())
	}
}
func TestArticleLoopGroundedNewFindingIsNotDiscarded(t *testing.T) {
	d := &domain.ArticleReviewDelta{AffectedBlockIDs: []string{"changed"}}
	issue := domain.ArticleReviewIssue{Severity: "blocking", AC: "AC03", Location: "unchanged", EvidenceQuotes: []domain.ArticleEvidenceQuote{{SourceID: "t1", Quote: "Actual original evidence"}}}
	if err := requireNewFindingEvidence(domain.ArticleReviewReport{Issues: []domain.ArticleReviewIssue{issue}}, d); err != nil {
		t.Fatal("genuine new finding was rejected solely for being outside delta", err)
	}
	issue.EvidenceQuotes = nil
	if requireNewFindingEvidence(domain.ArticleReviewReport{Issues: []domain.ArticleReviewIssue{issue}}, d) == nil {
		t.Fatal("unsupported reopening accepted")
	}
}
