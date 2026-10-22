package store

import (
	"context"
	"encoding/json"
	"slices"
	"vidlens/internal/domain"
)

// An attempt ledger is independent of the current SSE cursor. Retry lineage
// never grants access to another session or splices old events into that cursor.
func requestAttempts(ctx context.Context, q Querier, current domain.Request) ([]map[string]any, error) {
	chain := []domain.Request{current}
	seenRequests := map[string]bool{current.ID: true}
	for len(chain) < 40 {
		last := chain[len(chain)-1]
		if last.RetryOfID == nil || seenRequests[*last.RetryOfID] {
			break
		}
		prior, err := loadRequest(ctx, q, *last.RetryOfID)
		if err != nil {
			return nil, err
		}
		if prior.SessionID != current.SessionID || prior.SourceID != current.SourceID {
			break
		}
		chain = append(chain, prior)
		seenRequests[prior.ID] = true
	}
	slices.Reverse(chain)
	out := []map[string]any{}
	seenRuns := map[string]bool{}
	for _, req := range chain {
		for _, seg := range req.Segments {
			if seenRuns[seg.RunID] {
				continue
			}
			seenRuns[seg.RunID] = true
			run, err := loadRun(ctx, q, seg.RunID)
			if err != nil {
				return nil, err
			}
			if run.Kind != "article.generate" {
				continue
			}
			var raw []byte
			if err = q.QueryRow(ctx, `SELECT COALESCE((SELECT result->'data' FROM run_operations WHERE run_id=$1 AND kind='article_draft' AND status='completed' ORDER BY (result->>'version')::int DESC LIMIT 1),'{}'::jsonb)`, run.ID).Scan(&raw); err != nil {
				return nil, err
			}
			var draft struct {
				Sections map[string]json.RawMessage `json:"sections"`
				Plan     []json.RawMessage          `json:"plan"`
				Article  struct {
					Sections []json.RawMessage `json:"sections"`
				} `json:"article"`
			}
			_ = json.Unmarshal(raw, &draft)
			var inheritedRaw []byte
			if err = q.QueryRow(ctx, `SELECT COALESCE((SELECT result->'data' FROM run_operations WHERE run_id=$1 AND operation_key='article-draft:retry' AND status='completed'),'{}'::jsonb)`, run.ID).Scan(&inheritedRaw); err != nil {
				return nil, err
			}
			var inherited struct {
				Sections map[string]json.RawMessage `json:"sections"`
				Article  struct {
					Sections []json.RawMessage `json:"sections"`
				} `json:"article"`
			}
			_ = json.Unmarshal(inheritedRaw, &inherited)
			var calls, sends int
			if err = q.QueryRow(ctx, `SELECT count(DISTINCT step_id) FILTER(WHERE step->>'kind'='model_response'),count(DISTINCT step_id) FILTER(WHERE step->>'kind'='model_submission') FROM run_events WHERE run_id=$1`, run.ID).Scan(&calls, &sends); err != nil {
				return nil, err
			}
			isCurrent := false
			for _, present := range current.Segments {
				if present.RunID == run.ID {
					isCurrent = true
				}
			}
			saved, inheritedCount := len(draft.Sections), len(inherited.Sections)
			openEnded := draft.Article.Sections != nil
			if openEnded {
				saved = len(draft.Article.Sections)
				inheritedCount = len(inherited.Article.Sections)
			}
			out = append(out, map[string]any{"open_ended": openEnded, "run_id": run.ID, "request_id": req.ID, "current": isCurrent, "status": run.Status, "saved_sections": saved, "planned_sections": len(draft.Plan), "inherited_sections": inheritedCount, "model_calls": calls, "model_submissions": sends, "history_url": "/api/v1/requests/" + req.ID + "/activity"})
		}
	}
	return out, nil
}
