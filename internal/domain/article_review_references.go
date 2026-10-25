package domain

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

const ArticleConvergenceReviewV6 = "reference-coverage-2026-10-24-v6"

// Canonicalize JSON objects crossing the jsonb boundary. Existing revision
// identities deliberately keep using ContentHash with their original types.
func ArticleJSONHash(value any) string {
	var decoded any
	if err := json.Unmarshal(JSON(value), &decoded); err != nil {
		return ""
	}
	return ContentHash(decoded)
}

// Publication repeats the strict envelope check against the immutable raw
// tool string. json.Unmarshal alone would silently accept duplicate fields.
func ValidateArticleReferenceJSON(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	var visit func() error
	visit = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		close := json.Delim(']')
		if delim == '{' {
			close = '}'
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate or invalid review JSON key")
				}
				seen[key] = true
				if e = visit(); e != nil {
					return e
				}
			}
		} else if delim == '[' {
			for d.More() {
				if e := visit(); e != nil {
					return e
				}
			}
		} else {
			return fmt.Errorf("invalid review JSON delimiter")
		}
		t, err = d.Token()
		if err != nil || t != close {
			return fmt.Errorf("incomplete review JSON")
		}
		return nil
	}
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		return fmt.Errorf("review needs a complete JSON object")
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("review needs exactly one JSON object")
	}
	return nil
}

