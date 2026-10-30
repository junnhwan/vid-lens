package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

const deliveryV6Fixture = "qa-article-reading-2026-10-30-v6"

func deliveryFixture(t *testing.T, handler http.HandlerFunc) (*session, domain.Article) {
	s, a := publicationReviewFixture(t, handler)
	s.cp.ExecutionContract.Version = domain.ExecutionContractV9
	s.cp.ExecutionContract.InputRevision = deliveryV6Fixture
	var request string
	if err := s.engine.Store.Pool.QueryRow(context.Background(), "SELECT id FROM generation_requests WHERE active_run_id=$1", s.claim.Run.ID).Scan(&request); err != nil {
		t.Fatal(err)
	}
	s.turn = &domain.Turn{RequestID: request, Question: "请解释这张图"}
	s.article = &a
	s.claim.Run.Kind = "qa.answer"
	if _, err := s.engine.Store.Pool.Exec(context.Background(), "UPDATE job_runs SET kind='qa.answer' WHERE id=$1", s.claim.Run.ID); err != nil {
		t.Fatal(err)
	}
	return s, a
}

func TestQuestionDeliveryUnrelatedUnobservedFrameDoesNotBlock(t *testing.T) {
	s, a := deliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("structural validation must not call a model") })
	catalog, err := s.citationCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unused, err := s.engine.Store.Asset(context.Background(), catalog["f1"].FrameID)
	if err != nil {
		t.Fatal(err)
	}
	unused.ID = domain.ID()
	data, err := os.ReadFile(filepath.Join(s.engine.Config.DataDir, unused.StorageKey))
	if err != nil {
		t.Fatal(err)
	}
	unused.StorageKey = unused.ID + ".jpg"
	if err = os.WriteFile(filepath.Join(s.engine.Config.DataDir, unused.StorageKey), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Store.AddAsset(context.Background(), s.claim, unused); err != nil {
		t.Fatal(err)
	}
	s.cp.Inspections = map[string][]string{"optional": {unused.ID}}
	answer := domain.Answer{Kind: "grounded", Summary: "这是本轮已观察的画面。", Blocks: []domain.Block{a.Sections[0].Blocks[1]}, Citations: map[string]domain.Citation{"f1": catalog["f1"]}}
	answer.Evidence = &domain.AnswerEvidence{Review: "pending", ViewedFrameIDs: []string{catalog["f1"].FrameID, unused.ID}}
	if err = s.validateAnswer(context.Background(), &answer); err != nil {
		t.Fatal("unused image blocked a cited, observed image", err)
	}
	if len(answer.Frames) != 2 {
		t.Fatal("viewed-but-unused pixels disappeared from the evidence gallery", answer.Frames)
	}
	uncited := answer
	uncited.Blocks = []domain.Block{{ID: "not-observed", Type: "figure", Origin: "video", FrameIDs: []string{unused.ID}, Caption: "不能以取帧代替观察", CitationIDs: []string{"f2"}}}
	uncited.Citations = map[string]domain.Citation{"f2": {ID: "f2", Kind: "video_frame", SnapshotID: s.snapshot.ID, FrameID: unused.ID, TimeMS: domain.Ptr(int64(2000))}}
	if err = s.validateAnswer(context.Background(), &uncited); err == nil {
		t.Fatal("a cited, unobserved image escaped the gate")
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV5
	if err = s.validateAnswer(context.Background(), &answer); err == nil {
		t.Fatal("historic gate changed")
	}
}

func TestQuestionDeliveryCollectsSplitReportsWithoutLosingUnknownCoverage(t *testing.T) {
	s, _ := deliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("reading durable reports called a model") })
	ctx := context.Background()
	units := []reviewUnit{{ID: "u1", Path: "/blocks/0", Value: "alpha", HasTextEvidence: true}, {ID: "u2", Path: "/blocks/1", Value: "beta", HasTextEvidence: true}}
	write := func(task reviewTask, state string) {
		ids := []string{}
		for _, u := range task.Units {
			ids = append(ids, u.ID)
		}
		key := "review-units:" + store.Hash(map[string]any{"candidate_hash": "fixture-hash", "mode": task.Mode, "unit_ids": ids})
		id := "r_" + strings.TrimPrefix(key, "review-units:")[:24]
		args := map[string]any{"request_id": id, "unit_ids": ids, "mode": task.Mode, "protocol": s.boundedReviewProtocol()}
		if _, err := s.engine.Store.BeginOperation(ctx, s.claim, key, "review_batch", args, false); err != nil {
			t.Fatal(err)
		}
		out := reviewBatchOutcome{State: state, Mode: task.Mode, RequestID: id, UnitIDs: ids}
		if state == "complete" {
			out.Report = &reviewUnitReport{RequestID: id, Results: []reviewUnitResult{{UnitID: ids[0], Verdict: "supported", Issues: []reviewUnitIssue{}}}}
		}
		if err := s.engine.Store.FinishOperation(ctx, s.claim, key, out, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	task := reviewTask{Mode: "source_text", Units: units}
	write(task, "split")
	write(reviewTask{Mode: task.Mode, Units: units[:1]}, "complete")
	results, err := s.questionTaskReports(ctx, "fixture-hash", task, map[string]domain.Citation{})
	if err != nil || results["u1"].Verdict != "supported" || results["u2"].Verdict != "" {
		t.Fatal("split coverage was discarded or invented", results, err)
	}
}

func TestQuestionDeliveryEmptyReviewRetainsLimitedAnswer(t *testing.T) {
	s, _ := deliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private only\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	input := readingAnswerInput{Kind: "grounded", Summary: "这是已查看画面的参考解释。", Blocks: []domain.ArticleReadingBlock{{ID: "visible", Type: "figure", Origin: "video", FrameIDs: []string{"f1"}, Caption: "合成画面中的可见内容。", CitationIDs: []string{"f1"}}}}
	if _, err := s.makeTools(); err != nil {
		t.Fatal(err)
	}
	feedback, err := planCall(s, "delivery-empty-review", "submit_answer", input)
	if err != nil || feedback["accepted_for_publication"] != true {
		t.Fatal("empty review discarded a structurally grounded answer", feedback, err)
	}
	op, err := s.engine.Store.Operation(context.Background(), s.claim.Run.ID, "candidate")
	var answer domain.Answer
	if err != nil || json.Unmarshal(op.Result, &answer) != nil || answer.Evidence == nil || answer.Evidence.Review != "unavailable" || answer.Kind != "limited" || len(answer.Blocks) == 0 {
		t.Fatal("missing finite delivery", string(op.Result), err)
	}
	if err = s.requireAnswerPublicationReview(context.Background(), answer); err != nil {
		t.Fatal("server receipt did not authorize its exact result", err)
	}
	answer.Evidence.Review = "passed"
	if err = s.requireAnswerPublicationReview(context.Background(), answer); err == nil {
		t.Fatal("forged approval bypassed delivery receipt")
	}
	answer.Evidence.Review = "unavailable"
	frame := answer.Citations["f1"].FrameID
	if _, err = s.engine.Store.Pool.Exec(context.Background(), "UPDATE media_assets SET sha256=$2 WHERE id=$1", frame, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if err = s.requireAnswerPublicationReview(context.Background(), answer); err == nil {
		t.Fatal("changed source bytes kept their old delivery authorization")
	}
}

func TestQuestionDeliveryDoesNotLabelUnreviewedBodyAsVerified(t *testing.T) {
	a := domain.Answer{Summary: "这说明只有一个正确方案。", Blocks: []domain.Block{{ID: "example", Type: "paragraph", Origin: "video", Content: []domain.Inline{{Type: "text", Text: "原文给出了一条示例。"}, {Type: "citation", CitationID: "t1"}}}}, Evidence: &domain.AnswerEvidence{}}
	units := []reviewUnit{{ID: "summary", Path: "/summary"}, {ID: "body", Path: "/blocks/0"}}
	verdicts := map[string]questionUnitVerdict{"summary": {Rejected: true, Reasons: []string{"原文没有排除其他方案。"}}, "body": {}}
	out := questionLimitedAnswer(a, "unavailable", "核验未完成", units, verdicts)
	if strings.Contains(out.Summary, "已核验") || strings.Contains(out.Summary, "只有一个") || len(out.Blocks) != 1 || out.Evidence.Review != "unavailable" || len(out.Evidence.Uncertainties) != 1 {
		t.Fatal("rejected summary promoted an unreviewed body", out)
	}
	if !recoverableQuestionReview(questionCallTimeLimit()) || !recoverableQuestionReview(reviewCapacityExhausted()) || recoverableQuestionReview(context.Canceled) || recoverableQuestionReview(domain.ErrFenced) {
		t.Fatal("review failure was confused with cancellation or fencing")
	}
}

func TestQuestionDeliverySelectedFrameCanUseTracedNearbyEvidence(t *testing.T) {
	s := &session{turn: &domain.Turn{}, cp: checkpoint{ExecutionContract: &domain.ExecutionContract{Version: domain.ExecutionContractV9, InputRevision: domain.QuestionReadingV6}, HarnessRevision: 16, Inspections: map[string][]string{"question-context": {"transition"}, "nearby": {"clear"}}, FrameRefinements: map[string]frameRefinement{"nearby": {ParentIDs: []string{"transition"}}}}}
	a := domain.Answer{Kind: "grounded", Blocks: []domain.Block{{ID: "nearby", Type: "figure", CitationIDs: []string{"f2"}, FrameIDs: []string{"clear"}}}, Citations: map[string]domain.Citation{"f2": {Kind: "video_frame", FrameID: "clear"}}}
	if err := s.validateQuestionFrameReference(a); err != nil {
		t.Fatal("selected transition still blocked its traced replacement", err)
	}
	a.Citations["f2"] = domain.Citation{Kind: "video_frame", FrameID: "unrelated"}
	if err := s.validateQuestionFrameReference(a); err == nil {
		t.Fatal("unrelated pixels replaced the user's selected image")
	}
	s.cp.ExecutionContract.InputRevision = domain.QuestionReadingV5
	a.Citations["f2"] = domain.Citation{Kind: "video_frame", FrameID: "clear"}
	if err := s.validateQuestionFrameReference(a); err == nil {
		t.Fatal("historic selected-frame policy changed")
	}
}
