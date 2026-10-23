package domain

import (
	"slices"
	"testing"
)

func TestArticleReviewQuoteBindingUsesOnlyUniqueExactDeliveredPassages(t *testing.T) {
	text := []Segment{{ID: "a", Text: "A unique opening. The speaker starts"}, {ID: "b", Text: "with the next sentence. Shared words."}, {ID: "c", Text: "Shared words."}, {ID: "outside", Text: "Hidden external evidence."}}
	scope := ArticleReviewScope{SourceIDs: []string{"a", "b"}, ContextSourceIDs: []string{"c"}}
	cases := []struct {
		name, source, quote string
		valid               bool
		sources             []string
	}{
		{"correct explicit repeated passage", "t2", "Shared words.", true, []string{"t2"}},
		{"wrong alias exact passage", "t2", "A unique opening.", true, []string{"t1"}},
		{"adjacent boundary", "t1", "The speaker starts with the next sentence.", true, []string{"t1", "t2"}},
		{"ambiguous relocation", "t1", "Shared words.", false, nil},
		{"paraphrase", "t1", "The speaker begins with another sentence.", false, nil},
		{"invented identity", "t99", "A unique opening.", false, nil},
		{"undelivered", "t1", "Hidden external evidence.", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := ArticleReviewReport{Complete: true, Issues: []ArticleReviewIssue{{Severity: "blocking", AC: "AC03", Location: "block", Problem: "A proposed factual conflict", Correction: "Verify the actual source before repair", EvidenceIDs: []string{tc.source}, EvidenceQuotes: []ArticleEvidenceQuote{{SourceID: tc.source, Quote: tc.quote}}}}}
			originalHash := ContentHash(before)
			after, bindings, err := BindArticleReviewQuotes(before, scope, text)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if ContentHash(before) != originalHash {
				t.Fatal("raw model report mutated")
			}
			if err != nil {
				return
			}
			if after.Issues[0].Problem != before.Issues[0].Problem || after.Issues[0].Correction != before.Issues[0].Correction || after.Issues[0].Severity != "blocking" {
				t.Fatal("binding changed semantic judgment")
			}
			ids := []string{}
			for _, q := range after.Issues[0].EvidenceQuotes {
				ids = append(ids, q.SourceID)
			}
			if !slices.Equal(ids, tc.sources) {
				t.Fatal(ids, tc.sources)
			}
			if err = ValidateArticleReviewQuotes(after, scope, text); err != nil {
				t.Fatal(err)
			}
			if err = ValidateArticleQuoteBindings(after, bindings, scope, text); err != nil {
				t.Fatal(err)
			}
			if len(bindings) > 0 {
				bindings[0].Resolved = []ArticleEvidenceQuote{{SourceID: "t1", Quote: "Invented text"}}
				if ValidateArticleQuoteBindings(after, bindings, scope, text) == nil {
					t.Fatal("forged binding receipt accepted")
				}
			}
		})
	}
	// Removing the middle segment must not stitch distant text into a quote.
	scope.SourceIDs = []string{"a"}
	scope.ContextSourceIDs = []string{"c"}
	idx := newArticleQuoteIndex(scope, text)
	if _, _, err := idx.bind(ArticleEvidenceQuote{SourceID: "t1", Quote: "The speaker starts Shared words."}); err == nil {
		t.Fatal("quote spanned an undelivered gap")
	}
}

func TestArticleReviewQuoteBindingDoesNotCloseOrReclassifyResolution(t *testing.T) {
	text := []Segment{{ID: "a", Text: "Exact counter evidence."}, {ID: "b", Text: "Another passage."}}
	scope := ArticleReviewScope{SourceIDs: []string{"a", "b"}}
	report := ArticleReviewReport{Resolutions: []ArticleReviewResolution{{IssueID: "original-issue", Outcome: "withdrawn", Explanation: "Independent semantic judgment", EvidenceQuotes: []ArticleEvidenceQuote{{SourceID: "t2", Quote: "Exact counter evidence."}}}}}
	got, bindings, err := BindArticleReviewQuotes(report, scope, text)
	if err != nil || got.Resolutions[0].IssueID != "original-issue" || got.Resolutions[0].Outcome != "withdrawn" || got.Resolutions[0].Explanation != report.Resolutions[0].Explanation || len(bindings) != 1 || bindings[0].Owner != "resolution" {
		t.Fatal(got, bindings, err)
	}
	if err = ValidateArticleQuoteBindings(got, bindings, scope, text); err != nil {
		t.Fatal(err)
	}
	bindings[0].Index = 4
	if ValidateArticleQuoteBindings(got, bindings, scope, text) == nil {
		t.Fatal("binding lost its owner")
	}
}
