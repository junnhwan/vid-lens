package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

const ArticleLoopPolicy = "article-ac01-10-lc01-12-v1"
const ArticleConvergenceReviewV1 = "scoped-convergence-2026-10-23-v1"
const ArticleConvergenceReviewV2 = "integrated-convergence-2026-10-23-v2"
const ArticleConvergenceReviewV3 = "delta-convergence-2026-10-23-v3"

const ArticleConvergenceReviewV4 = "evidence-convergence-2026-10-23-v4"

func ArticleConvergenceReview(revision string) bool {
	return revision == ArticleConvergenceReviewV1 || revision == ArticleConvergenceReviewV2 || revision == ArticleConvergenceReviewV3 || revision == ArticleConvergenceReviewV4
}

// These identities are owned by the program, never accepted from author tools.
type ArticleReviewScope struct {
	ID               string   `json:"id"`
	SectionIDs       []string `json:"section_ids"`
	SourceIDs        []string `json:"source_ids"`
	ContextSourceIDs []string `json:"context_source_ids,omitempty"`
	FrameIDs         []string `json:"frame_ids"`
	Global           bool     `json:"global"`
}
type ArticleEvidenceQuote struct {
	SourceID string `json:"source_id" jsonschema:"description=Delivered tN alias or exact original ID"`
	Quote    string `json:"quote" jsonschema:"description=Short verbatim passage from this original, not a paraphrase"`
}

type ArticleReviewIssue struct {
	EvidenceQuotes []ArticleEvidenceQuote `json:"evidence_quotes,omitempty" jsonschema:"description=For blocking text findings, short verbatim source passages establishing the actual conflict or missing binding; checked against originals. Do not infer absence from a partial source range."`
	Severity       string                 `json:"severity" jsonschema:"enum=blocking,enum=advisory,description=Use blocking for every remaining factual/citation/coverage/visual/uncertainty defect. Advisory is allowed ONLY for AC06 wording/layout preferences. Omit withdrawn criticism."`
	AC             string                 `json:"ac" jsonschema:"description=AC02 through AC07. AC02/AC03/AC04/AC05/AC07 require severity=blocking; only AC06 permits advisory."`
	Location       string                 `json:"location" jsonschema:"description=Candidate section/block or missing topic"`
	EvidenceIDs    []string               `json:"evidence_ids" jsonschema:"description=Delivered tN/fN aliases or their exact original/frame IDs from this review scope"`
	Problem        string                 `json:"problem"`
	Correction     string                 `json:"correction"`
}

// Reject an inconsistent report instead of silently regrading a model finding.
// The reviewer must decide whether a concrete defect remains. Its abandoned
// criticism must not become a blocking instruction to rewrite a correct draft.
func ValidateArticleReviewSeverity(issues []ArticleReviewIssue) error {
	for _, issue := range issues {
		if issue.Severity == "advisory" && issue.AC != "AC06" {
			return fmt.Errorf("inconsistent review: %s content defects cannot be waived as advisory. Report a concrete remaining defect as blocking; if your final conclusion is that there is no defect, omit this issue. General wording/layout preferences belong to AC06 advisory. Return final findings only, without withdrawn criticism or reasoning history", issue.AC)
		}
	}
	return nil
}

type ArticleReviewResolution struct {
	IssueID        string                 `json:"issue_id"`
	Outcome        string                 `json:"outcome" jsonschema:"enum=fixed,enum=withdrawn,enum=remaining"`
	Explanation    string                 `json:"explanation"`
	EvidenceQuotes []ArticleEvidenceQuote `json:"evidence_quotes,omitempty"`
}

