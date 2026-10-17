package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func planSession(t *testing.T) *session {
	t.Helper()
	db, resources, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateRequest(ctx, owner.ID, domain.ID(), "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := db.Request(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.Claim(ctx, *req.ActiveRunID, 1, "source")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: domain.ID(), SourceID: c.Run.SourceID, RunID: c.Run.ID, Kind: "video", StorageKey: domain.ID() + ".mp4", SHA256: "fixture", MIME: "video/mp4", Bytes: 42, ExpiresAt: domain.Ptr(time.Now().Add(time.Hour))}
	if err = db.AddAsset(ctx, *c, video); err != nil {
		t.Fatal(err)
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, DurationMS: 2000, VideoHash: "fixture", TextHash: "text", Metadata: []byte(`{}`)}
	transcript := []domain.Segment{}
	for i := 0; i < 25; i++ {
		start, end := int64(0), int64(1000)
		if i == 24 {
			start, end = 1000, 2000
		}
		transcript = append(transcript, domain.Segment{ID: domain.ID(), Ordinal: i, StartMS: start, EndMS: end, Text: fmt.Sprint("来源事实", i), Origin: "platform"})
	}
	if _, err = db.FinishSource(ctx, *c, snap, transcript, video.ID); err != nil {
		t.Fatal(err)
	}
	req, err = db.Request(ctx, owner.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err = db.Claim(ctx, *req.ActiveRunID, 1, "writer")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	spec, err := db.GenerationForRun(ctx, c.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: db, Resources: resources, Config: cfg}, claim: *c, profile: cfg.Profile, snapshot: snap, transcript: transcript, spec: spec, compositionReady: true, cp: checkpoint{SchemaVersion: 1, Ready: true, Protocol: domain.ArticleDraftProtocol, LayoutVersion: draftLayout, HarnessRevision: 3, Inspections: map[string][]string{}, Repeated: map[string]int{}}}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	return s
}

func badPlan() beginArticleInput {
	refs := []string{}
	for i := 1; i <= 25; i++ {
		refs = append(refs, fmt.Sprint("t", i))
	}
	return beginArticleInput{Title: "两个不同主题", Sections: []sectionPlan{
		{ID: "one", Title: "前半", StartMS: 0, EndMS: 1000, KeyPoints: []string{"第一个来源事实"}, EvidenceIDs: refs},
		{ID: "two", Title: "后半", StartMS: 1000, EndMS: 2000, KeyPoints: []string{"结尾的独立事实"}, EvidenceIDs: []string{"t1"}},
	}, Limitations: []domain.Limitation{}}
}

func planCall(s *session, id, name string, args any) (map[string]any, error) {
	raw, ok := args.(string)
	if !ok {
		raw = string(domain.JSON(args))
	}
	result, err := s.tools[name].InvokableRun(context.WithValue(context.Background(), callIDKey{}, id), &schema.ToolArgument{Text: raw})
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if len(result.Parts) > 0 {
		err = json.Unmarshal([]byte(result.Parts[0].Text), &body)
	}
	return body, err
}

func TestRetainedPlanRepairsThroughCheckedToolsAndReplays(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	bad := badPlan()
	feedback, err := planCall(s, "invalid-plan", "begin_article", bad)
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := feedback["error"].(map[string]any)
	if !ok || issue["issue_count"] != float64(3) || feedback["version"] != float64(0) || feedback["saved_section_count"] != float64(0) {
		t.Fatalf("incomplete plan feedback: %s", domain.JSON(feedback))
	}
	if !strings.Contains(string(domain.JSON(feedback)), "repair_article_plan") {
		t.Fatal("pre-plan advice requests chapter writes")
	}
	patch := candidatePatchInput{CandidateID: "invalid-plan", ExpectedHash: store.Hash(string(domain.JSON(bad))), Patches: []fieldPatch{
		{Op: "replace", Path: "/sections/0/evidence_ids", Value: domain.JSON([]string{"t1", "t24"})},
		{Op: "replace", Path: "/sections/1/evidence_ids/0", Value: domain.JSON("t25")},
	}}
	body, err := planCall(s, "fixed-plan", "repair_article_plan", patch)
	if err != nil || body["version"] != float64(1) {
		t.Fatalf("plan not accepted: %s %v", domain.JSON(body), err)
	}
	if s.cp.DraftIssue != nil || s.cp.LastCandidate != nil {
		t.Fatal("accepted plan still requests rejected candidate repair")
	}
	draft, err := s.engine.Store.ArticleDraft(ctx, s.claim.Run.ID)
	var saved articleDraft
	if err != nil || json.Unmarshal(draft.Data, &saved) != nil || len(saved.Plan) != 2 || len(saved.Sections) != 0 || saved.Plan[1].KeyPoints[0] != bad.Sections[1].KeyPoints[0] {
		t.Fatal("repair lost full plan or fabricated chapter bodies", err)
	}
	before := s.cp.Progress.Total
	if _, err = planCall(s, "fixed-plan", "repair_article_plan", patch); err != nil {
		t.Fatal("repair replay failed", err)
	}
	if s.cp.Progress.Total != before {
		t.Fatal("nested repair/replay counted twice")
	}
	used, err := s.repairUsage(ctx)
	if err != nil || used != 1 {
		t.Fatal("nested/replayed repair consumed extra budget", used, err)
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "draft-correction:fixed-plan")
	if err != nil || !strings.Contains(string(op.Result), `"invalid-plan"`) {
		t.Fatal("repair lineage not retained", err)
	}
	if _, err = s.engine.Store.Operation(ctx, s.claim.Run.ID, "candidate"); err == nil {
		t.Fatal("plan repair bypassed formal publication")
	}
}

func TestPlanCorrectionRejectsForeignHashAndStructuralPatch(t *testing.T) {
	for _, kind := range []string{"foreign", "stale_hash", "delete_points", "replace_sections", "unknown_field"} {
		t.Run(kind, func(t *testing.T) {
			s := planSession(t)
			bad := badPlan()
			if _, err := planCall(s, "bad", "begin_article", bad); err != nil {
				t.Fatal(err)
			}
			patch := candidatePatchInput{CandidateID: "bad", ExpectedHash: store.Hash(string(domain.JSON(bad))), Patches: []fieldPatch{{Op: "replace", Path: "/sections/0/evidence_ids", Value: domain.JSON([]string{"t1"})}}}
			switch kind {
			case "foreign":
				patch.CandidateID = "other-run-candidate"
			case "stale_hash":
				patch.ExpectedHash = "wrong"
			case "delete_points":
				patch.Patches[0].Path = "/sections/0/key_points"
			case "replace_sections":
				patch.Patches[0].Path = "/sections"
			case "unknown_field":
				patch.Patches[0].Path = "/model_revision_id"
			}
			body, err := planCall(s, "unsafe", "repair_article_plan", patch)
			if err != nil || body["error"] == nil {
				t.Fatalf("unsafe repair accepted: %s %v", domain.JSON(body), err)
			}
			draft, err := s.engine.Store.ArticleDraft(context.Background(), s.claim.Run.ID)
			if err != nil || draft.Version != 0 {
				t.Fatal("unsafe repair changed draft", err)
			}
		})
	}
}

func TestPlanCorrectionBudgetPersistsCanonicalDuplicatesAndLastIssue(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	bad := badPlan()
	if _, err := planCall(s, "first", "begin_article", bad); err != nil {
		t.Fatal(err)
	}
	// Same values with a different JSON object order must still be identified.
	raw := fmt.Sprintf(`{"sections":%s,"limitations":[],"title":%s}`, domain.JSON(bad.Sections), domain.JSON(bad.Title))
	feedback, err := planCall(s, "same", "begin_article", raw)
	if err != nil || feedback["error"].(map[string]any)["unchanged_candidate"] != true {
		t.Fatal("JSON ordering disguised unchanged plan", err)
	}
	// Simulate a worker takeover using only the durable checkpoint and ledger.
	run, err := s.engine.Store.Run(ctx, s.claim.Run.ID)
	if err != nil || json.Unmarshal(run.Checkpoint, &s.cp) != nil {
		t.Fatal(err)
	}
	if used, err := s.repairUsage(ctx); err != nil || used != 1 {
		t.Fatal("takeover reset correction budget", used, err)
	}
	for i := 1; i < s.spec.DetailPolicy.RepairLimit; i++ {
		_, err = planCall(s, fmt.Sprint("retry-", i), "begin_article", bad)
	}
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Meta["draft_version"] != 0 || problem.Meta["saved_sections"] != 0 || !strings.Contains(problem.Detail, "尚未建立") {
		t.Fatal("budget failure invented saved chapters or lost final cause", err)
	}
	if s.cp.DraftIssue["issue_count"] == nil || !s.cp.FinalizationFailed {
		t.Fatal("last specific issue was lost")
	}
	used, _ := s.repairUsage(ctx)
	if used != s.spec.DetailPolicy.RepairLimit {
		t.Fatal("correction budget changed", used)
	}
	budget, err := s.budget(ctx, []*schema.Message{schema.UserMessage("repair")}, 0)
	if err != nil || budget.Phase != "repair" || budget.OutputTokens != min(s.profile.MaxOutputTokens, s.spec.DetailPolicy.RepairOutputTokens) {
		t.Fatal("plan correction used investigation output budget", budget, err)
	}
}

func TestRejectedPreflightPersistsNumericBudgetWithoutSubmission(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	s.profile.InputCharsLimit = 1
	_, err := (&meteredModel{session: s}).Generate(ctx, []*schema.Message{schema.UserMessage("private source text never stored in budget diagnostic")})
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Meta["budget_reason"] != "input_chars_exceeded" || problem.Meta["can_retry"] != false {
		t.Fatal("preflight reason/retry semantics lost", err)
	}
	op, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "budget-diagnostic:model:0")
	if err != nil || strings.Contains(string(op.Result), "private source text") || !strings.Contains(string(op.Result), "input_chars_limit") {
		t.Fatal("missing numeric-only budget evidence", err)
	}
	var count int
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind IN ('model','model_submission','model_input')", s.claim.Run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected preflight submitted/froze a model call", count, err)
	}
}

