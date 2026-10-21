package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func TestWorkflowWriterRecoverySavesUncertaintyAndReusesDurableResult(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		n := sends.Add(1)
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		if n == 1 {
			writeReviewWireArgs(w, "write_video_chapters", map[string]any{"sections": []any{map[string]any{"id": "s1", "blocks": []any{}, "limitations": []any{}}}})
			return
		}
		feedback := string(domain.JSON(wire.Messages))
		if !strings.Contains(feedback, "/sections/0/limitations") || !strings.Contains(feedback, "rejected_candidate_data") || !strings.Contains(feedback, "shape_example_only") {
			t.Error("recovery omitted the exact bad field, rejected candidate or array example", feedback)
		}
		writeReviewWireArgs(w, "write_video_chapters", workflowBody{Sections: []workflowSection{{ID: "s1", Blocks: []workflowBlock{{Type: "paragraph", Origin: "video", Text: "来源事实0，专名无法确认。", CitationIDs: []string{"t1"}}}}}, Limitations: []domain.Limitation{{Code: "partial_text", Message: "原文专名仍然含糊。", SectionIDs: []string{"s1"}, CitationIDs: []string{"t1"}}}})
	})
	workflowFixtureState(s)
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := articleDraft{Title: "主题", Plan: []sectionPlan{{ID: "s1", Title: "首章"}}, Sections: map[string]domain.Section{}}
	payload := map[string]any{"snapshot_id": s.snapshot.ID, "assigned_chapters": draft.Plan}
	stage := func(current *session) workflowBody {
		t.Helper()
		body, e := workflowStage[workflowBody](ctx, current, "write:0", "write", "write_video_chapters", workflowWriteInstructions, payload, nil, func(b *workflowBody) error {
			_, e := workflowApplyBody(draft, *b, draft.Plan, catalog, false, false)
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		return body
	}
	body := stage(s)
	draft, err = workflowApplyBody(draft, body, draft.Plan, catalog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	workflowDraftCitations(&draft, catalog)
	if err = s.workflowDraft(ctx, "write:0", draft); err != nil {
		t.Fatal(err)
	}
	_, saved, err := s.loadDraft(ctx)
	if err != nil || len(saved.Limitations) != 1 || saved.Citations["t1"].ID != "t1" {
		t.Fatal("writer uncertainty did not reach persisted draft and evidence", saved, err)
	}
	recovered := *s
	if err = json.Unmarshal(domain.JSON(s.cp), &recovered.cp); err != nil {
		t.Fatal(err)
	}
	if store.Hash(stage(&recovered)) != store.Hash(body) || sends.Load() != 2 || recovered.cp.Calls != 2 || recovered.cp.Workflow.Calls["format"] != 1 {
		t.Fatal("recovery resent or erased a completed result", sends.Load(), recovered.cp)
	}
}

func TestWorkflowWriterNeverSavesStringSectionsAfterOneRecovery(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 1 {
			writeReviewWireArgs(w, "write_video_chapters", map[string]any{"sections": []any{map[string]any{"id": "s2", "blocks": []any{}, "limitations": []any{}}}})
		} else {
			writeReviewWireArgs(w, "write_video_chapters", map[string]any{"sections": "[]", "limitations": []any{}})
		}
	})
	workflowFixtureState(s)
	ctx := context.Background()
	draft := articleDraft{Title: "保留首章", Plan: []sectionPlan{{ID: "s1", Title: "首章"}, {ID: "s2", Title: "次章"}}, Sections: map[string]domain.Section{"s1": {ID: "s1", Title: "首章"}}}
	if err := s.workflowDraft(ctx, "plan", draft); err != nil {
		t.Fatal(err)
	}
	beforeVersion, before, err := s.loadDraft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = workflowStage[workflowBody](ctx, s, "write:1", "write", "write_video_chapters", workflowWriteInstructions, map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "WORKFLOW_RESULT_INVALID" || !strings.Contains(problem.Detail, "/sections") || sends.Load() != 2 || s.cp.Calls != 2 || s.cp.Workflow.Calls["format"] != 1 {
		t.Fatal("serialized sections were coerced, retried without bounds, or lost failure details", err, sends.Load(), s.cp)
	}
	afterVersion, after, err := s.loadDraft(ctx)
	if err != nil || afterVersion != beforeVersion || store.Hash(before) != store.Hash(after) {
		t.Fatal("failed second batch changed the retained first batch", after, err)
	}
}

func TestWorkflowRevisionSavesScopedUncertaintyAndRechecksOnlyAffectedChapter(t *testing.T) {
	var revised atomic.Bool
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		payload := workflowPayload(t, wire)
		if wire.Tools[0].Function.Name == "revise_video_chapters" {
			if payload["saved_limitations"] == nil {
				t.Error("author cannot preserve the prior uncertainty")
			}
			revised.Store(true)
			writeReviewWireArgs(w, "revise_video_chapters", workflowBody{Sections: []workflowSection{{ID: "s1", Blocks: []workflowBlock{{Type: "paragraph", Origin: "video", Text: "来源事实0，专名仍未明确。", CitationIDs: []string{"t1"}}}}}, Limitations: []domain.Limitation{{Code: "partial_text", Message: "首章专名仍不确定。", SectionIDs: []string{"s1"}, CitationIDs: []string{"t1"}}}})
			return
		}
		var system string
		_ = json.Unmarshal(wire.Messages[0].Content, &system)
		if strings.Contains(system, "视频内容作者") || !strings.Contains(system, "独立的原始证据审核器") {
			t.Error("review is still assigned author duties", system)
		}
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		results := []workflowVerdict{}
		for _, unit := range units {
			if revised.Load() && strings.HasPrefix(unit.ID, "s2") {
				t.Error("unrelated chapter uncertainty caused another review", unit.ID)
			}
			result := workflowVerdict{UnitID: unit.ID, Verdict: "supported", CitationIDs: []string{}}
			if !revised.Load() && unit.ID == "s1_b1" {
				result.Verdict, result.Reason, result.CitationIDs = "unverifiable", "t1只描述来源事实0，不能支持这个仓库专名；须限定措辞。", []string{"t1"}
			}
			results = append(results, result)
		}
		writeReviewWireArgs(w, workflowReviewToolName, workflowReviewWire{RequestID: id, Results: results})
	})
	workflowFixtureState(s)
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := articleDraft{Title: "主题", Plan: []sectionPlan{{ID: "s1", Title: "首章"}, {ID: "s2", Title: "末章"}}, Sections: map[string]domain.Section{
		"s1": {ID: "s1", Title: "首章", Blocks: []domain.Block{{ID: "s1_b1", Type: "code", Text: "来源未支持的仓库名称", CitationIDs: []string{"t1"}}}},
		"s2": {ID: "s2", Title: "末章", Blocks: []domain.Block{{ID: "s2_b1", Type: "code", Text: "来源事实24", CitationIDs: []string{"t25"}}}},
	}, Limitations: []domain.Limitation{{Code: "partial_text", Message: "首章旧局限。", SectionIDs: []string{"s1"}}, {Code: "not_observed", Message: "末章局部画面未见。", SectionIDs: []string{"s2"}}}}
	workflowDraftCitations(&draft, catalog)
	if err = s.workflowDraft(ctx, "write:0", draft); err != nil {
		t.Fatal(err)
	}
	article := domain.Article{Title: draft.Title, Sections: []domain.Section{draft.Sections["s1"], draft.Sections["s2"]}, Limitations: draft.Limitations}
	issues, err := s.reviewWorkflowArticle(ctx, article, draft.Plan, "review")
	if err != nil || len(issues) != 1 || s.requireWorkflowReview(ctx, workflowArticleCandidate(article), catalog) == nil {
		t.Fatal("wrong text binding was lost or approved", issues, err)
	}
	before := sends.Load()
	updated, err := s.reviseWorkflowArticle(ctx, draft, catalog, issues)
	if err != nil {
		t.Fatal(err)
	}
	_, saved, err := s.loadDraft(ctx)
	if err != nil || len(saved.Limitations) != 2 || store.Hash(saved.Sections["s2"]) != store.Hash(draft.Sections["s2"]) || store.Hash(saved.Limitations) != store.Hash(updated.Limitations) {
		t.Fatal("revision lost uncertainty or changed another chapter", saved, err)
	}
	article.Sections[0], article.Limitations = updated.Sections["s1"], updated.Limitations
	if s.requireWorkflowReview(ctx, workflowArticleCandidate(article), catalog) == nil {
		t.Fatal("new uncertainty reused stale approval")
	}
	issues, err = s.reviewWorkflowArticle(ctx, article, draft.Plan, "recheck")
	if err != nil || len(issues) != 0 || sends.Load()-before != 3 {
		t.Fatal("revision/local recheck/global recheck exceeded the fixed plan", issues, err, sends.Load()-before)
	}
}

