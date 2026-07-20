package ragtool

import (
	"testing"
	"time"

	"vid-lens/internal/service"
)

func TestVideoAgentAnswerEvalComputesCoverageCitationAndRates(t *testing.T) {
	report := EvaluateVideoAgentAnswers([]VideoAgentAnswerEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:              "Who starts the war?",
				ExpectedChunkKeywords: []string{"Fire Nation", "war"},
				ExpectedAnswerPoints:  []string{"The Fire Nation starts the war."},
			},
			Answer: "Fire Nation starts the war, based on the cited transcript.",
			Citations: []service.RetrievedChunk{
				{ChunkIndex: 2, Content: "The Fire Nation starts the war."},
			},
			Trace:    []service.VideoAgentStep{{Tool: service.VideoAgentToolSearchTranscript}, {Tool: service.VideoAgentToolBuildCitedAnswer}},
			Duration: 100 * time.Millisecond,
		},
		{
			Case: RAGEvalCase{
				Question:              "What does the Avatar do?",
				ExpectedChunkKeywords: []string{"Avatar", "four nations"},
				ExpectedAnswerPoints:  []string{"The Avatar keeps peace between four nations."},
			},
			Answer:          "当前视频片段中没有找到相关信息。",
			Trace:           []service.VideoAgentStep{{Tool: service.VideoAgentToolSearchTranscript, Error: "no context"}},
			Duration:        300 * time.Millisecond,
			FallbackOrError: true,
			Error:           "no context",
		},
	})

	if report.TotalCases != 2 {
		t.Fatalf("TotalCases = %d, want 2", report.TotalCases)
	}
	if report.AnswerPointCoverage != 0.5 {
		t.Fatalf("AnswerPointCoverage = %.3f, want 0.5", report.AnswerPointCoverage)
	}
	if report.CitationHitRate != 0.5 {
		t.Fatalf("CitationHitRate = %.3f, want 0.5", report.CitationHitRate)
	}
	if report.NoAnswerRate != 0.5 {
		t.Fatalf("NoAnswerRate = %.3f, want 0.5", report.NoAnswerRate)
	}
	if report.AvgToolSteps != 1.5 {
		t.Fatalf("AvgToolSteps = %.3f, want 1.5", report.AvgToolSteps)
	}
	if report.FallbackErrorRate != 0.5 {
		t.Fatalf("FallbackErrorRate = %.3f, want 0.5", report.FallbackErrorRate)
	}
	if report.AvgLatencyMs != 200 {
		t.Fatalf("AvgLatencyMs = %.3f, want 200", report.AvgLatencyMs)
	}
	if len(report.Cases) != 2 || !report.Cases[0].CitationHit || !report.Cases[1].NoAnswer {
		t.Fatalf("case reports = %+v", report.Cases)
	}
}

func TestVideoAgentAnswerEvalMatchesAnswerPointsByDeterministicTerms(t *testing.T) {
	report := EvaluateVideoAgentAnswers([]VideoAgentAnswerEvalCaseResult{
		{
			Case: RAGEvalCase{
				Question:             "What happened?",
				ExpectedAnswerPoints: []string{"The Avatar had been missing for more than a hundred years."},
			},
			Answer: "The answer says Avatar missing hundred years, even without copying the full expected sentence.",
		},
	})

	if report.AnswerPointCoverage != 1 {
		t.Fatalf("AnswerPointCoverage = %.3f, want 1", report.AnswerPointCoverage)
	}
}
