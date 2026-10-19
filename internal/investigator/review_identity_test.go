package investigator

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestReviewRecoveryCompactFieldsNeverGuessTargets(t *testing.T) {
	candidate := map[string]any{"summary": "实际已有字段。", "title": "另一个字段。"}
	fields := reviewFields(candidate, evidenceReviewBatch{Mode: "source_text"})
	catalog := map[string]domain.Citation{"t1": {Kind: "video_text"}}
	for _, tc := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"bound_id", `{"issues":[{"field":"F1","reason":"原文条件不同。","citation_ids":["t1"]}]}`, true},
		{"redundant_echo", `{"candidate_hash":"untrusted-echo","verdict":"supported","issues":[{"field":"F1","reason":"原文条件不同。","citation_ids":["t1"]}]}`, true},
		{"unknown_id", `{"issues":[{"field":"F999","reason":"原文条件不同。","citation_ids":["t1"]}]}`, false},
		{"conflicting_target", `{"issues":[{"field":"F1","path":"/title","reason":"原文条件不同。","citation_ids":["t1"]}]}`, false},
		{"invented_path", `{"issues":[{"path":"/summary/content/0/text","reason":"原文条件不同。","citation_ids":["t1"]}]}`, false},
		{"unknown_source", `{"issues":[{"field":"F1","reason":"原文条件不同。","citation_ids":["t999"]}]}`, false},
		{"missing_issues", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := schema.AssistantMessage("", []schema.ToolCall{{Function: schema.FunctionCall{Name: "report_evidence_review", Arguments: tc.raw}}})
			result, err := parseRecoveryEvidenceReview(response, "server-bound-candidate", domain.JSON(candidate), catalog, fields)
			if (err == nil) != tc.valid {
				t.Fatal("invalid review field resolution", result, err)
			}
			if tc.valid && (result.CandidateHash != "server-bound-candidate" || result.Verdict != "needs_correction" || result.Issues[0].Path != "/summary") {
				t.Fatal("model metadata overrode the server's actual candidate or target binding", result)
			}
		})
	}
}