type ArticleReviewReport struct {
	Resolutions []ArticleReviewResolution `json:"resolutions,omitempty" jsonschema:"description=Resolve every unresolved_findings issue_id exactly once: fixed only after a real candidate change, withdrawn only after checking counter-evidence, remaining requires a blocking issue. No silent omission."`
	Complete    bool                      `json:"complete" jsonschema:"description=True only when all required semantic review obligations are assessed; requires unassessed to be an empty array"`
	Unassessed  []string                  `json:"unassessed" jsonschema:"description=Only required review obligations that remain unassessed. Must be empty when complete is true. Material already classified as irrelevant is assessed, not an unassessed obligation; optional observations belong in advisory issues"`
	Topics      []ArticleTopic            `json:"topics" jsonschema:"description=Major source topics and substantive candidate locations; include the end of the video"`
	Issues      []ArticleReviewIssue      `json:"issues"`
}

// Observations are the reviewer's direct account of delivered pixels, never
// copied from the author's observations or a caption. They are evidence, not
// per-paragraph supported labels or a second publication review.
type ArticleVisualObservation struct {
	FrameID      string   `json:"frame_id" jsonschema:"description=One exact frame ID from delivered_pixels; do not use a transcript ID"`
	VisibleFacts []string `json:"visible_facts" jsonschema:"description=Concrete legible facts directly visible in this image, recorded independently before judging candidate claims; no narrative or temporal inference"`
	Uncertainty  []string `json:"uncertainty" jsonschema:"description=What the image cannot establish or read; empty array only when no relevant uncertainty"`
}

type ArticleGroundedReviewReport struct {
	VisualObservations []ArticleVisualObservation `json:"visual_observations" jsonschema:"description=Independent visible facts and limits for every delivered frame in this review scope; assess caption and visual claims against these observations and report unsupported claims as AC05 blocking"`
	ArticleReviewReport
}
type ArticleTopic struct {
	Topic      string   `json:"topic"`
	SourceIDs  []string `json:"source_ids" jsonschema:"description=Delivered tN aliases or exact original IDs from this review scope"`
	SectionIDs []string `json:"section_ids"`
}
type ArticleReviewRecord struct {
	ReconcilesReviewKey  string                     `json:"reconciles_review_key,omitempty"`
	ReconcilesReviewHash string                     `json:"reconciles_review_hash,omitempty"`
	CandidateHash        string                     `json:"candidate_hash"`
	EvidenceFingerprint  string                     `json:"evidence_fingerprint"`
	PolicyRevision       string                     `json:"policy_revision"`
	Scope                ArticleReviewScope         `json:"scope"`
	Report               ArticleReviewReport        `json:"report"`
	VisualObservations   []ArticleVisualObservation `json:"visual_observations,omitempty"`
	MaterialHash         string                     `json:"material_hash,omitempty"`
	ReusedFrom           string                     `json:"reused_from,omitempty"`
	ReuseKind            string                     `json:"reuse_kind,omitempty"`
	Delta                *ArticleReviewDelta        `json:"delta,omitempty"`
}
type ArticleApproval struct {
	CandidateHash       string   `json:"candidate_hash"`
	EvidenceFingerprint string   `json:"evidence_fingerprint"`
	PolicyRevision      string   `json:"policy_revision"`
	DraftVersion        int      `json:"draft_version"`
	ReviewKeys          []string `json:"review_keys"`
}

func ArticleHash(a Article) string {
	// Only server-assigned publication metadata may change after review.
	a.ID = ""
	a.PublishedAt = time.Time{}
	return ContentHash(a)
}

func ContentHash(value any) string { h := sha256.Sum256(JSON(value)); return hex.EncodeToString(h[:]) }
func ArticlePolicyIdentity(spec GenerationSpec) string {
	return ArticlePolicyIdentityForContract(spec, ArticleLoopV1)
}

func ArticlePolicyIdentityForContract(spec GenerationSpec, contract string) string {
	policy := ArticleLoopPolicy
	if contract == ArticleLoopV2 {
		policy += ":independent-pixels-v2:article-first-v1"
	}
	return ContentHash(struct {
		Policy string
		Spec   GenerationSpec
	}{policy, spec})
}

// 新运行冻结复核修订；既有检查点的空修订继续使用原策略身份。
func ArticleReviewPolicyIdentity(spec GenerationSpec, contract, revision string) string {
	base := ArticlePolicyIdentityForContract(spec, contract)
	if revision == "" {
		return base
	}
	return ContentHash([]string{base, revision})
}

