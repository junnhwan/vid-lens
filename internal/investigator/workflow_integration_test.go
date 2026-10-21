package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
	"vidlens/internal/media"
)

func workflowFixtureState(s *session) {
	s.cp.ExecutionContract = &domain.ExecutionContract{Version: domain.CurrentExecutionContract, RunID: s.claim.Run.ID}
	s.cp.Workflow = &workflowState{Calls: map[string]int{}}
	s.cp.Messages = []*schema.Message{schema.SystemMessage(workflowInstructions)}
	s.cp.CitationFrames = []string{}
}

func workflowPayload(t *testing.T, in reviewWireRequest) map[string]json.RawMessage {
	t.Helper()
	for _, message := range in.Messages {
		var text string
		var payload map[string]json.RawMessage
		if json.Unmarshal(message.Content, &text) == nil && json.Unmarshal([]byte(text), &payload) == nil && payload["snapshot_id"] != nil {
			return payload
		}
	}
	t.Fatal("missing complete original evidence")
	return nil
}

func TestWorkflowReviewPersistsGoodResultsAndRetriesOnlyInvalidUnit(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		n := sends.Add(1)
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		payload := workflowPayload(t, wire)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		if n == 2 && (len(units) != 1 || units[0].ID != "s1_b2") {
			t.Error("format recovery repeated already accepted units", units)
		}
		results := []workflowVerdict{}
		for _, unit := range units {
			v := workflowVerdict{UnitID: unit.ID, Verdict: "supported", CitationIDs: []string{}}
			if n == 1 && unit.ID == "s1_b2" {
				v.Verdict, v.Reason = "contradicted", "缺少来源身份的错误报告不能成为语义问题。"
			}
			results = append(results, v)
		}
		writeReviewWireArgs(w, workflowReviewToolName, workflowReviewWire{RequestID: id, Results: results})
	})
	workflowFixtureState(s)
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := domain.Article{Title: "标题", Sections: []domain.Section{{ID: "s1", Title: "首章", Blocks: []domain.Block{{ID: "s1_b1", Type: "code", Text: "来源事实0", CitationIDs: []string{"t1"}}, {ID: "s1_b2", Type: "code", Text: "来源事实1", CitationIDs: []string{"t2"}}}}}}
	units := workflowArticleUnits(a, catalog)
	if issues, err := s.workflowReviewBatch(context.Background(), a, workflowGlobalOutline(a), units, catalog, "review"); err != nil || len(issues) != 0 {
		t.Fatal(issues, err)
	}
	if sends.Load() != 2 || s.cp.Workflow.Calls["review"] != 1 || s.cp.Workflow.Calls["format"] != 1 || s.cp.Calls != 2 {
		t.Fatal("format retry escaped stage/total budgets", sends.Load(), s.cp)
	}
	if _, err = s.workflowReviewBatch(context.Background(), a, workflowGlobalOutline(a), units, catalog, "review"); err != nil || sends.Load() != 2 {
		t.Fatal("durable legal coverage was physically resent", err)
	}
	if s.requireWorkflowReview(context.Background(), workflowArticleCandidate(a), catalog) == nil {
		t.Fatal("missing global coverage allowed publication")
	}
}

