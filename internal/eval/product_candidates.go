package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"vid-lens/internal/repository"
)

// CandidateReview must be authored independently by a person who checked source
// material. observed_answer is deliberately absent: model output cannot be gold.
type CandidateReview struct {
	CandidateID       string     `json:"candidate_id"`
	CandidateSHA256   string     `json:"candidate_sha256"`
	ReviewedBy        string     `json:"reviewed_by"`
	ReviewedDate      string     `json:"reviewed_date"`
	SourceChecked     bool       `json:"source_checked"`
	RequiredPoints    []string   `json:"required_points"`
	EvidenceGroups    [][]string `json:"evidence_groups"`
	AnnotationVersion string     `json:"annotation_version"`
	ReviewNote        string     `json:"review_note,omitempty"`
}

func CandidateDigest(c repository.FeedbackCandidate) string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func AcceptProductCandidate(c repository.FeedbackCandidate, r CandidateReview, sessionID int64) (ProductCase, error) {
	fail := func() (ProductCase, error) {
		return ProductCase{}, errors.New("candidate requires matching independent human review, checked source, required points, evidence groups and a new evaluation session")
	}
	validOrigin := c.Origin == "feedback" && c.Feedback != nil && c.Feedback.Rating == "problem"
	validOrigin = validOrigin || (c.Origin == "product_result" && c.Feedback == nil && c.RunStatus != "completed")
	if c.Status != "candidate" || !validOrigin || c.CandidateID == "" || r.CandidateID != c.CandidateID || r.CandidateSHA256 != CandidateDigest(c) || strings.TrimSpace(r.ReviewedBy) == "" || !r.SourceChecked || strings.TrimSpace(r.AnnotationVersion) == "" || sessionID <= 0 || sessionID == c.SessionID || len(r.RequiredPoints) == 0 || len(r.EvidenceGroups) == 0 || strings.TrimSpace(c.Question) == "" || c.SourceGroup == "" || c.AssetVersion == "" {
		return fail()
	}
	if _, e := time.Parse(time.RFC3339, r.ReviewedDate); e != nil {
		if _, e = time.Parse("2006-01-02", r.ReviewedDate); e != nil {
			return fail()
		}
	}
	for _, p := range r.RequiredPoints {
		if strings.TrimSpace(p) == "" {
			return fail()
		}
	}
	for _, group := range r.EvidenceGroups {
		if len(group) == 0 {
			return fail()
		}
		for _, id := range group {
			if strings.TrimSpace(id) == "" {
				return fail()
			}
		}
	}
	family := "product-failure"
	if c.Feedback != nil {
		family = "feedback-" + c.Feedback.Category
	}
	turns := []ProductTurn{{UserID: c.UserID, SessionID: sessionID, Kind: c.Kind, Question: c.Question, TopK: 6, Stream: true}}
	if len(c.OriginalTurns) > 0 {
		if json.Unmarshal(c.OriginalTurns, &turns) != nil || len(turns) == 0 {
			return fail()
		}
		for i := range turns {
			turns[i].SessionID = sessionID
			turns[i].RunID = ""
		}
	}
	result := ProductCase{Version: ProductSchemaVersion, ID: fmt.Sprintf("%s-reviewed", c.CandidateID), SourceGroup: c.SourceGroup, Family: family, Split: "dev", AssetVersion: c.AssetVersion, AnnotationVersion: r.AnnotationVersion, ReviewStatus: "accepted", ReviewedBy: r.ReviewedBy, ReviewedDate: r.ReviewedDate, SourceChecked: true, ReviewNote: r.ReviewNote, RequiredPoints: append([]string(nil), r.RequiredPoints...), EvidenceGroups: r.EvidenceGroups, Turns: turns}
	if e := ValidateProductCases([]ProductCase{result}); e != nil {
		return ProductCase{}, e
	}
	return result, nil
}

// ProductFailureCandidates preserves the whole dependent turn sequence, including
// turns not reached after a failure. Collection is not review or acceptance.
func ProductFailureCandidates(cases []ProductCase, results []ProductResult, identity json.RawMessage) ([]repository.FeedbackCandidate, error) {
	if e := ValidateProductCases(cases); e != nil {
		return nil, e
	}
	byID := map[string]ProductCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	out := []repository.FeedbackCandidate{}
	for _, r := range results {
		c, ok := byID[r.CaseID]
		if !ok || c.SourceGroup != r.SourceGroup {
			return nil, errors.New("result does not match dataset")
		}
		if r.Classification == "completed" {
			continue
		}
		switch r.Classification {
		case "limited", "failed", "cancelled", "pending_confirmation":
		default:
			return nil, errors.New("unknown failure classification")
		}
		original, _ := json.Marshal(c.Turns)
		candidate := repository.FeedbackCandidate{CandidateID: "product-" + c.ID, Origin: "product_result", UserID: c.Turns[0].UserID, SessionID: c.Turns[0].SessionID, Question: c.Turns[0].Question, SourceGroup: c.SourceGroup, AssetVersion: c.AssetVersion, Kind: c.Turns[0].Kind, Status: "candidate", RunStatus: r.Classification, OriginalTurns: original, ExecutionIdentity: identity}
		candidate.ExecutionResult, _ = json.Marshal(r)
		if len(r.Turns) > 0 {
			last := r.Turns[len(r.Turns)-1]
			candidate.RunID = last.RunID
			candidate.ObservedAnswer = last.Answer
			candidate.StopReason = last.StopReason
			candidate.Model = last.Model
		}
		out = append(out, candidate)
	}
	return out, nil
}