// 局部材料包含完整章节、实际引用绑定、相关局限及全篇结构导航。
// 相同材料只能复用局部结论，全文批准和跨章核对始终属于当前候选。
func ArticleReviewMaterialIdentity(a Article, scope ArticleReviewScope, snap Snapshot, text []Segment, assets []Asset) string {
	projected := a
	projected.Sections, projected.Limitations = nil, nil
	projected.Citations = map[string]Citation{}
	structure := [][2]string{}
	for _, section := range a.Sections {
		structure = append(structure, [2]string{section.ID, section.Title})
		if slices.Contains(scope.SectionIDs, section.ID) {
			projected.Sections = append(projected.Sections, section)
		}
	}
	for _, limitation := range a.Limitations {
		if len(limitation.SectionIDs) == 0 || slices.ContainsFunc(limitation.SectionIDs, func(id string) bool { return slices.Contains(scope.SectionIDs, id) }) {
			projected.Limitations = append(projected.Limitations, limitation)
		}
	}
	reading := ReadingArticle(projected)
	used := map[string]bool{}
	for _, section := range reading.Sections {
		for _, block := range section.Blocks {
			for _, id := range block.CitationIDs {
				used[id] = true
			}
			for _, item := range block.Items {
				for _, id := range item.CitationIDs {
					used[id] = true
				}
			}
			for _, row := range block.Rows {
				for _, id := range row.CitationIDs {
					used[id] = true
				}
			}
		}
	}
	for _, limitation := range projected.Limitations {
		for _, id := range limitation.CitationIDs {
			used[id] = true
		}
	}
	for id := range used {
		projected.Citations[id] = a.Citations[id]
	}
	frames := []Asset{}
	for _, asset := range assets {
		if slices.Contains(scope.FrameIDs, asset.ID) {
			frames = append(frames, asset)
		}
	}
	return ContentHash(struct {
		Article, Evidence string
		Scope             ArticleReviewScope
		Structure         [][2]string
	}{
		ArticleHash(projected), ArticleEvidenceIdentity(snap, text, frames), scope, structure,
	})
}

// ValidateArticleVisualObservations checks coverage and evidence identities.
// Whether visible facts support a claim remains a semantic review obligation.
func ValidateArticleVisualObservations(observations []ArticleVisualObservation, scope ArticleReviewScope) error {
	seen := map[string]bool{}
	for _, observation := range observations {
		id := observation.FrameID
		if !slices.Contains(scope.FrameIDs, id) || seen[id] {
			return fmt.Errorf("visual observation frame %q is duplicate or outside delivered scope", id)
		}
		if len(observation.VisibleFacts) == 0 {
			return fmt.Errorf("visual observation %s needs independent visible facts", id)
		}
		for _, fact := range append(append([]string{}, observation.VisibleFacts...), observation.Uncertainty...) {
			if strings.TrimSpace(fact) == "" {
				return fmt.Errorf("visual observation %s contains an empty fact or uncertainty", id)
			}
		}
		seen[id] = true
	}
	for _, id := range scope.FrameIDs {
		if !seen[id] {
			return fmt.Errorf("review incomplete: delivered frame %s has no independent pixel observation; inspect pixels and compare all corresponding candidate visual claims", id)
		}
	}
	return nil
}
func ArticleEvidenceIdentity(snap Snapshot, text []Segment, assets []Asset) string {
	type frame struct {
		ID, Hash string
		Time     *int64
		Crop     *Crop
	}
	fs := []frame{}
	for _, a := range assets {
		fs = append(fs, frame{a.ID, a.SHA256, a.TimeMS, a.Crop})
	}
	slices.SortFunc(fs, func(a, b frame) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return ContentHash(struct {
		ID, TextHash, VideoHash string
		Duration                int64
		Source                  Source
		Text                    []Segment
		Frames                  []frame
	}{snap.ID, snap.TextHash, snap.VideoHash, snap.DurationMS, snap.Source, text, fs})
}
