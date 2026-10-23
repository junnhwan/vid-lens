package domain

import (
	"fmt"
	"slices"
	"strings"
)

// This proves provenance, not semantic entailment. The independent reviewer
// still decides whether these real passages establish the alleged defect.
func ValidateArticleEvidenceQuotes(quotes []ArticleEvidenceQuote, scope ArticleReviewScope, text []Segment) error {
	originals := map[string]string{}
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for i, t := range text {
		if slices.Contains(scope.SourceIDs, t.ID) || slices.Contains(scope.ContextSourceIDs, t.ID) {
			originals[t.ID] = normalize(t.Text)
			originals[fmt.Sprint("t", i+1)] = normalize(t.Text)
		}
	}
	for _, q := range quotes {
		source, ok := originals[q.SourceID]
		quote := normalize(q.Quote)
		if !ok || quote == "" || !strings.Contains(source, quote) {
			return fmt.Errorf("evidence quote for %s is not a verbatim passage in delivered originals", q.SourceID)
		}
	}
	return nil
}

func ValidateArticleReviewQuotes(report ArticleReviewReport, scope ArticleReviewScope, text []Segment) error {
	for _, issue := range report.Issues {
		if err := ValidateArticleEvidenceQuotes(issue.EvidenceQuotes, scope, text); err != nil {
			return err
		}
		for _, q := range issue.EvidenceQuotes {
			if !slices.Contains(issue.EvidenceIDs, q.SourceID) {
				return fmt.Errorf("quote source %s must also be in evidence_ids", q.SourceID)
			}
		}
	}
	return nil
}

// Scope deltas retain the exact initial work partition, including its coverage
// and context obligations. Relations-only passes cannot supply factual proofs.
func ArticleScopedReviewDeltaFor(key string, parent ArticleReviewRecord, before, after Article, scope ArticleReviewScope) (*ArticleReviewDelta, bool) {
	if ContentHash(parent.Scope) != ContentHash(scope) || len(scope.SourceIDs) == 0 || parent.CandidateHash != ArticleHash(before) {
		return nil, false
	}
	project := func(a Article) Article {
		out := a
		out.Sections = nil
		for _, s := range a.Sections {
			if slices.Contains(scope.SectionIDs, s.ID) {
				out.Sections = append(out.Sections, s)
			}
		}
		return out
	}
	old, current := project(before), project(after)
	// Reuse the exact delta algorithm with a projected immutable candidate.
	projected := parent
	projected.Scope.Global = true
	projected.CandidateHash = ArticleHash(old)
	d, ok := ArticleReviewDeltaFor(key, projected, old, current)
	if ok {
		d.BaseReviewHash = ContentHash(parent)
	}
	return d, ok
}

// Every previously open finding must receive an explicit independent verdict.
// New findings are retained in Issues and become the next round's obligations.
func ValidateArticleReviewResolutions(report ArticleReviewReport, prior ArticleReviewRecord, currentHash string, scope ArticleReviewScope, text []Segment) error {
	pending := map[string]ArticleReviewIssue{}
	for _, issue := range prior.Report.Issues {
		if issue.Severity == "blocking" {
			pending[ContentHash(issue)] = issue
		}
	}
	seen := map[string]bool{}
	for _, r := range report.Resolutions {
		issue, ok := pending[r.IssueID]
		if !ok || seen[r.IssueID] || strings.TrimSpace(r.Explanation) == "" {
			return fmt.Errorf("resolution must identify each outstanding issue exactly once with an explanation")
		}
		seen[r.IssueID] = true
		if err := ValidateArticleEvidenceQuotes(r.EvidenceQuotes, scope, text); err != nil {
			return err
		}
		switch r.Outcome {
		case "fixed":
			if prior.CandidateHash == currentHash {
				return fmt.Errorf("unchanged candidate cannot claim fixed; verify and withdraw a false finding with evidence")
			}
		case "withdrawn":
			if len(scope.SourceIDs) > 0 && len(r.EvidenceQuotes) == 0 {
				return fmt.Errorf("withdrawn finding %s requires original counter-evidence", r.IssueID)
			}
		case "remaining":
			if !slices.ContainsFunc(report.Issues, func(i ArticleReviewIssue) bool { return i.Severity == "blocking" && i.AC == issue.AC }) {
				return fmt.Errorf("remaining finding must be retained as blocking")
			}
		default:
			return fmt.Errorf("unknown finding resolution %q", r.Outcome)
		}
	}
	if len(seen) != len(pending) {
		return fmt.Errorf("every unresolved_findings issue_id needs an explicit resolution; resolved %d of %d", len(seen), len(pending))
	}
	return nil
}