func TestWorkflowEnginePublishesThroughCompleteBoundedStages(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		payload := workflowPayload(t, wire)
		if payload["original_passages"] == nil || strings.Contains(string(domain.JSON(wire)), "record_source_notes") {
			t.Error("writer was coordinated through derived source cards")
		}
		name := wire.Tools[0].Function.Name
		switch name {
		case "plan_video_article":
			if !strings.Contains(string(domain.JSON(wire)), "data:image/jpeg;") {
				t.Error("planning did not receive actual pixels")
			}
			writeReviewWireArgs(w, name, workflowPlan{Title: "两个来源事实的说明", Chapters: []workflowChapter{{Title: "最初的事实", StartMS: 0}, {Title: "最后的事实", StartMS: 1000}}, FrameNotes: []workflowFrameNote{{FrameAlias: "f1", VisibleFacts: []string{"画面是一块黑色方形区域。"}, Uncertain: []string{}}}, Investigations: []workflowInvestigation{}, Limitations: []domain.Limitation{{Code: "not_observed", Message: "单帧不能证明连续变化。", CitationIDs: []string{"f1"}, SectionIDs: []string{}}}})
		case "write_video_chapters":
			var chapters []sectionPlan
			_ = json.Unmarshal(payload["assigned_chapters"], &chapters)
			body := workflowBody{}
			for _, chapter := range chapters {
				blocks := []workflowBlock{{Type: "paragraph", Origin: "video", Text: "最初的来源事实0描述了开头的内容，这一事实是阅读当前主题时需要保留的依据。", CitationIDs: []string{"t1"}}}
				if chapter.ID == "s1" {
					blocks = append(blocks, workflowBlock{Type: "figure", Origin: "video", FrameIDs: []string{"f1"}, Caption: "实际画面显示一块黑色方形区域。"})
				} else {
					blocks[0].Text, blocks[0].CitationIDs = "最后的来源事实24说明结尾独立的内容，与前面的开头事实分别保留，不能省略这段后半部分。", []string{"t25"}
				}
				body.Sections = append(body.Sections, workflowSection{ID: chapter.ID, Blocks: blocks})
				if chapter.ID == "s1" {
					body.Limitations = append(body.Limitations, domain.Limitation{Code: "visual_unclear", Message: "首章单帧没有可辨认文字。", SectionIDs: []string{"s1"}, CitationIDs: []string{"f1"}})
				}
			}
			writeReviewWireArgs(w, name, body)
		case workflowReviewToolName:
			var id string
			var units []reviewUnit
			_ = json.Unmarshal(payload["request_id"], &id)
			_ = json.Unmarshal(payload["units"], &units)
			results := []workflowVerdict{}
			for _, unit := range units {
				if len(unit.FrameIDs) > 0 && !strings.Contains(string(domain.JSON(wire)), "data:image/jpeg;") {
					t.Error("visual review omitted pixels")
				}
				results = append(results, workflowVerdict{UnitID: unit.ID, Verdict: "supported", CitationIDs: []string{}})
			}
			writeReviewWireArgs(w, name, workflowReviewWire{RequestID: id, Results: results})
		case "answer_video_question":
			var frames []workflowFrame
			_ = json.Unmarshal(payload["delivered_pixels"], &frames)
			answer := workflowAnswerDraft{Kind: "grounded", Summary: "结尾保留了来源事实24。", Blocks: []workflowBlock{{Type: "paragraph", Origin: "video", Text: "视频结尾给出了来源事实24，这是后半部分独立的原文事实。", CitationIDs: []string{"t25"}}}, FrameNotes: []workflowFrameNote{}, Limitations: []domain.Limitation{}}
			for _, frame := range frames {
				answer.FrameNotes = append(answer.FrameNotes, workflowFrameNote{FrameAlias: frame.Alias, VisibleFacts: []string{"画面显示一块黑色方形区域。"}, Uncertain: []string{}})
				answer.Summary, answer.Blocks[0].Text, answer.Blocks[0].CitationIDs = "图片显示黑色区域。", "图片中可见黑色方形区域；单帧不能证明动态变化。", []string{frame.Alias}
			}
			writeReviewWireArgs(w, name, answer)
		default:
			t.Error("unexpected orchestration call", name)
		}
	})
	workflowFixtureState(s)
	ctx := context.Background()
	var err error
	s.engine.Media, err = media.New(s.engine.Store, s.engine.Resources, s.engine.Config)
	if err != nil {
		t.Fatal(err)
	}
	var pixels bytes.Buffer
	_ = jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	id := domain.ID()
	hash := sha256.Sum256(pixels.Bytes())
	if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, id+".jpg"), pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: id, SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "frame", StorageKey: id + ".jpg", SHA256: hex.EncodeToString(hash[:]), MIME: "image/jpeg", Bytes: int64(pixels.Len()), TimeMS: domain.Ptr(int64(500)), RequestedTimeMS: domain.Ptr(int64(500))}
	if err = s.engine.Store.AddAsset(ctx, s.claim, asset); err != nil {
		t.Fatal(err)
	}
	s.cp.OverviewIDs, s.cp.CitationFrames = []string{id}, []string{id}
	s.snapshot.Source.TextProvenance = &domain.Provenance{Origin: "platform", Coverage: "complete"}
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE source_snapshots SET source=$2 WHERE id=$1", s.snapshot.ID, domain.JSON(s.snapshot.Source)); err != nil {
		t.Fatal(err)
	}
	if err = s.persist(ctx, s.cp.Messages); err != nil {
		t.Fatal(err)
	}
	s.claim.Run.Checkpoint = domain.JSON(s.cp)
	if err = s.engine.Run(ctx, s.claim, s.profile); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&count); err != nil || count != 1 || sends.Load() > 20 {
		t.Fatal("complete workflow did not publish within budget", count, sends.Load(), err)
	}
	var publishedLimits []domain.Limitation
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT content->'limitations' FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&publishedLimits); err != nil || len(publishedLimits) != 2 {
		t.Fatal("planning/writing uncertainty did not survive publication", publishedLimits, err)
	}
	stats, err := s.engine.Store.ModelExecution(ctx, s.claim.Run.ID)
	if err != nil || stats.Submissions != int(sends.Load()) {
		t.Fatal("actual submissions were not accounted", stats, err)
	}
	t.Log(fmt.Sprintf("published with %d actual submissions", sends.Load()))
	var owner, request string
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT session_id,id FROM generation_requests WHERE article_id IN (SELECT id FROM articles WHERE generation_run_id=$1)", s.claim.Run.ID).Scan(&owner, &request); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := s.engine.Store.EnsureConversation(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, imageQuestion := range []bool{false, true} {
		before := sends.Load()
		input := domain.QuestionInput{Question: "解释结尾的来源事实", ExpectedLastTurnID: conversation.LastTurnID}
		if imageQuestion {
			input.Question, input.Context.FrameID = "解释这张真实图片", &id
		}
		accepted, err := s.engine.Store.CreateTurn(ctx, owner, conversation.ID, domain.ID(), input, "")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.engine.Store.Turn(ctx, owner, conversation.ID, accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := s.engine.Store.Claim(ctx, turn.RunID, 1, "workflow-question")
		if err != nil || claim == nil {
			t.Fatal(err)
		}
		if err = s.engine.Run(ctx, *claim, s.profile); err != nil {
			t.Fatal("question workflow", imageQuestion, err)
		}
		turn, err = s.engine.Store.Turn(ctx, owner, conversation.ID, turn.ID)
		if err != nil || turn.Status != "completed" || turn.Answer == nil || sends.Load()-before != 2 {
			t.Fatal("question did not complete in two physical sends", imageQuestion, turn.Status, sends.Load()-before, err)
		}
		conversation.LastTurnID = &turn.ID
	}
}

