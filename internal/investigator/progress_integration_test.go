package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/store"
	"vidlens/internal/testsupport"
)

func TestUnknownRejectedAndBatchedToolsCannotResetProgressOnRecovery(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, err := db.NewSession(ctx, domain.ID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateRequest(ctx, owner.ID, "progress", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := db.Request(ctx, owner.ID, a.ID)
	c, err := db.Claim(ctx, *r.ActiveRunID, 1, "first")
	if err != nil || c == nil {
		t.Fatal(err)
	}
	spec, _ := db.GenerationForRun(ctx, c.Run.ID)
	s := &session{engine: &Engine{Store: db, Resources: res, Config: cfg}, claim: *c, profile: cfg.Profile, spec: spec, cp: checkpoint{SchemaVersion: 1, HarnessRevision: 1, Inspections: map[string][]string{}, Repeated: map[string]int{}, Progress: progressState{Seen: map[string]int{}}}}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	arg := `{"question_id":"q","question":"look","window":{"start_ms":0,"end_ms":1000,"count":1}}`
	calls := []schema.ToolCall{{ID: "batch-one", Function: schema.FunctionCall{Name: "inspect_video_window", Arguments: arg}}, {ID: "batch-two", Function: schema.FunctionCall{Name: "inspect_video_window", Arguments: arg}}}
	m := &middleware{s: s}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{schema.AssistantMessage("", calls)}}
	if _, _, err = m.AfterModelRewriteState(ctx, state, nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		result, err := s.tools[call.Function.Name].InvokableRun(context.WithValue(ctx, callIDKey{}, call.ID), &schema.ToolArgument{Text: arg})
		if err != nil || !strings.Contains(string(domain.JSON(result)), "整组拒绝") {
			t.Fatalf("batch partially executed: %v %v", result, err)
		}
	}
	var questions, frames int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM evidence_questions WHERE run_id=$1", c.Run.ID).Scan(&questions)
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM media_assets WHERE run_id=$1", c.Run.ID).Scan(&frames)
	if questions != 0 || frames != 0 {
		t.Fatal("batch rejection left media/question side effects")
	}
	_, err = s.tools["record_observation"].InvokableRun(context.WithValue(ctx, callIDKey{}, "invalid-arguments"), &schema.ToolArgument{Text: `{"visible_facts":"wrong type"}`})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = s.unknownTool(context.WithValue(ctx, callIDKey{}, fmt.Sprint("unknown-", i)), "imagined_tool", `{}`); err != nil {
			t.Fatal(err)
		}
	}
	if s.cp.Progress.Stalled != 6 || s.cp.Progress.Total != 6 {
		t.Fatalf("refusals were not persisted: %+v", s.cp.Progress)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE job_runs SET lease_expires_at=now()-interval '1 second' WHERE id=$1", c.Run.ID); err != nil {
		t.Fatal(err)
	}
	next, err := db.Claim(ctx, c.Run.ID, 1, "recovery")
	if err != nil || next == nil {
		t.Fatal(err)
	}
	s.claim = *next
	if err = json.Unmarshal(next.Run.Checkpoint, &s.cp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.unknownTool(context.WithValue(ctx, callIDKey{}, "unknown-2"), "imagined_tool", `{}`); err != nil {
		t.Fatal(err)
	}
	if s.cp.Progress.Total != 6 {
		t.Fatal("replay counted as new work")
	}
	if _, _, err = m.BeforeModelRewriteState(ctx, &adk.ChatModelAgentState{}, nil); err == nil || !strings.Contains(err.Error(), "AGENT_NO_PROGRESS") {
		t.Fatal("recovery reset no-progress limit", err)
	}
	if _, err = db.Operation(ctx, c.Run.ID, "candidate"); err == nil {
		t.Fatal("tool refusal entered publish slot")
	}
}

func TestFieldRepairValidatesAnswerAndRejectsLateOrForeignCandidate(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	turn, c := testsupport.Question(t, db)
	snap, _ := db.Snapshot(ctx, turn.SnapshotID)
	transcript, _ := db.Transcript(ctx, turn.SnapshotID)
	article, _ := db.PublishedArticle(ctx, turn.ArticleID)
	spec, _ := db.GenerationForRun(ctx, c.Run.ID)
	s := &session{engine: &Engine{Store: db, Resources: res, Config: cfg}, claim: c, profile: cfg.Profile, spec: spec, snapshot: snap, transcript: transcript, turn: &turn, article: &article, cp: checkpoint{SchemaVersion: 1, HarnessRevision: 1, Inspections: map[string][]string{}, Repeated: map[string]int{}}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	bad := answerInput{Kind: "bad_kind", Summary: "只能确认来源中的文字。", Blocks: []domain.Block{{ID: "p", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "来源实际提供了fixture text。"}, {Type: "citation", CitationID: "t1"}}}}, Citations: map[string]domain.Citation{"t1": {ID: "t1", Kind: "video_text", SnapshotID: snap.ID, Range: &domain.Range{StartMS: 0, EndMS: 1000}, SegmentIDs: []string{transcript[0].ID}, TextOrigin: "platform_subtitles"}}}
	citation := bad.Citations["t1"]
	citation.Label = "视频原文"
	bad.Citations["t1"] = citation
	raw := string(domain.JSON(bad))
	if _, err := s.tools["submit_answer"].InvokableRun(context.WithValue(ctx, callIDKey{}, "bad-answer"), &schema.ToolArgument{Text: raw}); err == nil {
		t.Fatal("invalid answer accepted")
	}
	patch := candidatePatchInput{CandidateID: "bad-answer", ExpectedHash: store.Hash(raw), Patches: []fieldPatch{{Op: "replace", Path: "/kind", Value: json.RawMessage(`"grounded"`)}}}
	result, err := s.tools["repair_candidate"].InvokableRun(context.WithValue(ctx, callIDKey{}, "fixed-answer"), &schema.ToolArgument{Text: string(domain.JSON(patch))})
	if err != nil || !strings.Contains(string(domain.JSON(result)), "accepted_for_publication") {
		t.Fatalf("field repair failed: %v %v", result, err)
	}
	op, err := db.Operation(ctx, c.Run.ID, "candidate")
	if err != nil || op.Status != "completed" {
		t.Fatal("validated repair missing", err)
	}
	if err = s.publish(ctx, op.Result); err != nil {
		t.Fatal(err)
	}
	if _, err = s.tools["repair_candidate"].InvokableRun(context.WithValue(ctx, callIDKey{}, "late-answer"), &schema.ToolArgument{Text: string(domain.JSON(patch))}); err == nil {
		t.Fatal("late repair escaped fencing")
	}
}