func DecodeArticleReferenceReport(raw []byte) (ArticleReferenceReport, error) {
	var report ArticleReferenceReport
	if err := ValidateArticleReferenceJSON(raw); err != nil {
		return report, err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&report); err != nil {
		return report, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, name := range []string{"complete", "unassessed", "topics", "issues", "resolutions", "visual_observations", "coverage_questions", "coverage_decisions"} {
		if fields[name] == nil || string(fields[name]) == "null" {
			return report, fmt.Errorf("complete reference report requires %s", name)
		}
	}
	return report, nil
}

// The directory belongs to one frozen input. Aliases are ordinal references,
// never fuzzy matches or assertions that the selected text supports a finding.
type ArticleEvidenceEntry struct {
	Ref        string `json:"ref"`
	SnapshotID string `json:"snapshot_id"`
	SegmentID  string `json:"segment_id"`
	TextHash   string `json:"text_hash"`
	Identity   string `json:"identity"`
}

func ArticleEvidenceDirectory(snapshot string, text []Segment, delivered []string) []ArticleEvidenceEntry {
	out := []ArticleEvidenceEntry{}
	for i, segment := range text {
		ref := fmt.Sprint("t", i+1)
		if slices.Contains(delivered, ref) {
			out = append(out, ArticleEvidenceEntry{ref, snapshot, segment.ID, ContentHash(segment.Text), ContentHash(segment)})
		}
	}
	return out
}

type ArticleReferenceIssue struct {
	Severity     string   `json:"severity" jsonschema:"enum=blocking,enum=advisory"`
	AC           string   `json:"ac" jsonschema:"enum=AC02,enum=AC03,enum=AC04,enum=AC05,enum=AC06,enum=AC07"`
	Location     string   `json:"location"`
	EvidenceRefs []string `json:"evidence_refs" jsonschema:"description=Only tN references in this input's evidence_directory or actual delivered frame IDs; the program binds exact original text. Do not recopy quotes."`
	Problem      string   `json:"problem"`
	Correction   string   `json:"correction"`
}
type ArticleReferenceResolution struct {
	IssueID      string   `json:"issue_id"`
	Outcome      string   `json:"outcome" jsonschema:"enum=fixed,enum=withdrawn,enum=remaining"`
	Explanation  string   `json:"explanation"`
	EvidenceRefs []string `json:"evidence_refs"`
}
type ArticleCoverageQuestion struct {
	ID                string   `json:"id,omitempty"`
	OriginCandidate   string   `json:"origin_candidate,omitempty"`
	OriginScope       string   `json:"origin_scope,omitempty"`
	Topic             string   `json:"topic"`
	EvidenceRefs      []string `json:"evidence_refs"`
	CheckedBlockIDs   []string `json:"checked_block_ids"`
	RequiredPlacement string   `json:"required_placement" jsonschema:"enum=block,enum=section,enum=anywhere"`
	TargetID          string   `json:"target_id,omitempty"`
}
type ArticleCoverageDecision struct {
	QuestionID   string   `json:"question_id"`
	Outcome      string   `json:"outcome" jsonschema:"enum=covered,enum=missing,enum=unclear"`
	EvidenceRefs []string `json:"evidence_refs"`
	BlockIDs     []string `json:"block_ids"`
	Explanation  string   `json:"explanation"`
	Correction   string   `json:"correction,omitempty"`
}
type ArticleReferenceReport struct {
	Complete           bool                         `json:"complete"`
	Unassessed         []string                     `json:"unassessed"`
	Topics             []ArticleTopic               `json:"topics"`
	Issues             []ArticleReferenceIssue      `json:"issues"`
	Resolutions        []ArticleReferenceResolution `json:"resolutions"`
	VisualObservations []ArticleVisualObservation   `json:"visual_observations"`
	CoverageQuestions  []ArticleCoverageQuestion    `json:"coverage_questions"`
	CoverageDecisions  []ArticleCoverageDecision    `json:"coverage_decisions"`
}

type ArticleReferenceError struct {
	Path string `json:"path"`
	Ref  string `json:"ref"`
}
type ArticleReferenceBinding struct {
	Path  string               `json:"path"`
	Entry ArticleEvidenceEntry `json:"entry"`
}

// Only individual invalid reference positions can be changed. A valid choice,
// findings, order, completion, observations and coverage remain frozen.
type ArticleReferenceTicket struct {
	ID            string                  `json:"id"`
	RunID         string                  `json:"run_id"`
	CandidateHash string                  `json:"candidate_hash"`
	DraftVersion  int                     `json:"draft_version"`
	ScopeHash     string                  `json:"scope_hash"`
	InputHash     string                  `json:"input_hash"`
	ReportHash    string                  `json:"report_hash"`
	Errors        []ArticleReferenceError `json:"errors"`
}
type ArticleReferencePatch struct {
	Path string `json:"path"`
	Ref  string `json:"ref"`
}
type ArticleReferenceRepair struct {
	TicketID string                  `json:"ticket_id"`
	Patches  []ArticleReferencePatch `json:"patches"`
}

// Iteration order is the stable wire order. Paths name one array cell, so a
// repair cannot delete a finding or replace an entire reference array.
func visitArticleReferences(r *ArticleReferenceReport, visit func(string, *string)) {
	for i := range r.Issues {
		for j := range r.Issues[i].EvidenceRefs {
			visit(fmt.Sprintf("/issues/%d/evidence_refs/%d", i, j), &r.Issues[i].EvidenceRefs[j])
		}
	}
	for i := range r.Resolutions {
		for j := range r.Resolutions[i].EvidenceRefs {
			visit(fmt.Sprintf("/resolutions/%d/evidence_refs/%d", i, j), &r.Resolutions[i].EvidenceRefs[j])
		}
	}
	for i := range r.CoverageQuestions {
		for j := range r.CoverageQuestions[i].EvidenceRefs {
			visit(fmt.Sprintf("/coverage_questions/%d/evidence_refs/%d", i, j), &r.CoverageQuestions[i].EvidenceRefs[j])
		}
	}
	for i := range r.CoverageDecisions {
		for j := range r.CoverageDecisions[i].EvidenceRefs {
			visit(fmt.Sprintf("/coverage_decisions/%d/evidence_refs/%d", i, j), &r.CoverageDecisions[i].EvidenceRefs[j])
		}
	}
}

func CopyArticleReferenceReport(r ArticleReferenceReport) ArticleReferenceReport {
	// JSON round trip also isolates every nested slice used by a repair.
	var out ArticleReferenceReport
	_ = json.Unmarshal(JSON(r), &out)
	return out
}

// Bind exactly the segments actually delivered. Caller validates the report's
// non-reference obligations before offering a ticket for any returned errors.
func BindArticleReferences(r ArticleReferenceReport, directory []ArticleEvidenceEntry, snapshot string, text []Segment, frames []string) (ArticleReviewReport, []ArticleReferenceBinding, []ArticleReferenceError, error) {
	delivered := []string{}
	for _, entry := range directory {
		delivered = append(delivered, entry.Ref)
	}
	if ContentHash(ArticleEvidenceDirectory(snapshot, text, delivered)) != ContentHash(directory) {
		return ArticleReviewReport{}, nil, nil, fmt.Errorf("evidence directory identity conflict")
	}
	entries := map[string]ArticleEvidenceEntry{}
	segments := map[string]Segment{}
	for i, segment := range text {
		segments[fmt.Sprint("t", i+1)] = segment
	}
	for _, entry := range directory {
		entries[entry.Ref] = entry
	}
	bindings := []ArticleReferenceBinding{}
	invalid := []ArticleReferenceError{}
	visitArticleReferences(&r, func(path string, ref *string) {
		if entry, ok := entries[*ref]; ok {
			bindings = append(bindings, ArticleReferenceBinding{path, entry})
		} else if !slices.Contains(frames, *ref) {
			invalid = append(invalid, ArticleReferenceError{path, *ref})
		}
	})
	quotes := func(refs []string) []ArticleEvidenceQuote {
		out := []ArticleEvidenceQuote{}
		for _, ref := range refs {
			if _, ok := entries[ref]; ok {
				out = append(out, ArticleEvidenceQuote{SourceID: ref, Quote: segments[ref].Text})
			}
		}
		return out
	}
	out := ArticleReviewReport{Complete: r.Complete, Unassessed: r.Unassessed, Topics: r.Topics, CoverageQuestions: r.CoverageQuestions, CoverageDecisions: r.CoverageDecisions}
	for _, i := range r.Issues {
		out.Issues = append(out.Issues, ArticleReviewIssue{Severity: i.Severity, AC: i.AC, Location: i.Location, Problem: i.Problem, Correction: i.Correction, EvidenceIDs: i.EvidenceRefs, EvidenceQuotes: quotes(i.EvidenceRefs)})
	}
	for _, resolution := range r.Resolutions {
		out.Resolutions = append(out.Resolutions, ArticleReviewResolution{IssueID: resolution.IssueID, Outcome: resolution.Outcome, Explanation: resolution.Explanation, EvidenceQuotes: quotes(resolution.EvidenceRefs)})
	}
	return out, bindings, invalid, nil
}

func ApplyArticleReferenceRepair(original ArticleReferenceReport, ticket ArticleReferenceTicket, repair ArticleReferenceRepair) (ArticleReferenceReport, error) {
	if ticket.ID == "" || repair.TicketID != ticket.ID || len(ticket.Errors) == 0 || len(repair.Patches) != len(ticket.Errors) {
		return ArticleReferenceReport{}, fmt.Errorf("reference repair ticket or patch count conflict")
	}
	out := CopyArticleReferenceReport(original)
	allowed := map[string]string{}
	for _, e := range ticket.Errors {
		allowed[e.Path] = e.Ref
	}
	patches := map[string]string{}
	for _, patch := range repair.Patches {
		if _, ok := allowed[patch.Path]; !ok || strings.TrimSpace(patch.Ref) == "" {
			return ArticleReferenceReport{}, fmt.Errorf("repair path is not an invalid reference cell")
		}
		if _, duplicate := patches[patch.Path]; duplicate {
			return ArticleReferenceReport{}, fmt.Errorf("duplicate repair path")
		}
		patches[patch.Path] = patch.Ref
	}
	changed := 0
	visitArticleReferences(&out, func(path string, ref *string) {
		if replacement, ok := patches[path]; ok && *ref == allowed[path] {
			*ref = replacement
			changed++
		}
	})
	if changed != len(ticket.Errors) {
		return ArticleReferenceReport{}, fmt.Errorf("staged reference positions changed")
	}
	return out, nil
}