func TestWorkflowRevisionReusesOnlyUnaffectedChapterEvidence(t *testing.T) {
	var sends atomic.Int32
	var revised atomic.Bool
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var wire reviewWireRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		payload := workflowPayload(t, wire)
		var id string
		var units []reviewUnit
		_ = json.Unmarshal(payload["request_id"], &id)
		_ = json.Unmarshal(payload["units"], &units)
		results := []workflowVerdict{}
		for _, unit := range units {
			if revised.Load() && strings.HasPrefix(unit.ID, "s2") {
				t.Error("unchanged chapter was reviewed again")
			}
			v := workflowVerdict{UnitID: unit.ID, Verdict: "supported", CitationIDs: []string{}}
			if !revised.Load() && unit.ID == "s1_b1" {
				v.Verdict, v.Reason, v.CitationIDs = "contradicted", "该处应按来源事实0修订。", []string{"t1"}
			}
			results = append(results, v)
		}
		writeReviewWireArgs(w, workflowReviewToolName, workflowReviewWire{RequestID: id, Results: results})
	})
	workflowFixtureState(s)
	a := domain.Article{Title: "标题", Sections: []domain.Section{{ID: "s1", Title: "开头", Blocks: []domain.Block{{ID: "s1_b1", Type: "code", Text: "错误陈述", CitationIDs: []string{"t1"}}}}, {ID: "s2", Title: "结尾", Blocks: []domain.Block{{ID: "s2_b1", Type: "code", Text: "来源事实24", CitationIDs: []string{"t25"}}}}}}
	issues, err := s.reviewWorkflowArticle(context.Background(), a, nil, "review")
	if err != nil || len(issues) != 1 || s.requireWorkflowReview(context.Background(), workflowArticleCandidate(a), nil) == nil {
		t.Fatal("contradiction was lost or published", issues, err)
	}
	before := sends.Load()
	revised.Store(true)
	a.Sections[0].Blocks[0].Text = "来源事实0"
	issues, err = s.reviewWorkflowArticle(context.Background(), a, nil, "recheck")
	if err != nil || len(issues) != 0 || sends.Load()-before != 2 {
		t.Fatal("local revision and global check were not bounded", issues, sends.Load()-before, err)
	}
	if err = s.requireWorkflowReview(context.Background(), workflowArticleCandidate(a), nil); err != nil {
		t.Fatal("complete current coverage did not pass", err)
	}
	s.snapshot.TextHash = "changed source"
	if s.requireWorkflowReview(context.Background(), workflowArticleCandidate(a), nil) == nil {
		t.Fatal("changed original source retained approval")
	}
}

func TestWorkflowProviderFailureRequiresExplicitRetryWithoutResending(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	workflowFixtureState(s)
	_, err := workflowStage[struct {
		Value string `json:"value"`
	}](context.Background(), s, "one-provider-failure", "plan", "record_test_value", "Record one value.", map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
	var problem *domain.Problem
	if !errors.As(err, &problem) || problem.Code != "PROVIDER_UNAVAILABLE" || problem.Status != 502 || problem.Meta["can_retry"] != true || sends.Load() != 1 {
		t.Fatal("large logical input was automatically resent or failure identity was lost", sends.Load(), err)
	}
}

func TestWorkflowPlainJSONStagePersistsWithoutEnvelopeRepair(t *testing.T) {
	var sends atomic.Int32
	s := boundedReviewSession(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"{\"value\":\"complete result\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`+"\n\ndata: [DONE]\n\n")
	})
	workflowFixtureState(s)
	type output struct {
		Value string `json:"value"`
	}
	for i := 0; i < 2; i++ {
		value, err := workflowStage[output](context.Background(), s, "one-json-result", "plan", "record_value", "Return one value.", map[string]any{"snapshot_id": s.snapshot.ID}, nil, nil)
		if err != nil || value.Value != "complete result" || sends.Load() != 1 || s.cp.Calls != 1 || s.cp.Workflow.Calls["format"] != 0 {
			t.Fatal("transport envelope caused a paid retry or lost its durable result", value, err, sends.Load())
		}
	}
}
