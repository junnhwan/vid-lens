package domain

import (
	"fmt"
	"slices"
)

type ArticleContextBlock struct {
	SectionID string              `json:"section_id"`
	Block     ArticleReadingBlock `json:"block"`
	Bindings  map[string]Citation `json:"citation_bindings"`
	Hash      string              `json:"hash"`
}

func ArticleReviewContextBlocks(a Article, ids []string) []ArticleContextBlock {
	out := []ArticleContextBlock{}
	for _, section := range ReadingArticle(a).Sections {
		for _, block := range section.Blocks {
			if !slices.Contains(ids, block.ID) {
				continue
			}
			bindings := map[string]Citation{}
			refs := slices.Clone(block.CitationIDs)
			for _, item := range block.Items {
				refs = append(refs, item.CitationIDs...)
			}
			for _, row := range block.Rows {
				refs = append(refs, row.CitationIDs...)
			}
			for _, ref := range refs {
				bindings[ref] = a.Citations[ref]
			}
			b := ArticleContextBlock{SectionID: section.ID, Block: block, Bindings: bindings}
			b.Hash = ContentHash(b)
			out = append(out, b)
		}
	}
	return out
}

// Reverse citations select real body content, never a coverage verdict. A miss
// leaves the question pending for the complete reading view.
func ArticleCrossChapterBlocks(a Article, scope ArticleReviewScope) []string {
	out := []string{}
	for _, section := range a.Sections {
		if slices.Contains(scope.SectionIDs, section.ID) {
			continue
		}
		for _, block := range ArticleReviewContextBlocks(a, blockIDs(section)) {
			for _, citation := range block.Bindings {
				if slices.ContainsFunc(citation.SegmentIDs, func(id string) bool {
					return slices.Contains(scope.SourceIDs, id) || slices.Contains(scope.ContextSourceIDs, id)
				}) {
					out = append(out, block.Block.ID)
					break
				}
			}
		}
	}
	return out
}

func blockIDs(s Section) []string {
	ids := []string{}
	for _, b := range s.Blocks {
		ids = append(ids, b.ID)
	}
	return ids
}

type ArticleFullView struct {
	CandidateHash string   `json:"candidate_hash"`
	ReadingHash   string   `json:"reading_hash"`
	BindingsHash  string   `json:"bindings_hash"`
	SectionIDs    []string `json:"section_ids"`
	BlockHashes   []string `json:"block_hashes"`
}

func ArticleFullViewManifest(a Article) ArticleFullView {
	out := ArticleFullView{CandidateHash: ArticleHash(a), ReadingHash: ContentHash(ReadingArticle(a)), BindingsHash: ContentHash(a.Citations), SectionIDs: []string{}, BlockHashes: []string{}}
	for _, section := range ReadingArticle(a).Sections {
		out.SectionIDs = append(out.SectionIDs, section.ID)
		for _, block := range section.Blocks {
			out.BlockHashes = append(out.BlockHashes, ContentHash([]any{section.ID, block}))
		}
	}
	return out
}

func ArticleReviewProjection(a Article, scope ArticleReviewScope) Article {
	out := a
	out.Sections = nil
	for _, section := range a.Sections {
		if slices.Contains(scope.SectionIDs, section.ID) {
			out.Sections = append(out.Sections, section)
		}
	}
	if !scope.Global {
		out.Limitations = nil
		for _, l := range a.Limitations {
			if len(l.SectionIDs) == 0 || slices.ContainsFunc(l.SectionIDs, func(id string) bool { return slices.Contains(scope.SectionIDs, id) }) {
				out.Limitations = append(out.Limitations, l)
			}
		}
	}
	return out
}