func TestRecoveryKeepsFrozenSchemaAcrossPlanProtocolUpgrade(t *testing.T) {
	s := planSession(t)
	ctx := context.Background()
	info, err := s.tools["begin_article"].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldInfo := *info
	oldInfo.Desc = "persisted pre-upgrade begin schema"
	messages := []*schema.Message{schema.UserMessage("original full source")}
	budget, err := s.budget(ctx, messages, 0)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"messages": stripPixels(messages), "budget": budget, "tools_hash": digest(domain.JSON([]*schema.ToolInfo{&oldInfo})), "policy_revision": s.spec.DetailPolicyRevision}
	frozen, err := s.freezeModelInput(ctx, "model:0", messages, args, budget, []*schema.ToolInfo{&oldInfo})
	if err != nil {
		t.Fatal(err)
	}
	var canonical any
	if err = json.Unmarshal(frozen.Args, &canonical); err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.Store.BeginOperation(ctx, s.claim, "model:0", "model", canonical, false); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.FinishOperation(ctx, s.claim, "model:0", schema.AssistantMessage("stored original reply", nil), "", nil); err != nil {
		t.Fatal(err)
	}
	bound, err := (&meteredModel{session: s}).WithTools([]*schema.ToolInfo{info})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bound.Generate(ctx, []*schema.Message{schema.UserMessage("changed messages after recovery")})
	if err != nil || result.Content != "stored original reply" {
		t.Fatal("schema upgrade rewrote frozen logical input", result, err)
	}
	current, err := s.engine.Store.Operation(ctx, s.claim.Run.ID, "input:model:0")
	var restored frozenModelInput
	if err != nil || json.Unmarshal(current.Result, &restored) != nil || restored.ToolSchemaJSON != frozen.ToolSchemaJSON {
		t.Fatal("frozen schema changed", err)
	}
}

// Find the current retained begin candidate in the actual model messages, not
// an in-memory session shortcut. Tool feedback/readiness is the public contract.
func retainedPlanCandidate(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if v["tool"] == "begin_article" && v["candidate_id"] != nil && v["expected_hash"] != nil {
				out["candidate_id"], _ = v["candidate_id"].(string)
				out["expected_hash"], _ = v["expected_hash"].(string)
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case string:
			var nested any
			if json.Unmarshal([]byte(v), &nested) == nil {
				walk(nested)
			}
		}
	}
	var value any
	if json.Unmarshal(raw, &value) == nil {
		walk(value)
	}
	return out
}
