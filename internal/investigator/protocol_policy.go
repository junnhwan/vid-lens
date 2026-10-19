package investigator

import "vidlens/internal/domain"

// executionPolicy interprets the protocol identity already frozen in the
// checkpoint. Runtime decisions never resolve against today's default policy.
// The value is cached per session and rebuilt only when initialization or the
// historical draft-resume transition explicitly sets that checkpoint identity.
type executionPolicy struct {
	Revision             int
	Enabled              bool
	Legacy               bool
	CitationContract     bool
	ContentPlan          bool
	Paced                bool
	ObservationBudget    bool
	CompactAnswers       bool
	SectionRepairs       bool
	ConceptMaps          bool
	ComposedAnswers      bool
	EvidenceFirstAnswers bool
	SectionAuthoring     bool
	Recovery             bool
	EvidenceGrounding    bool
	ContentReview        bool
	ReviewRecovery       bool
}

func policyAt(revision int) executionPolicy {
	return executionPolicy{Revision: revision, Enabled: revision > 0, Legacy: revision == 0,
		CitationContract:     revision >= 2,
		ContentPlan:          revision >= 3,
		Paced:                revision >= 4,
		ObservationBudget:    revision >= 5,
		CompactAnswers:       revision >= 6,
		SectionRepairs:       revision >= 7,
		ConceptMaps:          revision >= 9,
		ComposedAnswers:      revision >= 10,
		EvidenceFirstAnswers: revision >= 11,
		SectionAuthoring:     revision >= 12,
		Recovery:             revision >= 13,
		EvidenceGrounding:    revision >= 14,
		ContentReview:        revision >= 15,
		ReviewRecovery:       revision >= 17,
	}
}

func (s *session) policy() executionPolicy {
	if s.resolvedPolicy == nil || s.resolvedPolicy.Revision != s.cp.HarnessRevision {
		p := policyAt(s.cp.HarnessRevision)
		s.resolvedPolicy = &p
	}
	return *s.resolvedPolicy
}

// Resolve only a new protocol. A checkpoint that already has a layout retains
// its recorded revision and existing initialization behavior, including v0.
func initialProtocol(spec domain.GenerationSpec, question bool, layout string, newLayout bool, revision int) (int, string) {
	review := spec.DetailPolicy != nil && (spec.DetailPolicy.Revision == reviewPolicy || spec.DetailPolicy.Revision == reviewBudgetPolicy || spec.DetailPolicy.Revision == reviewRecoveryPolicy)
	instruction := instructions
	if layout == draftLayout {
		instruction += draftInstructions
	}
	if newLayout && spec.Version >= 2 && spec.DetailPolicy != nil {
		revision = 1
		instruction += harnessInstructions
		if !question {
			instruction += "\n" + spec.DetailPolicy.Instruction
			if spec.DetailPolicy.ContentPlan {
				revision = 3
				instruction += contentPlanInstructions
				instruction += planRepairInstructions
				if spec.DetailPolicy.Revision == "interpretation-2026-10-17-v4" {
					revision = 4
					instruction += pacedHarnessInstructions
				}
				if spec.DetailPolicy.Revision == "interpretation-2026-10-17-v5" {
					revision = 5
					instruction += pacedHarnessInstructions + observationBudgetInstructions
				}
				if spec.DetailPolicy.Revision == "interpretation-2026-10-17-v6" {
					revision = 7
					instruction += pacedHarnessInstructions + observationBudgetInstructions + efficientDraftInstructions
				}
				if spec.DetailPolicy.Revision == "interpretation-2026-10-17-v7" {
					revision = 9
					instruction += pacedHarnessInstructions + observationBudgetInstructions + efficientDraftInstructions + conceptMapInstructions
				}
			}
		} else {
			instruction += "\n本轮问答以当前问题要求的深度为先，不要求重写全片。文章详细程度只是表达偏好，不能限制后续追问的证据或解释深度。"
			// A new private turn freezes its own protocol before its first call.
			// Existing Ready checkpoints never pass through this initialization.
			revision = 11
			instruction += compactAnswerInstructions + observationBudgetInstructions + composedAnswerInstructions + evidenceFirstAnswerInstructions
			if spec.DetailPolicy.Revision == evidencePolicy || review {
				revision = 14
				instruction += evidenceGroundingInstructions
			}
			if review {
				revision = 15
				instruction += reviewAuthorInstructions
				if spec.DetailPolicy.Revision == reviewBudgetPolicy {
					revision = 16
				}
				if spec.DetailPolicy.Revision == reviewRecoveryPolicy {
					revision = 17
					instruction += reviewRecoveryInstructions
				}
			}
		}
	}
	if newLayout && !question && spec.DetailPolicy != nil && (spec.DetailPolicy.Revision == authoringPolicy || spec.DetailPolicy.Revision == recoveryPolicy || spec.DetailPolicy.Revision == evidencePolicy || review) {
		revision = 12
		instruction = authoringInstructions(spec.DetailPolicy.Instruction)
		if spec.DetailPolicy.Revision == recoveryPolicy || spec.DetailPolicy.Revision == evidencePolicy || review {
			revision = 13
			instruction += repairProgressInstructions
		}
		if spec.DetailPolicy.Revision == evidencePolicy || review {
			revision = 14
			instruction += evidenceGroundingInstructions
		}
		if review {
			revision = 15
			instruction += reviewAuthorInstructions
			if spec.DetailPolicy.Revision == reviewBudgetPolicy {
				revision = 16
			}
			if spec.DetailPolicy.Revision == reviewRecoveryPolicy {
				revision = 17
				instruction += reviewRecoveryInstructions
			}
		}
	}
	return revision, instruction
}
