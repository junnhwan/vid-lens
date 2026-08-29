package eval

import (
	"strings"
	"testing"
)

func TestBuildBlindReviewInputsFromArtifactsPairsCasesAndKeepsContextsWithOutput(t *testing.T) {
	baseline := blindArtifact("baseline", []CaseArtifact{
		blindCaseArtifact("c1", "g1", "baseline answer 1", "baseline context 1"),
		blindCaseArtifact("c2", "g2", "baseline answer 2", "baseline context 2"),
	})
	candidate := blindArtifact("candidate", []CaseArtifact{
		blindCaseArtifact("c2", "g2", "candidate answer 2", "candidate context 2"),
		blindCaseArtifact("c1", "g1", "candidate answer 1", "candidate context 1"),
	})
	inputs, err := BuildBlindReviewInputsFromArtifacts(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].CaseID != "c1" || inputs[1].CaseID != "c2" {
		t.Fatalf("paired inputs = %+v", inputs)
	}
	first := inputs[0]
	if first.Outputs[0].VariantID != "baseline" || first.Outputs[0].Response != "baseline answer 1" || first.Outputs[0].Contexts[0] != "baseline context 1" {
		t.Fatalf("baseline output = %+v", first.Outputs[0])
	}
	if first.Outputs[1].VariantID != "candidate" || first.Outputs[1].Response != "candidate answer 1" || first.Outputs[1].Contexts[0] != "candidate context 1" {
		t.Fatalf("candidate output = %+v", first.Outputs[1])
	}
	if first.Reference != "required fact" {
		t.Fatalf("reference = %q", first.Reference)
	}
}

func TestBuildBlindReviewInputsFromArtifactsRejectsProvenanceDriftAndMissingGeneration(t *testing.T) {
	baseline := blindArtifact("baseline", []CaseArtifact{blindCaseArtifact("c1", "g1", "answer", "context")})
	candidate := blindArtifact("candidate", []CaseArtifact{blindCaseArtifact("c1", "g1", "answer", "context")})
	candidate.Metadata.CorpusSHA256 = strings.Repeat("b", 64)
	if _, err := BuildBlindReviewInputsFromArtifacts(baseline, candidate); err == nil || !strings.Contains(err.Error(), "corpus") {
		t.Fatalf("provenance drift error = %v", err)
	}

	candidate = blindArtifact("candidate", []CaseArtifact{blindCaseArtifact("c1", "g1", "", "context")})
	if _, err := BuildBlindReviewInputsFromArtifacts(baseline, candidate); err == nil || !strings.Contains(err.Error(), "response") {
		t.Fatalf("missing generation error = %v", err)
	}
}

func blindArtifact(variant string, cases []CaseArtifact) RunArtifact {
	digest := strings.Repeat("a", 64)
	return RunArtifact{Metadata: RunMetadata{
		DatasetVersion: "rag-v1", DatasetSHA256: digest, SourceManifestSHA256: digest,
		CorpusSHA256: digest, ChunkManifestSHA256: digest, VectorArtifactSHA256: digest,
		Split: SplitDev, ExperimentID: "exp-1", VariantID: variant,
	}, Cases: cases}
}

func blindCaseArtifact(caseID, group, response, context string) CaseArtifact {
	c := Case{
		CaseID: caseID, VideoID: "video-" + caseID, SourceGroup: group, Split: SplitDev,
		Question: "question " + caseID, Category: "direct_fact", Answerable: true,
		AnswerPoints: []AnswerPoint{{ID: "p1", Text: "required fact", Required: true}},
	}
	return CaseArtifact{CaseID: caseID, Result: EvaluationCaseResult{
		Case: c, Response: response,
		Retrieved: []RetrievedContext{{ContextID: caseID + "-ctx", Text: context}},
	}}
}
