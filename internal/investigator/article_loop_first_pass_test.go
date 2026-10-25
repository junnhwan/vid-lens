package investigator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"vidlens/internal/domain"
)

func TestArticleLoopSplitFirstPassSettlesCoverageBeforeRepair(t *testing.T) {
	global, authors := false, 0
	s := freshSufficiencyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := loopPayload(t, r)
		if p["scope"] != nil && p["source_plans"] != nil {
			t.Error("complete source inventory duplicated in review input")
		}
		if string(p["material_role"]) == `"source_requirements"` {
			var refs []string
			_ = json.Unmarshal(p["primary_source_ids"], &refs)
			writeReviewWireArgs(w, loopSourceTool, domain.ArticleSourceRequirements{Complete: true, Requirements: []domain.ArticleSourceRequirement{{Requirement: "前半段必要概念", EvidenceRefs: refs[:1]}, {Requirement: "后半段的必要条件", EvidenceRefs: refs[len(refs)-1:]}}, Incidental: []domain.ArticleIncidentalSource{{Reason: "此合成范围只用于验证调度，不声称语义理解", EvidenceRefs: refs[1 : len(refs)-1]}}})
			return
		}
		if p["scope"] != nil {
			report := sufficiencyReport(t, p)
			var scope domain.ArticleReviewScope
			_ = json.Unmarshal(p["scope"], &scope)
			if scope.Global {
				global = true
				if p["source_inventory_receipts"] == nil {
					t.Error("source-only provenance missing")
				}
			} else {
				var units []domain.ArticleSourceRequirement
				_ = json.Unmarshal(p["source_requirements_for_this_scope"], &units)
				var primary []string
				_ = json.Unmarshal(p["primary_source_ids"], &primary)
				var directory []domain.ArticleEvidenceEntry
				_ = json.Unmarshal(p["evidence_directory"], &directory)
				for _, unit := range units {
					owned := false
					for _, ref := range unit.EvidenceRefs {
						found := false
						for _, entry := range directory {
							found = found || entry.Ref == ref
						}
						if !found {
							t.Error("local requirement retained undelivered source", ref)
						}
						for _, id := range primary {
							owned = owned || id == ref
						}
					}
					if !owned {
						t.Error("local requirement expanded primary coverage obligation")
					}
				}
				report.CoverageDecisions = []domain.ArticleCoverageDecision{}
				report.Issues = []domain.ArticleReferenceIssue{{Severity: "blocking", AC: "AC03", Location: scope.SectionIDs[0] + "_b1", EvidenceRefs: []string{"t1"}, Problem: "合成实际冲突，首轮仍需汇总来源覆盖义务", Correction: "核对原文并修复对应事实"}}
			}
			writeReviewWireArgs(w, loopReviewTool, report)
			return
		}
		authors++
		if authors > 1 {
			if !global {
				t.Error("author repair started before pending source requirements received the complete-body decision")
			}
			writeReviewWireArgs(w, "stop_article", map[string]any{"reason": "order probe completed", "evidence_ids": []string{"t1"}})
			return
		}
		in := loopGoodWrite()
		in.Sections[0].Content[0].Sources = []string{"t1"}
		text := "后半段的必要条件也必须在第一次修稿前核对。"
		in.Sections = append(in.Sections, loopSection{Key: "ending", Title: "后半段", Content: []loopContent{{Text: &text, Sources: []string{"t25"}}}})
		writeReviewWireArgs(w, loopSaveTool, in)
	})
	s.cp.ArticleLoop.ReviewWorkLimit = 1000
	if err := s.persist(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	s = reloadLoopSession(t, s)
	_ = s.engine.Run(context.Background(), s.claim, s.profile)
	if !global {
		t.Fatal("split first pass skipped complete-body source coverage before repair")
	}
}