func TestWorkflowV1RetainsFrozenWriterSchemaAndRole(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var raw map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		if strings.Contains(string(raw["tools"]), `"limitations"`) || !strings.Contains(string(raw["messages"]), "视频内容作者") {
			t.Error("admitted V1 writer silently changed schema or role")
		}
		writeReviewWireArgs(w, "write_video_chapters", workflowBodyV1{Sections: []workflowSection{}})
	})
	workflowFixtureState(s)
	s.cp.ExecutionContract.Version = domain.ExecutionContractV10
	_, err := workflowStage[workflowBody](context.Background(), s, "legacy-write", "write", "write_video_chapters", workflowWriteV1Instructions, map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
	if err != nil || sends.Load() != 1 {
		t.Fatal("legacy frozen writer did not remain usable", err)
	}
}

func TestWorkflowRetryValidatesInheritedPlanUnderNewStageIdentity(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		t.Error("compatible inherited plan was sent to a model again")
		w.WriteHeader(http.StatusInternalServerError)
	})
	workflowFixtureState(s)
	ctx := context.Background()
	plan := workflowPlan{Title: "保留主题", Chapters: []workflowChapter{{Title: "主题", StartMS: 0}}, FrameNotes: []workflowFrameNote{}, Investigations: []workflowInvestigation{}, Limitations: []domain.Limitation{}}
	if _, err := s.engine.Store.BeginOperation(ctx, s.claim, "workflow-inherited:plan", "workflow_artifact", map[string]string{"schema": "previous"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Store.FinishOperation(ctx, s.claim, "workflow-inherited:plan", plan, "", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := workflowStage[workflowPlan](ctx, s, "plan", "plan", "plan_video_article", workflowPlanInstructions, map[string]any{"snapshot_id": s.snapshot.ID}, nil, func(p *workflowPlan) error {
			return validateWorkflowPlan(p, s.snapshot.DurationMS, nil, nil)
		})
		if err != nil || store.Hash(got) != store.Hash(plan) || sends.Load() != 0 || s.cp.Calls != 0 {
			t.Fatal("artifact import lost the plan, reused a call, or changed the budget", got, err, sends.Load())
		}
	}
}

func TestWorkflowRetryRetainsOriginalInvestigationFrameAliases(t *testing.T) {
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) { t.Error("inherited observation must not call a model") })
	workflowFixtureState(s)
	ctx := context.Background()
	frame := domain.Asset{ID: domain.ID(), SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "frame", StorageKey: "retained.jpg", SHA256: "retained-original-pixels", MIME: "image/jpeg", Bytes: 42, TimeMS: domain.Ptr(int64(500))}
	if err := s.engine.Store.AddAsset(ctx, s.claim, frame); err != nil {
		t.Fatal(err)
	}
	s.cp.CitationFrames = []string{frame.ID}
	saved := map[string]any{"frame_notes": []workflowFrameNote{{FrameAlias: "f1", VisibleFacts: []string{"原观察保留。"}, Uncertain: []string{}}}}
	if _, err := s.engine.Store.BeginOperation(ctx, s.claim, "workflow-inherited:investigate:0", "workflow_artifact", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Store.FinishOperation(ctx, s.claim, "workflow-inherited:investigate:0", saved, "", nil); err != nil {
		t.Fatal(err)
	}
	ids, inherited, err := s.inheritedWorkflowFrames(ctx, "investigate:0")
	if err != nil || !inherited || len(ids) != 1 || ids[0] != frame.ID {
		t.Fatal("explicit retry changed an inherited investigation's image identity", ids, inherited, err)
	}
	s.cp.CitationFrames = nil
	if _, _, err = s.inheritedWorkflowFrames(ctx, "investigate:0"); err == nil {
		t.Fatal("missing original image silently became a new inspection")
	}
}
