package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/testsupport"
)

func TestQuestionTimingLetsActiveReviewFinishBeyondOldRoundLimit(t *testing.T) {
	// Scale seconds to milliseconds, keeping the real response reader and model
	// adapter. No provider, real video, or paid request is involved.
	var cp checkpoint
	raw := []byte(`{"question_timing":{"revision":"qa-timing-2026-10-30-v1","active_seconds":1200,"wall_seconds":1500,"author_call_seconds":600,"review_call_seconds":600,"first_activity_seconds":120,"idle_seconds":120}}`)
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	s := &session{profile: config.DefaultProfile(), cp: cp, turn: &domain.Turn{}, reviewing: true, reviewStep: &reviewStep{}}
	limits := s.responseLimits()
	if limits.Total != 600*time.Second {
		t.Errorf("review still has the legacy cutoff: %v", limits.Total)
	}
	limits.Total /= 1000
	limits.First /= 1000
	limits.Idle /= 1000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 24; i++ {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"private-review-progress\"}}]}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"review complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := config.DefaultProfile()
	p.ModelBaseURL = server.URL
	p, err := p.ForRun("qa.answer", raw)
	if err != nil {
		t.Fatal(err)
	}
	// Replay the observed 71 seconds spent before review. Both the worker's
	// remaining round and HTTP client must outlive the streaming review.
	ctx, cancel := context.WithTimeout(context.Background(), (p.ActiveBudget("qa.answer")-71*time.Second)/1000)
	defer cancel()
	ctx = context.WithValue(ctx, responsePolicyKey{}, s.responsePolicy())
	var progress []*domain.ModelProgress
	ctx = context.WithValue(ctx, modelProgressKey{}, &modelProgressEmitter{startedAt: time.Now(), limit: limits.Total, emit: func(receipt *domain.ModelProgress) { progress = append(progress, receipt) }})
	client := server.Client()
	client.Timeout = modelHTTPTimeout(ctx) / 1000
	base, err := newModelWithClient(ctx, p, "fixture", client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiveModelResponse(ctx, base, []*schema.Message{schema.UserMessage("test")}, limits)
	if err != nil || result == nil || result.Content != "review complete" {
		t.Fatalf("continuous review thinking was cut off: result=%v err=%v", result, err)
	}
	if strings.Contains(string(domain.JSON(result)), "private-review-progress") {
		t.Fatal("private reasoning leaked into the answer")
	}
	if len(progress) == 0 || progress[0].State != "thinking" || strings.Contains(string(domain.JSON(progress)), "private-review-progress") {
		t.Fatal("real stream activity was missing or private thinking leaked", progress)
	}
}

func TestQuestionTimingKeepsLegacyCallsAndHTTPFrozen(t *testing.T) {
	s := &session{turn: &domain.Turn{}, reviewing: true, reviewStep: &reviewStep{}}
	if s.responsePolicy() != nil || s.responseLimits().Total != 300*time.Second || modelHTTPTimeout(context.Background()) != 300*time.Second {
		t.Fatal("historic question received the new policy")
	}
	s.cp.QuestionTiming = config.NewQuestionTiming()
	for _, review := range []bool{false, true} {
		s.reviewing = review
		ctx := context.WithValue(context.Background(), responsePolicyKey{}, s.responsePolicy())
		if !s.responsePolicy().valid() || modelHTTPTimeout(ctx) != s.responseLimits().Total || !s.responseLimits().BudgetDeadline {
			t.Fatal("question/transport policies disagree")
		}
	}
}

func TestModelProgressReceiptsStayBoundedAcrossStateChanges(t *testing.T) {
	start := time.Now()
	var receipts []*domain.ModelProgress
	p := modelProgressEmitter{startedAt: start, limit: 10 * time.Minute, emit: func(receipt *domain.ModelProgress) { receipts = append(receipts, receipt) }}
	p.observe("waiting", start)
	for i := 0; i < 5000; i++ {
		state := "thinking"
		if i%2 == 1 {
			state = "responding"
		}
		p.observe(state, start.Add(time.Duration(i)*time.Millisecond))
	}
	p.observe("thinking", start.Add(6*time.Second))
	if len(receipts) != 3 || receipts[0].State != "waiting" || receipts[1].State != "thinking" || receipts[2].ElapsedMS != 6000 {
		t.Fatal("chunk stream flooded progress events", len(receipts))
	}
}

func TestQuestionKnownTimeLimitCannotResendOnTakeover(t *testing.T) {
	db, resources, cfg := testsupport.Services(t)
	ctx := context.Background()
	turn, claim := testsupport.Question(t, db)
	p, err := db.RunProfile(ctx, claim.Run)
	if err != nil {
		t.Fatal(err)
	}
	var cp checkpoint
	if err = json.Unmarshal(claim.Run.Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	s := &session{engine: &Engine{Store: db, Resources: resources, Config: cfg}, claim: claim, profile: p, cp: cp, turn: &turn, reviewing: true, reviewStep: &reviewStep{}}
	input := []*schema.Message{schema.UserMessage("synthetic review")}
	args := stripPixels(input)
	if _, err = db.BeginOperation(ctx, claim, "model:0", "model", args, false); err != nil {
		t.Fatal(err)
	}
	op, err := db.BeginModelSubmission(ctx, claim, "model:0", args, claim.Run.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RecordSubmission(ctx, claim, "model:0", op.Submissions, map[string]any{"failure_code": "MODEL_CALL_BUDGET_EXHAUSTED"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = (&meteredModel{session: s}).Generate(ctx, input)
		problem, ok := err.(*domain.Problem)
		if !ok || problem.Code != "MODEL_CALL_BUDGET_EXHAUSTED" || problem.Meta["can_retry"] != true {
			t.Fatal("known timeout did not stop with manual retry", err)
		}
	}
	stats, err := db.ModelExecution(ctx, claim.Run.ID)
	if err != nil || stats.Submissions != 1 {
		t.Fatal("timed-out input was resent", stats, err)
	}
}
