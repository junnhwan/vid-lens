package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

func TestAuthoringCandidateFieldsSurviveMiddlewareAndEnablePlanRepair(t *testing.T) {
	s := planSession(t)
	s.cp.HarnessRevision = 13
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	tree := &domain.MindMap{RootID: "wrong", Nodes: []domain.MapNode{{ID: "root", Label: "主题"}, {ID: "group", Label: "机制", ParentID: domain.Ptr("root")}, {ID: "leaf", Label: "来源事实", ParentID: domain.Ptr("group"), SectionID: domain.Ptr("one")}}}
	plan := conceptPlanInput{Title: "来源事实", Sections: []sectionPlan{{ID: "one", Title: "起始条件", KeyPoints: []string{"第一个事实"}, EvidenceIDs: []string{"t1"}}, {ID: "two", Title: "结束条件", KeyPoints: []string{"最后一个事实"}, EvidenceIDs: []string{"t25"}}}, MindMap: tree, Limitations: []domain.Limitation{}}
	feedback, err := planCall(s, "bad-root", "begin_article", plan)
	if err != nil || !strings.Contains(string(domain.JSON(feedback)), "invalid_root") {
		t.Fatal(feedback, err)
	}
	candidate := s.cp.LastCandidate
	args := map[string]any{"candidate_id": candidate["candidate_id"], "expected_hash": candidate["expected_hash"], "paths": []string{"/mind_map"}}
	fields, err := planCall(s, "read-tree", "read_candidate_fields", args)
	if err != nil {
		t.Fatal(err)
	}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("frozen"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "bad-root", Function: schema.FunctionCall{Name: "begin_article", Arguments: string(domain.JSON(plan))}}}},
		{Role: schema.Tool, ToolCallID: "bad-root", Content: string(domain.JSON(feedback))},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "read-tree", Function: schema.FunctionCall{Name: "read_candidate_fields", Arguments: string(domain.JSON(args))}}}},
		{Role: schema.Tool, ToolCallID: "read-tree", Content: string(domain.JSON(fields))},
	}}
	for i := 0; i < 3; i++ {
		if _, _, err = (&middleware{s: s}).BeforeModelRewriteState(context.Background(), state, nil); err != nil {
			t.Fatal(err)
		}
		raw := string(domain.JSON(state.Messages))
		if !strings.Contains(raw, `\"fields\"`) || !strings.Contains(raw, "invalid_root") || !strings.Contains(raw, "wrong") {
			t.Fatal("middleware lost the information needed for a repair", raw)
		}
		if err = validateProtocol(state.Messages); err != nil {
			t.Fatal(err)
		}
	}
	s.compositionReady = true
	patch := candidatePatchInput{CandidateID: candidate["candidate_id"].(string), ExpectedHash: candidate["expected_hash"].(string), Patches: []fieldPatch{{Op: "replace", Path: "/mind_map/root_id", Value: domain.JSON("root")}}}
	if feedback, err = planCall(s, "fixed-root", "repair_article_plan", patch); err != nil || feedback["version"] != float64(1) {
		t.Fatal("retained tree was not repairable", feedback, err)
	}
}

func TestMeteredModelRecoversAfterCooldownWithoutAnotherRequest(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	model := verifiedFixtureRoute(t, s.engine.Store, "recovery-fixture", upstream.URL, "glm-5.3-flash")
	var cid string
	if err := s.engine.Store.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE id=$1", model.RevisionID).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	s.claim.Run.ConfigID = cid
	if _, err := s.engine.Store.Pool.Exec(ctx, "UPDATE job_runs SET config_id=$2 WHERE id=$1", s.claim.Run.ID, cid); err != nil {
		t.Fatal(err)
	}
	var err error
	s.profile, err = s.engine.Store.Profile(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	s.cp.HarnessRevision = 13
	info, err := s.tools["find_evidence"].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metered := &meteredModel{session: s, tools: []*schema.ToolInfo{info}}
	messages := []*schema.Message{schema.UserMessage("complete original input")}
	var work *domain.WorkError
	if _, err = metered.Generate(ctx, messages); !errors.As(err, &work) || !work.Transient || sends.Load() != 2 {
		t.Fatal("initial bounded attempts", sends.Load(), err)
	}
	if _, err = metered.Generate(ctx, messages); !errors.As(err, &work) || !work.Transient || work.RetryAfter == 0 || sends.Load() != 3 {
		t.Fatal("cooldown recovery not scheduled", sends.Load(), err)
	}
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE run_operations SET result=jsonb_set(result,'{not_before}',to_jsonb(now()-interval '1 second')) WHERE run_id=$1 AND kind='model_recovery'", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	result, err := metered.Generate(ctx, messages)
	if err != nil || result.Content != "complete result" || sends.Load() != 4 {
		t.Fatal("did not recover exact frozen call", sends.Load(), result, err)
	}
	if _, err = metered.Generate(ctx, messages); err != nil || sends.Load() != 4 {
		t.Fatal("completed replay resubmitted", sends.Load(), err)
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:0")
	var frozen frozenModelInput
	if err != nil || json.Unmarshal(op.Result, &frozen) != nil || !strings.Contains(string(domain.JSON(frozen.Messages)), "complete original input") {
		t.Fatal("recovery lost frozen original input", err)
	}
	stats, _ := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if stats.Calls != 1 || stats.Submissions != 4 {
		t.Fatal("recovery hid actual physical sends", stats)
	}
}

func TestRepeatedCandidateReadsDirectRepairWithoutResettingProgress(t *testing.T) {
	s := &session{cp: checkpoint{HarnessRevision: 13, DraftIssue: map[string]any{"code": "plan_invalid"}, LastCandidate: map[string]any{"tool": "begin_article"}, Progress: progressState{Stalled: 3, Total: 5}}}
	in := []*schema.ToolInfo{{Name: "begin_article"}, {Name: "read_candidate_fields"}, {Name: "repair_article_plan"}, {Name: "inspect_video_window"}}
	out := s.phaseTools(in)
	if len(out) != 1 || out[0].Name != "repair_article_plan" || s.cp.Progress.Stalled != 3 || s.cp.Progress.Total != 5 {
		t.Fatal("repeated read loop did not select repair or reset budgets", out, s.cp.Progress)
	}
	s.cp.HarnessRevision = 12
	if len(s.phaseTools(in)) == 1 {
		t.Fatal("legacy frozen protocol silently upgraded")
	}
}
