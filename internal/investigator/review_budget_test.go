package investigator

import (
	"context"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func TestEvidenceReviewReservesReasoningBudgetIndependentlyOfWriterPhase(t *testing.T) {
	p := config.DefaultProfile()
	p.MaxOutputTokens, p.InputCharsLimit, p.ContextTokens = 65536, 300000, 1048576
	policy, _ := domain.InterpretationPolicy("standard")
	for _, phase := range []string{"investigate", "repair", "final_submit"} {
		t.Run(phase, func(t *testing.T) {
			s := session{profile: p, reviewing: true, spec: domain.GenerationSpec{DetailPolicy: &policy}, cp: checkpoint{HarnessRevision: 16}}
			s.cp.DraftCorrection = phase == "repair"
			s.forceDraftSubmit = phase == "final_submit"
			b, err := s.budget(context.Background(), nil, 0)
			if err != nil || b.Phase != "evidence_review" || b.OutputTokens != 32768 {
				t.Fatalf("source review inherited an inadequate writer budget: %+v %v", b, err)
			}
			s.profile.MaxOutputTokens = 12000
			b, err = s.budget(context.Background(), nil, 0)
			if err != nil || b.OutputTokens != 12000 {
				t.Fatalf("review exceeded the frozen model capacity: %+v %v", b, err)
			}
		})
	}
	s := session{profile: p, reviewing: true, spec: domain.GenerationSpec{DetailPolicy: &policy}, cp: checkpoint{HarnessRevision: 15}}
	b, err := s.budget(context.Background(), nil, 0)
	if err != nil || b.OutputTokens != 4096 {
		t.Fatalf("historical review reservation changed: %+v %v", b, err)
	}
}
