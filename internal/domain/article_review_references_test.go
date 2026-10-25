package domain

import (
	"slices"
	"testing"
)

func TestArticleReferencesBindExactDeliveredSegments(t *testing.T) {
	text := []Segment{{ID: "one", Text: "Away from the kitchen.", Origin: "asr"}, {ID: "two", Text: "Away from the kitchen.", StartMS: 1000, EndMS: 2000, Origin: "asr"}}
	directory := ArticleEvidenceDirectory("snapshot", text, []string{"t2"})
	r := ArticleReferenceReport{Issues: []ArticleReferenceIssue{{EvidenceRefs: []string{"t2", "t1"}}}}
	bound, bindings, invalid, err := BindArticleReferences(r, directory, "snapshot", text, nil)
	if err != nil || len(bindings) != 1 || bindings[0].Entry.SegmentID != "two" || len(invalid) != 1 || invalid[0].Path != "/issues/0/evidence_refs/1" || bound.Issues[0].EvidenceQuotes[0].Quote != text[1].Text {
		t.Fatal("lost exact delivered identity", bound, bindings, invalid, err)
	}
	for _, tamper := range []string{"snapshot", "hash", "timing"} {
		t.Run(tamper, func(t *testing.T) {
			d := slices.Clone(directory)
			originals := slices.Clone(text)
			switch tamper {
			case "snapshot":
				d[0].SnapshotID = "other"
			case "hash":
				d[0].TextHash = "forged"
			case "timing":
				originals[1].StartMS++
			}
			if _, _, _, err := BindArticleReferences(r, d, "snapshot", originals, nil); err == nil {
				t.Fatal("tampered directory accepted")
			}
		})
	}
}

func TestArticleReferenceRepairCannotChangeAnyOtherField(t *testing.T) {
	original := ArticleReferenceReport{Complete: true, Issues: []ArticleReferenceIssue{{AC: "AC04", Severity: "blocking", Problem: "specific gap", Correction: "bind actual source", EvidenceRefs: []string{"bad", "t2"}}}}
	before := ContentHash(original)
	ticket := ArticleReferenceTicket{ID: "ticket", Errors: []ArticleReferenceError{{Path: "/issues/0/evidence_refs/0", Ref: "bad"}}}
	repaired, err := ApplyArticleReferenceRepair(original, ticket, ArticleReferenceRepair{TicketID: "ticket", Patches: []ArticleReferencePatch{{Path: "/issues/0/evidence_refs/0", Ref: "t1"}}})
	if err != nil || repaired.Issues[0].EvidenceRefs[0] != "t1" || ContentHash(original) != before {
		t.Fatal("repair mutated staged report", err)
	}
	repaired.Issues[0].EvidenceRefs[0] = "bad"
	if ContentHash(repaired) != before {
		t.Fatal("repair modified semantic fields or valid references")
	}
	for _, path := range []string{"/issues/0/ac", "/issues/0/problem", "/issues/0/correction", "/issues/0/severity", "/issues/0/evidence_refs", "/issues/0/evidence_refs/1", "/complete", "/resolutions/0/outcome", "/issues"} {
		if _, err := ApplyArticleReferenceRepair(original, ticket, ArticleReferenceRepair{TicketID: "ticket", Patches: []ArticleReferencePatch{{Path: path, Ref: "t1"}}}); err == nil {
			t.Fatal("repair exceeded field authority", path)
		}
	}
}

func coverageMaterial() (Article, ArticleReviewScope, []Segment) {
	a := Article{SnapshotID: "snapshot", Title: "full article", Sections: []Section{
		{ID: "start", Title: "background", Blocks: []Block{{ID: "b3", Type: "paragraph", CitationIDs: []string{"c1"}, Content: []Inline{{Type: "text", Text: "The necessary condition and complete method are retained here."}}}}},
		{ID: "kitchen", Title: "local example", Blocks: []Block{{ID: "quote", Type: "paragraph", CitationIDs: []string{"c1"}, Content: []Inline{{Type: "text", Text: "A short related kitchen quotation."}}}}},
	}, Citations: map[string]Citation{"c1": {ID: "c1", Kind: "video_text", SegmentIDs: []string{"original"}}}}
	return a, ArticleReviewScope{ID: "local", SectionIDs: []string{"kitchen"}, SourceIDs: []string{"original"}}, []Segment{{ID: "original", Text: "Necessary condition and complete method."}}
}

func TestArticleCoverageRequiresDeliveredFullBodyAndCurrentWitness(t *testing.T) {
	a, scope, text := coverageMaterial()
	ids := ArticleCrossChapterBlocks(a, scope)
	if !slices.Equal(ids, []string{"b3"}) {
		t.Fatal("reverse citations did not deliver actual background", ids)
	}
	context := ArticleReviewContextBlocks(a, ids)
	if len(context) != 1 || context[0].Block.Text == "" {
		t.Fatal("context was reduced to navigation")
	}
	r := ArticleReferenceReport{Complete: true, Topics: []ArticleTopic{{Topic: "necessary method", SourceIDs: []string{"t1"}, SectionIDs: []string{}}}, CoverageQuestions: []ArticleCoverageQuestion{{Topic: "necessary method", EvidenceRefs: []string{"t1"}, CheckedBlockIDs: []string{"quote"}, RequiredPlacement: "anywhere"}}}
	if err := ValidateArticleCoverage(r, scope, a, nil, nil); err != nil {
		t.Fatal(err)
	}
	r.Issues = []ArticleReferenceIssue{{AC: "AC02", Severity: "blocking"}}
	if ValidateArticleCoverage(r, scope, a, nil, nil) == nil {
		t.Fatal("local absence became a full omission finding")
	}
	r.Issues = nil
	r.CoverageQuestions = nil
	question := ArticleCoverageQuestion{ID: "q", Topic: "method", RequiredPlacement: "anywhere", EvidenceRefs: []string{"t1"}}
	r.CoverageDecisions = []ArticleCoverageDecision{{QuestionID: "q", Outcome: "covered", BlockIDs: []string{"b3"}, EvidenceRefs: []string{"t1"}, Explanation: "Full body preserves method and condition."}}
	full := ArticleFullViewManifest(a)
	if err := ValidateArticleCoverage(r, scope, a, []ArticleCoverageQuestion{question}, &full); err != nil {
		t.Fatal(err)
	}
	if ValidateArticleCoverage(r, scope, a, []ArticleCoverageQuestion{question}, nil) == nil {
		t.Fatal("partial candidate closed coverage")
	}
	r.CoverageDecisions[0].BlockIDs = []string{"start"}
	if ValidateArticleCoverage(r, scope, a, []ArticleCoverageQuestion{question}, &full) == nil {
		t.Fatal("title accepted as body witness")
	}
	r.CoverageDecisions[0].BlockIDs = []string{"b3"}
	scope.ContextBlockIDs = ids
	before := ArticleReviewMaterialIdentity(a, scope, Snapshot{ID: "snapshot"}, text, nil)
	a.Sections[0].Blocks[0].Content[0].Text = "The sole necessary information was removed."
	if before == ArticleReviewMaterialIdentity(a, scope, Snapshot{ID: "snapshot"}, text, nil) {
		t.Fatal("changed cross-chapter dependency reused")
	}
	if ValidateArticleCoverage(r, scope, a, []ArticleCoverageQuestion{question}, &full) == nil {
		t.Fatal("stale complete-view witness accepted")
	}
}
