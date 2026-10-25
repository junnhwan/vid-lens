package domain

import (
	"fmt"
	"slices"
	"strings"
)

func ValidateArticleReferenceMaterials(r ArticleReferenceReport, scope ArticleReviewScope, a Article, directory []ArticleEvidenceEntry, questions []ArticleCoverageQuestion, full *ArticleFullView) error {
	if !r.Complete || len(r.Unassessed) > 0 || len(r.Topics) == 0 {
		return fmt.Errorf("complete report requires all local obligations assessed and major topic mappings")
	}
	if err := ValidateArticleCoverage(r, scope, a, questions, full); err != nil {
		return err
	}
	textRefs := map[string]bool{}
	for _, e := range directory {
		textRefs[e.Ref] = true
	}
	textProof := func(refs []string) bool {
		return slices.ContainsFunc(refs, func(ref string) bool { return textRefs[ref] })
	}
	seen := map[string]bool{}
	for _, issue := range r.Issues {
		if issue.Location == "" || strings.TrimSpace(issue.Problem) == "" || strings.TrimSpace(issue.Correction) == "" || !slices.Contains([]string{"blocking", "advisory"}, issue.Severity) || !slices.Contains([]string{"AC02", "AC03", "AC04", "AC05", "AC06", "AC07"}, issue.AC) {
			return fmt.Errorf("finding needs concrete AC, severity, location, problem and correction")
		}
		if issue.Severity == "advisory" && issue.AC != "AC06" {
			return fmt.Errorf("only AC06 preferences may be advisory")
		}
		positions := strings.FieldsFunc(issue.Location, func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-')
		})
		known := scope.Global && slices.Contains(positions, "title")
		for _, section := range a.Sections {
			if !slices.Contains(scope.SectionIDs, section.ID) {
				continue
			}
			known = known || slices.Contains(positions, section.ID)
			for _, b := range section.Blocks {
				known = known || slices.Contains(positions, b.ID)
			}
		}
		if issue.AC != "AC02" && !known {
			return fmt.Errorf("finding must locate body actually delivered as primary material")
		}
		if issue.Severity == "blocking" {
			switch issue.AC {
			case "AC02", "AC03", "AC04", "AC07":
				if !textProof(issue.EvidenceRefs) {
					return fmt.Errorf("text finding needs an original in this frozen input")
				}
			case "AC05":
				if !slices.ContainsFunc(issue.EvidenceRefs, func(id string) bool { return slices.Contains(scope.FrameIDs, id) }) {
					return fmt.Errorf("visual finding needs an actually delivered frame")
				}
			}
		}
	}
	for _, resolution := range r.Resolutions {
		if seen[resolution.IssueID] || resolution.IssueID == "" || strings.TrimSpace(resolution.Explanation) == "" || !slices.Contains([]string{"fixed", "withdrawn", "remaining"}, resolution.Outcome) {
			return fmt.Errorf("resolution needs a unique prior issue, final outcome and explanation")
		}
		seen[resolution.IssueID] = true
	}
	for _, topic := range r.Topics {
		if strings.TrimSpace(topic.Topic) == "" || len(scope.SourceIDs) > 0 && len(topic.SourceIDs) == 0 {
			return fmt.Errorf("topic needs its major content and primary original positions")
		}
		for _, ref := range topic.SourceIDs {
			if !textRefs[ref] {
				return fmt.Errorf("topic refers to an undelivered original")
			}
		}
		if len(topic.SectionIDs) == 0 && len(r.CoverageQuestions) == 0 && !slices.ContainsFunc(r.Issues, func(i ArticleReferenceIssue) bool { return i.AC == "AC02" && i.Severity == "blocking" }) {
			return fmt.Errorf("unmapped major topic must retain a coverage obligation")
		}
		if len(topic.SectionIDs) == 0 && !scope.Global && !slices.ContainsFunc(r.CoverageQuestions, func(q ArticleCoverageQuestion) bool {
			return slices.ContainsFunc(q.EvidenceRefs, func(id string) bool { return slices.Contains(topic.SourceIDs, id) })
		}) {
			return fmt.Errorf("unmapped primary topic needs its own source-backed coverage question")
		}
		for _, id := range topic.SectionIDs {
			inPrimary := slices.Contains(scope.SectionIDs, id)
			inContext := slices.ContainsFunc(ArticleReviewContextBlocks(a, scope.ContextBlockIDs), func(b ArticleContextBlock) bool { return b.SectionID == id })
			if !inPrimary && !inContext {
				return fmt.Errorf("topic location is navigation only, not delivered body")
			}
		}
	}
	for _, q := range r.CoverageQuestions {
		if !textProof(q.EvidenceRefs) {
			return fmt.Errorf("coverage question needs actual original text")
		}
	}
	for _, d := range r.CoverageDecisions {
		if !textProof(d.EvidenceRefs) {
			return fmt.Errorf("coverage decision needs actual original text")
		}
	}
	return nil
}

func ValidateArticleReviewAdmission(record ArticleReviewRecord, a Article, snapshot string, text []Segment) error {
	r := record.Admission
	if r == nil || r.RunID == "" || r.CandidateHash != ArticleHash(a) || r.CandidateHash != record.CandidateHash || r.DraftVersion < 1 || r.InputHash == "" || r.InputKey == "" || r.ModelKey == "" {
		return fmt.Errorf("missing frozen admission identity")
	}
	final := r.OriginalReport
	_, _, invalid, err := BindArticleReferences(final, r.Directory, snapshot, text, record.Scope.FrameIDs)
	if err != nil {
		return err
	}
	if r.Ticket != nil {
		t := *r.Ticket
		id := t.ID
		t.ID = ""
		if id != ContentHash(t) || t.RunID != r.RunID || t.CandidateHash != r.CandidateHash || t.DraftVersion != r.DraftVersion || t.ScopeHash != ContentHash(record.Scope) || t.InputHash != r.InputHash || t.ReportHash != ContentHash(r.OriginalToolArguments) || ContentHash(t.Errors) != ContentHash(invalid) || r.Repair == nil || r.RepairInputKey == "" || r.RepairModelKey == "" {
			return fmt.Errorf("reference repair receipt identity mismatch")
		}
		final, err = ApplyArticleReferenceRepair(r.OriginalReport, *r.Ticket, *r.Repair)
		if err != nil {
			return err
		}
	} else if len(invalid) > 0 || r.Repair != nil || r.RepairInputKey != "" || r.RepairModelKey != "" {
		return fmt.Errorf("invalid references or an unbound repair")
	}
	if ContentHash(final) != ContentHash(r.FinalReport) {
		return fmt.Errorf("reference-only repair changed the staged report")
	}
	if err = ValidateArticleReferenceMaterials(final, record.Scope, a, r.Directory, r.Questions, r.FullView); err != nil {
		return err
	}
	bound, bindings, remaining, err := ResolveArticleReferenceReport(*r, record.Scope, snapshot, text)
	if err != nil || len(remaining) > 0 || ContentHash(bound) != ContentHash(record.Report) || ContentHash(bindings) != ContentHash(r.Bindings) || (len(record.VisualObservations) > 0 || len(final.VisualObservations) > 0) && ContentHash(record.VisualObservations) != ContentHash(final.VisualObservations) {
		return fmt.Errorf("final report does not match program-bound original references")
	}
	if record.Scope.Global && r.FullView == nil {
		return fmt.Errorf("global review needs actual current complete body")
	}
	return nil
}
