package investigator

import "vidlens/internal/domain"

// executionPolicy interprets the protocol identity already frozen in the
// checkpoint. Runtime decisions never resolve against today's default policy.
// The value is cached per session. Bootstrap selects a compatible revision
// once; a continuation retains the checkpoint identity and its counters.
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
	}
}

func (s *session) policy() executionPolicy {
	if s.articleLoop() {
		// These are shared transport/identity capabilities, not legacy planning,
		// pacing, authoring, recovery or per-unit review policy.
		return executionPolicy{Enabled: true, ObservationBudget: true, EvidenceGrounding: true}
	}
	if s.resolvedPolicy == nil || s.resolvedPolicy.Revision != s.cp.HarnessRevision {
		p := policyAt(s.cp.HarnessRevision)
		s.resolvedPolicy = &p
	}
	return *s.resolvedPolicy
}

// Resolve only a new protocol. A checkpoint that already has a layout retains
// its recorded revision and existing initialization behavior, including v0.
func initialProtocol(spec domain.GenerationSpec, question bool, layout string, newLayout bool, revision int) (int, string) {
	if !newLayout {
		instruction := instructions
		if layout == draftLayout {
			instruction += draftInstructions
		}
		return revision, instruction
	}
	p := spec.DetailPolicy
	review := p != nil && (p.Revision == reviewPolicy || p.Revision == reviewBudgetPolicy)
	if spec.Version >= 2 && p != nil {
		revision = 1
		if !question {
			if p.ContentPlan {
				revision = 3
				if p.Revision == "interpretation-2026-10-17-v4" {
					revision = 4
				}
				if p.Revision == "interpretation-2026-10-17-v5" {
					revision = 5
				}
				if p.Revision == "interpretation-2026-10-17-v6" {
					revision = 7
				}
				if p.Revision == "interpretation-2026-10-17-v7" {
					revision = 9
				}
			}
		} else {
			revision = 11
			if p.Revision == evidencePolicy || review {
				revision = 14
			}
			if review {
				revision = 15
				if p.Revision == reviewBudgetPolicy {
					revision = 16
				}
			}
		}
	}
	if !question && p != nil && (p.Revision == authoringPolicy || p.Revision == recoveryPolicy || p.Revision == evidencePolicy || review) {
		revision = 12
		if p.Revision == recoveryPolicy || p.Revision == evidencePolicy || review {
			revision = 13
		}
		if p.Revision == evidencePolicy || review {
			revision = 14
		}
		if review {
			revision = 15
			if p.Revision == reviewBudgetPolicy {
				revision = 16
			}
		}
	}
	return revision, protocolInstructions(spec, question, layout, revision)
}

// Both fresh input and inherited draft input use the recorded execution
// revision. The immutable detail policy supplies user intent and budgets;
// its revision is never rewritten to select a prompt.
func protocolInstructions(spec domain.GenerationSpec, question bool, layout string, revision int) string {
	detail := ""
	if spec.DetailPolicy != nil {
		detail = spec.DetailPolicy.Instruction
	}
	var instruction string
	if !question && revision >= 12 {
		instruction = authoringInstructions(detail)
		if revision >= 13 {
			instruction += repairProgressInstructions
		}
	} else {
		instruction = instructions
		if layout == draftLayout {
			instruction += draftInstructions
		}
		if revision > 0 {
			instruction += harnessInstructions
			if question {
				instruction += "\n本轮问答以当前问题要求的深度为先，不要求重写全片。文章详细程度只是表达偏好，不能限制后续追问的证据或解释深度。"
				if revision >= 6 {
					instruction += compactAnswerInstructions
				}
				if revision >= 5 {
					instruction += observationBudgetInstructions
				}
				if revision >= 10 {
					instruction += composedAnswerInstructions
				}
				if revision >= 11 {
					instruction += evidenceFirstAnswerInstructions
				}
			} else {
				instruction += "\n" + detail
				if revision >= 3 {
					instruction += contentPlanInstructions + planRepairInstructions
				}
				if revision >= 4 {
					instruction += pacedHarnessInstructions
				}
				if revision >= 5 {
					instruction += observationBudgetInstructions
				}
				if revision >= 7 {
					instruction += efficientDraftInstructions
				}
				if revision >= 9 {
					instruction += conceptMapInstructions
				}
			}
		}
	}
	if revision >= 14 {
		instruction += evidenceGroundingInstructions
	}
	if revision >= 15 {
		instruction += reviewAuthorInstructions
	}
	return instruction
}
