package eval

import (
	"strings"
	"testing"
)

func TestRenderedReportsExposeFailureRate(t *testing.T) {
	artifact := RunArtifact{
		Metadata: RunMetadata{RunID: "run-1"},
		Summary:  MetricReport{Overall: MetricResult{Cases: 4, FailedCases: 1, FailureRate: 0.25}},
	}
	markdown := RenderMarkdownReport(artifact)
	if !strings.Contains(markdown, "Failure Rate") || !strings.Contains(markdown, "0.250") {
		t.Fatalf("markdown report does not expose failure rate:\n%s", markdown)
	}
}