func ArticleReadingBindings(a Article, text []Segment) map[string]any {
	aliases := map[string]string{}
	for i, t := range text {
		aliases[t.ID] = fmt.Sprint("t", i+1)
	}
	used := map[string]bool{}
	for _, s := range ReadingArticle(a).Sections {
		for _, b := range s.Blocks {
			for _, id := range b.CitationIDs {
				used[id] = true
			}
			for _, item := range b.Items {
				for _, id := range item.CitationIDs {
					used[id] = true
				}
			}
			for _, row := range b.Rows {
				for _, id := range row.CitationIDs {
					used[id] = true
				}
			}
		}
	}
	for _, l := range a.Limitations {
		for _, id := range l.CitationIDs {
			used[id] = true
		}
	}
	out := map[string]any{}
	for id := range used {
		c := a.Citations[id]
		binding := map[string]any{"kind": c.Kind}
		if c.FrameID != "" {
			binding["frame_id"] = c.FrameID
		}
		if len(c.SegmentIDs) > 0 {
			refs := []string{}
			for _, id := range c.SegmentIDs {
				refs = append(refs, aliases[id])
			}
			binding["originals"] = refs
		}
		if c.Quote != nil {
			binding["verbatim_quote"] = *c.Quote
		}
		out[id] = binding
	}
	return out
}

// Receipt and report are stored as one immutable journal result. A staged
// report has no authority to write a draft, close findings or approve anything.
type ArticleReviewAdmission struct {
	RunID                 string                    `json:"run_id"`
	CandidateHash         string                    `json:"candidate_hash"`
	DraftVersion          int                       `json:"draft_version"`
	InputKey              string                    `json:"input_key"`
	InputHash             string                    `json:"input_hash"`
	ModelKey              string                    `json:"model_key"`
	Original              string                    `json:"original"`
	OriginalToolArguments string                    `json:"original_tool_arguments"`
	OriginalReport        ArticleReferenceReport    `json:"original_report"`
	FinalReport           ArticleReferenceReport    `json:"final_report"`
	Directory             []ArticleEvidenceEntry    `json:"directory"`
	Bindings              []ArticleReferenceBinding `json:"bindings"`
	Ticket                *ArticleReferenceTicket   `json:"ticket,omitempty"`
	Repair                *ArticleReferenceRepair   `json:"repair,omitempty"`
	RepairInputKey        string                    `json:"repair_input_key,omitempty"`
	RepairModelKey        string                    `json:"repair_model_key,omitempty"`
	FullView              *ArticleFullView          `json:"full_view,omitempty"`
	Questions             []ArticleCoverageQuestion `json:"questions"`
}

func ResolveArticleReferenceReport(receipt ArticleReviewAdmission, scope ArticleReviewScope, snapshot string, text []Segment) (ArticleReviewReport, []ArticleReferenceBinding, []ArticleReferenceError, error) {
	r, bindings, invalid, err := BindArticleReferences(receipt.FinalReport, receipt.Directory, snapshot, text, scope.FrameIDs)
	if err != nil || len(invalid) > 0 {
		return r, bindings, invalid, err
	}
	r.CoverageQuestions = slices.Clone(r.CoverageQuestions)
	for i := range r.CoverageQuestions {
		q := &r.CoverageQuestions[i]
		q.ID = ContentHash([]any{receipt.RunID, receipt.CandidateHash, scope.ID, receipt.InputHash, i, *q})
		q.OriginCandidate, q.OriginScope = receipt.CandidateHash, scope.ID
	}
	for _, decision := range r.CoverageDecisions {
		if decision.Outcome != "missing" {
			continue
		}
		location := "coverage:" + decision.QuestionID
		for _, q := range receipt.Questions {
			if q.ID == decision.QuestionID && q.TargetID != "" {
				location = q.TargetID
			}
		}
		proof, _, _, e := BindArticleReferences(ArticleReferenceReport{Issues: []ArticleReferenceIssue{{Severity: "blocking", AC: "AC02", Location: location, EvidenceRefs: decision.EvidenceRefs, Problem: decision.Explanation, Correction: decision.Correction}}}, receipt.Directory, snapshot, text, nil)
		if e != nil {
			return r, nil, nil, e
		}
		r.Issues = append(r.Issues, proof.Issues...)
	}
	return r, bindings, invalid, nil
}

