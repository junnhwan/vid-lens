package store

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
)

type diagnosticRejection struct {
	OperationKey string            `json:"operation_key"`
	CallID       string            `json:"call_id"`
	Tool         string            `json:"tool"`
	Stalled      int               `json:"consecutive_no_progress"`
	Issues       []diagnosticIssue `json:"issues"`
}

type diagnosticIssue struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

type rejectionIssueEnvelope struct {
	Code             string                   `json:"code"`
	Path             string                   `json:"path"`
	Issues           []rejectionIssueEnvelope `json:"issues"`
	AffectedSections []json.RawMessage        `json:"affected_sections"`
}

// Rejection text can contain the complete private candidate. Project only
// server error categories and field paths, never actual values or prose.
func diagnosticRejectionIssues(reason string) []diagnosticIssue {
	// String-valued tool errors are encoded once more in tool_feedback. Unwrap
	// only that known envelope, then project allowlisted validation fields.
	var decoded string
	if json.Unmarshal([]byte(reason), &decoded) == nil {
		reason = decoded
	}
	out := []diagnosticIssue{}
	add := func(code, path string) {
		if len(code) == 0 || len(code) > 80 || strings.IndexFunc(code, func(r rune) bool {
			return r != '_' && r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) >= 0 {
			code = "tool_rejected"
		}
		if len(path) > 400 || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\n\r\t ?#") {
			path = ""
		}
		if len(out) < 64 {
			out = append(out, diagnosticIssue{Code: code, Path: path})
		}
	}
	if strings.HasPrefix(reason, "字段/") && strings.HasSuffix(reason, "不存在") {
		add("repair_field_missing", strings.TrimSuffix(strings.TrimPrefix(reason, "字段"), "不存在"))
		return out
	}
	var issue rejectionIssueEnvelope
	if json.Unmarshal([]byte(reason), &issue) == nil {
		var project func(rejectionIssueEnvelope, int)
		project = func(next rejectionIssueEnvelope, depth int) {
			if len(out) >= 64 {
				return
			}
			if depth < 4 && len(next.Issues) > 0 {
				for _, child := range next.Issues[:min(len(next.Issues), 64)] {
					project(child, depth+1)
				}
			} else if depth < 4 && len(next.AffectedSections) > 0 {
				before := len(out)
				for _, raw := range next.AffectedSections[:min(len(next.AffectedSections), 40)] {
					var section struct {
						Issue *rejectionIssueEnvelope `json:"issue"`
					}
					if json.Unmarshal(raw, &section) == nil && section.Issue != nil {
						project(*section.Issue, depth+1)
					}
				}
				if len(out) == before {
					add(next.Code, next.Path)
				}
			} else {
				add(next.Code, next.Path)
			}
		}
		project(issue, 0)
	}
	if len(out) == 0 {
		add("tool_rejected", "")
	}
	return out
}

func toolRejectionDiagnostics(ctx context.Context, q Querier, run string) ([]diagnosticRejection, error) {
	rows, err := q.Query(ctx, `SELECT f.operation_key,COALESCE(f.result->>'call_id',''),COALESCE(f.result->>'tool',''),
 COALESCE(f.result->'progress'->>'stalled','0')::integer,
 CASE WHEN jsonb_typeof(c.result->'issue')='object' THEN (c.result->'issue')::text
 ELSE COALESCE(f.result->>'rejection_reason','') END
 FROM run_operations f
 LEFT JOIN run_operations c ON c.run_id=f.run_id
 AND c.operation_key='candidate-attempt:'||(f.result->>'call_id')||':invalid' AND c.kind='candidate_attempt'
 WHERE f.run_id=$1 AND f.kind='tool_feedback' AND f.result->>'rejected'='true'
 ORDER BY f.started_at,f.operation_key LIMIT 400`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diagnosticRejection{}
	for rows.Next() {
		var entry diagnosticRejection
		var reason string
		if err = rows.Scan(&entry.OperationKey, &entry.CallID, &entry.Tool, &entry.Stalled, &reason); err != nil {
			return nil, err
		}
		entry.Issues = diagnosticRejectionIssues(reason)
		out = append(out, entry)
	}
	return out, rows.Err()
}
