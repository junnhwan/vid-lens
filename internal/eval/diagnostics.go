package eval

import (
	"fmt"
	"sort"
	"strings"
)

type ErrorCategory string

const (
	ErrorRetrievalMiss           ErrorCategory = "retrieval_miss"
	ErrorRetrievalNoise          ErrorCategory = "retrieval_noise"
	ErrorGenerationOmission      ErrorCategory = "generation_omission"
	ErrorGenerationHallucination ErrorCategory = "generation_hallucination"
	ErrorCitationMismatch        ErrorCategory = "citation_mismatch"
	ErrorWrongRejection          ErrorCategory = "wrong_rejection"
)

type RaterKind string

const (
	RaterHuman RaterKind = "human"
	RaterLLM   RaterKind = "llm"
)

type EvidenceGrade string

const (
	EvidenceGradeAuxiliaryOnly EvidenceGrade = "auxiliary_only"
	EvidenceGradeHumanVerified EvidenceGrade = "human_verified"
)

type AssessmentProvenance struct {
	RaterKind     RaterKind `json:"rater_kind"`
	RaterID       string    `json:"rater_id"`
	Model         string    `json:"model,omitempty"`
	PromptVersion string    `json:"prompt_version,omitempty"`
	PromptSHA256  string    `json:"prompt_sha256,omitempty"`
}

type AnswerPointAssessment struct {
	AnswerPointID string               `json:"answer_point_id"`
	Covered       bool                 `json:"covered"`
	EvidenceIDs   []string             `json:"evidence_ids,omitempty"`
	Reason        string               `json:"reason,omitempty"`
	Provenance    AssessmentProvenance `json:"provenance"`
}

type ResponseClaimAssessment struct {
	ClaimID         string               `json:"claim_id"`
	Text            string               `json:"text"`
	Supported       bool                 `json:"supported"`
	CitationMatched bool                 `json:"citation_matched"`
	EvidenceIDs     []string             `json:"evidence_ids,omitempty"`
	CitationIDs     []string             `json:"citation_ids,omitempty"`
	Reason          string               `json:"reason,omitempty"`
	Provenance      AssessmentProvenance `json:"provenance"`
}

type ClaimDiagnosticInput struct {
	CaseID                string                    `json:"case_id"`
	VariantID             string                    `json:"variant_id"`
	Answerable            bool                      `json:"answerable"`
	RetrievedContextCount int                       `json:"retrieved_context_count"`
	RelevantContextCount  int                       `json:"relevant_context_count"`
	MinContextPrecision   float64                   `json:"min_context_precision"`
	Rejected              bool                      `json:"rejected"`
	AnswerPoints          []AnswerPointAssessment   `json:"answer_points"`
	Claims                []ResponseClaimAssessment `json:"claims"`
}

type ClaimDiagnostic struct {
	CaseID          string                    `json:"case_id"`
	VariantID       string                    `json:"variant_id"`
	EvidenceGrade   EvidenceGrade             `json:"evidence_grade"`
	ErrorCategories []ErrorCategory           `json:"error_categories"`
	AnswerPoints    []AnswerPointAssessment   `json:"answer_points"`
	Claims          []ResponseClaimAssessment `json:"claims"`
}

func (d ClaimDiagnostic) HasError(category ErrorCategory) bool {
	for _, got := range d.ErrorCategories {
		if got == category {
			return true
		}
	}
	return false
}

