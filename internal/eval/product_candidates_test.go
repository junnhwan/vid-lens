package eval

import (
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

func candidateFixture() (repository.FeedbackCandidate, CandidateReview) {
	c := repository.FeedbackCandidate{CandidateID: "feedback-1", Origin: "feedback", UserID: 7, SessionID: 8, Feedback: &model.ChatFeedback{UserID: 7, SessionID: 8, Rating: "problem", Category: "content"}, Question: "source question", ObservedAnswer: "WRONG MODEL OUTPUT MUST NOT BE GOLD", SourceGroup: "video-2", AssetVersion: "source-hash", Status: "candidate", Kind: "agent"}
	r := CandidateReview{CandidateID: c.CandidateID, CandidateSHA256: CandidateDigest(c), ReviewedBy: "fixture-reviewer", ReviewedDate: "2026-09-12", SourceChecked: true, RequiredPoints: []string{"source-backed fact"}, EvidenceGroups: [][]string{{"chunk:2"}}, AnnotationVersion: "human-v1"}
	return c, r
}
func TestCandidateAcceptanceRequiresIndependentBoundReview(t *testing.T) {
	c, r := candidateFixture()
	accepted, e := AcceptProductCandidate(c, r, 9)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(accepted)
	if strings.Contains(string(b), c.ObservedAnswer) || accepted.Split != "dev" || accepted.ReviewStatus != "accepted" || accepted.ReviewedBy != r.ReviewedBy || !accepted.SourceChecked || accepted.Turns[0].SessionID != 9 {
		t.Fatalf("bad acceptance %s", b)
	}
	tests := []func(*CandidateReview){func(r *CandidateReview) { r.SourceChecked = false }, func(r *CandidateReview) { r.ReviewedBy = "" }, func(r *CandidateReview) { r.ReviewedDate = "yesterday" }, func(r *CandidateReview) { r.CandidateSHA256 = "changed" }, func(r *CandidateReview) { r.RequiredPoints = nil }, func(r *CandidateReview) { r.EvidenceGroups = [][]string{{""}} }}
	for i, mutate := range tests {
		copy := r
		mutate(&copy)
		if _, e = AcceptProductCandidate(c, copy, 9); e == nil {
			t.Fatalf("missing review requirement %d accepted", i)
		}
	}
	if _, e = AcceptProductCandidate(c, r, 8); e == nil {
		t.Fatal("source session reuse accepted")
	}
	c.ObservedAnswer = "tampered"
	if _, e = AcceptProductCandidate(c, r, 9); e == nil {
		t.Fatal("modified candidate accepted")
	}
}

func TestProductFailureCandidateRetainsDependentTurnsWithoutInventingFeedback(t *testing.T) {
	c, r := candidateFixture()
	reviewed, e := AcceptProductCandidate(c, r, 9)
	if e != nil {
		t.Fatal(e)
	}
	reviewed.Turns = append(reviewed.Turns, ProductTurn{UserID: 7, SessionID: 9, Kind: "agent", Question: "follow up", Stream: true})
	results := []ProductResult{{CaseID: reviewed.ID, SourceGroup: reviewed.SourceGroup, Classification: "cancelled", Turns: []ProductObservation{{RunID: "cancelled-run", Status: "cancelled", StopReason: "request_cancelled"}}}}
	candidates, e := ProductFailureCandidates([]ProductCase{reviewed}, results, json.RawMessage(`{"executable_sha256":"fixture"}`))
	if e != nil || len(candidates) != 1 {
		t.Fatalf("%+v %v", candidates, e)
	}
	failure := candidates[0]
	if failure.Feedback != nil || failure.Origin != "product_result" || failure.RunID != "cancelled-run" {
		t.Fatalf("invented feedback %+v", failure)
	}
	r.CandidateID = failure.CandidateID
	r.CandidateSHA256 = CandidateDigest(failure)
	accepted, e := AcceptProductCandidate(failure, r, 10)
	if e != nil || len(accepted.Turns) != 2 || accepted.Turns[1].Question != "follow up" || accepted.Turns[1].SessionID != 10 {
		t.Fatalf("lost dependent turn %+v %v", accepted, e)
	}
	results[0].Classification = "completed"
	candidates, e = ProductFailureCandidates([]ProductCase{reviewed}, results, nil)
	if e != nil || len(candidates) != 0 {
		t.Fatal("completed answer became failure candidate")
	}
}
