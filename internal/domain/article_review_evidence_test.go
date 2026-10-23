package domain

import (
	"slices"
	"testing"
)

func TestArticleEvidenceQuotesRequireDeliveredVerbatimOriginal(t *testing.T) {
	text := []Segment{{ID: "a", Text: "The host asks a question. The guest answers."}, {ID: "b", Text: "Thank you Matt for inviting me."}}
	scope := ArticleReviewScope{SourceIDs: []string{"a"}, ContextSourceIDs: []string{"b"}}
	for _, tc := range []struct {
		name, id, quote string
		valid           bool
	}{
		{"tail context", "t2", "Thank you Matt for inviting me.", true},
		{"paraphrase", "t1", "The host answers the question.", false},
		{"wrong source", "t1", "Thank you Matt for inviting me.", false},
		{"unknown", "t3", "The guest answers.", false},
		{"empty", "t1", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArticleEvidenceQuotes([]ArticleEvidenceQuote{{SourceID: tc.id, Quote: tc.quote}}, scope, text)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
	scope.ContextSourceIDs = nil
	if ValidateArticleEvidenceQuotes([]ArticleEvidenceQuote{{SourceID: "t2", Quote: "Thank you Matt"}}, scope, text) == nil {
		t.Fatal("undelivered original accepted")
	}
}
func TestArticleScopedDeltaKeepsSplitReviewAndDetectsCitationChange(t *testing.T) {
	before := Article{SnapshotID: "snapshot", Sections: []Section{{ID: "first", Blocks: []Block{{ID: "b1", Type: "paragraph", Content: []Inline{{Type: "text", Text: "A supported claim"}}, CitationIDs: []string{"c1"}}}}, {ID: "second", Blocks: []Block{{ID: "b2"}}}}, Citations: map[string]Citation{"c1": {Kind: "video_text", SegmentIDs: []string{"t1"}}}}
	scope := ArticleReviewScope{ID: "split", SectionIDs: []string{"first"}, SourceIDs: []string{"t1"}, ContextSourceIDs: []string{"t2"}}
	parent := ArticleReviewRecord{CandidateHash: ArticleHash(before), Scope: scope, Report: ArticleReviewReport{Complete: true, Topics: []ArticleTopic{{Topic: "topic", SourceIDs: []string{"t1"}, SectionIDs: []string{"first"}}}}}
	after := before
	after.Citations = map[string]Citation{"c1": {Kind: "video_text", SegmentIDs: []string{"t2"}}}
	d, ok := ArticleScopedReviewDeltaFor("original", parent, before, after, scope)
	if !ok || !slices.Equal(d.AffectedBlockIDs, []string{"b1"}) || d.BaseReviewHash != ContentHash(parent) {
		t.Fatal("split source-bound change lost", d, ok)
	}
	changed := scope
	changed.SourceIDs = []string{"t2"}
	if _, ok := ArticleScopedReviewDeltaFor("original", parent, before, after, changed); ok {
		t.Fatal("different coverage inherited review")
	}
	parent.Delta = d
	if _, ok := ArticleScopedReviewDeltaFor("original", parent, before, after, scope); ok {
		t.Fatal("recursive unverified delta chain accepted")
	}
}

func TestArticleReviewResolutionsCannotSilentlyCloseFindings(t *testing.T) {
	issue := ArticleReviewIssue{Severity: "blocking", AC: "AC03", Location: "b1", Problem: "incorrect subject"}
	prior := ArticleReviewRecord{CandidateHash: "same", Report: ArticleReviewReport{Complete: true, Issues: []ArticleReviewIssue{issue}}}
	scope := ArticleReviewScope{SourceIDs: []string{"a"}}
	text := []Segment{{ID: "a", Text: "The guest, not the host, describes their framework."}}
	report := ArticleReviewReport{Complete: true}
	if ValidateArticleReviewResolutions(report, prior, "new", scope, text) == nil {
		t.Fatal("missing resolution closed previous blocker")
	}
	report.Resolutions = []ArticleReviewResolution{{IssueID: ContentHash(issue), Outcome: "fixed", Explanation: "fixed"}}
	if ValidateArticleReviewResolutions(report, prior, "same", scope, text) == nil {
		t.Fatal("unchanged article claimed repaired")
	}
	report.Resolutions[0].Outcome = "withdrawn"
	if ValidateArticleReviewResolutions(report, prior, "same", scope, text) == nil {
		t.Fatal("withdrawal without counter-evidence accepted")
	}
	report.Resolutions[0].EvidenceQuotes = []ArticleEvidenceQuote{{SourceID: "t1", Quote: text[0].Text}}
	if err := ValidateArticleReviewResolutions(report, prior, "same", scope, text); err != nil {
		t.Fatal(err)
	}
	report.Resolutions[0].Outcome = "remaining"
	if ValidateArticleReviewResolutions(report, prior, "same", scope, text) == nil {
		t.Fatal("remaining blocker disappeared from report")
	}
	report.Issues = []ArticleReviewIssue{issue}
	if err := ValidateArticleReviewResolutions(report, prior, "same", scope, text); err != nil {
		t.Fatal(err)
	}
}
