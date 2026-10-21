package store

import (
	"context"
	"encoding/json"
	"testing"

	"vidlens/internal/domain"
)

func TestWorkflowRetryUpgradesArtifactsWithoutCopyingCallsOrApproval(t *testing.T) {
	for _, version := range []string{domain.ExecutionContractV10, domain.ExecutionContractV11, domain.ExecutionContractV12} {
		t.Run(version, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			owner := session(t, s)
			req, claim := prepared(t, s, owner)
			cp := map[string]any{"schema_version": 1, "ready": true, "layout_version": "source-context-v3", "protocol": domain.ArticleDraftProtocol,
				"execution_contract": domain.ExecutionContract{Version: version, RunID: claim.Run.ID}, "workflow": map[string]any{"calls": map[string]int{"write": 2, "format": 1}}, "calls": 4}
			if err := s.Checkpoint(ctx, claim, cp); err != nil {
				t.Fatal(err)
			}
			plan := map[string]any{"title": "保留来源计划", "chapters": []any{map[string]any{"title": "主题", "start_ms": 0}}, "frame_notes": []any{}, "investigations": []any{}, "limitations": []any{}}
			for _, op := range []struct{ key, kind string }{{"workflow:plan", "workflow_stage"}, {"workflow:write:0", "workflow_stage"}, {"workflow-call:write:0:0", "workflow_call"}, {"workflow-review:approved", "workflow_result"}} {
				if _, err := s.BeginOperation(ctx, claim, op.key, op.kind, map[string]any{"old_schema": version}, false); err != nil {
					t.Fatal(err)
				}
				if err := s.FinishOperation(ctx, claim, op.key, plan, "", nil); err != nil {
					t.Fatal(err)
				}
			}
			data := map[string]any{"title": "保留来源计划", "plan": plan["chapters"], "sections": map[string]any{"s1": "已保存正文"}, "limitations": []any{map[string]any{"code": "partial_text", "message": "旧专名尚未确认。", "section_ids": []string{"s1"}, "citation_ids": []string{"t1"}}}}
			if _, err := s.SaveArticleDraft(ctx, claim, "first", nil, 0, data); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Fail(ctx, claim, domain.Failure{Code: "WORKFLOW_RESULT_INVALID", Message: "原失败保留", CanRetry: true}, false, 0); err != nil {
				t.Fatal(err)
			}
			retry, err := s.CreateRequest(ctx, owner, domain.ID(), "", req.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.Request(ctx, owner, retry.ID)
			if err != nil || next.ActiveRunID == nil {
				t.Fatal(err)
			}
			run, err := s.Run(ctx, *next.ActiveRunID)
			if err != nil {
				t.Fatal(err)
			}
			var restored struct {
				Contract domain.ExecutionContract `json:"execution_contract"`
				Calls    int                      `json:"calls"`
			}
			if err = json.Unmarshal(run.Checkpoint, &restored); err != nil || restored.Contract.Version != domain.CurrentExecutionContract || restored.Contract.RunID != run.ID || restored.Calls != 0 {
				t.Fatal("explicit retry did not freeze an independent current contract", restored, err)
			}
			artifact, err := s.Operation(ctx, run.ID, "workflow-inherited:plan")
			if err != nil || artifact.Kind != "workflow_artifact" || !draftJSONEqual(artifact.Result, domain.JSON(plan)) {
				t.Fatal("compatible plan was lost or confused with a frozen current call", artifact, err)
			}
			var inherited int
			if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND (kind IN ('workflow_call','workflow_stage','workflow_result') OR operation_key LIKE 'model:%')", run.ID).Scan(&inherited); err != nil || inherited != 0 {
				t.Fatal("retry copied call reservations, approval, or old schema stages", inherited, err)
			}
			draft, err := s.ArticleDraft(ctx, run.ID)
			if err != nil || !draftJSONEqual(draft.Data, domain.JSON(data)) {
				t.Fatal("retry lost saved uncertainty/draft", draft, err)
			}
			ancestor, err := s.Run(ctx, claim.Run.ID)
			if err != nil || ancestor.Status != "failed" || !draftJSONEqual(ancestor.Checkpoint, domain.JSON(cp)) {
				t.Fatal("retry rewrote the original failure or ledger", ancestor, err)
			}
		})
	}
}
