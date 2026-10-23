package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Bindings are program receipts, not model assertions or semantic approvals.
// Raw model reports remain immutable in the operation ledger.
type ArticleQuoteBinding struct {
	Owner    string                 `json:"owner"`
	Index    int                    `json:"index"`
	Original ArticleEvidenceQuote   `json:"original"`
	Resolved []ArticleEvidenceQuote `json:"resolved"`
}

type quoteSpan struct {
	start, end int
	alias      string
}
type quoteRun struct {
	text  string
	spans []quoteSpan
}
type articleQuoteIndex struct {
	originals map[string]string
	runs      []quoteRun
}

func quoteText(s string) string { return strings.Join(strings.Fields(s), " ") }
func newArticleQuoteIndex(scope ArticleReviewScope, text []Segment) articleQuoteIndex {
	idx := articleQuoteIndex{originals: map[string]string{}}
	run := quoteRun{}
	flush := func() {
		if len(run.spans) > 0 {
			idx.runs = append(idx.runs, run)
		}
		run = quoteRun{}
	}
	for i, t := range text {
		if !slices.Contains(scope.SourceIDs, t.ID) && !slices.Contains(scope.ContextSourceIDs, t.ID) {
			flush()
			continue
		}
		normalized := quoteText(t.Text)
		alias := fmt.Sprint("t", i+1)
		idx.originals[t.ID], idx.originals[alias] = normalized, normalized
		if len(run.spans) > 0 {
			run.text += " "
		}
		start := len(run.text)
		run.text += normalized
		run.spans = append(run.spans, quoteSpan{start, len(run.text), alias})
	}
	flush()
	return idx
}
func (idx articleQuoteIndex) bind(q ArticleEvidenceQuote) ([]ArticleEvidenceQuote, bool, error) {
	original, delivered := idx.originals[q.SourceID]
	quote := quoteText(q.Quote)
	if !delivered || quote == "" {
		return nil, false, fmt.Errorf("quote requires a delivered original and nonempty verbatim text: %s", q.SourceID)
	}
	if strings.Contains(original, quote) {
		return []ArticleEvidenceQuote{q}, false, nil
	}
	matches := 0
	var bound []ArticleEvidenceQuote
	for _, run := range idx.runs {
		for offset := 0; offset < len(run.text); {
			at := strings.Index(run.text[offset:], quote)
			if at < 0 {
				break
			}
			at += offset
			end := at + len(quote)
			matches++
			if matches > 1 {
				return nil, false, fmt.Errorf("quote %q for %s has multiple original locations; supply an exact unambiguous passage", q.Quote, q.SourceID)
			}
			for _, span := range run.spans {
				lo, hi := max(at, span.start), min(end, span.end)
				if lo < hi {
					part := strings.TrimSpace(run.text[lo:hi])
					if part != "" {
						bound = append(bound, ArticleEvidenceQuote{SourceID: span.alias, Quote: part})
					}
				}
			}
			offset = at + 1
		}
	}
	if matches != 1 || len(bound) == 0 {
		return nil, false, fmt.Errorf("evidence quote %q for %s is not a unique verbatim passage in delivered originals; recopy a short contiguous passage without changing spelling, case or punctuation", q.Quote, q.SourceID)
	}
	return bound, true, nil
}

// Resolve only exact, unique passages in delivered consecutive originals.
// No fuzzy match, paraphrase, outside evidence, or finding/outcome edits.
func BindArticleReviewQuotes(report ArticleReviewReport, scope ArticleReviewScope, text []Segment) (ArticleReviewReport, []ArticleQuoteBinding, error) {
	idx := newArticleQuoteIndex(scope, text)
	bindings := []ArticleQuoteBinding{}
	bind := func(owner string, index int, quotes []ArticleEvidenceQuote) ([]ArticleEvidenceQuote, error) {
		out := []ArticleEvidenceQuote{}
		for _, q := range quotes {
			resolved, changed, err := idx.bind(q)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved...)
			if changed {
				bindings = append(bindings, ArticleQuoteBinding{Owner: owner, Index: index, Original: q, Resolved: resolved})
			}
		}
		if len(quotes) == 0 {
			return quotes, nil
		}
		return out, nil
	}
	report.Issues = slices.Clone(report.Issues)
	report.Resolutions = slices.Clone(report.Resolutions)
	for i := range report.Issues {
		issue := &report.Issues[i]
		quotes, err := bind("issue", i, issue.EvidenceQuotes)
		if err != nil {
			return report, nil, err
		}
		issue.EvidenceQuotes = quotes
		issue.EvidenceIDs = slices.Clone(issue.EvidenceIDs)
		for _, binding := range bindings {
			if binding.Owner != "issue" || binding.Index != i {
				continue
			}
			for _, q := range binding.Resolved {
				if !slices.Contains(issue.EvidenceIDs, q.SourceID) {
					issue.EvidenceIDs = append(issue.EvidenceIDs, q.SourceID)
				}
			}
		}
	}
	for i := range report.Resolutions {
		quotes, err := bind("resolution", i, report.Resolutions[i].EvidenceQuotes)
		if err != nil {
			return report, nil, err
		}
		report.Resolutions[i].EvidenceQuotes = quotes
	}
	return report, bindings, nil
}

func ValidateArticleQuoteBindings(report ArticleReviewReport, bindings []ArticleQuoteBinding, scope ArticleReviewScope, text []Segment) error {
	idx := newArticleQuoteIndex(scope, text)
	for _, b := range bindings {
		resolved, changed, err := idx.bind(b.Original)
		if err != nil || !changed || ContentHash(resolved) != ContentHash(b.Resolved) {
			return fmt.Errorf("quote binding receipt does not match original evidence")
		}
		var quotes []ArticleEvidenceQuote
		switch b.Owner {
		case "issue":
			if b.Index >= 0 && b.Index < len(report.Issues) {
				quotes = report.Issues[b.Index].EvidenceQuotes
			}
		case "resolution":
			if b.Index >= 0 && b.Index < len(report.Resolutions) {
				quotes = report.Resolutions[b.Index].EvidenceQuotes
			}
		}
		for _, q := range resolved {
			if !slices.Contains(quotes, q) {
				return fmt.Errorf("quote binding receipt is not present in the recorded finding")
			}
		}
	}
	return nil
}
