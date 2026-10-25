package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const ArticleConvergenceReviewV7 = "source-first-convergence-2026-10-24-v7"
const ArticleConvergenceReviewV8 = "complete-first-pass-2026-10-24-v8"
const ArticleConvergenceReviewV9 = "bounded-source-work-2026-10-24-v9"
const ArticleConvergenceReviewV10 = "measured-output-cap-2026-10-24-v10"
const ArticleConvergenceReviewV11 = "planned-review-work-2026-10-24-v11"
const ArticleConvergenceReviewV12 = "executable-author-budget-2026-10-24-v12"
const ArticleConvergenceReviewV13 = "semantic-review-work-2026-10-24-v13"
const ArticleSourceWorkRunes = 16000
const ArticleSourceRequirementUnits = 6
const ArticleReviewLocalWorkRunes = 24000
const ArticleCoverageWorkRunes = 24000
const ArticleCoverageDecisionUnits = 8

func ArticleWorkPlannedReview(revision string) bool {
	return revision == ArticleConvergenceReviewV11 || revision == ArticleConvergenceReviewV12 || revision == ArticleConvergenceReviewV13
}

func ArticleCompleteFirstPass(revision string) bool {
	return revision == ArticleConvergenceReviewV8 || revision == ArticleConvergenceReviewV9 || revision == ArticleConvergenceReviewV10 || ArticleWorkPlannedReview(revision)
}

func ArticleSourceFirstReview(revision string) bool {
	return revision == ArticleConvergenceReviewV7 || ArticleCompleteFirstPass(revision)
}

// Work is bounded independently of provider context capacity. A model may fit
// all originals yet spend its entire response budget reasoning over them.
func ArticleSourceWorkBatch(text []Segment, remaining []string) []string {
	refs, work := []string{}, 0
	for i, segment := range text {
		ref := fmt.Sprint("t", i+1)
		if !slices.Contains(remaining, ref) {
			continue
		}
		n := utf8.RuneCountInString(segment.Text)
		if work+n > ArticleSourceWorkRunes {
			break
		}
		refs = append(refs, ref)
		work += n
	}
	return refs
}

// Adjacent complete originals explain speech that crosses a batch boundary.
// They are context only and never extra classification or citation authority.
func ArticleSourceBoundaryRefs(text []Segment, primary []string) []string {
	out := []string{}
	for i := range text {
		if !slices.Contains(primary, fmt.Sprint("t", i+1)) {
			continue
		}
		for _, j := range []int{i - 1, i + 1} {
			if j >= 0 && j < len(text) {
				ref := fmt.Sprint("t", j+1)
				if !slices.Contains(primary, ref) && !slices.Contains(out, ref) {
					out = append(out, ref)
				}
			}
		}
	}
	return out
}

func ArticleReferenceReview(revision string) bool {
	return revision == ArticleConvergenceReviewV6 || ArticleSourceFirstReview(revision)
}

// Extracted without candidate prose. These are review obligations, not facts
// certified by a model and not permission to invent text missing in originals.
type ArticleSourceRequirement struct {
	Requirement  string   `json:"requirement" jsonschema:"description=One major source concept or coherent process, including its necessary steps, prerequisites, exceptions and uncertainty. Preserve attribution; no external verification demands."`
	EvidenceRefs []string `json:"evidence_refs"`
}
type ArticleIncidentalSource struct {
	Reason       string   `json:"reason" jsonschema:"description=Why this source passage adds only greetings, repetition or an optional example; do not discard an operational condition or the end of the video."`
	EvidenceRefs []string `json:"evidence_refs"`
}
type ArticleSourceRequirements struct {
	Complete     bool                       `json:"complete"`
	Requirements []ArticleSourceRequirement `json:"requirements"`
	Incidental   []ArticleIncidentalSource  `json:"incidental"`
}
type ArticleSourcePlan struct {
	RunID             string                    `json:"run_id"`
	SnapshotID        string                    `json:"snapshot_id"`
	PolicyRevision    string                    `json:"policy_revision"`
	InputKey          string                    `json:"input_key"`
	InputHash         string                    `json:"input_hash"`
	ModelKey          string                    `json:"model_key"`
	OriginalArguments string                    `json:"original_arguments"`
	PrimaryRefs       []string                  `json:"primary_refs"`
	Directory         []ArticleEvidenceEntry    `json:"directory"`
	Report            ArticleSourceRequirements `json:"report"`
}

func ValidateArticleSourceRequirements(r ArticleSourceRequirements, primary []string, directory []ArticleEvidenceEntry) error {
	if !r.Complete || r.Requirements == nil || r.Incidental == nil || len(primary) == 0 || len(r.Requirements)+len(r.Incidental) == 0 || len(r.Requirements)+len(r.Incidental) > 80 {
		return fmt.Errorf("source inventory must completely assess this source range with at most 80 coherent units")
	}
	seen := map[string]bool{}
	check := func(statement string, refs []string) error {
		if strings.TrimSpace(statement) == "" || len([]rune(statement)) > 1600 || len(refs) == 0 {
			return fmt.Errorf("source unit needs a concise substantive requirement/reason and actual references")
		}
		local := map[string]bool{}
		for _, ref := range refs {
			if !slices.Contains(primary, ref) || local[ref] || !slices.ContainsFunc(directory, func(e ArticleEvidenceEntry) bool { return e.Ref == ref }) {
				return fmt.Errorf("source unit refers to duplicate or undelivered primary original %s", ref)
			}
			local[ref], seen[ref] = true, true
		}
		return nil
	}
	for _, unit := range r.Requirements {
		if err := check(unit.Requirement, unit.EvidenceRefs); err != nil {
			return err
		}
	}
	for _, unit := range r.Incidental {
		if err := check(unit.Reason, unit.EvidenceRefs); err != nil {
			return err
		}
	}
	for _, ref := range primary {
		if !seen[ref] {
			return fmt.Errorf("source inventory did not assess %s; the final source range also remains mandatory", ref)
		}
	}
	return nil
}

// Granularity is part of the admitted policy, never retroactively applied to
// an older inventory. Bound major concepts, not the originals or their details.
func ValidateArticleSourceRequirementsForRevision(r ArticleSourceRequirements, primary []string, directory []ArticleEvidenceEntry, revision string) error {
	if err := ValidateArticleSourceRequirements(r, primary, directory); err != nil {
		return err
	}
	if ArticleWorkPlannedReview(revision) && (len(r.Requirements) > ArticleSourceRequirementUnits || len(r.Incidental) > 8) {
		return fmt.Errorf("source range needs at most %d coherent major concepts and 8 incidental groups; group related steps without dropping prerequisites, exceptions or uncertainty", ArticleSourceRequirementUnits)
	}
	return nil
}

func (p ArticleSourcePlan) Questions(key string) []ArticleCoverageQuestion {
	out := []ArticleCoverageQuestion{}
	for i, unit := range p.Report.Requirements {
		out = append(out, ArticleCoverageQuestion{ID: ContentHash([]any{key, i, unit}), OriginScope: key, Topic: unit.Requirement, EvidenceRefs: slices.Clone(unit.EvidenceRefs), CheckedBlockIDs: []string{}, RequiredPlacement: "anywhere"})
	}
	return out
}
