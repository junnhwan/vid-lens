package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildClaimDiagnosticClassifiesErrorsAndKeepsLLMJudgeAuxiliary(t *testing.T) {
	report, err := BuildClaimDiagnostic(ClaimDiagnosticInput{
		CaseID: "case-1", VariantID: "hybrid", Answerable: true,
		RetrievedContextCount: 4, RelevantContextCount: 1, MinContextPrecision: 0.5,
		Rejected: true,
		AnswerPoints: []AnswerPointAssessment{
			{AnswerPointID: "p1", Covered: false, Provenance: llmAssessmentProvenance()},
		},
		Claims: []ResponseClaimAssessment{
			{ClaimID: "c1", Text: "unsupported", Supported: false, CitationMatched: false, Provenance: llmAssessmentProvenance()},
			{ClaimID: "c2", Text: "supported but wrongly cited", Supported: true, CitationMatched: false, Provenance: llmAssessmentProvenance()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range []ErrorCategory{
		ErrorRetrievalNoise, ErrorGenerationOmission, ErrorGenerationHallucination,
		ErrorCitationMismatch, ErrorWrongRejection,
	} {
		if !report.HasError(category) {
			t.Errorf("missing error category %q in %v", category, report.ErrorCategories)
		}
	}
	if report.HasError(ErrorRetrievalMiss) {
		t.Fatalf("retrieval miss should be false when relevant evidence was retrieved: %v", report.ErrorCategories)
	}
	if report.EvidenceGrade != EvidenceGradeAuxiliaryOnly {
		t.Fatalf("evidence grade = %q, want auxiliary_only", report.EvidenceGrade)
	}
}

func TestBuildClaimDiagnosticRequiresAuditableJudgeProvenanceAndHumanCanVerify(t *testing.T) {
	_, err := BuildClaimDiagnostic(ClaimDiagnosticInput{
		CaseID: "case-1", VariantID: "candidate", Answerable: true,
		RetrievedContextCount: 0, RelevantContextCount: 0, MinContextPrecision: 0.5,
		AnswerPoints: []AnswerPointAssessment{{AnswerPointID: "p1", Covered: false, Provenance: AssessmentProvenance{RaterKind: RaterLLM, RaterID: "judge", Model: "judge-model", PromptVersion: "v1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "prompt_sha256") {
		t.Fatalf("missing LLM provenance error = %v", err)
	}

	human := AssessmentProvenance{RaterKind: RaterHuman, RaterID: "reviewer-2"}
	report, err := BuildClaimDiagnostic(ClaimDiagnosticInput{
		CaseID: "case-2", VariantID: "candidate", Answerable: true,
		RetrievedContextCount: 0, RelevantContextCount: 0, MinContextPrecision: 0.5,
		AnswerPoints: []AnswerPointAssessment{{AnswerPointID: "p1", Covered: false, Provenance: human}},
		Claims:       []ResponseClaimAssessment{{ClaimID: "c1", Text: "claim", Supported: true, CitationMatched: true, Provenance: human}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasError(ErrorRetrievalMiss) || report.EvidenceGrade != EvidenceGradeHumanVerified {
		t.Fatalf("human report = %+v", report)
	}
}

func TestBuildBlindReviewBatchHidesVariantsRandomizesOrderAndWritesSeparatedKey(t *testing.T) {
	inputs := makeBlindInputs(60)
	batch, err := BuildBlindReviewBatch(inputs, 50, 17)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Rows) != 50 || len(batch.Key) != 50 {
		t.Fatalf("batch rows/key = %d/%d", len(batch.Rows), len(batch.Key))
	}
	keyByID := make(map[string]BlindReviewKeyEntry)
	swapped := 0
	for _, key := range batch.Key {
		keyByID[key.BlindID] = key
		if key.VariantA == "candidate" {
			swapped++
		}
	}
	if swapped == 0 || swapped == len(batch.Key) {
		t.Fatalf("output order was not randomized: candidate in A for %d/%d", swapped, len(batch.Key))
	}
	for _, row := range batch.Rows {
		serialized, _ := json.Marshal(row)
		if bytes.Contains(serialized, []byte("baseline")) || bytes.Contains(serialized, []byte("candidate")) {
			t.Fatalf("public review row leaked variant identity: %s", serialized)
		}
		if _, ok := keyByID[row.BlindID]; !ok {
			t.Fatalf("missing private key for %s", row.BlindID)
		}
	}

	var publicCSV, privateJSON bytes.Buffer
	if err := WriteBlindReviewCSV(&publicCSV, batch.Rows); err != nil {
		t.Fatal(err)
	}
	if err := WriteBlindReviewKeyJSON(&privateJSON, batch.Key); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(publicCSV.String(), "baseline") || strings.Contains(publicCSV.String(), "candidate") {
		t.Fatalf("public CSV leaked variant: %s", publicCSV.String())
	}
	if !strings.Contains(privateJSON.String(), "baseline") || !strings.Contains(privateJSON.String(), "candidate") {
		t.Fatalf("private key omitted variants: %s", privateJSON.String())
	}
}

func TestBuildBlindReviewBatchEnforcesCalibrationSampleRangeAndUniqueCases(t *testing.T) {
	if _, err := BuildBlindReviewBatch(makeBlindInputs(60), 49, 1); err == nil {
		t.Fatal("sample size below 50 unexpectedly accepted")
	}
	if _, err := BuildBlindReviewBatch(makeBlindInputs(101), 101, 1); err == nil {
		t.Fatal("sample size above 100 unexpectedly accepted")
	}
	inputs := makeBlindInputs(50)
	inputs[1].CaseID = inputs[0].CaseID
	if _, err := BuildBlindReviewBatch(inputs, 50, 1); err == nil || !strings.Contains(err.Error(), "duplicate case_id") {
		t.Fatalf("duplicate case error = %v", err)
	}
}

func TestCalibrateJudgeReportsAgreementKappaAndDisagreements(t *testing.T) {
	key := []BlindReviewKeyEntry{
		{BlindID: "blind-1", CaseID: "c1", VariantA: "baseline", VariantB: "candidate"},
		{BlindID: "blind-2", CaseID: "c2", VariantA: "candidate", VariantB: "baseline"},
		{BlindID: "blind-3", CaseID: "c3", VariantA: "baseline", VariantB: "candidate"},
		{BlindID: "blind-4", CaseID: "c4", VariantA: "candidate", VariantB: "baseline"},
	}
	human := []BlindRating{
		{BlindID: "blind-1", RaterID: "human", Preference: PreferenceA},
		{BlindID: "blind-2", RaterID: "human", Preference: PreferenceB},
		{BlindID: "blind-3", RaterID: "human", Preference: PreferenceTie},
		{BlindID: "blind-4", RaterID: "human", Preference: PreferenceA},
	}
	judge := []BlindRating{
		{BlindID: "blind-1", RaterID: "judge", Preference: PreferenceA},
		{BlindID: "blind-2", RaterID: "judge", Preference: PreferenceA},
		{BlindID: "blind-3", RaterID: "judge", Preference: PreferenceTie},
		{BlindID: "blind-4", RaterID: "judge", Preference: PreferenceA},
	}
	report, err := CalibrateJudge(key, human, judge)
	if err != nil {
		t.Fatal(err)
	}
	if report.Compared != 4 || report.Agreements != 3 || report.AgreementRate != 0.75 {
		t.Fatalf("agreement report = %+v", report)
	}
	if report.CohensKappa <= 0 || report.CohensKappa >= 1 {
		t.Fatalf("kappa = %f, want partial agreement", report.CohensKappa)
	}
	if len(report.Disagreements) != 1 || report.Disagreements[0].CaseID != "c2" || report.Disagreements[0].HumanVariant != "baseline" || report.Disagreements[0].JudgeVariant != "candidate" {
		t.Fatalf("disagreements = %+v", report.Disagreements)
	}
}

func llmAssessmentProvenance() AssessmentProvenance {
	return AssessmentProvenance{
		RaterKind: RaterLLM, RaterID: "offline-judge", Model: "judge-model",
		PromptVersion: "claim-v1", PromptSHA256: strings.Repeat("a", 64),
	}
}

func makeBlindInputs(count int) []BlindReviewInput {
	out := make([]BlindReviewInput, count)
	for i := range out {
		id := fmt.Sprintf("case-%03d", i+1)
		out[i] = BlindReviewInput{
			CaseID: id, SourceGroup: fmt.Sprintf("group-%02d", i%20), Category: []string{"direct_fact", "multi_evidence", "unanswerable"}[i%3],
			Question: "question " + id, Reference: "reference " + id,
			Outputs: [2]BlindVariantOutput{{VariantID: "baseline", Response: "first response " + id, Contexts: []string{"first context " + id}}, {VariantID: "candidate", Response: "second response " + id, Contexts: []string{"second context " + id}}},
		}
	}
	return out
}