func TestDifferentQueriesReturningSameCandidatesDoNotClaimProgress(t *testing.T) {
	db, res, cfg := testsupport.Services(t)
	ctx := context.Background()
	owner, e := db.NewSession(ctx, domain.ID(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	a, e := db.CreateRequest(ctx, owner.ID, "same-candidates", "https://youtube.com/watch?v=abcdefghijk", "", false)
	if e != nil {
		t.Fatal(e)
	}
	r, _ := db.Request(ctx, owner.ID, a.ID)
	c, e := db.Claim(ctx, *r.ActiveRunID, 1, "reader")
	if e != nil || c == nil {
		t.Fatal(e)
	}
	spec, _ := db.GenerationForRun(ctx, c.Run.ID)
	s := &session{engine: &Engine{Store: db, Resources: res, Config: cfg}, claim: *c, profile: cfg.Profile, spec: spec, transcript: []domain.Segment{{ID: "real-candidate", StartMS: 0, EndMS: 1000, Text: "pool queue"}}, cp: checkpoint{SchemaVersion: 1, HarnessRevision: 2, Inspections: map[string][]string{}, Repeated: map[string]int{}}}
	if _, e = s.makeTools(); e != nil {
		t.Fatal(e)
	}
	for i, query := range []string{"pool", "queue"} {
		_, e = s.tools["find_evidence"].InvokableRun(context.WithValue(ctx, callIDKey{}, fmt.Sprint("read-", i)), &schema.ToolArgument{Text: string(domain.JSON(findInput{Query: query}))})
		if e != nil {
			t.Fatal(e)
		}
	}
	if s.cp.Progress.Stalled != 1 {
		t.Fatalf("different query counted repeated evidence as progress: %+v", s.cp.Progress)
	}
}
