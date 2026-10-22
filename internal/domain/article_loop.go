package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"
)

const ArticleLoopPolicy = "article-ac01-10-lc01-12-v1"

// These identities are owned by the program, never accepted from author tools.
type ArticleReviewScope struct {
	ID         string   `json:"id"`
	SectionIDs []string `json:"section_ids"`
	SourceIDs  []string `json:"source_ids"`
	FrameIDs   []string `json:"frame_ids"`
	Global     bool     `json:"global"`
}
type ArticleReviewIssue struct {
	Severity    string   `json:"severity" jsonschema:"enum=blocking,enum=advisory"`
	AC          string   `json:"ac" jsonschema:"description=AC02 through AC07"`
	Location    string   `json:"location" jsonschema:"description=Candidate section/block or missing topic"`
	EvidenceIDs []string `json:"evidence_ids" jsonschema:"description=Delivered tN/fN aliases or their exact original/frame IDs from this review scope"`
	Problem     string   `json:"problem"`
	Correction  string   `json:"correction"`
}
type ArticleReviewReport struct {
	Complete   bool                 `json:"complete" jsonschema:"description=True only when all required semantic review obligations are assessed; requires unassessed to be an empty array"`
	Unassessed []string             `json:"unassessed" jsonschema:"description=Only required review obligations that remain unassessed. Must be empty when complete is true. Material already classified as irrelevant is assessed, not an unassessed obligation; optional observations belong in advisory issues"`
	Topics     []ArticleTopic       `json:"topics" jsonschema:"description=Major source topics and substantive candidate locations; include the end of the video"`
	Issues     []ArticleReviewIssue `json:"issues"`
}
type ArticleTopic struct {
	Topic      string   `json:"topic"`
	SourceIDs  []string `json:"source_ids" jsonschema:"description=Delivered tN aliases or exact original IDs from this review scope"`
	SectionIDs []string `json:"section_ids"`
}
type ArticleReviewRecord struct {
	CandidateHash       string              `json:"candidate_hash"`
	EvidenceFingerprint string              `json:"evidence_fingerprint"`
	PolicyRevision      string              `json:"policy_revision"`
	Scope               ArticleReviewScope  `json:"scope"`
	Report              ArticleReviewReport `json:"report"`
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
	return ContentHash(struct {
		Policy string
		Spec   GenerationSpec
	}{ArticleLoopPolicy, spec})
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