func BuildClaimDiagnostic(input ClaimDiagnosticInput) (ClaimDiagnostic, error) {
	if strings.TrimSpace(input.CaseID) == "" || strings.TrimSpace(input.VariantID) == "" {
		return ClaimDiagnostic{}, fmt.Errorf("case_id and variant_id are required")
	}
	if input.RetrievedContextCount < 0 || input.RelevantContextCount < 0 || input.RelevantContextCount > input.RetrievedContextCount {
		return ClaimDiagnostic{}, fmt.Errorf("invalid retrieved/relevant context counts")
	}
	if input.MinContextPrecision <= 0 || input.MinContextPrecision > 1 {
		return ClaimDiagnostic{}, fmt.Errorf("min_context_precision must be in (0,1]")
	}

	humanVerified := len(input.AnswerPoints)+len(input.Claims) > 0
	seenPoints := make(map[string]bool, len(input.AnswerPoints))
	omitted := false
	for i := range input.AnswerPoints {
		point := input.AnswerPoints[i]
		if strings.TrimSpace(point.AnswerPointID) == "" {
			return ClaimDiagnostic{}, fmt.Errorf("answer_points[%d] missing answer_point_id", i)
		}
		if seenPoints[point.AnswerPointID] {
			return ClaimDiagnostic{}, fmt.Errorf("duplicate answer_point_id %q", point.AnswerPointID)
		}
		seenPoints[point.AnswerPointID] = true
		if err := point.Provenance.Validate(); err != nil {
			return ClaimDiagnostic{}, fmt.Errorf("answer_points[%d] provenance: %w", i, err)
		}
		humanVerified = humanVerified && point.Provenance.RaterKind == RaterHuman
		omitted = omitted || !point.Covered
	}

	seenClaims := make(map[string]bool, len(input.Claims))
	hallucination, citationMismatch := false, false
	for i := range input.Claims {
		claim := input.Claims[i]
		if strings.TrimSpace(claim.ClaimID) == "" || strings.TrimSpace(claim.Text) == "" {
			return ClaimDiagnostic{}, fmt.Errorf("claims[%d] missing claim_id or text", i)
		}
		if seenClaims[claim.ClaimID] {
			return ClaimDiagnostic{}, fmt.Errorf("duplicate claim_id %q", claim.ClaimID)
		}
		seenClaims[claim.ClaimID] = true
		if err := claim.Provenance.Validate(); err != nil {
			return ClaimDiagnostic{}, fmt.Errorf("claims[%d] provenance: %w", i, err)
		}
		humanVerified = humanVerified && claim.Provenance.RaterKind == RaterHuman
		hallucination = hallucination || !claim.Supported
		citationMismatch = citationMismatch || !claim.CitationMatched
	}

	categories := make([]ErrorCategory, 0, 6)
	if input.Answerable && input.RelevantContextCount == 0 {
		categories = append(categories, ErrorRetrievalMiss)
	}
	if input.RetrievedContextCount > 0 && float64(input.RelevantContextCount)/float64(input.RetrievedContextCount) < input.MinContextPrecision {
		categories = append(categories, ErrorRetrievalNoise)
	}
	if omitted {
		categories = append(categories, ErrorGenerationOmission)
	}
	if hallucination {
		categories = append(categories, ErrorGenerationHallucination)
	}
	if citationMismatch {
		categories = append(categories, ErrorCitationMismatch)
	}
	if input.Answerable && input.Rejected {
		categories = append(categories, ErrorWrongRejection)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i] < categories[j] })

	grade := EvidenceGradeAuxiliaryOnly
	if humanVerified {
		grade = EvidenceGradeHumanVerified
	}
	return ClaimDiagnostic{
		CaseID: input.CaseID, VariantID: input.VariantID, EvidenceGrade: grade,
		ErrorCategories: categories, AnswerPoints: input.AnswerPoints, Claims: input.Claims,
	}, nil
}

func (p AssessmentProvenance) Validate() error {
	if strings.TrimSpace(p.RaterID) == "" {
		return fmt.Errorf("rater_id is required")
	}
	switch p.RaterKind {
	case RaterHuman:
		return nil
	case RaterLLM:
		if strings.TrimSpace(p.Model) == "" {
			return fmt.Errorf("model is required for LLM judge")
		}
		if strings.TrimSpace(p.PromptVersion) == "" {
			return fmt.Errorf("prompt_version is required for LLM judge")
		}
		if !isSHA256(strings.ToLower(strings.TrimSpace(p.PromptSHA256))) {
			return fmt.Errorf("prompt_sha256 must be a 64-character SHA-256 digest for LLM judge")
		}
		return nil
	default:
		return fmt.Errorf("unsupported rater_kind %q", p.RaterKind)
	}
}