func ArticleReadingHasBlock(a Article, id string) (string, bool) {
	for _, s := range a.Sections {
		for _, b := range s.Blocks {
			if b.ID == id {
				return s.ID, true
			}
		}
	}
	return "", false
}

// Validates authority and material identities, not semantic entailment.
func ValidateArticleCoverage(report ArticleReferenceReport, scope ArticleReviewScope, a Article, questions []ArticleCoverageQuestion, full *ArticleFullView) error {
	whole := full != nil && ContentHash(*full) == ContentHash(ArticleFullViewManifest(a))
	if full != nil && !whole {
		return fmt.Errorf("complete reading view identity mismatch")
	}
	if !whole && len(report.CoverageDecisions) > 0 {
		return fmt.Errorf("coverage decisions require the actual complete current reading view")
	}
	for _, issue := range report.Issues {
		if !whole && issue.AC == "AC02" {
			return fmt.Errorf("local AC02 findings must be coverage_questions; local absence cannot establish full-article omission")
		}
	}
	for _, q := range report.CoverageQuestions {
		if whole || q.ID != "" || q.OriginCandidate != "" || q.OriginScope != "" || q.Topic == "" || len(q.EvidenceRefs) == 0 || len(q.CheckedBlockIDs) == 0 {
			return fmt.Errorf("coverage question requires a local topic, original references and checked body locations; identities are program-owned")
		}
		for _, id := range q.CheckedBlockIDs {
			section, ok := ArticleReadingHasBlock(a, id)
			if !ok || !slices.Contains(scope.SectionIDs, section) && !slices.Contains(scope.ContextBlockIDs, id) {
				return fmt.Errorf("coverage question location was not delivered")
			}
		}
		switch q.RequiredPlacement {
		case "anywhere":
			if q.TargetID != "" {
				return fmt.Errorf("anywhere question cannot constrain a target")
			}
		case "block":
			section, ok := ArticleReadingHasBlock(a, q.TargetID)
			if !ok || !slices.Contains(scope.SectionIDs, section) {
				return fmt.Errorf("coverage question needs its actual target block")
			}
		case "section":
			if !slices.Contains(scope.SectionIDs, q.TargetID) {
				return fmt.Errorf("coverage question needs its actual target section")
			}
		default:
			return fmt.Errorf("unknown coverage placement")
		}
	}
	seen := map[string]bool{}
	for _, decision := range report.CoverageDecisions {
		at := slices.IndexFunc(questions, func(q ArticleCoverageQuestion) bool { return q.ID == decision.QuestionID })
		if at < 0 || seen[decision.QuestionID] || decision.Explanation == "" || len(decision.EvidenceRefs) == 0 {
			return fmt.Errorf("each pending coverage question needs one evidence-backed decision")
		}
		seen[decision.QuestionID] = true
		q := questions[at]
		switch decision.Outcome {
		case "covered":
			if len(decision.BlockIDs) == 0 {
				return fmt.Errorf("covered decision requires actual body witnesses")
			}
			for _, id := range decision.BlockIDs {
				section, ok := ArticleReadingHasBlock(a, id)
				if !ok || q.RequiredPlacement == "block" && id != q.TargetID || q.RequiredPlacement == "section" && section != q.TargetID {
					return fmt.Errorf("coverage witness is absent or violates required placement")
				}
			}
		case "missing":
			if decision.Correction == "" {
				return fmt.Errorf("missing coverage needs a concrete minimal correction")
			}
		case "unclear":
			return fmt.Errorf("coverage remains unassessed; cannot complete or approve")
		default:
			return fmt.Errorf("unknown coverage decision")
		}
	}
	if whole && len(seen) != len(questions) {
		return fmt.Errorf("unresolved coverage questions: decided %d of %d", len(seen), len(questions))
	}
	return nil
}
